package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/config"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
	"github.com/stretchr/testify/require"
)

func TestNewRejectsInvalidDatabaseURL(t *testing.T) {
	t.Parallel()

	for _, url := range []string{"postgres://%zz", "postgres://localhost:invalid/example"} {
		t.Run(url, func(t *testing.T) {
			t.Parallel()

			database, err := postgres.New(t.Context(), config.Database{URL: url})
			require.ErrorContains(t, err, "parse database configuration")
			require.Nil(t, database)
		})
	}
}

func TestNewHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	database, err := postgres.New(ctx, config.Database{
		URL:            "postgres://localhost/unused?sslmode=disable",
		ConnectTimeout: time.Second,
		MaxConns:       1,
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, database)
}
