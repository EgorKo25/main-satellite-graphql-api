package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Store) List(ctx context.Context, mainID *int64, limit, offset int) ([]*domain.Main, error) {
	rows, err := s.pool.Query(ctx, `SELECT
    m.id, m.title, m.sub_id, m.sub_obj, m.created_at, m.update_at, m.deleted_at,
    t.id, t.main_id, t.description1, t.created_at, t.update_at, t.deleted_at,
    b.id, b.main_id, b.description2, b.created_at, b.update_at, b.deleted_at,
    c.id, c.main_id, c.description3, c.type::text, c.created_at, c.update_at, c.deleted_at
FROM main m
LEFT JOIN tools t ON t.main_id = m.id
LEFT JOIN tables b ON b.main_id = m.id
LEFT JOIN chairs c ON c.main_id = m.id
WHERE m.deleted_at IS NULL AND ($1::bigint IS NULL OR m.id = $1)
ORDER BY m.id ASC LIMIT $2 OFFSET $3`, mainID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query Main list: %w", err)
	}
	defer rows.Close()

	result := make([]*domain.Main, 0)

	for rows.Next() {
		main, err := scanAggregate(rows)
		if err != nil {
			return nil, err
		}

		result = append(result, main)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read Main list: %w", err)
	}

	return result, nil
}

func (s *Store) ReadTx(ctx context.Context, transaction pgx.Tx, mainID int64) (*domain.Main, error) {
	return scanAggregate(transaction.QueryRow(ctx, `SELECT
    m.id, m.title, m.sub_id, m.sub_obj, m.created_at, m.update_at, m.deleted_at,
    t.id, t.main_id, t.description1, t.created_at, t.update_at, t.deleted_at,
    b.id, b.main_id, b.description2, b.created_at, b.update_at, b.deleted_at,
    c.id, c.main_id, c.description3, c.type::text, c.created_at, c.update_at, c.deleted_at
FROM main m
LEFT JOIN tools t ON t.main_id = m.id
LEFT JOIN tables b ON b.main_id = m.id
LEFT JOIN chairs c ON c.main_id = m.id
WHERE m.id = $1`, mainID))
}

func (s *Store) LockMain(ctx context.Context, transaction pgx.Tx, mainID int64) (*domain.Main, error) {
	var main domain.Main

	if err := transaction.QueryRow(ctx, `SELECT id, title, sub_id, sub_obj, created_at, update_at, deleted_at
FROM main WHERE id = $1 FOR UPDATE`, mainID).Scan(
		&main.ID, &main.Title, &main.SubID, &main.SubObj, &main.CreatedAt, &main.UpdatedAt, &main.DeletedAt,
	); err != nil {
		return nil, fmt.Errorf("lock main %d: %w", mainID, err)
	}

	return &main, nil
}

func (s *Store) InsertMain(ctx context.Context, transaction pgx.Tx, main *domain.Main) (int64, error) {
	var mainID int64

	if err := transaction.QueryRow(ctx, `INSERT INTO main (title, sub_id, sub_obj, created_at, update_at)
VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		main.Title, main.SubID, main.SubObj, main.CreatedAt, main.UpdatedAt).Scan(&mainID); err != nil {
		return 0, fmt.Errorf("insert main: %w", err)
	}

	return mainID, nil
}

func (s *Store) UpdateMain(ctx context.Context, transaction pgx.Tx, main *domain.Main, now time.Time) error {
	tag, err := transaction.Exec(ctx, `UPDATE main SET title = $2, update_at = $3 WHERE id = $1 AND deleted_at IS NULL`,
		main.ID, main.Title, now)

	return exactlyOne(tag, err)
}

func (s *Store) DeleteMain(ctx context.Context, transaction pgx.Tx, mainID int64, now time.Time) error {
	tag, err := transaction.Exec(ctx, `UPDATE main SET deleted_at = $2, update_at = $2
WHERE id = $1 AND deleted_at IS NULL`, mainID, now)

	return exactlyOne(tag, err)
}
