//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

type traceCounter struct {
	queries atomic.Int64
}

func (counter *traceCounter) TraceQueryStart(
	ctx context.Context,
	_ *pgx.Conn,
	_ pgx.TraceQueryStartData,
) context.Context {
	counter.queries.Add(1)

	return ctx
}

func (*traceCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestListQueryCount(t *testing.T) {
	t.Parallel()

	database, inspection := setupDatabase(t)
	for range 25 {
		_, err := database.CreateTool(t.Context(), "bulk", postgres.ToolCreate{})
		require.NoError(t, err)
	}

	for _, limit := range []int{1, 20} {
		t.Run(fmt.Sprintf("limit_%d", limit), func(t *testing.T) {
			t.Parallel()

			var trace traceCounter

			poolConfig := inspection.Config()
			poolConfig.ConnConfig.Tracer = &trace
			pool, err := pgxpool.NewWithConfig(t.Context(), poolConfig)
			require.NoError(t, err)
			t.Cleanup(pool.Close)
			require.NoError(t, pool.Ping(t.Context()))

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
			handler := graph.NewHandler(postgres.NewReadDBForTest(pool), slog.New(slog.DiscardHandler))

			trace.queries.Store(0)
			handler.ServeHTTP(recorder, request)

			queryCount := trace.queries.Load()

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
			require.Equal(t, int64(1), queryCount)
		})
	}
}
