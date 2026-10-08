package logger_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/config"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/logger"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

const (
	jsonEncoding = "json"
	errorLevel   = "error"
	infoLevel    = "info"
	warnLevel    = "warn"
	debugLevel   = "debug"
	fileOutput   = "file"
	utcFormat    = "utc"
)

//nolint:paralleltest // Replaces the process-global logger.
func TestInitialize(t *testing.T) {
	filesystem := afero.NewMemMapFs()
	path := filepath.Join("logs", "output.log")
	require.NoError(t, logger.Initialize(filesystem, config.Logger{Cores: []config.LoggerCore{{
		Level: infoLevel, Encoding: jsonEncoding, Output: fileOutput, Path: path, TimeFormat: utcFormat,
	}}}))
	t.Cleanup(func() { require.NoError(t, logger.Get("main").Close()) })

	logger.Get("graphql").Info("initialized")
	require.Error(t, logger.Initialize(filesystem, config.Logger{Cores: []config.LoggerCore{{Level: "invalid"}}}))
	logger.Get("postgres").Info("previous logger retained")
	require.NoError(t, logger.Get("main").Sync())

	contents, err := afero.ReadFile(filesystem, path)
	require.NoError(t, err)

	decoder := json.NewDecoder(bytes.NewReader(contents))

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

			log, filesystem, path := newTestLogger(t, test.level, jsonEncoding, utcFormat)
			log.Debug(debugLevel)
			log.Info(infoLevel)
			log.Warn(warnLevel)
			log.Error("failure", errors.New("operation failed"))
			require.NoError(t, log.Sync())

			contents, err := afero.ReadFile(filesystem, path)
			require.NoError(t, err)

			decoder := json.NewDecoder(bytes.NewReader(contents))
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

			log, filesystem, path := newTestLogger(t, infoLevel, encoding, utcFormat)
			log.Info("server ready", logger.String("address", ":8080"))

			contents, err := afero.ReadFile(filesystem, path)
			require.NoError(t, err)

			if encoding == jsonEncoding {
				var entry struct {
					Time    string `json:"ts"`
					Message string `json:"msg"`
					Address string `json:"address"`
				}

				require.NoError(t, json.Unmarshal(contents, &entry))
				require.Equal(t, "server ready", entry.Message)
				require.Equal(t, ":8080", entry.Address)
				require.True(t, strings.HasSuffix(entry.Time, "Z"))

				_, err = time.Parse(time.RFC3339Nano, entry.Time)
				require.NoError(t, err)
			} else {
				parts := strings.Split(strings.TrimSpace(string(contents)), "\t")
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

func TestLocalTime(t *testing.T) {
	t.Parallel()

	log, filesystem, path := newTestLogger(t, infoLevel, jsonEncoding, "local")
	log.Info("local timestamp")

	contents, err := afero.ReadFile(filesystem, path)
	require.NoError(t, err)

	var entry struct {
		Time string `json:"ts"`
	}

	require.NoError(t, json.Unmarshal(contents, &entry))

	timestamp, err := time.Parse("2006-01-02T15:04:05.000Z0700", entry.Time)
	require.NoError(t, err)

	_, expectedOffset := timestamp.In(time.Local).Zone()
	_, actualOffset := timestamp.Zone()
	require.Equal(t, expectedOffset, actualOffset)
}

func TestInvalidLevel(t *testing.T) {
	t.Parallel()

	log, err := logger.New(afero.NewMemMapFs(), config.Logger{Cores: []config.LoggerCore{{Level: "verbose"}}})
	require.ErrorContains(t, err, "parse logger level")
	require.Nil(t, log)
}

func TestErrorAttributes(t *testing.T) {
	t.Parallel()

	log, filesystem, path := newTestLogger(t, infoLevel, jsonEncoding, utcFormat)
	log.Error("database failure", errors.New("connection refused"),
		logger.String("operation", "update"), logger.Any("attempt", 2))

	contents, err := afero.ReadFile(filesystem, path)
	require.NoError(t, err)

	var entry struct {
		Level     string `json:"level"`
		Message   string `json:"msg"`
		Error     string `json:"error"`
		Operation string `json:"operation"`
		Attempt   int    `json:"attempt"`
	}

	require.NoError(t, json.Unmarshal(contents, &entry))
	require.Equal(t, errorLevel, entry.Level)
	require.Equal(t, "database failure", entry.Message)
	require.Equal(t, "connection refused", entry.Error)
	require.Equal(t, "update", entry.Operation)
	require.Equal(t, 2, entry.Attempt)
}

func TestDerivedLoggerIsolation(t *testing.T) {
	t.Parallel()

	log, filesystem, path := newTestLogger(t, infoLevel, jsonEncoding, utcFormat)
	child := log.Named("graphql").With(logger.String("request", "first"))
	grandchild := child.Named("resolver").With(logger.String("operation", "create"))
	grandchild.Info("grandchild")
	child.Info("child")
	log.Info("parent")
	require.NoError(t, grandchild.Close())
	require.NoError(t, child.Close())
	require.NoError(t, log.Close())

	contents, err := afero.ReadFile(filesystem, path)
	require.NoError(t, err)

	decoder := json.NewDecoder(bytes.NewReader(contents))

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

	log, filesystem, path := newTestLogger(t, infoLevel, jsonEncoding, utcFormat)
	_, _, line, ok := runtime.Caller(0)

	log.Info("caller")
	require.True(t, ok)

	contents, err := afero.ReadFile(filesystem, path)
	require.NoError(t, err)

	var entry struct {
		Caller string `json:"caller"`
	}

	require.NoError(t, json.Unmarshal(contents, &entry))
	require.Equal(t, fmt.Sprintf("logger/logger_test.go:%d", line+2), entry.Caller)
}

func TestMultipleCores(t *testing.T) {
	t.Parallel()

	var messages = []string{"ready\nsecond line", "warning", "failure"}

	tests := []struct {
		name       string
		level      string
		wantSecond []string
	}{
		{name: "same thresholds", level: infoLevel, wantSecond: messages},
		{name: "different thresholds", level: errorLevel, wantSecond: []string{"failure"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			filesystem := afero.NewMemMapFs()
			cores := []config.LoggerCore{
				{
					Level:      infoLevel,
					Encoding:   jsonEncoding,
					Output:     fileOutput,
					Path:       filepath.Join("logs", "all.log"),
					TimeFormat: utcFormat,
				},
				{
					Level:      test.level,
					Encoding:   jsonEncoding,
					Output:     fileOutput,
					Path:       filepath.Join("logs", "filtered.log"),
					TimeFormat: utcFormat,
				},
			}
			log, err := logger.New(filesystem, config.Logger{Cores: cores})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, log.Close()) })

			log.Info(messages[0])
			log.Warn(messages[1])
			log.Error(messages[2], errors.New("database failed"))
			require.NoError(t, log.Sync())

			for index, expected := range [][]string{messages, test.wantSecond} {
				contents, readErr := afero.ReadFile(filesystem, cores[index].Path)
				require.NoError(t, readErr)

				lines := bytes.Split(bytes.TrimSpace(contents), []byte("\n"))
				require.Len(t, lines, len(expected))

				for entryIndex, line := range lines {
					var entry struct {
						Message string `json:"msg"`
					}

					require.NoError(t, json.Unmarshal(line, &entry))
					require.Equal(t, expected[entryIndex], entry.Message)
				}
			}
		})
	}
}

func TestAppendExistingFile(t *testing.T) {
	t.Parallel()

	const (
		path     = "output.log"
		previous = "previous contents\n"
	)

	filesystem := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(filesystem, path, []byte(previous), 0o600))

	log, err := logger.New(filesystem, config.Logger{Cores: []config.LoggerCore{{
		Level: infoLevel, Encoding: jsonEncoding, Output: fileOutput, Path: path, TimeFormat: utcFormat,
	}}})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, log.Close()) })

	log.Info("appended")
	require.NoError(t, log.Close())

	contents, err := afero.ReadFile(filesystem, path)
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(contents, []byte(previous)), "existing file contents must remain intact")

	var entry struct {
		Message string `json:"msg"`
	}

	require.NoError(t, json.Unmarshal(contents[len(previous):], &entry))
	require.Equal(t, "appended", entry.Message)
}

func TestConcurrentWritesKeepCompleteRecords(t *testing.T) {
	t.Parallel()

	const (
		workerCount = 4
		entryCount  = 25
	)

	log, filesystem, path := newTestLogger(t, infoLevel, jsonEncoding, utcFormat)

	var workers sync.WaitGroup

	for worker := range workerCount {
		workers.Go(func() {
			for entry := range entryCount {
				log.Info("concurrent", logger.Any("worker", worker), logger.Any("entry", entry))
			}
		})
	}

	workers.Wait()
	require.NoError(t, log.Close())

	contents, err := afero.ReadFile(filesystem, path)
	require.NoError(t, err)

	type record struct {
		Message string `json:"msg"`
		Worker  int    `json:"worker"`
		Entry   int    `json:"entry"`
	}

	want := make([]record, 0, workerCount*entryCount)
	for worker := range workerCount {
		for entry := range entryCount {
			want = append(want, record{Message: "concurrent", Worker: worker, Entry: entry})
		}
	}

	lines := bytes.Split(bytes.TrimSpace(contents), []byte("\n"))
	got := make([]record, len(lines))

	for index, line := range lines {
		require.NoError(t, json.Unmarshal(line, &got[index]))
	}

	require.ElementsMatch(t, want, got)
}

func TestConsoleOutputs(t *testing.T) {
	t.Parallel()

	for _, output := range []string{"stdout", "stderr"} {
		t.Run(output, func(t *testing.T) {
			t.Parallel()

			log, err := logger.New(afero.NewMemMapFs(), config.Logger{Cores: []config.LoggerCore{{
				Level: infoLevel, Encoding: jsonEncoding, Output: output, TimeFormat: utcFormat,
			}}})
			require.NoError(t, err)
			require.NoError(t, log.Sync())
			require.NoError(t, log.Close())
		})
	}
}

func newTestLogger(t *testing.T, level, encoding, timeFormat string) (logger.Logger, afero.Fs, string) {
	t.Helper()

	filesystem := afero.NewMemMapFs()
	path := filepath.Join("nested", "output.log")
	log, err := logger.New(filesystem, config.Logger{Cores: []config.LoggerCore{{
		Level: level, Encoding: encoding, Output: fileOutput, Path: path, TimeFormat: timeFormat,
	}}})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, log.Close()) })

	return log, filesystem, path
}
