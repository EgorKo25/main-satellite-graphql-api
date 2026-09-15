package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

type Store struct{ pool *pgxpool.Pool }

func (s *Store) Begin(ctx context.Context) (pgx.Tx, error) {
	return s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
}

const aggregateSelect = `SELECT
    m.id, m.title, m.sub_id, m.sub_obj, m.created_at, m.update_at, m.deleted_at,
    t.id, t.main_id, t.description1, t.created_at, t.update_at, t.deleted_at,
    b.id, b.main_id, b.description2, b.created_at, b.update_at, b.deleted_at,
    c.id, c.main_id, c.description3, c.type::text, c.created_at, c.update_at, c.deleted_at
FROM main m
LEFT JOIN tools t ON t.main_id = m.id
LEFT JOIN tables b ON b.main_id = m.id
LEFT JOIN chairs c ON c.main_id = m.id`

func (s *Store) List(ctx context.Context, input domain.ListInput) ([]*domain.Main, error) {
	rows, err := s.pool.Query(ctx, aggregateSelect+`
WHERE m.deleted_at IS NULL AND ($1::bigint IS NULL OR m.id = $1)
ORDER BY m.id ASC LIMIT $2 OFFSET $3`, input.ID, input.Limit, input.Offset)
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

func (s *Store) ReadTx(ctx context.Context, tx pgx.Tx, id int64) (*domain.Main, error) {
	return scanAggregate(tx.QueryRow(ctx, aggregateSelect+` WHERE m.id = $1`, id))
}

type nullableSatellite struct {
	id          *int64
	mainID      *int64
	description *string
	typeName    *string
	createdAt   *time.Time
	updatedAt   *time.Time
	deletedAt   *time.Time
}

func scanAggregate(row pgx.Row) (*domain.Main, error) {
	var main domain.Main
	var tool, table, chair nullableSatellite
	err := row.Scan(
		&main.ID, &main.Title, &main.SubID, &main.SubObj, &main.CreatedAt, &main.UpdatedAt, &main.DeletedAt,
		&tool.id, &tool.mainID, &tool.description, &tool.createdAt, &tool.updatedAt, &tool.deletedAt,
		&table.id, &table.mainID, &table.description, &table.createdAt, &table.updatedAt, &table.deletedAt,
		&chair.id, &chair.mainID, &chair.description, &chair.typeName, &chair.createdAt, &chair.updatedAt, &chair.deletedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan Main aggregate: %w", err)
	}
	var selected nullableSatellite
	switch main.SubObj {
	case domain.Tools:
		selected = tool
	case domain.Tables:
		selected = table
	case domain.Chairs:
		selected = chair
	default:
		return nil, fmt.Errorf("Main %d has invalid sub_obj %q", main.ID, main.SubObj)
	}
	count := 0
	for _, satellite := range []nullableSatellite{tool, table, chair} {
		if satellite.id != nil {
			count++
		}
	}
	if count != 1 || selected.id == nil || selected.mainID == nil ||
		*selected.id != main.SubID || *selected.mainID != main.ID ||
		selected.createdAt == nil || selected.updatedAt == nil ||
		!equalNullableTime(main.DeletedAt, selected.deletedAt) {
		return nil, fmt.Errorf("Main %d has an inconsistent satellite relationship", main.ID)
	}
	satellite := domain.Satellite{
		ID: *selected.id, MainID: *selected.mainID,
		CreatedAt: selected.createdAt.UTC(),
		UpdatedAt: selected.updatedAt.UTC(), DeletedAt: utcPointer(selected.deletedAt),
	}
	switch main.SubObj {
	case domain.Tools:
		main.Satellite = &domain.Tool{Satellite: satellite, Description1: selected.description}
	case domain.Tables:
		main.Satellite = &domain.Table{Satellite: satellite, Description2: selected.description}
	case domain.Chairs:
		if selected.typeName == nil || (*selected.typeName != string(domain.ABC) && *selected.typeName != string(domain.CDE)) {
			return nil, fmt.Errorf("Main %d has an invalid chair type", main.ID)
		}
		main.Satellite = &domain.Chair{Satellite: satellite, Description3: selected.description, Type: domain.ChairType(*selected.typeName)}
	}
	main.CreatedAt = main.CreatedAt.UTC()
	main.UpdatedAt = main.UpdatedAt.UTC()
	main.DeletedAt = utcPointer(main.DeletedAt)
	return &main, nil
}

func equalNullableTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func utcPointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	converted := value.UTC()
	return &converted
}

func (s *Store) LockMain(ctx context.Context, tx pgx.Tx, id int64) (*domain.Main, error) {
	var main domain.Main
	err := tx.QueryRow(ctx, `SELECT id, title, sub_id, sub_obj, created_at, update_at, deleted_at
FROM main WHERE id = $1 FOR UPDATE`, id).Scan(
		&main.ID, &main.Title, &main.SubID, &main.SubObj, &main.CreatedAt, &main.UpdatedAt, &main.DeletedAt,
	)
	if err != nil {
		return nil, err
	}
	return &main, nil
}

func (s *Store) LockSatellite(ctx context.Context, tx pgx.Tx, main *domain.Main) (*domain.Main, error) {
	var query string
	switch main.SubObj {
	case domain.Tools:
		query = `SELECT id FROM tools WHERE id = $1 AND main_id = $2 FOR UPDATE`
	case domain.Tables:
		query = `SELECT id FROM tables WHERE id = $1 AND main_id = $2 FOR UPDATE`
	case domain.Chairs:
		query = `SELECT id FROM chairs WHERE id = $1 AND main_id = $2 FOR UPDATE`
	default:
		return nil, fmt.Errorf("invalid stored satellite kind %q", main.SubObj)
	}
	var id int64
	if err := tx.QueryRow(ctx, query, main.SubID, main.ID).Scan(&id); err != nil {
		return nil, fmt.Errorf("lock satellite for Main %d: %w", main.ID, err)
	}
	return s.ReadTx(ctx, tx, main.ID)
}

func (s *Store) NextSatelliteID(ctx context.Context, tx pgx.Tx, kind domain.Kind) (int64, error) {
	var query string
	switch kind {
	case domain.Tools:
		query = `SELECT nextval(pg_get_serial_sequence('tools', 'id'))`
	case domain.Tables:
		query = `SELECT nextval(pg_get_serial_sequence('tables', 'id'))`
	case domain.Chairs:
		query = `SELECT nextval(pg_get_serial_sequence('chairs', 'id'))`
	default:
		return 0, fmt.Errorf("invalid satellite kind %q", kind)
	}
	var id int64
	err := tx.QueryRow(ctx, query).Scan(&id)
	return id, err
}

func (s *Store) Now(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	var now time.Time
	err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now)
	return now.UTC(), err
}

func (s *Store) InsertMain(ctx context.Context, tx pgx.Tx, input domain.CreateInput, subID int64, now time.Time) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `INSERT INTO main (title, sub_id, sub_obj, created_at, update_at)
VALUES ($1, $2, $3, $4, $4) RETURNING id`, input.Title, subID, input.Kind, now).Scan(&id)
	return id, err
}

func (s *Store) InsertSatellite(ctx context.Context, tx pgx.Tx, input domain.CreateInput, mainID, subID int64, now time.Time) error {
	var query string
	args := []any{subID, mainID, input.Description, now}
	switch input.Kind {
	case domain.Tools:
		query = `INSERT INTO tools (id, main_id, description1, created_at, update_at) VALUES ($1, $2, $3, $4, $4)`
	case domain.Tables:
		query = `INSERT INTO tables (id, main_id, description2, created_at, update_at) VALUES ($1, $2, $3, $4, $4)`
	case domain.Chairs:
		query = `INSERT INTO chairs (id, main_id, description3, created_at, update_at, type) VALUES ($1, $2, $3, $4, $4, $5)`
		args = append(args, input.ChairType)
	default:
		return fmt.Errorf("invalid satellite kind %q", input.Kind)
	}
	tag, err := tx.Exec(ctx, query, args...)
	return exactlyOne(tag, err)
}

func (s *Store) UpdateMain(ctx context.Context, tx pgx.Tx, main *domain.Main, now time.Time) error {
	tag, err := tx.Exec(ctx, `UPDATE main SET title = $2, update_at = $3 WHERE id = $1 AND deleted_at IS NULL`, main.ID, main.Title, now)
	return exactlyOne(tag, err)
}

func (s *Store) UpdateSatellite(ctx context.Context, tx pgx.Tx, satellite domain.SubObject, now time.Time) error {
	var query string
	var args []any
	switch satellite := satellite.(type) {
	case *domain.Tool:
		query = `UPDATE tools SET description1 = $3, update_at = $4 WHERE id = $1 AND main_id = $2 AND deleted_at IS NULL`
		args = []any{satellite.ID, satellite.MainID, satellite.Description1, now}
	case *domain.Table:
		query = `UPDATE tables SET description2 = $3, update_at = $4 WHERE id = $1 AND main_id = $2 AND deleted_at IS NULL`
		args = []any{satellite.ID, satellite.MainID, satellite.Description2, now}
	case *domain.Chair:
		query = `UPDATE chairs SET description3 = $3, update_at = $4, type = $5 WHERE id = $1 AND main_id = $2 AND deleted_at IS NULL`
		args = []any{satellite.ID, satellite.MainID, satellite.Description3, now, satellite.Type}
	default:
		return fmt.Errorf("invalid satellite type %T", satellite)
	}
	tag, err := tx.Exec(ctx, query, args...)
	return exactlyOne(tag, err)
}

func (s *Store) DeleteMain(ctx context.Context, tx pgx.Tx, id int64, now time.Time) error {
	tag, err := tx.Exec(ctx, `UPDATE main SET deleted_at = $2, update_at = $2 WHERE id = $1 AND deleted_at IS NULL`, id, now)
	return exactlyOne(tag, err)
}

func (s *Store) DeleteSatellite(ctx context.Context, tx pgx.Tx, satellite domain.SubObject, now time.Time) error {
	var query string
	var args []any
	switch satellite := satellite.(type) {
	case *domain.Tool:
		query = `UPDATE tools SET deleted_at = $3, update_at = $3 WHERE id = $1 AND main_id = $2 AND deleted_at IS NULL`
		args = []any{satellite.ID, satellite.MainID, now}
	case *domain.Table:
		query = `UPDATE tables SET deleted_at = $3, update_at = $3 WHERE id = $1 AND main_id = $2 AND deleted_at IS NULL`
		args = []any{satellite.ID, satellite.MainID, now}
	case *domain.Chair:
		query = `UPDATE chairs SET deleted_at = $3, update_at = $3 WHERE id = $1 AND main_id = $2 AND deleted_at IS NULL`
		args = []any{satellite.ID, satellite.MainID, now}
	default:
		return fmt.Errorf("invalid satellite type %T", satellite)
	}
	tag, err := tx.Exec(ctx, query, args...)
	return exactlyOne(tag, err)
}

func exactlyOne(tag pgconn.CommandTag, err error) error {
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("expected one changed row, got %d", tag.RowsAffected())
	}
	return nil
}
