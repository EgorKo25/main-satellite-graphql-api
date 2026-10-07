package postgres

import (
	"fmt"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/jackc/pgx/v5"
)

func scanMain(row pgx.CollectableRow) (*domain.Main, error) {
	var (
		main  domain.Main
		valid bool
	)

	if err := row.Scan(
		&main.ID, &main.Title, &main.SubID, &main.SubObj, &main.CreatedAt, &main.UpdatedAt, &main.DeletedAt,
		&valid, &main.SatelliteData,
	); err != nil {
		return nil, fmt.Errorf("scan Main: %w", err)
	}

	if !valid {
		return nil, fmt.Errorf("main %d has an inconsistent satellite relationship", main.ID)
	}

	return &main, nil
}
