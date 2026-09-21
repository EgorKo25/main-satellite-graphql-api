package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/jackc/pgx/v5"
)

func nextToolID(ctx context.Context, tx pgx.Tx) (int64, error) {
	var subID int64

	if err := tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('tools', 'id'))`).Scan(&subID); err != nil {
		return 0, fmt.Errorf("allocate tool ID: %w", err)
	}

	return subID, nil
}

func lockTool(ctx context.Context, tx pgx.Tx, main *domain.Main) error {
	var subID int64

	if err := tx.QueryRow(ctx, `SELECT id FROM tools
WHERE id = $1 AND main_id = $2
FOR UPDATE`, main.SubID, main.ID).Scan(&subID); err != nil {
		return fmt.Errorf("lock tool for Main %d: %w", main.ID, err)
	}

	return nil
}

func createTool(ctx context.Context, tx pgx.Tx, main *domain.Main) error {
	tool, ok := main.Satellite.(*domain.Tool)
	if !ok || tool == nil {
		return fmt.Errorf("create tool: invalid satellite %T", main.Satellite)
	}

	tag, err := tx.Exec(ctx, `INSERT INTO tools (id, main_id, description1, created_at, update_at)
VALUES ($1, $2, $3, $4, $5)`,
		main.SubID, main.ID, tool.Description1, tool.CreatedAt, tool.UpdatedAt)

	return exactlyOne(tag, err)
}

func updateTool(ctx context.Context, tx pgx.Tx, main *domain.Main, now time.Time) error {
	tool, ok := main.Satellite.(*domain.Tool)
	if !ok || tool == nil {
		return fmt.Errorf("update tool: invalid satellite %T", main.Satellite)
	}

	tag, err := tx.Exec(ctx, `UPDATE tools SET description1 = $3, update_at = $4
WHERE id = $1 AND main_id = $2 AND deleted_at IS NULL`,
		main.SubID, main.ID, tool.Description1, now)

	return exactlyOne(tag, err)
}

func softDeleteTool(ctx context.Context, tx pgx.Tx, main *domain.Main, now time.Time) error {
	tag, err := tx.Exec(ctx, `UPDATE tools SET deleted_at = $3, update_at = $3
WHERE id = $1 AND main_id = $2 AND deleted_at IS NULL`,
		main.SubID, main.ID, now)

	return exactlyOne(tag, err)
}
