package logger

import (
	"fmt"
	"io"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var globalLogger Logger = &zapLogger{log: zap.NewNop()}

func Initialize(cfg config.Logger, output io.Writer) error {
	log, err := New(cfg, output)
	if err != nil {
		return err
	}

	globalLogger = log

	return nil
}

func Get(name string) Logger {
	return globalLogger.Named(name)
}

func New(cfg config.Logger, output io.Writer) (Logger, error) {
	level, err := zapcore.ParseLevel(cfg.Level)
	if err != nil {
		return nil, fmt.Errorf("parse logger level: %w", err)
	}

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = func(timestamp time.Time, encoder zapcore.PrimitiveArrayEncoder) {
		encoder.AppendString(timestamp.UTC().Format(time.RFC3339Nano))
	}

	var encoder zapcore.Encoder

	if cfg.Encoding == "console" {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	core := zapcore.NewCore(encoder, zapcore.Lock(zapcore.AddSync(output)), level)

	return &zapLogger{log: zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))}, nil
}

type zapLogger struct {
	log *zap.Logger
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
	return &zapLogger{log: logger.log.With(zapFields(attrs)...)}
}

func (logger *zapLogger) Named(name string) Logger {
	return &zapLogger{log: logger.log.Named(name)}
}

func (logger *zapLogger) Sync() error {
	if err := logger.log.Sync(); err != nil {
		return fmt.Errorf("sync logger: %w", err)
	}

	return nil
}

func zapFields(attrs []Attr) []zap.Field {
	fields := make([]zap.Field, len(attrs))

	for index, attr := range attrs {
		fields[index] = attr.field
	}

	return fields
}
