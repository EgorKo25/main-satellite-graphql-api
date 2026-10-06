package config_test

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"testing"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/go-cmp/cmp"
	"github.com/samber/lo"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/config"
)

func TestLoadFromYAML(t *testing.T) {
	t.Parallel()

	const (
		databaseUser     = "graphql"
		databasePassword = "graphql_dev"
	)

	tests := []struct {
		name     string
		contents string
		want     config.Database
	}{
		{
			name: "zero minimum connections",
			contents: `
            database:
              url: postgres://localhost/graphql
              user: graphql
              password: graphql_dev
              connect_timeout: 5s
              max_conns: 10
              min_conns: 0
            `,
			want: config.Database{
				URL:            "postgres://localhost/graphql",
				User:           databaseUser,
				Password:       databasePassword,
				ConnectTimeout: 5 * time.Second,
				MaxConns:       10,
				MinConns:       0,
			},
		},
		{
			name: "equal connection limits",
			contents: `
            database:
              url: postgres://localhost/graphql
              user: graphql
              password: graphql_dev
              connect_timeout: 250ms
              max_conns: 3
              min_conns: 3
            `,
			want: config.Database{
				URL:            "postgres://localhost/graphql",
				User:           databaseUser,
				Password:       databasePassword,
				ConnectTimeout: 250 * time.Millisecond,
				MaxConns:       3,
				MinConns:       3,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			filesystem := afero.NewMemMapFs()
			require.NoError(t, afero.WriteFile(filesystem, "config.yaml", []byte(test.contents), 0o600))

			app, err := config.Load(filesystem, "config.yaml")
			require.NoError(t, err)
			require.Empty(t, cmp.Diff(test.want, app.Database))
		})
	}
}

func TestLoadIgnoresDatabaseURLFromEnvironment(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://ignored:ignored@localhost/environment")

	var contents = `
            database:
              url: postgres://localhost/file
              user: graphql
              password: graphql_dev
              connect_timeout: 5s
              max_conns: 10
              min_conns: 2
            `

	filesystem := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(filesystem, "config.yaml", []byte(contents), 0o600))

	app, err := config.Load(filesystem, "config.yaml")
	require.NoError(t, err)
	require.Empty(t, cmp.Diff(config.Database{
		URL:            "postgres://localhost/file",
		User:           "graphql",
		Password:       "graphql_dev",
		ConnectTimeout: 5 * time.Second,
		MaxConns:       10,
		MinConns:       2,
	}, app.Database))
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	const (
		urlField      = "App.Database.URL"
		userField     = "App.Database.User"
		passwordField = "App.Database.Password"
		timeoutField  = "App.Database.ConnectTimeout"
		maxConnsField = "App.Database.MaxConns"
		minConnsField = "App.Database.MinConns"
		requiredTag   = "required"
	)

	tests := []struct {
		name           string
		contents       string
		wantValidation map[string]string
	}{
		{name: "empty document", contents: ""},
		{name: "top-level sequence", contents: "[database]"},
		{name: "top-level scalar", contents: "database"},
		{
			name: "missing database",
			wantValidation: map[string]string{
				urlField:      requiredTag,
				userField:     requiredTag,
				passwordField: requiredTag,
				timeoutField:  "gt",
				maxConnsField: "gt",
			},
			contents: "{}",
		},
		{
			name: "unknown section",
			contents: `
            database:
              user: graphql
              password: graphql_dev
              url: postgres://localhost/test
              connect_timeout: 5s
              max_conns: 10
            databaze: {}
            `,
		},
		{
			name: "unknown database field",
			contents: `
            database:
              url: postgres://localhost/test
              user: graphql
              password: graphql_dev
              connect_timeout: 5s
              max_conns: 10
              pool_size: 10
            `,
		},
		{name: "invalid YAML", contents: "database: ["},
		{
			name: "duplicate field",
			contents: `
            database:
              url: postgres://localhost/test
              user: graphql
              password: graphql_dev
              connect_timeout: 5s
              max_conns: 10
              max_conns: 20
            `,
		},
		{
			name: "missing URL", wantValidation: map[string]string{urlField: requiredTag},
			contents: `
            database:
              user: graphql
              password: graphql_dev
              connect_timeout: 5s
              max_conns: 10
            `,
		},
		{
			name: "empty URL", wantValidation: map[string]string{urlField: requiredTag},
			contents: `
            database:
              user: graphql
              password: graphql_dev
              url: ''
              connect_timeout: 5s
              max_conns: 10
              min_conns: 0
            `,
		},
		{
			name: "null URL", wantValidation: map[string]string{urlField: requiredTag},
			contents: `
            database:
              user: graphql
              password: graphql_dev
              url: null
              connect_timeout: 5s
              max_conns: 10
              min_conns: 0
            `,
		},
		{
			name: "missing user", wantValidation: map[string]string{userField: requiredTag},
			contents: `
            database:
              url: postgres://localhost/test
              password: graphql_dev
              connect_timeout: 5s
              max_conns: 10
            `,
		},
		{
			name: "empty user", wantValidation: map[string]string{userField: requiredTag},
			contents: `
            database:
              url: postgres://localhost/test
              user: ''
              password: graphql_dev
              connect_timeout: 5s
              max_conns: 10
            `,
		},
		{
			name: "null user", wantValidation: map[string]string{userField: requiredTag},
			contents: `
            database:
              url: postgres://localhost/test
              user: null
              password: graphql_dev
              connect_timeout: 5s
              max_conns: 10
            `,
		},
		{
			name: "missing password", wantValidation: map[string]string{passwordField: requiredTag},
			contents: `
            database:
              url: postgres://localhost/test
              user: graphql
              connect_timeout: 5s
              max_conns: 10
            `,
		},
		{
			name: "empty password", wantValidation: map[string]string{passwordField: requiredTag},
			contents: `
            database:
              url: postgres://localhost/test
              user: graphql
              password: ''
              connect_timeout: 5s
              max_conns: 10
            `,
		},
		{
			name: "null password", wantValidation: map[string]string{passwordField: requiredTag},
			contents: `
            database:
              url: postgres://localhost/test
              user: graphql
              password: null
              connect_timeout: 5s
              max_conns: 10
            `,
		},
		{
			name: "missing timeout", wantValidation: map[string]string{timeoutField: "gt"},
			contents: `
            database:
              user: graphql
              password: graphql_dev
              url: postgres://localhost/test
              max_conns: 10
              min_conns: 0
            `,
		},
		{
			name: "zero timeout", wantValidation: map[string]string{timeoutField: "gt"},
			contents: `
            database:
              user: graphql
              password: graphql_dev
              url: postgres://localhost/test
              connect_timeout: 0s
              max_conns: 10
            `,
		},
		{
			name: "negative timeout", wantValidation: map[string]string{timeoutField: "gt"},
			contents: `
            database:
              user: graphql
              password: graphql_dev
              url: postgres://localhost/test
              connect_timeout: -1s
              max_conns: 10
            `,
		},
		{
			name: "invalid timeout",
			contents: `
            database:
              user: graphql
              password: graphql_dev
              url: postgres://localhost/test
              connect_timeout: immediately
              max_conns: 10
            `,
		},
		{
			name: "numeric timeout",
			contents: `
            database:
              user: graphql
              password: graphql_dev
              url: postgres://localhost/test
              connect_timeout: 5
              max_conns: 10
            `,
		},
		{
			name: "missing maximum connections", wantValidation: map[string]string{maxConnsField: "gt"},
			contents: `
            database:
              user: graphql
              password: graphql_dev
              url: postgres://localhost/test
              connect_timeout: 5s
              min_conns: 0
            `,
		},
		{
			name: "zero maximum connections", wantValidation: map[string]string{maxConnsField: "gt"},
			contents: `
            database:
              user: graphql
              password: graphql_dev
              url: postgres://localhost/test
              connect_timeout: 5s
              max_conns: 0
            `,
		},
		{
			name:           "negative maximum connections",
			wantValidation: map[string]string{maxConnsField: "gt", minConnsField: "ltefield"},
			contents: `
            database:
              user: graphql
              password: graphql_dev
              url: postgres://localhost/test
              connect_timeout: 5s
              max_conns: -1
            `,
		},
		{
			name: "maximum connections overflow",
			contents: `
            database:
              user: graphql
              password: graphql_dev
              url: postgres://localhost/test
              connect_timeout: 5s
              max_conns: 2147483648
            `,
		},
		{
			name: "negative minimum connections", wantValidation: map[string]string{minConnsField: "gte"},
			contents: `
            database:
              url: postgres://localhost/test
              user: graphql
              password: graphql_dev
              connect_timeout: 5s
              max_conns: 10
              min_conns: -1
            `,
		},
		{
			name: "minimum exceeds maximum", wantValidation: map[string]string{minConnsField: "ltefield"},
			contents: `
            database:
              url: postgres://localhost/test
              user: graphql
              password: graphql_dev
              connect_timeout: 5s
              max_conns: 10
              min_conns: 11
            `,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			filesystem := afero.NewMemMapFs()
			require.NoError(t, afero.WriteFile(filesystem, "config.yaml", []byte(test.contents), 0o600))

			app, err := config.Load(filesystem, "config.yaml")
			require.Error(t, err)
			require.Nil(t, app)

			if test.wantValidation == nil {
				require.ErrorContains(t, err, "decode configuration:")

				return
			}

			var validationErrors validator.ValidationErrors

			require.ErrorAs(t, err, &validationErrors)

			got := lo.Associate(validationErrors, func(failure validator.FieldError) (string, string) {
				return failure.Namespace(), failure.Tag()
			})

			require.Equal(t, test.wantValidation, got)
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	t.Parallel()

	filesystem := afero.NewMemMapFs()

	app, err := config.Load(filesystem, "missing.yaml")
	require.ErrorIs(t, err, fs.ErrNotExist)
	require.Nil(t, app)
}

func TestLoadPropagatesFileErrors(t *testing.T) {
	t.Parallel()

	readFailure := errors.New("read interrupted")

	tests := []struct {
		name    string
		openErr error
		readErr error
		wantErr error
	}{
		{name: "open permission denied", openErr: fs.ErrPermission, wantErr: fs.ErrPermission},
		{name: "read failure closes file", readErr: readFailure, wantErr: readFailure},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			filesystem := &readErrorFS{Fs: afero.NewMemMapFs(), openErr: test.openErr, readErr: test.readErr}
			require.NoError(t, afero.WriteFile(filesystem.Fs, "config.yaml", []byte("configuration contents"), 0o600))

			app, err := config.Load(filesystem, "config.yaml")
			require.ErrorIs(t, err, test.wantErr)
			require.ErrorContains(t, err, "read configuration:")
			require.Nil(t, app)

			if test.openErr != nil {
				var pathError *fs.PathError

				require.ErrorAs(t, err, &pathError)
				require.Equal(t, "config.yaml", pathError.Path)
				require.Nil(t, filesystem.opened)

				return
			}

			require.NotNil(t, filesystem.opened)
			require.True(t, filesystem.opened.closed)
		})
	}
}

func TestLoadSanitizesDecodeErrors(t *testing.T) {
	t.Parallel()

	const contents = `
            database:
              url: postgres://localhost/test
              user: graphql
              password: sensitive-password
              connect_timeout: sensitive-password
              max_conns: 10
            `

	filesystem := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(filesystem, "config.yaml", []byte(contents), 0o600))

	app, err := config.Load(filesystem, "config.yaml")
	require.ErrorContains(t, err, "decode configuration:")
	require.NotContains(t, err.Error(), "sensitive-password")
	require.Nil(t, app)
}

func TestLoadHTTPConfiguration(t *testing.T) {
	t.Parallel()

	const databaseYAML = `
            database:
              url: postgres://localhost/test
              user: graphql
              password: graphql_dev
              connect_timeout: 5s
              max_conns: 10
            `

	var defaultHTTP = config.HTTP{
		Addr:              "0.0.0.0:8080",
		RequestTimeout:    10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       time.Minute,
		ShutdownTimeout:   15 * time.Second,
	}

	tests := []struct {
		name     string
		contents string
		want     config.HTTP
	}{
		{
			name: "omitted section uses defaults",
			want: defaultHTTP,
		},
		{
			name: "empty section keeps defaults",
			contents: `
            http: {}
            `,
			want: defaultHTTP,
		},
		{
			name: "null section keeps defaults",
			contents: `
            http: null
            `,
			want: defaultHTTP,
		},
		{
			name: "omitted fields keep defaults",
			contents: `
            http: {addr: '127.0.0.1:8081', request_timeout: 3s}
            `,
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
			contents: `
            http:
              addr: localhost:9000
              request_timeout: 2s
              read_header_timeout: 1s
              read_timeout: 3s
              write_timeout: 4s
              idle_timeout: 30s
              shutdown_timeout: 5s
            `,
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

			filesystem := afero.NewMemMapFs()
			contents := databaseYAML + test.contents
			require.NoError(t, afero.WriteFile(filesystem, "config.yaml", []byte(contents), 0o600))

			app, err := config.Load(filesystem, "config.yaml")
			require.NoError(t, err)
			require.Empty(t, cmp.Diff(test.want, app.HTTP))
		})
	}
}

func TestLoadRejectsInvalidHTTPConfiguration(t *testing.T) {
	t.Parallel()

	const (
		addrField    = "App.HTTP.Addr"
		addressTag   = "hostname_port"
		databaseYAML = `
            database:
              url: postgres://localhost/test
              user: graphql
              password: graphql_dev
              connect_timeout: 5s
              max_conns: 10
            `
	)

	tests := []struct {
		name           string
		contents       string
		wantValidation map[string]string
	}{
		{name: "empty address", wantValidation: map[string]string{addrField: addressTag}, contents: "addr: ''"},
		{name: "missing port", wantValidation: map[string]string{addrField: addressTag}, contents: "addr: localhost"},
		{
			name:           "invalid port",
			wantValidation: map[string]string{addrField: addressTag},
			contents:       "addr: localhost:70000",
		},
		{
			name:           "zero request timeout",
			wantValidation: map[string]string{"App.HTTP.RequestTimeout": "gt"},
			contents:       "request_timeout: 0s",
		},
		{
			name:           "zero header timeout",
			wantValidation: map[string]string{"App.HTTP.ReadHeaderTimeout": "gt"},
			contents:       "read_header_timeout: 0s",
		},
		{
			name:           "zero read timeout",
			wantValidation: map[string]string{"App.HTTP.ReadTimeout": "gt"},
			contents:       "read_timeout: 0s",
		},
		{
			name:           "zero write timeout",
			wantValidation: map[string]string{"App.HTTP.WriteTimeout": "gt"},
			contents:       "write_timeout: 0s",
		},
		{
			name:           "zero idle timeout",
			wantValidation: map[string]string{"App.HTTP.IdleTimeout": "gt"},
			contents:       "idle_timeout: 0s",
		},
		{
			name:           "zero shutdown timeout",
			wantValidation: map[string]string{"App.HTTP.ShutdownTimeout": "gt"},
			contents:       "shutdown_timeout: 0s",
		},
		{
			name:           "negative timeout",
			wantValidation: map[string]string{"App.HTTP.RequestTimeout": "gt"},
			contents:       "request_timeout: -1s",
		},
		{name: "invalid timeout", contents: "request_timeout: immediately"},
		{name: "unknown field", contents: "timeout: 5s"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			filesystem := afero.NewMemMapFs()
			contents := databaseYAML + fmt.Sprintf(`
            http: {%s}
            `, test.contents)
			require.NoError(t, afero.WriteFile(filesystem, "config.yaml", []byte(contents), 0o600))

			app, err := config.Load(filesystem, "config.yaml")
			require.Error(t, err)
			require.Nil(t, app)

			if test.wantValidation == nil {
				require.ErrorContains(t, err, "decode configuration:")

				return
			}

			var validationErrors validator.ValidationErrors

			require.ErrorAs(t, err, &validationErrors)

			got := lo.Associate(validationErrors, func(failure validator.FieldError) (string, string) {
				return failure.Namespace(), failure.Tag()
			})

			require.Equal(t, test.wantValidation, got)
		})
	}
}

func TestLoadLoggerConfiguration(t *testing.T) {
	t.Parallel()

	const databaseYAML = `
            database:
              url: postgres://localhost/test
              user: graphql
              password: graphql_dev
              connect_timeout: 5s
              max_conns: 10
            `

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
			name: "empty section keeps defaults",
			contents: `
            logger: {}
            `,
			want: defaultLogger,
		},
		{
			name: "null section keeps defaults",
			contents: `
            logger: null
            `,
			want: defaultLogger,
		},
		{
			name: "configured core replaces default",
			contents: `
            logger:
              cores:
                - level: debug
                  encoding: console
                  output: stderr
                  time_format: local
            `,
			want: config.Logger{Cores: []config.LoggerCore{{
				Level:      "debug",
				Encoding:   "console",
				Output:     "stderr",
				TimeFormat: "local",
			}}},
		},
		{
			name: "independent console and file cores",
			contents: `
            logger:
              cores:
                - level: warn
                  encoding: console
                  output: stdout
                  time_format: local
                - level: error
                  encoding: json
                  output: file
                  path: logs/api.log
                  time_format: utc
            `,
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

			filesystem := afero.NewMemMapFs()
			contents := databaseYAML + test.contents
			require.NoError(t, afero.WriteFile(filesystem, "config.yaml", []byte(contents), 0o600))

			app, err := config.Load(filesystem, "config.yaml")
			require.NoError(t, err)
			require.Empty(t, cmp.Diff(test.want, app.Logger))
		})
	}
}

func TestLoadLoggerLevels(t *testing.T) {
	t.Parallel()

	const databaseYAML = `
            database:
              url: postgres://localhost/test
              user: graphql
              password: graphql_dev
              connect_timeout: 5s
              max_conns: 10
            `

	for _, level := range []string{"debug", "info", "warn", "error", "dpanic", "panic", "fatal"} {
		t.Run(level, func(t *testing.T) {
			t.Parallel()

			filesystem := afero.NewMemMapFs()
			contents := databaseYAML + fmt.Sprintf(`
            logger:
              cores:
                - {level: %s, encoding: json, output: stdout, time_format: utc}
            `, level)
			require.NoError(t, afero.WriteFile(filesystem, "config.yaml", []byte(contents), 0o600))

			app, err := config.Load(filesystem, "config.yaml")
			require.NoError(t, err)
			require.Len(t, app.Cores, 1)
			require.Equal(t, level, app.Cores[0].Level)
		})
	}
}

func TestDefaultLoggerCoresAreNotSharedBetweenLoads(t *testing.T) {
	t.Parallel()

	const contents = `
            database:
              url: postgres://localhost/test
              user: graphql
              password: graphql_dev
              connect_timeout: 5s
              max_conns: 10
            `

	filesystem := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(filesystem, "config.yaml", []byte(contents), 0o600))

	modified, err := config.Load(filesystem, "config.yaml")
	require.NoError(t, err)

	loaded, err := config.Load(filesystem, "config.yaml")
	require.NoError(t, err)

	require.Len(t, modified.Cores, 1)
	require.Len(t, loaded.Cores, 1)

	want := slices.Clone(loaded.Cores)
	modified.Cores[0].Level = "fatal"

	require.Empty(t, cmp.Diff(want, loaded.Cores))
}

func TestLoadRejectsInvalidLoggerConfiguration(t *testing.T) {
	t.Parallel()

	const (
		coresField      = "App.Logger.Cores"
		levelField      = "App.Logger.Cores[0].Level"
		encodingField   = "App.Logger.Cores[0].Encoding"
		outputField     = "App.Logger.Cores[0].Output"
		timeFormatField = "App.Logger.Cores[0].TimeFormat"
		pathField       = "App.Logger.Cores[0].Path"
		enumTag         = "oneof"
		filePathTag     = "required_if"
		databaseYAML    = `
            database:
              url: postgres://localhost/test
              user: graphql
              password: graphql_dev
              connect_timeout: 5s
              max_conns: 10
            `
	)

	tests := []struct {
		name           string
		contents       string
		wantValidation map[string]string
	}{
		{
			name: "empty cores", wantValidation: map[string]string{coresField: "min"},
			contents: `
            logger: {cores: []}
            `,
		},
		{
			name: "null cores", wantValidation: map[string]string{coresField: "required"},
			contents: `
            logger: {cores: null}
            `,
		},
		{
			name: "empty core",
			wantValidation: map[string]string{
				levelField:      enumTag,
				encodingField:   enumTag,
				outputField:     enumTag,
				timeFormatField: enumTag,
			},
			contents: `
            logger: {cores: [{}]}
            `,
		},
		{
			name: "null core", wantValidation: map[string]string{coresField: "min"},
			contents: `
            logger: {cores: [null]}
            `,
		},
		{
			name: "flat logger configuration",
			contents: `
            logger: {level: info}
            `,
		},
		{
			name: "unknown field",
			contents: `
            logger: {format: json}
            `,
		},
		{
			name: "unknown core field",
			contents: `
            logger:
              cores:
                - {level: info, encoding: json, output: stdout, time_format: utc, extra: true}
            `,
		},
		{
			name: "cores must be a sequence",
			contents: `
            logger: {cores: {level: info}}
            `,
		},
		{
			name: "empty level", wantValidation: map[string]string{levelField: enumTag},
			contents: `
            logger:
              cores:
                - {level: '', encoding: json, output: stdout, time_format: utc}
            `,
		},
		{
			name: "unknown level", wantValidation: map[string]string{levelField: enumTag},
			contents: `
            logger:
              cores:
                - {level: trace, encoding: json, output: stdout, time_format: utc}
            `,
		},
		{
			name: "uppercase level", wantValidation: map[string]string{levelField: enumTag},
			contents: `
            logger:
              cores:
                - {level: INFO, encoding: json, output: stdout, time_format: utc}
            `,
		},
		{
			name: "numeric level", wantValidation: map[string]string{levelField: enumTag},
			contents: `
            logger:
              cores:
                - {level: 1, encoding: json, output: stdout, time_format: utc}
            `,
		},
		{
			name: "empty encoding", wantValidation: map[string]string{encodingField: enumTag},
			contents: `
            logger:
              cores:
                - {level: info, encoding: '', output: stdout, time_format: utc}
            `,
		},
		{
			name: "unknown encoding", wantValidation: map[string]string{encodingField: enumTag},
			contents: `
            logger:
              cores:
                - {level: info, encoding: text, output: stdout, time_format: utc}
            `,
		},
		{
			name: "uppercase encoding", wantValidation: map[string]string{encodingField: enumTag},
			contents: `
            logger:
              cores:
                - {level: info, encoding: JSON, output: stdout, time_format: utc}
            `,
		},
		{
			name: "unknown output", wantValidation: map[string]string{outputField: enumTag},
			contents: `
            logger:
              cores:
                - {level: info, encoding: json, output: network, time_format: utc}
            `,
		},
		{
			name: "missing output", wantValidation: map[string]string{outputField: enumTag},
			contents: `
            logger:
              cores:
                - {level: info, encoding: json, time_format: utc}
            `,
		},
		{
			name: "file without path", wantValidation: map[string]string{pathField: filePathTag},
			contents: `
            logger:
              cores:
                - {level: info, encoding: json, output: file, time_format: utc}
            `,
		},
		{
			name: "file with empty path", wantValidation: map[string]string{pathField: filePathTag},
			contents: `
            logger:
              cores:
                - {level: info, encoding: json, output: file, path: '', time_format: utc}
            `,
		},
		{
			name: "unknown time format", wantValidation: map[string]string{timeFormatField: enumTag},
			contents: `
            logger:
              cores:
                - {level: info, encoding: json, output: stdout, time_format: unix}
            `,
		},
		{
			name: "missing time format", wantValidation: map[string]string{timeFormatField: enumTag},
			contents: `
            logger:
              cores:
                - {level: info, encoding: json, output: stdout}
            `,
		},
		{
			name: "invalid second core", wantValidation: map[string]string{"App.Logger.Cores[1].Path": filePathTag},
			contents: `
            logger:
              cores:
                - {level: info, encoding: json, output: stdout, time_format: utc}
                - {level: error, encoding: json, output: file, time_format: utc}
            `,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			filesystem := afero.NewMemMapFs()
			contents := databaseYAML + test.contents
			require.NoError(t, afero.WriteFile(filesystem, "config.yaml", []byte(contents), 0o600))

			app, err := config.Load(filesystem, "config.yaml")
			require.Error(t, err)
			require.Nil(t, app)

			if test.wantValidation == nil {
				require.ErrorContains(t, err, "decode configuration:")

				return
			}

			var validationErrors validator.ValidationErrors

			require.ErrorAs(t, err, &validationErrors)

			got := lo.Associate(validationErrors, func(failure validator.FieldError) (string, string) {
				return failure.Namespace(), failure.Tag()
			})

			require.Equal(t, test.wantValidation, got)
		})
	}
}

type readErrorFS struct {
	afero.Fs

	openErr error
	readErr error
	opened  *readErrorFile
}

func (filesystem *readErrorFS) Open(name string) (afero.File, error) {
	if filesystem.openErr != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: filesystem.openErr}
	}

	file, err := filesystem.Fs.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open test configuration: %w", err)
	}

	filesystem.opened = &readErrorFile{File: file, err: filesystem.readErr}

	return filesystem.opened, nil
}

type readErrorFile struct {
	afero.File

	err    error
	closed bool
}

func (file *readErrorFile) Read([]byte) (int, error) {
	return 0, file.err
}

func (file *readErrorFile) Close() error {
	file.closed = true

	if err := file.File.Close(); err != nil {
		return fmt.Errorf("close test configuration: %w", err)
	}

	return nil
}
