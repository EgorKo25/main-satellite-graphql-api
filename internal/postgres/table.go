package postgres

import (
	"context"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (db *DB) CreateTable(ctx context.Context, title string, input TableCreate) (*domain.Main, error) {
	return db.create(ctx, title, domain.Tables, func(tx pgx.Tx, main *domain.Main) error {
		tag, err := tx.Exec(ctx, `INSERT INTO tables (id, main_id, description2, created_at, update_at)
VALUES ($1, $2, $3, $4, $4)`, main.SubID, main.ID, input.Description2, main.CreatedAt)

		return exactlyOne(tag, err)
	})
}

func (db *DB) UpdateTable(
	ctx context.Context,
	mainID int64,
	title graphql.Omittable[*string],
	input TableUpdate,
) (*domain.Main, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	return db.update(ctx, mainID, title, domain.Tables, func(tx pgx.Tx, main *domain.Main, now time.Time) error {
		tag, err := tx.Exec(ctx, `UPDATE tables SET description2 = $3, update_at = $4
WHERE id = $1 AND main_id = $2 AND deleted_at IS NULL`,
			main.SubID, main.ID, input.Description2.Value(), now)

		return exactlyOne(tag, err)
	})
}

func (db *DB) deleteTable(ctx context.Context, tx pgx.Tx, main *domain.Main, now time.Time) error {
	tag, err := tx.Exec(ctx, `UPDATE tables SET deleted_at = $3, update_at = $3
WHERE id = $1 AND main_id = $2 AND deleted_at IS NULL`,
		main.SubID, main.ID, now)

	return exactlyOne(tag, err)
}
