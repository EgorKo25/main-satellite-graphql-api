//go:build integration

package integrationtests_test

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph/benchmarks"
	"github.com/jackc/pgx/v5"
	"github.com/mailru/easyjson"
	"github.com/samber/lo"
)

type readResult struct {
	Main      *domain.Main
	Satellite domain.SubObject
}

type columnRow struct {
	domain.Main

	Valid       bool
	SatelliteID int64
	MainID      int64
	Description *string
	SCreatedAt  time.Time
	SUpdatedAt  time.Time
	SDeletedAt  *time.Time
	ChairType   *domain.ChairType
}

func (row columnRow) result() (readResult, error) {
	if !row.Valid {
		return readResult{}, fmt.Errorf("main %d has an inconsistent satellite relationship", row.ID)
	}

	metadata := domain.Satellite{
		ID: row.SatelliteID, MainID: row.MainID,
		CreatedAt: row.SCreatedAt, UpdatedAt: row.SUpdatedAt, DeletedAt: row.SDeletedAt,
	}
	satellite := lo.Switch[domain.Kind, domain.SubObject](row.SubObj).
		CaseF(domain.Tools, func() domain.SubObject {
			return &domain.Tool{Satellite: metadata, Description1: row.Description}
		}).
		CaseF(domain.Tables, func() domain.SubObject {
			return &domain.Table{Satellite: metadata, Description2: row.Description}
		}).
		CaseF(domain.Chairs, func() domain.SubObject {
			return &domain.Chair{Satellite: metadata, Description3: row.Description, Type: *row.ChairType}
		}).Default(nil)

	return readResult{Main: &row.Main, Satellite: satellite}, nil
}

func scanColumns(rows pgx.CollectableRow) (readResult, error) {
	var row columnRow

	err := rows.Scan(&row.ID, &row.Title, &row.SubID, &row.SubObj, &row.CreatedAt, &row.UpdatedAt,
		&row.DeletedAt, &row.Valid, &row.SatelliteID, &row.MainID, &row.Description,
		&row.SCreatedAt, &row.SUpdatedAt, &row.SDeletedAt, &row.ChairType)
	if err != nil {
		return readResult{}, fmt.Errorf("scan columns: %w", err)
	}

	return row.result()
}

func collectColumns(rows pgx.CollectableRow) (readResult, error) {
	row, err := pgx.RowToStructByName[columnRow](rows)
	if err != nil {
		return readResult{}, fmt.Errorf("collect columns: %w", err)
	}

	return row.result()
}

func scanJSON(rows pgx.CollectableRow, decode func(domain.Kind, []byte) (domain.SubObject, error)) (readResult, error) {
	var (
		main  domain.Main
		data  json.RawMessage
		valid bool
	)

	err := rows.Scan(&main.ID, &main.Title, &main.SubID, &main.SubObj, &main.CreatedAt, &main.UpdatedAt,
		&main.DeletedAt, &valid, &data)
	if err != nil {
		return readResult{}, fmt.Errorf("scan JSON projection: %w", err)
	}

	if !valid {
		return readResult{}, fmt.Errorf("main %d has an inconsistent satellite relationship", main.ID)
	}

	satellite, err := decode(main.SubObj, data)
	if err != nil {
		return readResult{}, err
	}

	main.Satellite = nil

	return readResult{Main: &main, Satellite: satellite}, nil
}

func decodeReadEasy(kind domain.Kind, data []byte) (domain.SubObject, error) {
	var err error

	value := lo.Switch[domain.Kind, domain.SubObject](kind).
		CaseF(domain.Tools, func() domain.SubObject {
			target := new(benchmarks.Tool)
			err = easyjson.Unmarshal(data, target)

			return (*domain.Tool)(target)
		}).
		CaseF(domain.Tables, func() domain.SubObject {
			target := new(benchmarks.Table)
			err = easyjson.Unmarshal(data, target)

			return (*domain.Table)(target)
		}).
		CaseF(domain.Chairs, func() domain.SubObject {
			target := new(benchmarks.Chair)
			err = easyjson.Unmarshal(data, target)

			return (*domain.Chair)(target)
		}).Default(nil)

	if err != nil {
		return nil, fmt.Errorf("decode easyjson: %w", err)
	}

	if value == nil {
		return nil, fmt.Errorf("unknown satellite kind %q", kind)
	}

	return value, nil
}

var readFactories = map[domain.Kind]func() domain.SubObject{
	domain.Tools:  func() domain.SubObject { return new(domain.Tool) },
	domain.Tables: func() domain.SubObject { return new(domain.Table) },
	domain.Chairs: func() domain.SubObject { return new(domain.Chair) },
}

func decodeReadStandard(kind domain.Kind, data []byte) (domain.SubObject, error) {
	factory, ok := readFactories[kind]
	if !ok {
		return nil, fmt.Errorf("unknown satellite kind %q", kind)
	}

	value := factory()
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("decode standard JSON: %w", err)
	}

	return value, nil
}

func readPaths() []struct {
	name  string
	query string
	scan  pgx.RowToFunc[readResult]
} {
	return []struct {
		name  string
		query string
		scan  pgx.RowToFunc[readResult]
	}{
		{name: "columns_scan", query: readQuery(false), scan: scanColumns},
		{name: "columns_collector", query: readQuery(false), scan: collectColumns},
		{name: "json_lo_easy", query: readQuery(true), scan: func(rows pgx.CollectableRow) (readResult, error) {
			return scanJSON(rows, decodeReadEasy)
		}},
		{name: "json_map_standard", query: readQuery(true), scan: func(rows pgx.CollectableRow) (readResult, error) {
			return scanJSON(rows, decodeReadStandard)
		}},
	}
}

func readQuery(jsonProjection bool) string {
	var (
		projection = `s.id AS satellite_id, s.main_id, s.description,
		    s.created_at AS s_created_at, s.update_at AS s_updated_at, s.deleted_at AS s_deleted_at, s.type AS chair_type`
		branches = `
		    SELECT 'tools' AS kind, id, main_id, description1 AS description, created_at, update_at, deleted_at,
		        NULL::text AS type FROM tools WHERE main_id = m.id
		    UNION ALL
		    SELECT 'tables', id, main_id, description2, created_at, update_at, deleted_at, NULL::text
		        FROM tables WHERE main_id = m.id
		    UNION ALL
		    SELECT 'chairs', id, main_id, description3, created_at, update_at, deleted_at, type::text
		        FROM chairs WHERE main_id = m.id
		`
	)

	if jsonProjection {
		projection = "s.data"
		branches = `
		    SELECT 'tools' AS kind, id, deleted_at, row_to_json(tools) AS data FROM tools WHERE main_id = m.id
		    UNION ALL
		    SELECT 'tables', id, deleted_at, row_to_json(tables) FROM tables WHERE main_id = m.id
		    UNION ALL
		    SELECT 'chairs', id, deleted_at, row_to_json(chairs) FROM chairs WHERE main_id = m.id
		`
	}

	return fmt.Sprintf(`
	    WITH page AS (
	        SELECT id, title, sub_id, sub_obj, created_at, update_at, deleted_at
	        FROM main
	        WHERE deleted_at IS NULL AND ($1::bigint IS NULL OR id = $1)
	        ORDER BY id ASC LIMIT $2 OFFSET $3
	    )
	    SELECT m.id, m.title, m.sub_id, m.sub_obj, m.created_at, m.update_at, m.deleted_at,
	        COALESCE(s.kind = m.sub_obj AND s.id = m.sub_id AND s.deleted_at IS NULL, false) AS valid,
	        %s
	    FROM page m LEFT JOIN LATERAL (%s) s ON true
	    ORDER BY m.id ASC;
	`, projection, branches)
}
