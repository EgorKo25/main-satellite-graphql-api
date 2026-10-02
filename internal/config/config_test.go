package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/config"
)

const databaseYAML = `database:
  url: postgres://localhost/test
  user: graphql
  password: graphql_dev
  connect_timeout: 5s
  max_conns: 10
`

func TestLoadFromYAML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		contents string
		want     config.Database
	}{
		{
			name: "zero minimum connections",
			contents: `database:
  url: postgres://localhost/first
  user: first_user
  password: first_password
  connect_timeout: 5s
  max_conns: 10
  min_conns: 0
`,
			want: config.Database{
				URL:            "postgres://localhost/first",
				User:           "first_user",
				Password:       "first_password",
				ConnectTimeout: 5 * time.Second,
				MaxConns:       10,
				MinConns:       0,
			},
		},
		{
			name: "equal connection limits",
			contents: `database:
  url: postgres://localhost/second
  user: second_user
  password: second_password
  connect_timeout: 250ms
  max_conns: 3
  min_conns: 3
`,
			want: config.Database{
				URL:            "postgres://localhost/second",
				User:           "second_user",
				Password:       "second_password",
				ConnectTimeout: 250 * time.Millisecond,
				MaxConns:       3,
				MinConns:       3,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(test.contents), 0o600))

			app, err := config.Load(path)
			require.NoError(t, err)
			require.Equal(t, test.want, app.Database)
		})
	}
}

func TestLoadIgnoresDatabaseURLFromEnvironment(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://ignored:ignored@localhost/environment")

	var contents = `database:
  url: postgres://localhost/file
  user: graphql
  password: graphql_dev
  connect_timeout: 5s
  max_conns: 10
  min_conns: 2
`

	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

	app, err := config.Load(path)
	require.NoError(t, err)
	require.Equal(t, config.Database{
		URL:            "postgres://localhost/file",
		User:           "graphql",
		Password:       "graphql_dev",
		ConnectTimeout: 5 * time.Second,
		MaxConns:       10,
		MinConns:       2,
	}, app.Database)
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		contents string
	}{
		{name: "empty document", contents: ""},
		{name: "missing database", contents: "{}"},
		{
			name: "unknown section",
			contents: `database:
  user: graphql
  password: graphql_dev
  url: postgres://localhost/test
  connect_timeout: 5s
  max_conns: 10
databaze: {}`,
		},
		{
			name: "unknown database field",
			contents: `database:
  url: postgres://localhost/test
  user: graphql
  password: graphql_dev
  connect_timeout: 5s
  max_conns: 10
  pool_size: 10`,
		},
		{name: "invalid YAML", contents: "database: ["},
		{
			name: "duplicate field",
			contents: `database:
  url: postgres://localhost/test
  user: graphql
  password: graphql_dev
  connect_timeout: 5s
  max_conns: 10
  max_conns: 20`,
		},
		{
			name: "empty URL",
			contents: `database:
  user: graphql
  password: graphql_dev
  url: ''
  connect_timeout: 5s
  max_conns: 10
  min_conns: 0`,
		},
		{
			name: "null URL",
			contents: `database:
  user: graphql
  password: graphql_dev
  url: null
  connect_timeout: 5s
  max_conns: 10
  min_conns: 0`,
		},
		{
			name: "missing user",
			contents: `database:
  url: postgres://localhost/test
  password: graphql_dev
  connect_timeout: 5s
  max_conns: 10`,
		},
		{
			name: "empty user",
			contents: `database:
  url: postgres://localhost/test
  user: ''
  password: graphql_dev
  connect_timeout: 5s
  max_conns: 10`,
		},
		{
			name: "null user",
			contents: `database:
  url: postgres://localhost/test
  user: null
  password: graphql_dev
  connect_timeout: 5s
  max_conns: 10`,
		},
		{
			name: "missing password",
			contents: `database:
  url: postgres://localhost/test
  user: graphql
  connect_timeout: 5s
  max_conns: 10`,
		},
		{
			name: "empty password",
			contents: `database:
  url: postgres://localhost/test
  user: graphql
  password: ''
  connect_timeout: 5s
  max_conns: 10`,
		},
		{
			name: "null password",
			contents: `database:
  url: postgres://localhost/test
  user: graphql
  password: null
  connect_timeout: 5s
  max_conns: 10`,
		},
		{
			name: "missing timeout",
			contents: `database:
  user: graphql
  password: graphql_dev
  url: postgres://localhost/test
  max_conns: 10
  min_conns: 0`,
		},
		{
			name: "zero timeout",
			contents: `database:
  user: graphql
  password: graphql_dev
  url: postgres://localhost/test
  connect_timeout: 0s
  max_conns: 10`,
		},
		{
			name: "negative timeout",
			contents: `database:
  user: graphql
  password: graphql_dev
  url: postgres://localhost/test
  connect_timeout: -1s
  max_conns: 10`,
		},
		{
			name: "invalid timeout",
			contents: `database:
  user: graphql
  password: graphql_dev
  url: postgres://localhost/test
  connect_timeout: immediately
  max_conns: 10`,
		},
		{
			name: "numeric timeout",
			contents: `database:
  user: graphql
  password: graphql_dev
  url: postgres://localhost/test
  connect_timeout: 5
  max_conns: 10`,
		},
		{
			name: "missing maximum connections",
			contents: `database:
  user: graphql
  password: graphql_dev
  url: postgres://localhost/test
  connect_timeout: 5s
  min_conns: 0`,
		},
		{
			name: "zero maximum connections",
			contents: `database:
  user: graphql
  password: graphql_dev
  url: postgres://localhost/test
  connect_timeout: 5s
  max_conns: 0`,
		},
		{
			name: "negative maximum connections",
			contents: `database:
  user: graphql
  password: graphql_dev
  url: postgres://localhost/test
  connect_timeout: 5s
  max_conns: -1`,
		},
		{
			name: "maximum connections overflow",
			contents: `database:
  user: graphql
  password: graphql_dev
  url: postgres://localhost/test
  connect_timeout: 5s
  max_conns: 2147483648`,
		},
		{
			name: "negative minimum connections",
			contents: `database:
  url: postgres://localhost/test
  user: graphql
  password: graphql_dev
  connect_timeout: 5s
  max_conns: 10
  min_conns: -1
`,
		},
		{
			name: "minimum exceeds maximum",
			contents: `database:
  url: postgres://localhost/test
  user: graphql
  password: graphql_dev
  connect_timeout: 5s
  max_conns: 10
  min_conns: 11
`,
		},
		{
			name: "secrets are absent from YAML errors",
			contents: `database:
  url: postgres://localhost/test
  user: graphql
  password: sensitive-password
  connect_timeout: sensitive-password
  max_conns: 10
  min_conns: 0
`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(test.contents), 0o600))

			app, err := config.Load(path)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "sensitive-password")
			require.Nil(t, app)
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	t.Parallel()

	app, err := config.Load(filepath.Join(t.TempDir(), "missing.yaml"))
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Nil(t, app)
}

func TestLoadReturnsIndependentConfigurations(t *testing.T) {
	t.Parallel()

	const (
		firstContents = `database:
  url: postgres://localhost/first
  user: graphql
  password: graphql_dev
  connect_timeout: 5s
  max_conns: 10
  min_conns: 0
`
		secondContents = `database:
  url: postgres://localhost/second
  user: graphql
  password: graphql_dev
  connect_timeout: 5s
  max_conns: 10
  min_conns: 0
`
	)

	firstPath := filepath.Join(t.TempDir(), "first.yaml")
	secondPath := filepath.Join(t.TempDir(), "second.yaml")
	require.NoError(t, os.WriteFile(firstPath, []byte(firstContents), 0o600))
	require.NoError(t, os.WriteFile(secondPath, []byte(secondContents), 0o600))

	first, err := config.Load(firstPath)
	require.NoError(t, err)

	second, err := config.Load(secondPath)
	require.NoError(t, err)
	require.NotSame(t, first, second)
	require.Equal(t, "postgres://localhost/first", first.Database.URL)
	require.Equal(t, "postgres://localhost/second", second.Database.URL)

	second.Database.MaxConns = 1
	require.EqualValues(t, 10, first.Database.MaxConns)

	second.Logger.Cores[0].Level = "fatal"
	require.Equal(t, "info", first.Logger.Cores[0].Level)
}

func TestLoadHTTPConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		contents string
		want     config.HTTP
	}{
		{
			name: "omitted section uses defaults",
			want: config.HTTP{
				Addr:              "0.0.0.0:8080",
				RequestTimeout:    10 * time.Second,
				ReadHeaderTimeout: 5 * time.Second,
				ReadTimeout:       10 * time.Second,
				WriteTimeout:      15 * time.Second,
				IdleTimeout:       time.Minute,
				ShutdownTimeout:   15 * time.Second,
			},
		},
		{
			name:     "omitted fields keep defaults",
			contents: "http: {addr: '127.0.0.1:8081', request_timeout: 3s}",
			want: config.HTTP{
				Addr:              "127.0.0.1:8081",
				RequestTimeout:    3 * time.Second,
				ReadHeaderTimeout: 5 * time.Second,
				ReadTimeout:       10 * time.Second,
				WriteTimeout:      15 * time.Second,
				IdleTimeout:       time.Minute,
				ShutdownTimeout:   15 * time.Second,
			},
		},
		{
			name: "configured fields replace defaults",
			contents: `http:
  addr: localhost:9000
  request_timeout: 2s
  read_header_timeout: 1s
  read_timeout: 3s
  write_timeout: 4s
  idle_timeout: 30s
  shutdown_timeout: 5s`,
			want: config.HTTP{
				Addr:              "localhost:9000",
				RequestTimeout:    2 * time.Second,
				ReadHeaderTimeout: time.Second,
				ReadTimeout:       3 * time.Second,
				WriteTimeout:      4 * time.Second,
				IdleTimeout:       30 * time.Second,
				ShutdownTimeout:   5 * time.Second,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "config.yaml")
			contents := databaseYAML + test.contents
			require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

			app, err := config.Load(path)
			require.NoError(t, err)
			require.Equal(t, test.want, app.HTTP)
		})
	}
}

func TestLoadRejectsInvalidHTTPConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		contents string
	}{
		{name: "empty address", contents: "addr: ''"},
		{name: "missing port", contents: "addr: localhost"},
		{name: "invalid port", contents: "addr: localhost:70000"},
		{name: "zero request timeout", contents: "request_timeout: 0s"},
		{name: "zero header timeout", contents: "read_header_timeout: 0s"},
		{name: "zero read timeout", contents: "read_timeout: 0s"},
		{name: "zero write timeout", contents: "write_timeout: 0s"},
		{name: "zero idle timeout", contents: "idle_timeout: 0s"},
		{name: "zero shutdown timeout", contents: "shutdown_timeout: 0s"},
		{name: "negative timeout", contents: "request_timeout: -1s"},
		{name: "invalid timeout", contents: "request_timeout: immediately"},
		{name: "unknown field", contents: "timeout: 5s"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "config.yaml")
			contents := databaseYAML + "http: {" + test.contents + "}"
			require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

			app, err := config.Load(path)
			require.Error(t, err)
			require.Nil(t, app)
		})
	}
}

func TestLoadLoggerConfiguration(t *testing.T) {
	t.Parallel()

	var defaultLogger = config.Logger{Cores: []config.LoggerCore{{
		Level:      "info",
		Encoding:   "json",
		Output:     "stdout",
		TimeFormat: "utc",
	}}}

	tests := []struct {
		name     string
		contents string
		want     config.Logger
	}{
		{
			name: "omitted section uses defaults",
			want: defaultLogger,
		},
		{
			name:     "empty section keeps defaults",
			contents: "logger: {}",
			want:     defaultLogger,
		},
		{
			name:     "null section keeps defaults",
			contents: "logger: null",
			want:     defaultLogger,
		},
		{
			name: "configured core replaces default",
			contents: `logger:
  cores:
    - level: debug
      encoding: console
      output: stderr
      time_format: local`,
			want: config.Logger{Cores: []config.LoggerCore{{
				Level:      "debug",
				Encoding:   "console",
				Output:     "stderr",
				TimeFormat: "local",
			}}},
		},
		{
			name: "independent console and file cores",
			contents: `logger:
  cores:
    - level: warn
      encoding: console
      output: stdout
      time_format: local
    - level: error
      encoding: json
      output: file
      path: logs/api.log
      time_format: utc`,
			want: config.Logger{Cores: []config.LoggerCore{
				{
					Level:      "warn",
					Encoding:   "console",
					Output:     "stdout",
					TimeFormat: "local",
				},
				{
					Level:      "error",
					Encoding:   "json",
					Output:     "file",
					Path:       "logs/api.log",
					TimeFormat: "utc",
				},
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "config.yaml")
			contents := databaseYAML + test.contents
			require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

			app, err := config.Load(path)
			require.NoError(t, err)
			require.Equal(t, test.want, app.Logger)
		})
	}
}

func TestLoadLoggerLevels(t *testing.T) {
	t.Parallel()

	for _, level := range []string{"debug", "info", "warn", "error", "dpanic", "panic", "fatal"} {
		t.Run(level, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "config.yaml")
			contents := databaseYAML + "logger:\n  cores:\n    - {level: " + level +
				", encoding: json, output: stdout, time_format: utc}"
			require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

			app, err := config.Load(path)
			require.NoError(t, err)
			require.Len(t, app.Logger.Cores, 1)
			require.Equal(t, level, app.Logger.Cores[0].Level)
		})
	}
}

func TestLoadRejectsInvalidLoggerConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		contents string
	}{
		{name: "empty cores", contents: "cores: []"},
		{name: "null cores", contents: "cores: null"},
		{name: "empty core", contents: "cores: [{}]"},
		{name: "null core", contents: "cores: [null]"},
		{name: "flat logger configuration", contents: "level: info"},
		{name: "unknown field", contents: "format: json"},
		{
			name: "empty level",
			contents: `cores:
    - {level: '', encoding: json, output: stdout, time_format: utc}`,
		},
		{
			name: "unknown level",
			contents: `cores:
    - {level: trace, encoding: json, output: stdout, time_format: utc}`,
		},
		{
			name: "uppercase level",
			contents: `cores:
    - {level: INFO, encoding: json, output: stdout, time_format: utc}`,
		},
		{
			name: "numeric level",
			contents: `cores:
    - {level: 1, encoding: json, output: stdout, time_format: utc}`,
		},
		{
			name: "empty encoding",
			contents: `cores:
    - {level: info, encoding: '', output: stdout, time_format: utc}`,
		},
		{
			name: "unknown encoding",
			contents: `cores:
    - {level: info, encoding: text, output: stdout, time_format: utc}`,
		},
		{
			name: "uppercase encoding",
			contents: `cores:
    - {level: info, encoding: JSON, output: stdout, time_format: utc}`,
		},
		{
			name: "unknown output",
			contents: `cores:
    - {level: info, encoding: json, output: network, time_format: utc}`,
		},
		{
			name: "missing output",
			contents: `cores:
    - {level: info, encoding: json, time_format: utc}`,
		},
		{
			name: "file without path",
			contents: `cores:
    - {level: info, encoding: json, output: file, time_format: utc}`,
		},
		{
			name: "file with empty path",
			contents: `cores:
    - {level: info, encoding: json, output: file, path: '', time_format: utc}`,
		},
		{
			name: "unknown time format",
			contents: `cores:
    - {level: info, encoding: json, output: stdout, time_format: unix}`,
		},
		{
			name: "missing time format",
			contents: `cores:
    - {level: info, encoding: json, output: stdout}`,
		},
		{
			name: "invalid second core",
			contents: `cores:
    - {level: info, encoding: json, output: stdout, time_format: utc}
    - {level: error, encoding: json, output: file, time_format: utc}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "config.yaml")
			contents := databaseYAML + "logger:\n  " + test.contents
			require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

			app, err := config.Load(path)
			require.Error(t, err)
			require.Nil(t, app)
		})
	}
}
