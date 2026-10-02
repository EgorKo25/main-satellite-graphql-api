package logger_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/config"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/logger"
	"github.com/stretchr/testify/require"
)

const (
	jsonEncoding = "json"
	errorLevel   = "error"
	infoLevel    = "info"
	warnLevel    = "warn"
	debugLevel   = "debug"
)

//nolint:paralleltest // Replaces the process-global logger.
func TestInitialize(t *testing.T) {
	var output bytes.Buffer

	require.NoError(t, logger.Initialize(config.Logger{Level: infoLevel, Encoding: jsonEncoding}, &output))
	logger.Get("graphql").Info("initialized")
	require.Error(t, logger.Initialize(config.Logger{Level: "invalid", Encoding: jsonEncoding}, &output))
	logger.Get("postgres").Info("previous logger retained")
	require.NoError(t, logger.Get("main").Sync())

	decoder := json.NewDecoder(&output)

	for _, expected := range []struct {
		Name    string `json:"logger"`
		Message string `json:"msg"`
	}{
		{Name: "graphql", Message: "initialized"},
		{Name: "postgres", Message: "previous logger retained"},
	} {
		var actual struct {
			Name    string `json:"logger"`
			Message string `json:"msg"`
		}

		require.NoError(t, decoder.Decode(&actual))
		require.Equal(t, expected, actual)
	}
}

func TestLevels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		level string
		want  []string
	}{
		{level: debugLevel, want: []string{debugLevel, infoLevel, warnLevel, errorLevel}},
		{level: infoLevel, want: []string{infoLevel, warnLevel, errorLevel}},
		{level: warnLevel, want: []string{warnLevel, errorLevel}},
		{level: errorLevel, want: []string{errorLevel}},
	}

	for _, test := range tests {
		t.Run(test.level, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer

			log, err := logger.New(config.Logger{Level: test.level, Encoding: jsonEncoding}, &output)
			require.NoError(t, err)

			log.Debug(debugLevel)
			log.Info(infoLevel)
			log.Warn(warnLevel)
			log.Error("failure", errors.New("operation failed"))
			require.NoError(t, log.Sync())

			decoder := json.NewDecoder(&output)
			levels := make([]string, 0, len(test.want))

			for decoder.More() {
				var entry struct {
					Level string `json:"level"`
				}

				require.NoError(t, decoder.Decode(&entry))

				levels = append(levels, entry.Level)
			}

			require.Equal(t, test.want, levels)
		})
	}
}

func TestEncoding(t *testing.T) {
	t.Parallel()

	for _, encoding := range []string{jsonEncoding, "console"} {
		t.Run(encoding, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer

			log, err := logger.New(config.Logger{Level: infoLevel, Encoding: encoding}, &output)
			require.NoError(t, err)

			log.Info("server ready", logger.String("address", ":8080"))

			if encoding == jsonEncoding {
				var entry struct {
					Time    string `json:"ts"`
					Message string `json:"msg"`
					Address string `json:"address"`
				}

				require.NoError(t, json.Unmarshal(output.Bytes(), &entry))
				require.Equal(t, "server ready", entry.Message)
				require.Equal(t, ":8080", entry.Address)
				require.True(t, strings.HasSuffix(entry.Time, "Z"))

				_, err = time.Parse(time.RFC3339Nano, entry.Time)
				require.NoError(t, err)
			} else {
				parts := strings.Split(strings.TrimSpace(output.String()), "\t")
				require.Len(t, parts, 5)
				require.True(t, strings.HasSuffix(parts[0], "Z"))

				_, err = time.Parse(time.RFC3339Nano, parts[0])
				require.NoError(t, err)
				require.Equal(t, infoLevel, parts[1])
				require.Equal(t, "server ready", parts[3])
				require.JSONEq(t, `{"address":":8080"}`, parts[4])
			}
		})
	}
}

func TestInvalidLevel(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer

	log, err := logger.New(config.Logger{Level: "verbose", Encoding: jsonEncoding}, &output)
	require.ErrorContains(t, err, "parse logger level")
	require.Nil(t, log)
	require.Empty(t, output.String())
}

func TestErrorAttributes(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer

	log, err := logger.New(config.Logger{Level: infoLevel, Encoding: jsonEncoding}, &output)
	require.NoError(t, err)

	log.Error("database failure", errors.New("connection refused"),
		logger.String("operation", "update"), logger.Any("attempt", 2))

	var entry struct {
		Level     string `json:"level"`
		Message   string `json:"msg"`
		Error     string `json:"error"`
		Operation string `json:"operation"`
		Attempt   int    `json:"attempt"`
	}

	require.NoError(t, json.Unmarshal(output.Bytes(), &entry))
	require.Equal(t, errorLevel, entry.Level)
	require.Equal(t, "database failure", entry.Message)
	require.Equal(t, "connection refused", entry.Error)
	require.Equal(t, "update", entry.Operation)
	require.Equal(t, 2, entry.Attempt)
}

func TestDerivedLoggerIsolation(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer

	log, err := logger.New(config.Logger{Level: infoLevel, Encoding: jsonEncoding}, &output)
	require.NoError(t, err)

	child := log.Named("graphql").With(logger.String("request", "first"))
	grandchild := child.Named("resolver").With(logger.String("operation", "create"))
	grandchild.Info("grandchild")
	child.Info("child")
	log.Info("parent")

	decoder := json.NewDecoder(&output)

	var (
		grandchildEntry map[string]any
		childEntry      map[string]any
		parentEntry     map[string]any
	)

	require.NoError(t, decoder.Decode(&grandchildEntry))
	require.NoError(t, decoder.Decode(&childEntry))
	require.NoError(t, decoder.Decode(&parentEntry))
	require.Equal(t, "graphql.resolver", grandchildEntry["logger"])
	require.Equal(t, "first", grandchildEntry["request"])
	require.Equal(t, "create", grandchildEntry["operation"])
	require.Equal(t, "graphql", childEntry["logger"])
	require.Equal(t, "first", childEntry["request"])
	require.NotContains(t, childEntry, "operation")
	require.NotContains(t, parentEntry, "logger")
	require.NotContains(t, parentEntry, "request")
	require.NotContains(t, parentEntry, "operation")
}

func TestCaller(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer

	log, err := logger.New(config.Logger{Level: infoLevel, Encoding: jsonEncoding}, &output)
	require.NoError(t, err)

	_, _, line, ok := runtime.Caller(0)

	log.Info("caller")
	require.True(t, ok)

	var entry struct {
		Caller string `json:"caller"`
	}

	require.NoError(t, json.Unmarshal(output.Bytes(), &entry))
	require.Equal(t, fmt.Sprintf("logger/logger_test.go:%d", line+2), entry.Caller)
}

func TestSync(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{name: "success"},
		{name: "sink failure", err: errors.New("flush failed")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			output := &syncWriter{err: test.err}
			log, err := logger.New(config.Logger{Level: infoLevel, Encoding: jsonEncoding}, output)
			require.NoError(t, err)

			log.Info("entry")
			require.ErrorIs(t, log.Sync(), test.err)
			require.Equal(t, 1, output.calls)
			require.Contains(t, output.String(), "entry")
		})
	}
}

type syncWriter struct {
	bytes.Buffer

	err   error
	calls int
}

func (writer *syncWriter) Sync() error {
	writer.calls++

	return writer.err
}
