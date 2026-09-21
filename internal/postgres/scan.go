package postgres

import (
	"fmt"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/jackc/pgx/v5"
)

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

	if err := row.Scan(
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
	); err != nil {
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

	satellite, err := selected.domainObject(&main)
	if err != nil {
		return nil, err
	}

	main.Satellite = satellite
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
