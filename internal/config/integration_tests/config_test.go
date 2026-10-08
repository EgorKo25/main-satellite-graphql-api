//go:build integration

package integrationtests_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/config"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/logger"
)

//nolint:paralleltest // Initializes the process-global logger.
func TestYAMLConfiguresFileLoggerOutputs(t *testing.T) {
	const (
		loggerName = "startup"
		operation  = "connect"
		contents   = `
            database:
              url: postgres://localhost/test
              user: graphql
              password: graphql_dev
              connect_timeout: 5s
              max_conns: 10
            logger:
              cores:
                - level: info
                  encoding: json
                  output: file
                  path: logs/application.json
                  time_format: utc
                - level: error
                  encoding: json
                  output: file
                  path: logs/errors.json
                  time_format: utc
            `
	)

	filesystem := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(filesystem, "config.yaml", []byte(contents), 0o600))

	app, err := config.Load(filesystem, "config.yaml")
	require.NoError(t, err)
	require.NoError(t, logger.Initialize(filesystem, app.Logger))

	log := logger.Get(loggerName).With(logger.String("operation", operation))

	t.Cleanup(func() { require.NoError(t, log.Close()) })

	log.Debug("filtered out")
	log.Info("starting")
	log.Error("connection failed", errors.New("database unavailable"))
	require.NoError(t, log.Close())

	type record struct {
		Level     string `json:"level"`
		Name      string `json:"logger"`
		Message   string `json:"msg"`
		Operation string `json:"operation"`
		Error     string `json:"error"`
	}

	for _, output := range []struct {
		path string
		want []record
	}{
		{
			path: "logs/application.json",
			want: []record{
				{Level: "info", Name: loggerName, Message: "starting", Operation: operation},
				{
					Level: "error", Name: loggerName, Message: "connection failed",
					Operation: operation, Error: "database unavailable",
				},
			},
		},
		{
			path: "logs/errors.json",
			want: []record{{
				Level: "error", Name: loggerName, Message: "connection failed",
				Operation: operation, Error: "database unavailable",
			}},
		},
	} {
		written, readErr := afero.ReadFile(filesystem, output.path)
		require.NoError(t, readErr)

		lines := bytes.Split(bytes.TrimSpace(written), []byte("\n"))
		got := make([]record, len(lines))

		for index, line := range lines {
			require.NoError(t, json.Unmarshal(line, &got[index]))
		}

		require.Empty(t, cmp.Diff(output.want, got), output.path)
	}
}

//nolint:paralleltest // Initializes the process-global logger.
func TestReadOnlyFilesystemRejectsLoggerInitialization(t *testing.T) {
	const contents = `
            database:
              url: postgres://localhost/test
              user: graphql
              password: graphql_dev
              connect_timeout: 5s
              max_conns: 10
            logger:
              cores:
                - level: info
                  encoding: json
                  output: file
                  path: logs/retained.json
                  time_format: utc
            `

	filesystem := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(filesystem, "config.yaml", []byte(contents), 0o600))

	app, err := config.Load(filesystem, "config.yaml")
	require.NoError(t, err)
	require.NoError(t, logger.Initialize(filesystem, app.Logger))

	active := logger.Get("startup")

	t.Cleanup(func() { require.NoError(t, active.Close()) })

	active.Info("before failed initialization")

	readOnly := afero.NewReadOnlyFs(filesystem)
	app, err = config.Load(readOnly, "config.yaml")
	require.NoError(t, err)

	err = logger.Initialize(readOnly, app.Logger)
	require.ErrorIs(t, err, fs.ErrPermission)
	require.ErrorContains(t, err, "create logger directory:")

	logger.Get("startup").Info("after failed initialization")
	require.NoError(t, active.Close())

	written, err := afero.ReadFile(filesystem, "logs/retained.json")
	require.NoError(t, err)

	lines := bytes.Split(bytes.TrimSpace(written), []byte("\n"))
	messages := make([]string, len(lines))

	for index, line := range lines {
		var record struct {
			Message string `json:"msg"`
		}

		require.NoError(t, json.Unmarshal(line, &record))

		messages[index] = record.Message
	}

	require.Equal(t, []string{"before failed initialization", "after failed initialization"}, messages)
}
