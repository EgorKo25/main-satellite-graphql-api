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
	return db.create(ctx, title, domain.Tools, func(tx pgx.Tx, main *domain.Main) error {
		tag, err := tx.Exec(ctx, `
		    INSERT INTO tools (id, main_id, description1, created_at, update_at)
		    VALUES ($1, $2, $3, $4, $4);
		`, main.SubID, main.ID, input.Description1, main.CreatedAt)
		if err != nil {
			return fmt.Errorf("create tool: %w", err)
		}

		if tag.RowsAffected() != 1 {
			return fmt.Errorf("create tool: expected one changed row, got %d", tag.RowsAffected())
		}

		return nil
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
		tag, err := tx.Exec(ctx, `
		    UPDATE tools SET description1 = $3, update_at = $4
		    WHERE id = $1 AND main_id = $2 AND deleted_at IS NULL;
		`, main.SubID, main.ID, input.Description1.Value(), now)
		if err != nil {
			return fmt.Errorf("update tool: %w", err)
		}

		if tag.RowsAffected() != 1 {
			return fmt.Errorf("update tool: expected one changed row, got %d", tag.RowsAffected())
		}

		return nil
	})
}

func (db *DB) deleteTool(ctx context.Context, tx pgx.Tx, main *domain.Main, now time.Time) error {
	tag, err := tx.Exec(ctx, `
	    UPDATE tools SET deleted_at = $3, update_at = $3
	    WHERE id = $1 AND main_id = $2 AND deleted_at IS NULL;
	`, main.SubID, main.ID, now)
	if err != nil {
		return fmt.Errorf("delete tool: %w", err)
	}

	if tag.RowsAffected() != 1 {
		return fmt.Errorf("delete tool: expected one changed row, got %d", tag.RowsAffected())
	}

	return nil
}
