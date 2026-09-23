//go:build integration

package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/config"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
	"github.com/stretchr/testify/require"
)

func integrationURL(t *testing.T) string {
	t.Helper()

	url := os.Getenv("TEST_DATABASE_URL")
	require.NotEmpty(t, url, "TEST_DATABASE_URL must point to the dedicated test PostgreSQL")

	return url
}

func TestDatabasePoolLifecycle(t *testing.T) {
	t.Parallel()

	database, err := postgres.New(t.Context(), config.Database{
		URL:            integrationURL(t),
		ConnectTimeout: 5 * time.Second,
		MaxConns:       1,
	})
	require.NoError(t, err)
	t.Cleanup(database.Close)

	transaction, err := database.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		_ = transaction.Rollback(ctx)
	})

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	blocked, err := database.Begin(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, blocked)
	require.NoError(t, transaction.Rollback(t.Context()))

	transaction, err = database.Begin(t.Context())
	require.NoError(t, err)
	require.NoError(t, transaction.Commit(t.Context()))

	database.Close()

	closed, err := database.Begin(t.Context())
	require.Error(t, err)
	require.Nil(t, closed)
}

func TestUnknownSatelliteKind(t *testing.T) {
	t.Parallel()

	store, err := postgres.New(t.Context(), config.Database{
		URL:            integrationURL(t),
		ConnectTimeout: 5 * time.Second,
		MaxConns:       2,
	})
	require.NoError(t, err)
	t.Cleanup(store.Close)

	tests := []struct {
		name string
		kind domain.Kind
	}{
		{name: "empty"},
		{name: "unknown", kind: "unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			main := &domain.Main{ID: 1, SubID: 1, SubObj: test.kind}
			ctx := t.Context()

			id, err := store.NextSatelliteID(ctx, nil, test.kind)
			require.Error(t, err)
			require.Zero(t, id)

			locked, err := store.LockSatellite(ctx, nil, main)
			require.Error(t, err)
			require.Nil(t, locked)

			require.Error(t, store.InsertSatellite(ctx, nil, main))
			require.Error(t, store.UpdateSatellite(ctx, nil, main, time.Time{}))
			require.Error(t, store.DeleteSatellite(ctx, nil, main, time.Time{}))
		})
	}
}

func TestSatelliteWriteRejectsMismatchedObject(t *testing.T) {
	t.Parallel()

	store, err := postgres.New(t.Context(), config.Database{
		URL:            integrationURL(t),
		ConnectTimeout: 5 * time.Second,
		MaxConns:       2,
	})
	require.NoError(t, err)
	t.Cleanup(store.Close)

	tests := []struct {
		name      string
		kind      domain.Kind
		satellite domain.SubObject
	}{
		{name: "tool_with_chair", kind: domain.Tools, satellite: &domain.Chair{}},
		{name: "table_with_tool", kind: domain.Tables, satellite: &domain.Tool{}},
		{name: "chair_with_table", kind: domain.Chairs, satellite: &domain.Table{}},
		{name: "missing_tool", kind: domain.Tools},
		{name: "missing_table", kind: domain.Tables},
		{name: "missing_chair", kind: domain.Chairs},
		{name: "nil_tool", kind: domain.Tools, satellite: (*domain.Tool)(nil)},
		{name: "nil_table", kind: domain.Tables, satellite: (*domain.Table)(nil)},
		{name: "nil_chair", kind: domain.Chairs, satellite: (*domain.Chair)(nil)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			main := &domain.Main{
				ID:        1,
				SubID:     1,
				SubObj:    test.kind,
				Satellite: test.satellite,
			}
			ctx := t.Context()

			require.Error(t, store.InsertSatellite(ctx, nil, main))
			require.Error(t, store.UpdateSatellite(ctx, nil, main, time.Time{}))
		})
	}
}
