package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var globalLogger Logger = &zapLogger{
	log:   zap.NewNop(),
	close: func() error { return nil },
}

func Initialize(cfg config.Logger) error {
	log, err := New(cfg)
	if err != nil {
		return err
	}

	globalLogger = log

	return nil
}

func Get(name string) Logger {
	return globalLogger.Named(name)
}

func New(cfg config.Logger) (Logger, error) {
	cores := make([]zapcore.Core, 0, len(cfg.Cores))
	closers := make([]func(), 0, len(cfg.Cores))

	for _, coreConfig := range cfg.Cores {
		core, closeOutput, err := newCore(coreConfig)
		if err != nil {
			for _, closeOpened := range closers {
				closeOpened()
			}

			return nil, err
		}

		cores = append(cores, core)

		if closeOutput != nil {
			closers = append(closers, closeOutput)
		}
	}

	log := zap.New(zapcore.NewTee(cores...), zap.AddCaller(), zap.AddCallerSkip(1))
	closeOutputs := sync.OnceValue(func() error {
		err := log.Sync()

		for _, closeOutput := range closers {
			closeOutput()
		}

		if err != nil {
			return fmt.Errorf("sync logger outputs: %w", err)
		}

		return nil
	})

	return &zapLogger{log: log, close: closeOutputs}, nil
}

type zapLogger struct {
	log   *zap.Logger
	close func() error
}

func (logger *zapLogger) Debug(msg string, attrs ...Attr) {
	logger.log.Debug(msg, zapFields(attrs)...)
}

func (logger *zapLogger) Info(msg string, attrs ...Attr) {
	logger.log.Info(msg, zapFields(attrs)...)
}

func (logger *zapLogger) Warn(msg string, attrs ...Attr) {
	logger.log.Warn(msg, zapFields(attrs)...)
}

func (logger *zapLogger) Error(msg string, err error, attrs ...Attr) {
	fields := append(zapFields(attrs), zap.Error(err))
	logger.log.Error(msg, fields...)
}

func (logger *zapLogger) Fatal(msg string, err error, attrs ...Attr) {
	fields := append(zapFields(attrs), zap.Error(err))
	logger.log.Fatal(msg, fields...)
}

func (logger *zapLogger) With(attrs ...Attr) Logger {
	return &zapLogger{log: logger.log.With(zapFields(attrs)...), close: logger.close}
}

func (logger *zapLogger) Named(name string) Logger {
	return &zapLogger{log: logger.log.Named(name), close: logger.close}
}

func (logger *zapLogger) Sync() error {
	if err := logger.log.Sync(); err != nil {
		return fmt.Errorf("sync logger: %w", err)
	}

	return nil
}

func (logger *zapLogger) Close() error {
	if err := logger.close(); err != nil {
		return fmt.Errorf("close logger: %w", err)
	}

	return nil
}

func newCore(cfg config.LoggerCore) (zapcore.Core, func(), error) {
	level, err := zapcore.ParseLevel(cfg.Level)
	if err != nil {
		return nil, nil, fmt.Errorf("parse logger level: %w", err)
	}

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = func(timestamp time.Time, encoder zapcore.PrimitiveArrayEncoder) {
		encoder.AppendString(timestamp.UTC().Format(time.RFC3339Nano))
	}

	if cfg.TimeFormat == "local" {
		encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	}

	var (
		encoder     zapcore.Encoder
		output      zapcore.WriteSyncer
		closeOutput func()
	)

	if cfg.Encoding == "console" {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	switch cfg.Output {
	case "stdout":
		output = zapcore.Lock(zapcore.AddSync(io.MultiWriter(os.Stdout)))
	case "stderr":
		output = zapcore.Lock(zapcore.AddSync(io.MultiWriter(os.Stderr)))
	case "file":
		if err = os.MkdirAll(filepath.Dir(cfg.Path), 0o750); err != nil {
			return nil, nil, fmt.Errorf("create logger directory: %w", err)
		}

		output, closeOutput, err = zap.Open(filepath.Clean(cfg.Path))
		if err != nil {
			return nil, nil, fmt.Errorf("open logger output: %w", err)
		}
	default:
		return nil, nil, fmt.Errorf("unsupported logger output %q", cfg.Output)
	}

	return zapcore.NewCore(encoder, output, level), closeOutput, nil
}

func zapFields(attrs []Attr) []zap.Field {
	fields := make([]zap.Field, len(attrs))

	for index, attr := range attrs {
		fields[index] = attr.field
	}

	return fields
}
