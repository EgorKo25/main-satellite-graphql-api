package logger

import "go.uber.org/zap"

type Logger interface {
	Debug(msg string, attrs ...Attr)
	Info(msg string, attrs ...Attr)
	Warn(msg string, attrs ...Attr)
	Error(msg string, err error, attrs ...Attr)
	Fatal(msg string, err error, attrs ...Attr)
	With(attrs ...Attr) Logger
	Named(name string) Logger
	Sync() error
}

func String(key, value string) Attr {
	return Attr{field: zap.String(key, value)}
}

func Any(key string, value any) Attr {
	return Attr{field: zap.Any(key, value)}
}

type Attr struct {
	field zap.Field
}
