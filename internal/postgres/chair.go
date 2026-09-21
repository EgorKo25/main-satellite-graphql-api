package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/jackc/pgx/v5"
)

func nextChairID(ctx context.Context, tx pgx.Tx) (int64, error) {
	var subID int64

	if err := tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('chairs', 'id'))`).Scan(&subID); err != nil {
		return 0, fmt.Errorf("allocate chair ID: %w", err)
	}

	return subID, nil
}

func lockChair(ctx context.Context, tx pgx.Tx, main *domain.Main) error {
	var subID int64

	if err := tx.QueryRow(ctx, `SELECT id FROM chairs
WHERE id = $1 AND main_id = $2
FOR UPDATE`, main.SubID, main.ID).Scan(&subID); err != nil {
		return fmt.Errorf("lock chair for Main %d: %w", main.ID, err)
	}

	return nil
}

func createChair(ctx context.Context, tx pgx.Tx, main *domain.Main) error {
	chair, ok := main.Satellite.(*domain.Chair)
	if !ok || chair == nil {
		return fmt.Errorf("create chair: invalid satellite %T", main.Satellite)
	}

	tag, err := tx.Exec(ctx, `INSERT INTO chairs (id, main_id, description3, created_at, update_at, type)
VALUES ($1, $2, $3, $4, $5, $6)`,
		main.SubID, main.ID, chair.Description3, chair.CreatedAt, chair.UpdatedAt, chair.Type)

	return exactlyOne(tag, err)
}

func updateChair(ctx context.Context, tx pgx.Tx, main *domain.Main, now time.Time) error {
	chair, ok := main.Satellite.(*domain.Chair)
	if !ok || chair == nil {
		return fmt.Errorf("update chair: invalid satellite %T", main.Satellite)
	}

	tag, err := tx.Exec(ctx, `UPDATE chairs SET description3 = $3, update_at = $4, type = $5
WHERE id = $1 AND main_id = $2 AND deleted_at IS NULL`,
		main.SubID, main.ID, chair.Description3, now, chair.Type)

	return exactlyOne(tag, err)
}

func softDeleteChair(ctx context.Context, tx pgx.Tx, main *domain.Main, now time.Time) error {
	tag, err := tx.Exec(ctx, `UPDATE chairs SET deleted_at = $3, update_at = $3
WHERE id = $1 AND main_id = $2 AND deleted_at IS NULL`,
		main.SubID, main.ID, now)

	return exactlyOne(tag, err)
}
