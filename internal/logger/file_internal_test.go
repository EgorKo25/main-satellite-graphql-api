package logger

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/config"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	fileOutput   = "file"
	infoLevel    = "info"
	jsonEncoding = "json"
	firstLog     = "first.log"
	secondLog    = "second.log"
)

func TestFileInitializationFailureClosesAllOpenedFiles(t *testing.T) {
	t.Parallel()

	for _, operation := range []string{"mkdir", "open"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()

			setupErr := errors.New("filesystem setup failed")
			firstCloseErr := errors.New("close first file failed")
			secondCloseErr := errors.New("close second file failed")
			first := &faultFile{closeErr: firstCloseErr}
			second := &faultFile{closeErr: secondCloseErr}

			filesystem := &faultFS{
				Fs:    afero.NewMemMapFs(),
				files: map[string]*faultFile{firstLog: first, secondLog: second},
			}
			if operation == "mkdir" {
				filesystem.mkdirPath, filesystem.mkdirErr = "broken", setupErr
			} else {
				filesystem.openPath, filesystem.openErr = filepath.Join("broken", "output.log"), setupErr
			}

			log, err := New(filesystem, config.Logger{Cores: []config.LoggerCore{
				{Level: infoLevel, Encoding: jsonEncoding, Output: fileOutput, Path: firstLog},
				{Level: infoLevel, Encoding: jsonEncoding, Output: fileOutput, Path: secondLog},
				{
					Level:    infoLevel,
					Encoding: jsonEncoding,
					Output:   fileOutput,
					Path:     filepath.Join("broken", "output.log"),
				},
			}})
			require.Nil(t, log)
			require.ErrorIs(t, err, setupErr)
			require.ErrorIs(t, err, firstCloseErr)
			require.ErrorIs(t, err, secondCloseErr)
			require.Equal(t, 1, first.closeCalls)
			require.Equal(t, 1, second.closeCalls)
		})
	}
}

func TestReadOnlyFilesystem(t *testing.T) {
	t.Parallel()

	filesystem := afero.NewReadOnlyFs(afero.NewMemMapFs())
	log, err := New(filesystem, config.Logger{Cores: []config.LoggerCore{
		{Level: infoLevel, Encoding: jsonEncoding, Output: fileOutput, Path: "readonly/output.log"},
	}})
	require.Nil(t, log)
	require.ErrorIs(t, err, os.ErrPermission)
}

func TestFileWriteFailurePreservesOtherOutputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		partial bool
	}{
		{name: "write failure", err: errors.New("disk write failed")},
		{name: "partial write", err: io.ErrShortWrite, partial: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			failed := &faultFile{writeErr: test.err, partialWrite: test.partial}
			filesystem := &faultFS{
				Fs: afero.NewMemMapFs(), files: map[string]*faultFile{"failed.log": failed},
			}
			diagnostics, err := filesystem.Create("diagnostics.log")
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, diagnostics.Close()) })

			log, err := New(filesystem, config.Logger{Cores: []config.LoggerCore{
				{Level: infoLevel, Encoding: jsonEncoding, Output: fileOutput, Path: "failed.log"},
				{Level: infoLevel, Encoding: jsonEncoding, Output: fileOutput, Path: "healthy.log"},
			}})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, log.Close()) })

			concrete, valid := log.(*zapLogger)
			require.True(t, valid)

			concrete.log = concrete.log.WithOptions(zap.ErrorOutput(zapcore.Lock(diagnostics)))
			log.Info("delivered to healthy output")
			require.NoError(t, log.Sync())

			contents, err := afero.ReadFile(filesystem, "healthy.log")
			require.NoError(t, err)

			var entry struct {
				Message string `json:"msg"`
			}

			require.NoError(t, json.Unmarshal(contents, &entry))
			require.Equal(t, "delivered to healthy output", entry.Message)

			contents, err = afero.ReadFile(filesystem, "diagnostics.log")
			require.NoError(t, err)
			require.Contains(t, string(contents), test.err.Error())

			contents, err = afero.ReadFile(filesystem, "failed.log")
			require.NoError(t, err)

			if test.partial {
				require.NotEmpty(t, contents)
				require.False(t, json.Valid(contents))
			} else {
				require.Empty(t, contents)
			}
		})
	}
}

func TestSyncAndClosePreserveAllErrors(t *testing.T) {
	t.Parallel()

	first := &faultFile{syncErr: errors.New("first sync failed"), closeErr: errors.New("first close failed")}
	second := &faultFile{syncErr: errors.New("second sync failed"), closeErr: errors.New("second close failed")}
	filesystem := &faultFS{
		Fs: afero.NewMemMapFs(), files: map[string]*faultFile{firstLog: first, secondLog: second},
	}
	log, err := New(filesystem, config.Logger{Cores: []config.LoggerCore{
		{Level: infoLevel, Encoding: jsonEncoding, Output: fileOutput, Path: firstLog},
		{Level: infoLevel, Encoding: jsonEncoding, Output: fileOutput, Path: secondLog},
	}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = log.Close() })

	err = log.Sync()
	require.ErrorIs(t, err, first.syncErr)
	require.ErrorIs(t, err, second.syncErr)
	require.Equal(t, 1, first.syncCalls)
	require.Equal(t, 1, second.syncCalls)

	for _, output := range []Logger{log, log.Named("child"), log.With(String("request", "example"))} {
		err = output.Close()
		for _, cause := range []error{first.syncErr, second.syncErr, first.closeErr, second.closeErr} {
			require.ErrorIs(t, err, cause)
		}
	}

	for _, file := range []*faultFile{first, second} {
		require.Equal(t, 2, file.syncCalls)
		require.Equal(t, 1, file.closeCalls)
	}
}

type faultFS struct {
	afero.Fs

	files     map[string]*faultFile
	mkdirPath string
	mkdirErr  error
	openPath  string
	openErr   error
}

func (filesystem *faultFS) MkdirAll(path string, mode os.FileMode) error {
	if path == filesystem.mkdirPath && filesystem.mkdirErr != nil {
		return filesystem.mkdirErr
	}

	if err := filesystem.Fs.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("create test directory: %w", err)
	}

	return nil
}

func (filesystem *faultFS) OpenFile(path string, flags int, mode os.FileMode) (afero.File, error) {
	if path == filesystem.openPath && filesystem.openErr != nil {
		return nil, filesystem.openErr
	}

	file, err := filesystem.Fs.OpenFile(path, flags, mode)
	if err != nil {
		return nil, fmt.Errorf("open test file: %w", err)
	}

	if fault, exists := filesystem.files[path]; exists {
		fault.File = file

		return fault, nil
	}

	return file, nil
}

type faultFile struct {
	afero.File

	writeErr     error
	partialWrite bool
	syncErr      error
	closeErr     error
	syncCalls    int
	closeCalls   int
}

func (file *faultFile) Write(contents []byte) (int, error) {
	if file.writeErr != nil && !file.partialWrite {
		return 0, file.writeErr
	}

	if file.partialWrite {
		contents = contents[:len(contents)/2]
	}

	count, err := file.File.Write(contents)
	if err = errors.Join(err, file.writeErr); err != nil {
		return count, fmt.Errorf("write test file: %w", err)
	}

	return count, nil
}

func (file *faultFile) Sync() error {
	file.syncCalls++

	if err := errors.Join(file.File.Sync(), file.syncErr); err != nil {
		return fmt.Errorf("sync test file: %w", err)
	}

	return nil
}

func (file *faultFile) Close() error {
	file.closeCalls++

	if err := errors.Join(file.File.Close(), file.closeErr); err != nil {
		return fmt.Errorf("close test file: %w", err)
	}

	return nil
}
