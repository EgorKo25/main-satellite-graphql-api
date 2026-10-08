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
	"github.com/samber/lo"
)

const deletedAtColumn = "deleted_at"

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
	defer func() {
		if err := transaction.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			logger.Get("postgres").Error("rollback failed", err)
		}
	}()

	now := time.Now().UTC()

	rows, err := transaction.Query(ctx, `
	    INSERT INTO main (title, sub_id, sub_obj, created_at, update_at)
	    VALUES ($1, nextval(pg_get_serial_sequence($2, 'id')), $2, $3, $3)
	    RETURNING id, title, sub_id, sub_obj, created_at, update_at, deleted_at;
	`, title, string(kind), now)
	if err != nil {
		return nil, fmt.Errorf("insert Main: %w", err)
	}

	main, err := pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[domain.Main])
	if err != nil {
		return nil, fmt.Errorf("read inserted Main: %w", err)
	}

	columns := maps.Clone(fields)
	columns["id"] = main.SubID
	columns["main_id"] = main.ID
	columns["created_at"] = main.CreatedAt
	columns["update_at"] = main.CreatedAt

	query, args, err := squirrel.Insert(string(kind)).SetMap(columns).
		PlaceholderFormat(squirrel.Dollar).Suffix("RETURNING " + satelliteColumns(kind) + ";").ToSql()
	if err != nil {
		return nil, fmt.Errorf("build satellite insert: %w", err)
	}

	var inserted satelliteRow

	if err = transaction.QueryRow(ctx, query, args...).Scan(&inserted); err != nil {
		return nil, fmt.Errorf("read inserted satellite: %w", err)
	}

	main.Satellite = inserted.object(main.SubObj)

	if err = transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit create: %w", err)
	}

	return main, nil
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
	defer func() {
		if err := transaction.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			logger.Get("postgres").Error("rollback failed", err)
		}
	}()

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

	rows, err := transaction.Query(ctx, `
	    UPDATE main
	    SET title = CASE WHEN $2::boolean THEN $3::text ELSE title END, update_at = $4
	    WHERE id = $1 AND deleted_at IS NULL
	    RETURNING id, title, sub_id, sub_obj, created_at, update_at, deleted_at;
	`, main.ID, title.IsSet(), title.Value(), now)
	if err != nil {
		return nil, fmt.Errorf("update Main: %w", err)
	}

	main, err = pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[domain.Main])
	if err != nil {
		return nil, fmt.Errorf("read updated Main: %w", err)
	}

	var (
		query string
		args  []any
	)

	condition := squirrel.Eq{"id": main.SubID, "main_id": main.ID, deletedAtColumn: nil}
	projection := satelliteColumns(main.SubObj)

	if fields == nil {
		query, args, err = squirrel.Select(projection).From(string(main.SubObj)).Where(condition).
			PlaceholderFormat(squirrel.Dollar).Suffix(";").ToSql()
	} else {
		columns := maps.Clone(fields)
		columns["update_at"] = now

		query, args, err = squirrel.Update(string(main.SubObj)).SetMap(columns).Where(condition).
			PlaceholderFormat(squirrel.Dollar).Suffix("RETURNING " + projection + ";").ToSql()
	}

	if err != nil {
		return nil, fmt.Errorf("build satellite update: %w", err)
	}

	var updated satelliteRow

	if err = transaction.QueryRow(ctx, query, args...).Scan(&updated); err != nil {
		return nil, fmt.Errorf("read updated satellite: %w", err)
	}

	main.Satellite = updated.object(main.SubObj)

	if err = transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit update: %w", err)
	}

	return main, nil
}

func (db *DB) Delete(ctx context.Context, mainID int64) error {
	transaction, err := db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin delete: %w", err)
	}
	defer func() {
		if err := transaction.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			logger.Get("postgres").Error("rollback failed", err)
		}
	}()

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

	query, args, err := squirrel.Update(string(main.SubObj)).
		SetMap(map[string]any{deletedAtColumn: now, "update_at": now}).
		Where(squirrel.Eq{"id": main.SubID, "main_id": main.ID, deletedAtColumn: nil}).
		PlaceholderFormat(squirrel.Dollar).Suffix(";").ToSql()
	if err != nil {
		return fmt.Errorf("build satellite delete: %w", err)
	}

	tag, err = transaction.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("delete satellite: %w", err)
	}

	if tag.RowsAffected() != 1 {
		return fmt.Errorf("delete satellite: expected one changed row, got %d", tag.RowsAffected())
	}

	if err = transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete: %w", err)
	}

	return nil
}

func (db *DB) lockMain(ctx context.Context, transaction pgx.Tx, mainID int64) (*domain.Main, error) {
	var (
		main           domain.Main
		satelliteCount int
	)

	err := transaction.QueryRow(ctx, `
	    WITH locked AS (
	        SELECT id, title, sub_id, sub_obj, created_at, update_at, deleted_at
	        FROM main WHERE id = $1 FOR UPDATE
	    )
	    SELECT locked.*,
	        (EXISTS (SELECT 1 FROM tools WHERE main_id = locked.id))::integer +
	        (EXISTS (SELECT 1 FROM tables WHERE main_id = locked.id))::integer +
	        (EXISTS (SELECT 1 FROM chairs WHERE main_id = locked.id))::integer
	    FROM locked;
	`, mainID).Scan(
		&main.ID, &main.Title, &main.SubID, &main.SubObj, &main.CreatedAt, &main.UpdatedAt, &main.DeletedAt,
		&satelliteCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("lock main %d: %w", mainID, err)
	}

	if satelliteCount != 1 {
		return nil, fmt.Errorf("main %d must have exactly one satellite, got %d", mainID, satelliteCount)
	}

	if _, known := lo.FindKey(satelliteKinds, main.SubObj); !known {
		return nil, fmt.Errorf("main %d has invalid sub_obj %q", mainID, main.SubObj)
	}

	return &main, nil
}
