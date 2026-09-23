package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (db *DB) CreateTool(ctx context.Context, title string, input ToolCreate) (*domain.Main, error) {
	return db.create(ctx, title, domain.Tools, db.nextToolID, func(tx pgx.Tx, main *domain.Main) error {
		tag, err := tx.Exec(ctx, `INSERT INTO tools (id, main_id, description1, created_at, update_at)
VALUES ($1, $2, $3, $4, $4)`, main.SubID, main.ID, input.Description1, main.CreatedAt)

		return exactlyOne(tag, err)
	})
}

func (db *DB) UpdateTool(
	ctx context.Context,
	mainID int64,
	title graphql.Omittable[*string],
	input ToolUpdate,
) (*domain.Main, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	return db.update(ctx, mainID, title, domain.Tools, func(tx pgx.Tx, main *domain.Main, now time.Time) error {
		tag, err := tx.Exec(ctx, `UPDATE tools SET description1 = $3, update_at = $4
WHERE id = $1 AND main_id = $2 AND deleted_at IS NULL`,
			main.SubID, main.ID, input.Description1.Value(), now)

		return exactlyOne(tag, err)
	})
}

func (db *DB) nextToolID(ctx context.Context, tx pgx.Tx) (int64, error) {
	var subID int64

	if err := tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('tools', 'id'))`).Scan(&subID); err != nil {
		return 0, fmt.Errorf("allocate tool ID: %w", err)
	}

	return subID, nil
}

func (db *DB) deleteTool(ctx context.Context, tx pgx.Tx, main *domain.Main, now time.Time) error {
	tag, err := tx.Exec(ctx, `UPDATE tools SET deleted_at = $3, update_at = $3
WHERE id = $1 AND main_id = $2 AND deleted_at IS NULL`,
		main.SubID, main.ID, now)

	return exactlyOne(tag, err)
}
