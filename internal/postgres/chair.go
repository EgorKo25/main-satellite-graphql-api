package postgres

import (
	"context"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (db *DB) CreateChair(ctx context.Context, title string, input ChairCreate) (*domain.Main, error) {
	return db.create(ctx, title, domain.Chairs, func(tx pgx.Tx, main *domain.Main) error {
		tag, err := tx.Exec(ctx, `INSERT INTO chairs (id, main_id, description3, type, created_at, update_at)
VALUES ($1, $2, $3, $4, $5, $5)`, main.SubID, main.ID, input.Description3, input.Type, main.CreatedAt)

		return exactlyOne(tag, err)
	})
}

func (db *DB) UpdateChair(
	ctx context.Context,
	mainID int64,
	title graphql.Omittable[*string],
	input ChairUpdate,
) (*domain.Main, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	return db.update(ctx, mainID, title, domain.Chairs, func(tx pgx.Tx, main *domain.Main, now time.Time) error {
		tag, err := tx.Exec(ctx, `UPDATE chairs
SET description3 = CASE WHEN $3::boolean THEN $4::text ELSE description3 END,
    type = CASE WHEN $5::boolean THEN $6::chair_type ELSE type END,
    update_at = $7
WHERE id = $1 AND main_id = $2 AND deleted_at IS NULL`,
			main.SubID, main.ID, input.Description3.IsSet(), input.Description3.Value(),
			input.Type.IsSet(), input.Type.Value(), now)

		return exactlyOne(tag, err)
	})
}

func (db *DB) deleteChair(ctx context.Context, tx pgx.Tx, main *domain.Main, now time.Time) error {
	tag, err := tx.Exec(ctx, `UPDATE chairs SET deleted_at = $3, update_at = $3
WHERE id = $1 AND main_id = $2 AND deleted_at IS NULL`,
		main.SubID, main.ID, now)

	return exactlyOne(tag, err)
}
