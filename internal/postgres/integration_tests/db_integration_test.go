//go:build integration

package integrationtests_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/config"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/logger"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
	integrationtests "github.com/EgorKo25/main-satellite-graphql-api/internal/postgres/integration_tests"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

var testDatabaseURL string

func TestMain(tests *testing.M) {
	err := logger.Initialize(afero.NewMemMapFs(), config.Logger{Cores: []config.LoggerCore{{
		Level: "info", Encoding: "json", Output: "stderr", TimeFormat: "utc",
	}}})
	if err != nil {
		panic(err)
	}

	log := logger.Get("integration")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	server, err := integrationtests.New(ctx)

	cancel()

	if err != nil {
		log.Fatal("start PostgreSQL integration suite", err)
	}

	testDatabaseURL = server.URL
	exitCode := tests.Run()
	cleanupCtx, done := context.WithTimeout(context.Background(), 30*time.Second)
	err = server.Close(cleanupCtx)

	done()

	if err != nil {
		log.Error("clean up PostgreSQL integration suite", err)

		exitCode = 1
	}

	if err = log.Sync(); err != nil {
		fmt.Fprintln(os.Stderr, err)

		exitCode = 1
	}

	os.Exit(exitCode)
}

func setupDatabase(tb testing.TB) (*postgres.DB, *pgxpool.Pool) {
	tb.Helper()

	ctx, cancel := context.WithTimeout(tb.Context(), 30*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, testDatabaseURL)
	require.NoError(tb, err)
	tb.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()

		require.NoError(tb, admin.Close(cleanupCtx))
	})

	name := "graphql_ops_" + strings.ToLower(rand.Text())
	quoted := pgx.Identifier{name}.Sanitize()
	_, err = admin.Exec(ctx, "CREATE DATABASE "+quoted)
	require.NoError(tb, err)
	tb.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()

		_, dropErr := admin.Exec(cleanupCtx, "DROP DATABASE "+quoted)
		require.NoError(tb, dropErr, "drop only the database created by this test")
	})

	parsed, err := url.Parse(testDatabaseURL)
	require.NoError(tb, err)

	parsed.Path = "/" + name
	testDSN := parsed.String()

	connectionConfig, err := pgx.ParseConfig(testDSN)
	require.NoError(tb, err)
	require.Equal(tb, name, connectionConfig.Database)

	migrationDB := stdlib.OpenDB(*connectionConfig)

	tb.Cleanup(func() { require.NoError(tb, migrationDB.Close()) })

	migrations, err := goose.NewProvider(goose.DialectPostgres, migrationDB, os.DirFS("../../../migrations"))
	require.NoError(tb, err)

	_, err = migrations.Up(ctx)
	require.NoError(tb, err)
	require.NoError(tb, migrationDB.Close())

	poolConfig, err := pgxpool.ParseConfig(testDSN)
	require.NoError(tb, err)

	poolConfig.MaxConns = 2
	poolConfig.ConnConfig.ConnectTimeout = 5 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	require.NoError(tb, err)
	tb.Cleanup(pool.Close)

	parsed.User = nil
	database, err := postgres.New(ctx, config.Database{
		URL:            parsed.String(),
		User:           connectionConfig.User,
		Password:       connectionConfig.Password,
		ConnectTimeout: 5 * time.Second,
		MaxConns:       2,
	})
	require.NoError(tb, err)
	tb.Cleanup(database.Close)

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
