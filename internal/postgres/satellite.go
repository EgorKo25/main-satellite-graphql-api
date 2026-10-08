package postgres

import (
	"fmt"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/samber/lo"
)

type satelliteRow struct {
	SatelliteID        int64
	SatelliteMainID    int64
	SatelliteCreatedAt time.Time
	SatelliteUpdatedAt time.Time
	SatelliteDeletedAt *time.Time
	Description        *string
	ChairType          *domain.ChairType
}

func (row *satelliteRow) ScanRow(rows pgx.Rows) error {
	value, err := pgx.RowToStructByName[satelliteRow](rows)
	if err != nil {
		return fmt.Errorf("scan satellite: %w", err)
	}

	*row = value

	return nil
}

func (row satelliteRow) object(kind domain.Kind) domain.SubObject {
	metadata := domain.Satellite{
		ID: row.SatelliteID, MainID: row.SatelliteMainID,
		CreatedAt: row.SatelliteCreatedAt, UpdatedAt: row.SatelliteUpdatedAt, DeletedAt: row.SatelliteDeletedAt,
	}

	return lo.Switch[domain.Kind, domain.SubObject](kind).
		CaseF(domain.Tools, func() domain.SubObject {
			return &domain.Tool{Satellite: metadata, Description1: row.Description}
		}).
		CaseF(domain.Tables, func() domain.SubObject {
			return &domain.Table{Satellite: metadata, Description2: row.Description}
		}).
		CaseF(domain.Chairs, func() domain.SubObject {
			return &domain.Chair{Satellite: metadata, Description3: row.Description, Type: *row.ChairType}
		}).Default(nil)
}

func satelliteColumns(kind domain.Kind) string {
	fields := map[domain.Kind]string{
		domain.Tools:  "description1 AS description, NULL::text AS chair_type",
		domain.Tables: "description2 AS description, NULL::text AS chair_type",
		domain.Chairs: "description3 AS description, type::text AS chair_type",
	}

	return `id AS satellite_id, main_id AS satellite_main_id,
	    created_at AS satellite_created_at, update_at AS satellite_updated_at,
	    deleted_at AS satellite_deleted_at, ` + fields[kind]
}
