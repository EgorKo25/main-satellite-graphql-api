package postgres

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/logger"
	"github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
)

var satelliteKinds = map[string]domain.Kind{
	"tool":  domain.Tools,
	"table": domain.Tables,
	"chair": domain.Chairs,
}

func (db *DB) Create(ctx context.Context, title string, satellite map[string]any) (*domain.Main, error) {
	kind, fields, err := satelliteInput(satellite)
	if err != nil {
		return nil, err
	}

	transaction, err := db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin create: %w", err)
	}
	defer db.rollback(ctx, transaction)

	main := &domain.Main{Title: title, SubObj: kind, CreatedAt: time.Now().UTC()}
	main.UpdatedAt = main.CreatedAt

	if err = transaction.QueryRow(ctx, `
	    INSERT INTO main (title, sub_id, sub_obj, created_at, update_at)
	    VALUES ($1, nextval(pg_get_serial_sequence($2, 'id')), $2, $3, $3) RETURNING id, sub_id;
	`, main.Title, string(kind), main.CreatedAt).Scan(&main.ID, &main.SubID); err != nil {
		return nil, fmt.Errorf("insert Main: %w", err)
	}

	columns := maps.Clone(fields)
	columns["id"] = main.SubID
	columns["main_id"] = main.ID
	columns["created_at"] = main.CreatedAt
	columns["update_at"] = main.CreatedAt

	query, args, err := squirrel.Insert(string(kind)).SetMap(columns).
		PlaceholderFormat(squirrel.Dollar).Suffix(";").ToSql()
	if err != nil {
		return nil, fmt.Errorf("build satellite insert: %w", err)
	}

	tag, err := transaction.Exec(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("insert satellite: %w", err)
	}

	if tag.RowsAffected() != 1 {
		return nil, fmt.Errorf("insert satellite: expected one changed row, got %d", tag.RowsAffected())
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

func (db *DB) Update(
	ctx context.Context,
	mainID int64,
	title graphql.Omittable[*string],
	satellite graphql.Omittable[map[string]any],
) (*domain.Main, error) {
	kind, fields, err := updateInput(title, satellite)
	if err != nil {
		return nil, err
	}

	transaction, err := db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin update: %w", err)
	}
	defer db.rollback(ctx, transaction)

	result, err := db.applyUpdate(ctx, transaction, mainID, title, kind, fields)
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
	fields map[string]any,
) (*domain.Main, error) {
	main, err := db.lockMain(ctx, transaction, mainID)
	if err != nil {
		return nil, err
	}

	if main.DeletedAt != nil {
		return nil, ErrNotFound
	}

	if fields != nil && main.SubObj != kind {
		return nil, ErrSatelliteTypeMismatch
	}

	now := time.Now().UTC()

	tag, err := transaction.Exec(ctx, `
	    UPDATE main
	    SET title = CASE WHEN $2::boolean THEN $3::text ELSE title END, update_at = $4
	    WHERE id = $1 AND deleted_at IS NULL;
	`, main.ID, title.IsSet(), title.Value(), now)
	if err != nil {
		return nil, fmt.Errorf("update Main: %w", err)
	}

	if tag.RowsAffected() != 1 {
		return nil, fmt.Errorf("update Main: expected one changed row, got %d", tag.RowsAffected())
	}

	if fields != nil {
		if err = db.updateSatellite(ctx, transaction, main, fields, now); err != nil {
			return nil, err
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

	now := time.Now().UTC()

	tag, err := transaction.Exec(ctx, `
	    UPDATE main SET deleted_at = $2, update_at = $2
	    WHERE id = $1 AND deleted_at IS NULL;
	`, main.ID, now)
	if err != nil {
		return fmt.Errorf("delete Main: %w", err)
	}

	if tag.RowsAffected() != 1 {
		return fmt.Errorf("delete Main: expected one changed row, got %d", tag.RowsAffected())
	}

	if err = db.updateSatellite(ctx, transaction, main, map[string]any{"deleted_at": now}, now); err != nil {
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

func (db *DB) updateSatellite(
	ctx context.Context,
	transaction pgx.Tx,
	main *domain.Main,
	fields map[string]any,
	now time.Time,
) error {
	var table string

	for _, kind := range satelliteKinds {
		if main.SubObj == kind {
			table = string(kind)

			break
		}
	}

	if table == "" {
		return fmt.Errorf("update satellite: unknown satellite kind %q", main.SubObj)
	}

	columns := maps.Clone(fields)
	columns["update_at"] = now

	query, args, err := squirrel.Update(table).SetMap(columns).
		Where(squirrel.Eq{"id": main.SubID, "main_id": main.ID, "deleted_at": nil}).
		PlaceholderFormat(squirrel.Dollar).Suffix(";").ToSql()
	if err != nil {
		return fmt.Errorf("build satellite update: %w", err)
	}

	tag, err := transaction.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update satellite: %w", err)
	}

	if tag.RowsAffected() != 1 {
		return fmt.Errorf("update satellite: expected one changed row, got %d", tag.RowsAffected())
	}

	return nil
}

func (db *DB) rollback(ctx context.Context, transaction pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := transaction.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		logger.Get("postgres").Error("rollback failed", err)
	}
}
