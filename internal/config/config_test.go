package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/config"
)

func TestLoadFromYAML(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		want     config.Database
	}{
		{
			name: "zero minimum connections",
			contents: `database:
  url: postgres://localhost/first
  connect_timeout: 5s
  max_conns: 10
  min_conns: 0
`,
			want: config.Database{
				URL:            "postgres://localhost/first",
				ConnectTimeout: 5 * time.Second,
				MaxConns:       10,
				MinConns:       0,
			},
		},
		{
			name: "equal connection limits",
			contents: `database:
  url: postgres://localhost/second
  connect_timeout: 250ms
  max_conns: 3
  min_conns: 3
`,
			want: config.Database{
				URL:            "postgres://localhost/second",
				ConnectTimeout: 250 * time.Millisecond,
				MaxConns:       3,
				MinConns:       3,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "")
			require.NoError(t, os.Unsetenv("DATABASE_URL"))

			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(test.contents), 0o600))

			app, err := config.Load(path)
			require.NoError(t, err)
			require.Equal(t, test.want, app.Database)
		})
	}
}

func TestLoadDatabaseURLFromEnvironment(t *testing.T) {
	tests := []struct {
		name     string
		contents string
	}{
		{
			name: "override YAML URL",
			contents: `database:
  url: postgres://localhost/file
  connect_timeout: 5s
  max_conns: 10
  min_conns: 2
`,
		},
		{
			name: "supply missing YAML URL",
			contents: `database:
  connect_timeout: 5s
  max_conns: 10
  min_conns: 2
`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://localhost/environment")

			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(test.contents), 0o600))

			app, err := config.Load(path)
			require.NoError(t, err)
			require.Equal(t, config.Database{
				URL:            "postgres://localhost/environment",
				ConnectTimeout: 5 * time.Second,
				MaxConns:       10,
				MinConns:       2,
			}, app.Database)
		})
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		contents string
	}{
		{name: "empty document", contents: ""},
		{name: "missing database", contents: "{}"},
		{
			name: "unknown section",
			contents: `database: {url: postgres://localhost/test, connect_timeout: 5s, max_conns: 10}
databaze: {}`,
		},
		{
			name: "unknown database field",
			contents: `database:
  url: postgres://localhost/test
  connect_timeout: 5s
  max_conns: 10
  pool_size: 10`,
		},
		{name: "invalid YAML", contents: "database: ["},
		{
			name: "multiple documents",
			contents: `database: {url: postgres://localhost/test, connect_timeout: 5s, max_conns: 10}
---
database: {}`,
		},
		{
			name: "trailing empty document",
			contents: `database: {url: postgres://localhost/test, connect_timeout: 5s, max_conns: 10}
---`,
		},
		{
			name: "malformed trailing document",
			contents: `database: {url: postgres://localhost/test, connect_timeout: 5s, max_conns: 10}
---
[`,
		},
		{
			name: "duplicate field",
			contents: `database:
  url: postgres://localhost/test
  connect_timeout: 5s
  max_conns: 10
  max_conns: 20`,
		},
		{
			name:     "empty URL",
			contents: "database: {url: '', connect_timeout: 5s, max_conns: 10, min_conns: 0}",
		},
		{
			name:     "null URL",
			contents: "database: {url: null, connect_timeout: 5s, max_conns: 10, min_conns: 0}",
		},
		{
			name:     "missing timeout",
			contents: "database: {url: postgres://localhost/test, max_conns: 10, min_conns: 0}",
		},
		{
			name:     "zero timeout",
			contents: "database: {url: postgres://localhost/test, connect_timeout: 0s, max_conns: 10}",
		},
		{
			name:     "negative timeout",
			contents: "database: {url: postgres://localhost/test, connect_timeout: -1s, max_conns: 10}",
		},
		{
			name:     "invalid timeout",
			contents: "database: {url: postgres://localhost/test, connect_timeout: immediately, max_conns: 10}",
		},
		{
			name:     "numeric timeout",
			contents: "database: {url: postgres://localhost/test, connect_timeout: 5, max_conns: 10}",
		},
		{
			name:     "missing maximum connections",
			contents: "database: {url: postgres://localhost/test, connect_timeout: 5s, min_conns: 0}",
		},
		{
			name:     "zero maximum connections",
			contents: "database: {url: postgres://localhost/test, connect_timeout: 5s, max_conns: 0}",
		},
		{
			name:     "negative maximum connections",
			contents: "database: {url: postgres://localhost/test, connect_timeout: 5s, max_conns: -1}",
		},
		{
			name:     "maximum connections overflow",
			contents: "database: {url: postgres://localhost/test, connect_timeout: 5s, max_conns: 2147483648}",
		},
		{
			name: "negative minimum connections",
			contents: `database:
  url: postgres://localhost/test
  connect_timeout: 5s
  max_conns: 10
  min_conns: -1
`,
		},
		{
			name: "minimum exceeds maximum",
			contents: `database:
  url: postgres://localhost/test
  connect_timeout: 5s
  max_conns: 10
  min_conns: 11
`,
		},
		{
			name: "secrets are absent from YAML errors",
			contents: `database:
  url: postgres://user:sensitive-password@localhost/test
  connect_timeout: sensitive-password
  max_conns: 10
  min_conns: 0
`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "")
			require.NoError(t, os.Unsetenv("DATABASE_URL"))

			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(test.contents), 0o600))

			app, err := config.Load(path)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "sensitive-password")
			require.Nil(t, app)
		})
	}
}

func TestLoadRejectsEmptyEnvironmentURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	var contents = `database:
  url: postgres://localhost/file
  connect_timeout: 5s
  max_conns: 10
  min_conns: 0
`

	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

	app, err := config.Load(path)
	require.Error(t, err)
	require.Nil(t, app)
}

func TestLoadMissingFile(t *testing.T) {
	t.Parallel()

	app, err := config.Load(filepath.Join(t.TempDir(), "missing.yaml"))
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Nil(t, app)
}

func TestLoadReturnsIndependentConfigurations(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/first")

	var contents = `database:
  connect_timeout: 5s
  max_conns: 10
  min_conns: 0
`

	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

	first, err := config.Load(path)
	require.NoError(t, err)

	t.Setenv("DATABASE_URL", "postgres://localhost/second")

	second, err := config.Load(path)
	require.NoError(t, err)
	require.NotSame(t, first, second)
	require.Equal(t, "postgres://localhost/first", first.Database.URL)
	require.Equal(t, "postgres://localhost/second", second.Database.URL)

	second.Database.MaxConns = 1
	require.EqualValues(t, 10, first.Database.MaxConns)
}

func TestLoadHTTPConfiguration(t *testing.T) {
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
			t.Setenv("DATABASE_URL", "postgres://localhost/test")

			path := filepath.Join(t.TempDir(), "config.yaml")
			contents := "database: {connect_timeout: 5s, max_conns: 10}\n" + test.contents
			require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

			app, err := config.Load(path)
			require.NoError(t, err)
			require.Equal(t, test.want, app.HTTP)
		})
	}
}

func TestLoadRejectsInvalidHTTPConfiguration(t *testing.T) {
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
			t.Setenv("DATABASE_URL", "postgres://localhost/test")

			path := filepath.Join(t.TempDir(), "config.yaml")
			contents := "database: {connect_timeout: 5s, max_conns: 10}\nhttp: {" + test.contents + "}"
			require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

			app, err := config.Load(path)
			require.Error(t, err)
			require.Nil(t, app)
		})
	}
}
