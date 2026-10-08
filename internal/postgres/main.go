package postgres

import (
	"context"
	"fmt"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/jackc/pgx/v5"
)

type mainRow struct {
	domain.Main
	satelliteRow

	Valid bool
}

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
	        COALESCE(s.kind = m.sub_obj AND s.id = m.sub_id AND s.deleted_at IS NULL, false) AS valid,
	        s.id AS satellite_id, s.main_id AS satellite_main_id,
	        s.created_at AS satellite_created_at, s.update_at AS satellite_updated_at,
	        s.deleted_at AS satellite_deleted_at, s.description, s.type AS chair_type
	    FROM page m
	    LEFT JOIN LATERAL (
	        SELECT 'tools' AS kind, id, main_id, created_at, update_at, deleted_at,
	            description1 AS description, NULL::text AS type
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

	result, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*domain.Main, error) {
		item, scanErr := pgx.RowToAddrOfStructByName[mainRow](row)
		if scanErr != nil {
			return nil, fmt.Errorf("scan Main: %w", scanErr)
		}

		if !item.Valid {
			return nil, fmt.Errorf("main %d has an inconsistent satellite relationship", item.ID)
		}

		item.Satellite = item.object(item.SubObj)

		return &item.Main, nil
	})
	if err != nil {
		return nil, fmt.Errorf("read Main list: %w", err)
	}

	return result, nil
}
