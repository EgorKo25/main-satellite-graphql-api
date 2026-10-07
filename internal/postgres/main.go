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
	        COALESCE(s.kind = m.sub_obj AND s.id = m.sub_id AND s.deleted_at IS NULL, false), s.data
	    FROM page m
	    LEFT JOIN LATERAL (
	        SELECT 'tools' AS kind, id, deleted_at, row_to_json(tools) AS data
	        FROM tools WHERE main_id = m.id
	        UNION ALL
	        SELECT 'tables', id, deleted_at, row_to_json(tables)
	        FROM tables WHERE main_id = m.id
	        UNION ALL
	        SELECT 'chairs', id, deleted_at, row_to_json(chairs)
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
