package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (db *DB) create(
	ctx context.Context,
	title string,
	kind domain.Kind,
	insert func(pgx.Tx, *domain.Main) error,
) (*domain.Main, error) {
	transaction, err := db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin create: %w", err)
	}
	defer db.rollback(ctx, transaction)

	main := &domain.Main{Title: title, SubObj: kind, CreatedAt: time.Now().UTC()}
	main.UpdatedAt = main.CreatedAt

	if err = transaction.QueryRow(ctx, `INSERT INTO main (title, sub_id, sub_obj, created_at, update_at)
VALUES ($1, nextval(pg_get_serial_sequence($2, 'id')), $2, $3, $3) RETURNING id, sub_id`,
		main.Title, string(kind), main.CreatedAt).Scan(&main.ID, &main.SubID); err != nil {
		return nil, fmt.Errorf("insert Main: %w", err)
	}

	if err = insert(transaction, main); err != nil {
		return nil, fmt.Errorf("create satellite: %w", err)
	}

	result, err := db.readTx(ctx, transaction, main.ID)
	if err != nil {
		return nil, err
	}

	if err = transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit create: %w", err)
	}

	return result, nil
}

func (db *DB) UpdateMain(
	ctx context.Context,
	mainID int64,
	title graphql.Omittable[*string],
) (*domain.Main, error) {
	if !title.IsSet() {
		return nil, fmt.Errorf("%w: update must change at least one field", ErrInvalidInput)
	}

	return db.update(ctx, mainID, title, "", nil)
}

func (db *DB) update(
	ctx context.Context,
	mainID int64,
	title graphql.Omittable[*string],
	kind domain.Kind,
	apply func(pgx.Tx, *domain.Main, time.Time) error,
) (*domain.Main, error) {
	if title.IsSet() && title.Value() == nil {
		return nil, fmt.Errorf("%w: title cannot be null", ErrInvalidInput)
	}

	transaction, err := db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin update: %w", err)
	}
	defer db.rollback(ctx, transaction)

	result, err := db.applyUpdate(ctx, transaction, mainID, title, kind, apply)
	if err != nil {
		return nil, err
	}

	if err = transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit update: %w", err)
	}

	return result, nil
}

func (db *DB) applyUpdate(
	ctx context.Context,
	transaction pgx.Tx,
	mainID int64,
	title graphql.Omittable[*string],
	kind domain.Kind,
	apply func(pgx.Tx, *domain.Main, time.Time) error,
) (*domain.Main, error) {
	main, err := db.lockMain(ctx, transaction, mainID)
	if err != nil {
		return nil, err
	}

	if main.DeletedAt != nil {
		return nil, ErrNotFound
	}

	if apply != nil && main.SubObj != kind {
		return nil, ErrSatelliteTypeMismatch
	}

	now := time.Now().UTC()

	tag, err := transaction.Exec(ctx, `UPDATE main
SET title = CASE WHEN $2::boolean THEN $3::text ELSE title END, update_at = $4
WHERE id = $1 AND deleted_at IS NULL`, main.ID, title.IsSet(), title.Value(), now)
	if err = exactlyOne(tag, err); err != nil {
		return nil, fmt.Errorf("update Main: %w", err)
	}

	if apply != nil {
		if err = apply(transaction, main, now); err != nil {
			return nil, fmt.Errorf("update satellite: %w", err)
		}
	}

	return db.readTx(ctx, transaction, main.ID)
}

func (db *DB) Delete(ctx context.Context, mainID int64) error {
	transaction, err := db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin delete: %w", err)
	}
	defer db.rollback(ctx, transaction)

	main, err := db.lockMain(ctx, transaction, mainID)
	if err != nil {
		return err
	}

	if main.DeletedAt != nil {
		return ErrAlreadyDeleted
	}

	softDelete, ok := db.deleters[main.SubObj]
	if !ok {
		return fmt.Errorf("delete satellite: unknown satellite kind %q", main.SubObj)
	}

	now := time.Now().UTC()

	tag, err := transaction.Exec(ctx, `UPDATE main SET deleted_at = $2, update_at = $2
WHERE id = $1 AND deleted_at IS NULL`, main.ID, now)
	if err = exactlyOne(tag, err); err != nil {
		return fmt.Errorf("delete Main: %w", err)
	}

	if err = softDelete(ctx, transaction, main, now); err != nil {
		return fmt.Errorf("delete satellite: %w", err)
	}

	if _, err = db.readTx(ctx, transaction, main.ID); err != nil {
		return err
	}

	if err = transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete: %w", err)
	}

	return nil
}

func (db *DB) rollback(ctx context.Context, transaction pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := transaction.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		slog.ErrorContext(ctx, "rollback failed", "error", err)
	}
}
