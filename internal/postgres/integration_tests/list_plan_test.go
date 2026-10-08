//go:build integration

package integrationtests_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
	"github.com/stretchr/testify/require"
)

func TestListIDPlanAfterDeepPages(t *testing.T) {
	t.Parallel()

	database, pool := setupDatabase(t)
	seedReadDatabase(t, pool, 100000, 32)

	mainID := int64(100000)
	items, err := database.List(t.Context(), postgres.ListInput{ID: &mainID, Limit: 20})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, mainID, items[0].ID)

	var query string

	err = pool.QueryRow(t.Context(), `
	    SELECT query FROM pg_stat_activity
	    WHERE datname = current_database() AND pid <> pg_backend_pid()
	        AND query LIKE '%WITH page AS%';
	`).Scan(&query)
	require.NoError(t, err)

	for range 12 {
		items, err = database.List(t.Context(), postgres.ListInput{Limit: 100, Offset: 99900})
		require.NoError(t, err)
		require.Len(t, items, 100)
		require.Equal(t, int64(99901), items[0].ID)
		require.Equal(t, mainID, items[99].ID)
	}

	for _, testCase := range []struct {
		name     string
		id       int64
		rowCount int
	}{
		{name: "existing", id: mainID, rowCount: 1},
		{name: "missing", id: mainID + 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, listErr := database.List(t.Context(), postgres.ListInput{ID: &testCase.id, Limit: 20})
			require.NoError(t, listErr)
			require.Len(t, result, testCase.rowCount)

			connection, connectErr := pool.Acquire(t.Context())
			require.NoError(t, connectErr)

			defer connection.Release()

			_, connectErr = connection.Exec(t.Context(), "SET plan_cache_mode = force_generic_plan;")
			require.NoError(t, connectErr)

			_, connectErr = connection.Exec(
				t.Context(), "PREPARE lookup_"+testCase.name+" (bigint, bigint, bigint) AS "+query,
			)
			require.NoError(t, connectErr)

			var (
				data []byte
				plan []struct {
					Plan struct {
						Rows       int `json:"Actual Rows"`
						HitBlocks  int `json:"Shared Hit Blocks"`
						ReadBlocks int `json:"Shared Read Blocks"`
					} `json:"Plan"`
				}
			)

			statement := fmt.Sprintf(
				"EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT JSON) EXECUTE lookup_%s(20, 0, %d);",
				testCase.name, testCase.id,
			)

			listErr = connection.QueryRow(t.Context(), statement).Scan(&data)
			require.NoError(t, listErr)
			require.NoError(t, json.Unmarshal(data, &plan))
			require.Len(t, plan, 1)
			require.Equal(t, testCase.rowCount, plan[0].Plan.Rows)
			require.Less(t, plan[0].Plan.HitBlocks+plan[0].Plan.ReadBlocks, 128, string(data))
			t.Logf("generic ID plan: %s", data)
		})
	}
}
