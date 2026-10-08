//go:build integration

package integrationtests_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph"
	"github.com/jackc/pgx/v5/pgxpool"
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

			callsBefore := statementCount(t, connection)

			handler.ServeHTTP(recorder, request)

			callsAfter := statementCount(t, connection)

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

func TestWriteQueryCount(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		operation string
		input     map[string]any
		calls     int64
	}{
		{
			operation: createOperation,
			input: map[string]any{
				"title":     "counted",
				"satellite": map[string]any{chairName: map[string]any{chairTypeField: domain.ABC}},
			},
			calls: 8,
		},
		{
			operation: updateOperation,
			input: map[string]any{
				"title": "updated",
				"satellite": map[string]any{chairName: map[string]any{
					chairDescriptionField: "updated description",
					chairTypeField:        domain.CDE,
				}},
			},
			calls: 5,
		},
		{operation: deleteOperation, input: map[string]any{}, calls: 9},
	} {
		t.Run(test.operation, func(t *testing.T) {
			t.Parallel()

			database, inspection := setupDatabase(t)
			input := maps.Clone(test.input)

			var mainID string

			if test.operation != createOperation {
				created, err := database.Create(t.Context(), "existing",
					map[string]any{chairName: map[string]any{chairTypeField: domain.ABC}})
				require.NoError(t, err)

				mainID = strconv.FormatInt(created.ID, 10)
				input["id"] = mainID
			}

			connection, err := inspection.Acquire(t.Context())
			require.NoError(t, err)
			t.Cleanup(connection.Release)

			_, err = connection.Exec(t.Context(), "CREATE EXTENSION pg_stat_statements;")
			require.NoError(t, err)
			_, err = connection.Exec(t.Context(), "SET pg_stat_statements.track = 'none';")
			require.NoError(t, err)

			var trackUtility bool

			err = connection.QueryRow(t.Context(),
				"SELECT current_setting('pg_stat_statements.track_utility')::boolean;").Scan(&trackUtility)
			require.NoError(t, err)
			require.True(t, trackUtility, "statement counts include BEGIN and COMMIT")

			body, err := json.Marshal(map[string]any{
				"query": `mutation($input: MainMutationInput!) {
                    main(input: $input) { main { id } deletedId }
                }`,
				"variables": map[string]any{"input": map[string]any{test.operation: input}},
			})
			require.NoError(t, err)

			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/graphql", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")

			recorder := httptest.NewRecorder()
			handler := graph.NewHandler(database, database)
			callsBefore := statementCount(t, connection)

			handler.ServeHTTP(recorder, request)

			callsAfter := statementCount(t, connection)

			var response struct {
				Data struct {
					Main *struct {
						Main *struct {
							ID string `json:"id"`
						} `json:"main"`
						DeletedID *string `json:"deletedId"`
					} `json:"main"`
				} `json:"data"`
				Errors []json.RawMessage `json:"errors"`
			}

			require.Equal(t, http.StatusOK, recorder.Code)
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
			require.Empty(t, response.Errors)
			require.NotNil(t, response.Data.Main)
			require.Equal(t, test.calls, callsAfter-callsBefore)

			if test.operation == deleteOperation {
				require.Nil(t, response.Data.Main.Main)
				require.Equal(t, new(mainID), response.Data.Main.DeletedID)
			} else {
				require.NotNil(t, response.Data.Main.Main)
				require.NotEmpty(t, response.Data.Main.Main.ID)
				require.Nil(t, response.Data.Main.DeletedID)
			}
		})
	}
}

func statementCount(t *testing.T, connection *pgxpool.Conn) int64 {
	t.Helper()

	var calls int64

	err := connection.QueryRow(t.Context(), `
		SELECT coalesce(sum(calls), 0)::bigint
		FROM pg_stat_statements
		WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database());
	`).Scan(&calls)
	require.NoError(t, err)

	return calls
}
