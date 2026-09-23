package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/config"
)

const unchangedURL = "postgres://localhost/unchanged"

func TestLoadFromYAML(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	require.NoError(t, os.Unsetenv("DATABASE_URL"))

	previous := config.App

	t.Cleanup(func() { config.App = previous })

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
			require.NoError(t, config.Load(path))
			require.Equal(t, test.want, config.App.Database)
		})
	}
}

func TestLoadDatabaseURLFromEnvironment(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/environment")

	previous := config.App

	t.Cleanup(func() { config.App = previous })

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
			require.NoError(t, config.Load(path))
			require.Equal(t, config.Database{
				URL:            "postgres://localhost/environment",
				ConnectTimeout: 5 * time.Second,
				MaxConns:       10,
				MinConns:       2,
			}, config.App.Database)
		})
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	require.NoError(t, os.Unsetenv("DATABASE_URL"))

	previous := config.App

	t.Cleanup(func() { config.App = previous })

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

			config.App = config.Application{Database: config.Database{
				URL:            unchangedURL,
				ConnectTimeout: time.Second,
				MaxConns:       2,
				MinConns:       1,
			}}

			before := config.App
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(test.contents), 0o600))

			err := config.Load(path)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "sensitive-password")
			require.Equal(t, before, config.App)
		})
	}
}

func TestLoadRejectsEmptyEnvironmentURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	previous := config.App

	t.Cleanup(func() { config.App = previous })

	config.App = config.Application{Database: config.Database{URL: unchangedURL}}

	var (
		before   = config.App
		contents = `database:
  url: postgres://localhost/file
  connect_timeout: 5s
  max_conns: 10
  min_conns: 0
`
	)

	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	require.Error(t, config.Load(path))
	require.Equal(t, before, config.App)
}

func TestLoadMissingFile(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/environment")

	previous := config.App

	t.Cleanup(func() { config.App = previous })

	config.App = config.Application{Database: config.Database{URL: unchangedURL}}

	before := config.App
	err := config.Load(filepath.Join(t.TempDir(), "missing.yaml"))
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Equal(t, before, config.App)
}
