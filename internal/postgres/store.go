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
	transaction, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}

	return transaction, nil
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

func (s *Store) List(ctx context.Context, id *int64, limit, offset int) ([]*domain.Main, error) {
	rows, err := s.pool.Query(ctx, aggregateSelect+`
WHERE m.deleted_at IS NULL AND ($1::bigint IS NULL OR m.id = $1)
ORDER BY m.id ASC LIMIT $2 OFFSET $3`, id, limit, offset)
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

func (s *Store) ReadTx(ctx context.Context, transaction pgx.Tx, id int64) (*domain.Main, error) {
	return scanAggregate(transaction.QueryRow(ctx, aggregateSelect+` WHERE m.id = $1`, id))
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

func (s nullableSatellite) validateRelationship(main *domain.Main) error {
	if s.id == nil || s.mainID == nil || s.createdAt == nil || s.updatedAt == nil ||
		*s.id != main.SubID || *s.mainID != main.ID ||
		!equalNullableTime(main.DeletedAt, s.deletedAt) {
		return fmt.Errorf("main %d has an inconsistent satellite relationship", main.ID)
	}

	return nil
}

func (s nullableSatellite) domainObject(main *domain.Main) (domain.SubObject, error) {
	if err := s.validateRelationship(main); err != nil {
		return nil, err
	}

	satellite := domain.Satellite{
		ID:        *s.id,
		MainID:    *s.mainID,
		CreatedAt: s.createdAt.UTC(),
		UpdatedAt: s.updatedAt.UTC(),
		DeletedAt: utcPointer(s.deletedAt),
	}

	switch main.SubObj {
	case domain.Tools:
		return &domain.Tool{Satellite: satellite, Description1: s.description}, nil
	case domain.Tables:
		return &domain.Table{Satellite: satellite, Description2: s.description}, nil
	case domain.Chairs:
		if s.typeName == nil || (*s.typeName != string(domain.ABC) && *s.typeName != string(domain.CDE)) {
			return nil, fmt.Errorf("main %d has an invalid chair type", main.ID)
		}

		return &domain.Chair{
			Satellite:    satellite,
			Description3: s.description,
			Type:         domain.ChairType(*s.typeName),
		}, nil
	default:
		return nil, fmt.Errorf("main %d has invalid sub_obj %q", main.ID, main.SubObj)
	}
}

func scanAggregate(row pgx.Row) (*domain.Main, error) {
	var (
		main               domain.Main
		tool, table, chair nullableSatellite
	)

	err := row.Scan(
		&main.ID,
		&main.Title,
		&main.SubID,
		&main.SubObj,
		&main.CreatedAt,
		&main.UpdatedAt,
		&main.DeletedAt,
		&tool.id,
		&tool.mainID,
		&tool.description,
		&tool.createdAt,
		&tool.updatedAt,
		&tool.deletedAt,
		&table.id,
		&table.mainID,
		&table.description,
		&table.createdAt,
		&table.updatedAt,
		&table.deletedAt,
		&chair.id,
		&chair.mainID,
		&chair.description,
		&chair.typeName,
		&chair.createdAt,
		&chair.updatedAt,
		&chair.deletedAt,
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
		return nil, fmt.Errorf("main %d has invalid sub_obj %q", main.ID, main.SubObj)
	}

	count := 0

	for _, satellite := range []nullableSatellite{tool, table, chair} {
		if satellite.id != nil {
			count++
		}
	}

	if count != 1 {
		return nil, fmt.Errorf("main %d must have exactly one satellite, got %d", main.ID, count)
	}

	main.Satellite, err = selected.domainObject(&main)
	if err != nil {
		return nil, err
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

func (s *Store) LockMain(ctx context.Context, transaction pgx.Tx, mainID int64) (*domain.Main, error) {
	var main domain.Main

	err := transaction.QueryRow(ctx, `SELECT id, title, sub_id, sub_obj, created_at, update_at, deleted_at
FROM main WHERE id = $1 FOR UPDATE`, mainID).Scan(
		&main.ID, &main.Title, &main.SubID, &main.SubObj, &main.CreatedAt, &main.UpdatedAt, &main.DeletedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("lock main %d: %w", mainID, err)
	}

	return &main, nil
}

func (s *Store) LockSatellite(ctx context.Context, transaction pgx.Tx, main *domain.Main) (*domain.Main, error) {
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
	if err := transaction.QueryRow(ctx, query, main.SubID, main.ID).Scan(&id); err != nil {
		return nil, fmt.Errorf("lock satellite for Main %d: %w", main.ID, err)
	}

	return s.ReadTx(ctx, transaction, main.ID)
}

func (s *Store) NextSatelliteID(ctx context.Context, transaction pgx.Tx, kind domain.Kind) (int64, error) {
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

	var satelliteID int64

	err := transaction.QueryRow(ctx, query).Scan(&satelliteID)
	if err != nil {
		return 0, fmt.Errorf("allocate %s satellite ID: %w", kind, err)
	}

	return satelliteID, nil
}

func (s *Store) Now(ctx context.Context, transaction pgx.Tx) (time.Time, error) {
	var now time.Time

	err := transaction.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now)
	if err != nil {
		return time.Time{}, fmt.Errorf("read database time: %w", err)
	}

	return now.UTC(), nil
}

func (s *Store) InsertMain(ctx context.Context, transaction pgx.Tx, main *domain.Main) (int64, error) {
	var mainID int64

	err := transaction.QueryRow(ctx, `INSERT INTO main (title, sub_id, sub_obj, created_at, update_at)
VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		main.Title, main.SubID, main.SubObj, main.CreatedAt, main.UpdatedAt).Scan(&mainID)
	if err != nil {
		return 0, fmt.Errorf("insert main: %w", err)
	}

	return mainID, nil
}

func (s *Store) InsertSatellite(ctx context.Context, transaction pgx.Tx, main *domain.Main) error {
	var (
		query string
		args  []any
	)

	switch satellite := main.Satellite.(type) {
	case *domain.Tool:
		query = `INSERT INTO tools (id, main_id, description1, created_at, update_at) VALUES ($1, $2, $3, $4, $5)`
		args = []any{main.SubID, main.ID, satellite.Description1, satellite.CreatedAt, satellite.UpdatedAt}
	case *domain.Table:
		query = `INSERT INTO tables (id, main_id, description2, created_at, update_at) VALUES ($1, $2, $3, $4, $5)`
		args = []any{main.SubID, main.ID, satellite.Description2, satellite.CreatedAt, satellite.UpdatedAt}
	case *domain.Chair:
		query = `INSERT INTO chairs (id, main_id, description3, created_at, update_at, type) VALUES ($1, $2, $3, $4, $5, $6)`
		args = []any{
			main.SubID,
			main.ID,
			satellite.Description3,
			satellite.CreatedAt,
			satellite.UpdatedAt,
			satellite.Type,
		}
	default:
		return fmt.Errorf("invalid satellite type %T", satellite)
	}

	tag, err := transaction.Exec(ctx, query, args...)

	return exactlyOne(tag, err)
}

func (s *Store) UpdateMain(ctx context.Context, transaction pgx.Tx, main *domain.Main, now time.Time) error {
	tag, err := transaction.Exec(
		ctx,
		`UPDATE main SET title = $2, update_at = $3 WHERE id = $1 AND deleted_at IS NULL`,
		main.ID,
		main.Title,
		now,
	)

	return exactlyOne(tag, err)
}

func (s *Store) UpdateSatellite(
	ctx context.Context,
	transaction pgx.Tx,
	satellite domain.SubObject,
	now time.Time,
) error {
	var (
		query string
		args  []any
	)

	switch satellite := satellite.(type) {
	case *domain.Tool:
		query = `UPDATE tools SET description1 = $3, update_at = $4 WHERE id = $1 AND main_id = $2 AND deleted_at IS NULL`
		args = []any{satellite.ID, satellite.MainID, satellite.Description1, now}
	case *domain.Table:
		query = `UPDATE tables SET description2 = $3, update_at = $4 WHERE id = $1 AND main_id = $2 AND deleted_at IS NULL`
		args = []any{satellite.ID, satellite.MainID, satellite.Description2, now}
	case *domain.Chair:
		query = `UPDATE chairs SET description3 = $3, update_at = $4, type = $5
WHERE id = $1 AND main_id = $2 AND deleted_at IS NULL`
		args = []any{satellite.ID, satellite.MainID, satellite.Description3, now, satellite.Type}
	default:
		return fmt.Errorf("invalid satellite type %T", satellite)
	}

	tag, err := transaction.Exec(ctx, query, args...)

	return exactlyOne(tag, err)
}

func (s *Store) DeleteMain(ctx context.Context, transaction pgx.Tx, id int64, now time.Time) error {
	tag, err := transaction.Exec(
		ctx,
		`UPDATE main SET deleted_at = $2, update_at = $2 WHERE id = $1 AND deleted_at IS NULL`,
		id,
		now,
	)

	return exactlyOne(tag, err)
}

func (s *Store) DeleteSatellite(
	ctx context.Context,
	transaction pgx.Tx,
	satellite domain.SubObject,
	now time.Time,
) error {
	var (
		query string
		args  []any
	)

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

	tag, err := transaction.Exec(ctx, query, args...)

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
