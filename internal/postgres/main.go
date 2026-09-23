package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (db *DB) List(ctx context.Context, input ListInput) ([]*domain.Main, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	rows, err := db.pool.Query(ctx, `
	    SELECT
	        m.id, m.title, m.sub_id, m.sub_obj, m.created_at, m.update_at, m.deleted_at,
	        t.id, t.main_id, t.description1, t.created_at, t.update_at, t.deleted_at,
	        b.id, b.main_id, b.description2, b.created_at, b.update_at, b.deleted_at,
	        c.id, c.main_id, c.description3, c.type::text, c.created_at, c.update_at, c.deleted_at
	    FROM main m
	    LEFT JOIN tools t ON t.main_id = m.id
	    LEFT JOIN tables b ON b.main_id = m.id
	    LEFT JOIN chairs c ON c.main_id = m.id
	    WHERE m.deleted_at IS NULL AND ($1::bigint IS NULL OR m.id = $1)
	    ORDER BY m.id ASC LIMIT $2 OFFSET $3;
	`, input.ID, input.Limit, input.Offset)
	if err != nil {
		return nil, fmt.Errorf("query Main list: %w", err)
	}
	defer rows.Close()

	result := make([]*domain.Main, 0)

	var main *domain.Main

	for rows.Next() {
		main, err = scanAggregate(rows)
		if err != nil {
			return nil, err
		}

		result = append(result, main)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read Main list: %w", err)
	}

	return result, nil
}

func (db *DB) readTx(ctx context.Context, transaction pgx.Tx, mainID int64) (*domain.Main, error) {
	return scanAggregate(transaction.QueryRow(ctx, `
	    SELECT
	        m.id, m.title, m.sub_id, m.sub_obj, m.created_at, m.update_at, m.deleted_at,
	        t.id, t.main_id, t.description1, t.created_at, t.update_at, t.deleted_at,
	        b.id, b.main_id, b.description2, b.created_at, b.update_at, b.deleted_at,
	        c.id, c.main_id, c.description3, c.type::text, c.created_at, c.update_at, c.deleted_at
	    FROM main m
	    LEFT JOIN tools t ON t.main_id = m.id
	    LEFT JOIN tables b ON b.main_id = m.id
	    LEFT JOIN chairs c ON c.main_id = m.id
	    WHERE m.id = $1;
	`, mainID))
}

func (db *DB) lockMain(ctx context.Context, transaction pgx.Tx, mainID int64) (*domain.Main, error) {
	var main domain.Main

	err := transaction.QueryRow(ctx, `
	    SELECT id, title, sub_id, sub_obj, created_at, update_at, deleted_at
	    FROM main WHERE id = $1 FOR UPDATE;
	`, mainID).Scan(
		&main.ID, &main.Title, &main.SubID, &main.SubObj, &main.CreatedAt, &main.UpdatedAt, &main.DeletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("lock main %d: %w", mainID, err)
	}

	return &main, nil
}
