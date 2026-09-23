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
	nextID func(context.Context, pgx.Tx) (int64, error),
	insert func(pgx.Tx, *domain.Main) error,
) (*domain.Main, error) {
	transaction, err := db.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer db.rollback(ctx, transaction)

	main := &domain.Main{Title: title, SubObj: kind}

	main.SubID, err = nextID(ctx, transaction)
	if err != nil {
		return nil, err
	}

	main.CreatedAt, err = db.now(ctx, transaction)
	if err != nil {
		return nil, err
	}

	main.UpdatedAt = main.CreatedAt

	main.ID, err = db.insertMain(ctx, transaction, main)
	if err != nil {
		return nil, err
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
	if err := inputValidator.Var(mainID, "gt=0"); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	if title.IsSet() && title.Value() == nil {
		return nil, fmt.Errorf("%w: title cannot be null", ErrInvalidInput)
	}

	transaction, err := db.begin(ctx)
	if err != nil {
		return nil, err
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

	now, err := db.now(ctx, transaction)
	if err != nil {
		return nil, err
	}

	if err = db.updateMain(ctx, transaction, main.ID, title, now); err != nil {
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
	if err := inputValidator.Var(mainID, "gt=0"); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	transaction, err := db.begin(ctx)
	if err != nil {
		return err
	}
	defer db.rollback(ctx, transaction)

	if err = db.delete(ctx, transaction, mainID); err != nil {
		return err
	}

	if err = transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete: %w", err)
	}

	return nil
}

func (db *DB) delete(ctx context.Context, transaction pgx.Tx, mainID int64) error {
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

	now, err := db.now(ctx, transaction)
	if err != nil {
		return err
	}

	if err = db.deleteMain(ctx, transaction, main.ID, now); err != nil {
		return fmt.Errorf("delete Main: %w", err)
	}

	if err = softDelete(ctx, transaction, main, now); err != nil {
		return fmt.Errorf("delete satellite: %w", err)
	}

	if _, err = db.readTx(ctx, transaction, main.ID); err != nil {
		return err
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
