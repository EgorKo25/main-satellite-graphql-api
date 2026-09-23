//go:build integration

package postgres_test

import (
	"context"
	"crypto/rand"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/config"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

func setupDatabase(t *testing.T) (*postgres.DB, *pgxpool.Pool) {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	require.NotEmpty(t, dsn, "TEST_DATABASE_URL must point to the dedicated test PostgreSQL")

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()

		require.NoError(t, admin.Close(cleanupCtx))
	})

	name := "graphql_ops_" + strings.ToLower(rand.Text())
	quoted := pgx.Identifier{name}.Sanitize()
	_, err = admin.Exec(ctx, "CREATE DATABASE "+quoted)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()

		_, dropErr := admin.Exec(cleanupCtx, "DROP DATABASE "+quoted)
		require.NoError(t, dropErr, "drop only the database created by this test")
	})

	testDSN := dsn + " dbname=" + name
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, parseErr := url.Parse(dsn)
		require.NoError(t, parseErr)

		parsed.Path = "/" + name
		query := parsed.Query()
		query.Del("database")
		query.Del("dbname")
		parsed.RawQuery = query.Encode()
		testDSN = parsed.String()
	}

	connectionConfig, err := pgx.ParseConfig(testDSN)
	require.NoError(t, err)
	require.Equal(t, name, connectionConfig.Database)

	migrationDB := stdlib.OpenDB(*connectionConfig)

	t.Cleanup(func() { require.NoError(t, migrationDB.Close()) })

	migrations, err := goose.NewProvider(goose.DialectPostgres, migrationDB, os.DirFS("../../migrations"))
	require.NoError(t, err)

	_, err = migrations.Up(ctx)
	require.NoError(t, err)
	require.NoError(t, migrationDB.Close())

	poolConfig, err := pgxpool.ParseConfig(testDSN)
	require.NoError(t, err)

	poolConfig.MaxConns = 2
	poolConfig.ConnConfig.ConnectTimeout = 5 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	database, err := postgres.New(ctx, config.Database{
		URL:            testDSN,
		ConnectTimeout: 5 * time.Second,
		MaxConns:       2,
	})
	require.NoError(t, err)
	t.Cleanup(database.Close)

	return database, pool
}

func TestDatabasePoolLifecycle(t *testing.T) {
	t.Parallel()

	database, _ := setupDatabase(t)
	items, err := database.List(t.Context(), postgres.ListInput{Limit: 20})
	require.NoError(t, err)
	require.Empty(t, items)

	database.Close()

	items, err = database.List(t.Context(), postgres.ListInput{Limit: 20})
	require.Error(t, err)
	require.Empty(t, items)
}
