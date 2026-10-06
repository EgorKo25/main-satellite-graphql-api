//go:build integration

package integrationtests_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph"
	"github.com/stretchr/testify/require"
)

func TestListQueryCount(t *testing.T) {
	t.Parallel()

	for _, limit := range []int{1, 20} {
		t.Run(fmt.Sprintf("limit_%d", limit), func(t *testing.T) {
			t.Parallel()

			database, inspection := setupDatabase(t)
			for range 25 {
				_, err := database.Create(t.Context(), "bulk", map[string]any{toolName: map[string]any{}})
				require.NoError(t, err)
			}

			connection, err := inspection.Acquire(t.Context())
			require.NoError(t, err)
			t.Cleanup(connection.Release)

			_, err = connection.Exec(t.Context(), "CREATE EXTENSION pg_stat_statements;")
			require.NoError(t, err)
			_, err = connection.Exec(t.Context(), "SET pg_stat_statements.track = 'none';")
			require.NoError(t, err)

			body, err := json.Marshal(map[string]any{
				"query": `query($limit:Int!) { main(limit:$limit) { id title createdAt updatedAt deletedAt satellite {
 __typename
 ... on Tool { id description1 createdAt updatedAt deletedAt }
 ... on Table { id description2 createdAt updatedAt deletedAt }
 ... on Chair { id description3 type createdAt updatedAt deletedAt }
} } }`,
				"variables": map[string]any{"limit": limit},
			})
			require.NoError(t, err)

			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/graphql", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")

			recorder := httptest.NewRecorder()
			handler := graph.NewHandler(database, database)

			var callsBefore, callsAfter int64

			err = connection.QueryRow(t.Context(), `
				SELECT coalesce(sum(calls), 0)::bigint
				FROM pg_stat_statements
				WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database());
			`).Scan(&callsBefore)
			require.NoError(t, err)

			handler.ServeHTTP(recorder, request)

			err = connection.QueryRow(t.Context(), `
				SELECT coalesce(sum(calls), 0)::bigint
				FROM pg_stat_statements
				WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database());
			`).Scan(&callsAfter)
			require.NoError(t, err)

			var response struct {
				Data struct {
					Main []json.RawMessage `json:"main"`
				} `json:"data"`
				Errors []json.RawMessage `json:"errors"`
			}

			require.Equal(t, http.StatusOK, recorder.Code)
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
			require.Empty(t, response.Errors)
			require.Len(t, response.Data.Main, limit)
			require.Equal(t, int64(1), callsAfter-callsBefore)
		})
	}
}
