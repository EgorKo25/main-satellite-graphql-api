package postgres

import (
	"context"
	"fmt"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (db *DB) List(ctx context.Context, input ListInput) ([]*domain.Main, error) {
	if err := db.validator.Struct(input); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	rows, err := db.pool.Query(ctx, `
	    WITH page AS (
	        SELECT id, title, sub_id, sub_obj, created_at, update_at, deleted_at
	        FROM main
	        WHERE deleted_at IS NULL AND ($1::bigint IS NULL OR id = $1)
	        ORDER BY id ASC LIMIT $2 OFFSET $3
	    )
	    SELECT
	        m.id, m.title, m.sub_id, m.sub_obj, m.created_at, m.update_at, m.deleted_at,
	        s.kind, s.id, s.main_id, s.created_at, s.update_at, s.deleted_at, s.description, s.chair_type
	    FROM page m
	    LEFT JOIN LATERAL (
	        SELECT 'tools' AS kind, id, main_id, created_at, update_at, deleted_at,
	               description1 AS description, NULL::text AS chair_type
	        FROM tools WHERE main_id = m.id
	        UNION ALL
	        SELECT 'tables', id, main_id, created_at, update_at, deleted_at, description2, NULL::text
	        FROM tables WHERE main_id = m.id
	        UNION ALL
	        SELECT 'chairs', id, main_id, created_at, update_at, deleted_at, description3, type::text
	        FROM chairs WHERE main_id = m.id
	    ) s ON true
	    ORDER BY m.id ASC;
	`, input.ID, input.Limit, input.Offset)
	if err != nil {
		return nil, fmt.Errorf("query Main list: %w", err)
	}

	result, err := pgx.CollectRows(rows, scanMain)
	if err != nil {
		return nil, fmt.Errorf("read Main list: %w", err)
	}

	return result, nil
}
