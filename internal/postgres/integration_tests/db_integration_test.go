//go:build integration

package integrationtests_test

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/config"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
	integrationtests "github.com/EgorKo25/main-satellite-graphql-api/internal/postgres/integration_tests"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

var testDatabaseURL string

func TestMain(tests *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	server, err := integrationtests.New(ctx)

	cancel()

	if err != nil {
		slog.Error("start PostgreSQL integration suite", "error", err)
		os.Exit(1)
	}

	testDatabaseURL = server.URL
	exitCode := tests.Run()
	cleanupCtx, done := context.WithTimeout(context.Background(), 30*time.Second)
	err = server.Close(cleanupCtx)

	done()

	if err != nil {
		slog.Error("clean up PostgreSQL integration suite", "error", err)

		exitCode = 1
	}

	os.Exit(exitCode)
}

func setupDatabase(t *testing.T) (*postgres.DB, *pgxpool.Pool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, testDatabaseURL)
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

	parsed, err := url.Parse(testDatabaseURL)
	require.NoError(t, err)

	parsed.Path = "/" + name
	testDSN := parsed.String()

	connectionConfig, err := pgx.ParseConfig(testDSN)
	require.NoError(t, err)
	require.Equal(t, name, connectionConfig.Database)

	migrationDB := stdlib.OpenDB(*connectionConfig)

	t.Cleanup(func() { require.NoError(t, migrationDB.Close()) })

	migrations, err := goose.NewProvider(goose.DialectPostgres, migrationDB, os.DirFS("../../../migrations"))
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
