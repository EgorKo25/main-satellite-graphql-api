//go:build integration

package integrationtests_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func seedReadDatabase(tb testing.TB, pool *pgxpool.Pool, count, descriptionSize int) {
	tb.Helper()

	ctx, cancel := context.WithTimeout(tb.Context(), 5*time.Minute)
	defer cancel()

	transaction, err := pool.Begin(ctx)
	require.NoError(tb, err)

	defer func() {
		if rollbackErr := transaction.Rollback(ctx); !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			require.NoError(tb, rollbackErr)
		}
	}()

	_, err = transaction.Exec(ctx, `
	    INSERT INTO main (id, title, sub_id, sub_obj, created_at, update_at)
	    SELECT id, 'main-' || id, id + $1, (ARRAY['tools','tables','chairs'])[1 + (id - 1) % 3],
	        '2026-09-01 12:34:56.123456+00', '2026-09-02 12:34:56.654321+00'
	    FROM generate_series(1, $1::bigint) AS id;
	`, count)
	require.NoError(tb, err)

	for _, variant := range []struct {
		table       string
		description string
		extraColumn string
		extraValue  string
	}{
		{table: "tools", description: toolDescriptionField},
		{table: "tables", description: "description2"},
		{table: "chairs", description: "description3", extraColumn: ", type", extraValue: ", 'abc'"},
	} {
		_, err = transaction.Exec(ctx, fmt.Sprintf(`
		    INSERT INTO %s (id, main_id, %s, created_at, update_at%s)
		    SELECT sub_id, id, CASE id %% 5 WHEN 1 THEN NULL WHEN 2 THEN '' ELSE repeat('x', $1) END,
		        created_at, update_at%s
		    FROM main WHERE sub_obj = $2;
		`, variant.table, variant.description, variant.extraColumn, variant.extraValue), descriptionSize, variant.table)
		require.NoError(tb, err)
	}

	require.NoError(tb, transaction.Commit(ctx))

	_, err = pool.Exec(ctx, "ANALYZE main; ANALYZE tools; ANALYZE tables; ANALYZE chairs;")
	require.NoError(tb, err)

	var mainCount, satelliteCount int

	err = pool.QueryRow(ctx, `
	    SELECT (SELECT count(*) FROM main),
	        (SELECT count(*) FROM tools) + (SELECT count(*) FROM tables) + (SELECT count(*) FROM chairs);
	`).Scan(&mainCount, &satelliteCount)
	require.NoError(tb, err)
	require.Equal(tb, count, mainCount)
	require.Equal(tb, count, satelliteCount)
}

func TestReadPaths(t *testing.T) {
	t.Parallel()

	database, pool := setupDatabase(t)
	seedReadDatabase(t, pool, 15, 32)

	paths := readPaths()
	expected := make([]readResult, 0, 15)

	for mainID := int64(1); mainID <= 15; mainID++ {
		var description *string

		switch mainID % 5 {
		case 1:
		case 2:
			description = new("")
		default:
			description = new(strings.Repeat("x", 32))
		}

		metadata := domain.Satellite{
			ID: mainID + 15, MainID: mainID,
			CreatedAt: time.Date(2026, time.September, 1, 12, 34, 56, 123456000, time.UTC),
			UpdatedAt: time.Date(2026, time.September, 2, 12, 34, 56, 654321000, time.UTC),
		}

		var satellite domain.SubObject

		switch (mainID - 1) % 3 {
		case 0:
			satellite = &domain.Tool{Satellite: metadata, Description1: description}
		case 1:
			satellite = &domain.Table{Satellite: metadata, Description2: description}
		case 2:
			satellite = &domain.Chair{Satellite: metadata, Description3: description, Type: domain.ABC}
		}

		expected = append(expected, readResult{
			Main: &domain.Main{
				ID: mainID, Title: fmt.Sprintf("main-%d", mainID), SubID: metadata.ID, SubObj: satellite.Kind(),
				CreatedAt: metadata.CreatedAt, UpdatedAt: metadata.UpdatedAt,
			},
			Satellite: satellite,
		})
	}

	for _, path := range paths {
		t.Run(path.name, func(t *testing.T) {
			t.Parallel()

			for _, page := range []struct {
				limit  int
				offset int
			}{
				{limit: 15}, {limit: 3, offset: 7}, {limit: 3, offset: 15},
			} {
				rows, err := pool.Query(t.Context(), path.query, nil, page.limit, page.offset)
				require.NoError(t, err)

				got, err := pgx.CollectRows(rows, path.scan)
				require.NoError(t, err)
				require.Empty(t, cmp.Diff(expected[page.offset:min(15, page.offset+page.limit)], got))
			}
		})
	}

	items, err := database.List(t.Context(), postgres.ListInput{Limit: 15})
	require.NoError(t, err)
	require.Len(t, items, len(expected))

	for index, main := range items {
		satellite := main.Satellite

		main.Satellite = nil
		require.Empty(t, cmp.Diff(expected[index], readResult{Main: main, Satellite: satellite}))
	}
}

func TestReadPathsRejectBrokenRelation(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		sql  string
	}{
		{name: "missing", sql: "DELETE FROM tools WHERE main_id = 1;"},
		{name: "wrong_id", sql: "UPDATE main SET sub_id = 999 WHERE id = 1;"},
		{name: "wrong_kind", sql: "UPDATE main SET sub_obj = 'chairs' WHERE id = 1;"},
		{name: "deleted", sql: "UPDATE tools SET deleted_at = now() WHERE main_id = 1;"},
		{name: "extra", sql: "INSERT INTO tables (id, main_id) VALUES (999, 1);"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			database, pool := setupDatabase(t)
			seedReadDatabase(t, pool, 9, 32)
			_, err := pool.Exec(t.Context(), test.sql)

			var constraintError *pgconn.PgError

			require.ErrorAs(t, err, &constraintError)
			require.Equal(t, "23514", constraintError.Code)
			require.Equal(t, "main_satellite_integrity", constraintError.ConstraintName)

			for _, path := range readPaths() {
				rows, queryErr := pool.Query(t.Context(), path.query, nil, 9, 0)
				require.NoError(t, queryErr)

				result, queryErr := pgx.CollectRows(rows, path.scan)
				require.NoError(t, queryErr, path.name)
				require.Len(t, result, 9)
			}

			items, err := database.List(t.Context(), postgres.ListInput{Limit: 9})
			require.NoError(t, err)
			require.Len(t, items, 9)
		})
	}
}

func BenchmarkReadPostgres(b *testing.B) {
	for _, dataset := range []struct {
		count           int
		descriptionSize int
	}{
		{count: 100, descriptionSize: 32},
		{count: 500_000, descriptionSize: 32},
		{count: 1_000_000, descriptionSize: 32},
		{count: 100, descriptionSize: 4096},
	} {
		b.Run(fmt.Sprintf("records=%d/description=%d", dataset.count, dataset.descriptionSize), func(b *testing.B) {
			_, pool := setupDatabase(b)
			seedReadDatabase(b, pool, dataset.count, dataset.descriptionSize)

			paths := readPaths()

			for _, limit := range []int{20, 100} {
				for _, position := range []string{"first", "last"} {
					offset := 0
					if position == "last" {
						offset = dataset.count - limit
					}

					for _, path := range paths {
						name := fmt.Sprintf("limit=%d/page=%s/path=%s", limit, position, path.name)
						b.Run(name, func(b *testing.B) {
							rows, err := pool.Query(b.Context(), path.query, nil, limit, offset)
							require.NoError(b, err)

							warm, err := pgx.CollectRows(rows, path.scan)
							require.NoError(b, err)
							require.Len(b, warm, limit)

							var result []readResult

							b.ReportAllocs()

							for b.Loop() {
								rows, err = pool.Query(b.Context(), path.query, nil, limit, offset)
								if err != nil {
									break
								}

								result, err = pgx.CollectRows(rows, path.scan)
								if err != nil {
									break
								}
							}

							require.NoError(b, err)
							require.Empty(b, cmp.Diff(warm, result))
						})
					}
				}
			}
		})
	}
}
