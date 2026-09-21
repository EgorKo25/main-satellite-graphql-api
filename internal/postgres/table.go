package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/jackc/pgx/v5"
)

func nextTableID(ctx context.Context, tx pgx.Tx) (int64, error) {
	var subID int64

	if err := tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('tables', 'id'))`).Scan(&subID); err != nil {
		return 0, fmt.Errorf("allocate table ID: %w", err)
	}

	return subID, nil
}

func lockTable(ctx context.Context, tx pgx.Tx, main *domain.Main) error {
	var subID int64

	if err := tx.QueryRow(ctx, `SELECT id FROM tables
WHERE id = $1 AND main_id = $2
FOR UPDATE`, main.SubID, main.ID).Scan(&subID); err != nil {
		return fmt.Errorf("lock table for Main %d: %w", main.ID, err)
	}

	return nil
}

func createTable(ctx context.Context, tx pgx.Tx, main *domain.Main) error {
	table, ok := main.Satellite.(*domain.Table)
	if !ok || table == nil {
		return fmt.Errorf("create table: invalid satellite %T", main.Satellite)
	}

	tag, err := tx.Exec(ctx, `INSERT INTO tables (id, main_id, description2, created_at, update_at)
VALUES ($1, $2, $3, $4, $5)`,
		main.SubID, main.ID, table.Description2, table.CreatedAt, table.UpdatedAt)

	return exactlyOne(tag, err)
}

func updateTable(ctx context.Context, tx pgx.Tx, main *domain.Main, now time.Time) error {
	table, ok := main.Satellite.(*domain.Table)
	if !ok || table == nil {
		return fmt.Errorf("update table: invalid satellite %T", main.Satellite)
	}

	tag, err := tx.Exec(ctx, `UPDATE tables SET description2 = $3, update_at = $4
WHERE id = $1 AND main_id = $2 AND deleted_at IS NULL`,
		main.SubID, main.ID, table.Description2, now)

	return exactlyOne(tag, err)
}

func softDeleteTable(ctx context.Context, tx pgx.Tx, main *domain.Main, now time.Time) error {
	tag, err := tx.Exec(ctx, `UPDATE tables SET deleted_at = $3, update_at = $3
WHERE id = $1 AND main_id = $2 AND deleted_at IS NULL`,
		main.SubID, main.ID, now)

	return exactlyOne(tag, err)
}
