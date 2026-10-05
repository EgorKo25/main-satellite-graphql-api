//go:build integration

package integrationtests_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/config"
)

func TestLoadReturnsIndependentConfigurations(t *testing.T) {
	t.Parallel()

	const (
		firstContents = `
            database:
              url: postgres://localhost/first
              user: first_user
              password: first_password
              connect_timeout: 5s
              max_conns: 10
              min_conns: 0
            http:
              addr: 127.0.0.1:8081
              request_timeout: 3s
            `
		secondContents = `
            database:
              url: postgres://localhost/second
              user: second_user
              password: second_password
              connect_timeout: 250ms
              max_conns: 3
              min_conns: 3
            http:
              read_header_timeout: 2s
              shutdown_timeout: 20s
            `
	)

	var (
		wantFirst = config.App{
			Database: config.Database{
				URL:            "postgres://localhost/first",
				User:           "first_user",
				Password:       "first_password",
				ConnectTimeout: 5 * time.Second,
				MaxConns:       10,
				MinConns:       0,
			},
			HTTP: config.HTTP{
				Addr:              "127.0.0.1:8081",
				RequestTimeout:    3 * time.Second,
				ReadHeaderTimeout: 5 * time.Second,
				ReadTimeout:       10 * time.Second,
				WriteTimeout:      15 * time.Second,
				IdleTimeout:       time.Minute,
				ShutdownTimeout:   15 * time.Second,
			},
			Logger: config.Logger{Cores: []config.LoggerCore{{
				Level:      "info",
				Encoding:   "json",
				Output:     "stdout",
				TimeFormat: "utc",
			}}},
		}
		wantSecond = config.App{
			Database: config.Database{
				URL:            "postgres://localhost/second",
				User:           "second_user",
				Password:       "second_password",
				ConnectTimeout: 250 * time.Millisecond,
				MaxConns:       3,
				MinConns:       3,
			},
			HTTP: config.HTTP{
				Addr:              "0.0.0.0:8080",
				RequestTimeout:    10 * time.Second,
				ReadHeaderTimeout: 2 * time.Second,
				ReadTimeout:       10 * time.Second,
				WriteTimeout:      15 * time.Second,
				IdleTimeout:       time.Minute,
				ShutdownTimeout:   20 * time.Second,
			},
			Logger: config.Logger{Cores: []config.LoggerCore{{
				Level:      "info",
				Encoding:   "json",
				Output:     "stdout",
				TimeFormat: "utc",
			}}},
		}
	)

	filesystem := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(filesystem, "first.yaml", []byte(firstContents), 0o600))
	require.NoError(t, afero.WriteFile(filesystem, "second.yaml", []byte(secondContents), 0o600))

	first, err := config.Load(filesystem, "first.yaml")
	require.NoError(t, err)
	require.Empty(t, cmp.Diff(wantFirst, *first))

	second, err := config.Load(filesystem, "second.yaml")
	require.NoError(t, err)
	require.NotSame(t, first, second)
	require.Empty(t, cmp.Diff(wantSecond, *second))
	require.Empty(t, cmp.Diff(wantFirst, *first))

	second.MaxConns = 1
	second.Addr = "localhost:9000"
	second.Cores[0].Level = "fatal"

	require.Empty(t, cmp.Diff(wantFirst, *first))
}
