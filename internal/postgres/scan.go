package postgres

import (
	"fmt"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/jackc/pgx/v5"
)

func scanMain(row pgx.CollectableRow) (*domain.Main, error) {
	var (
		main        domain.Main
		satellite   domain.Satellite
		kind        domain.Kind
		description *string
		chairType   *domain.ChairType
	)

	if err := row.Scan(
		&main.ID, &main.Title, &main.SubID, &main.SubObj, &main.CreatedAt, &main.UpdatedAt, &main.DeletedAt,
		&kind, &satellite.ID, &satellite.MainID, &satellite.CreatedAt, &satellite.UpdatedAt,
		&satellite.DeletedAt, &description, &chairType,
	); err != nil {
		return nil, fmt.Errorf("scan Main: %w", err)
	}

	if kind != main.SubObj || satellite.ID != main.SubID || satellite.DeletedAt != nil {
		return nil, fmt.Errorf("main %d has an inconsistent satellite relationship", main.ID)
	}

	switch kind {
	case domain.Tools:
		main.Satellite = &domain.Tool{Satellite: satellite, Description1: description}
	case domain.Tables:
		main.Satellite = &domain.Table{Satellite: satellite, Description2: description}
	case domain.Chairs:
		if chairType == nil {
			return nil, fmt.Errorf("main %d has no chair type", main.ID)
		}

		main.Satellite = &domain.Chair{Satellite: satellite, Description3: description, Type: *chairType}
	default:
		return nil, fmt.Errorf("main %d has invalid satellite kind %q", main.ID, kind)
	}

	return &main, nil
}
