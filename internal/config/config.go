package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-playground/validator/v10"
	"go.yaml.in/yaml/v3"
)

var validate = validator.New(validator.WithRequiredStructEnabled())

func Load(path string) (*App, error) {
	contents, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)

	var app = App{
		HTTP: HTTP{
			Addr:              "0.0.0.0:8080",
			RequestTimeout:    10 * time.Second,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      15 * time.Second,
			IdleTimeout:       time.Minute,
			ShutdownTimeout:   15 * time.Second,
		},
		Logger: Logger{
			Cores: []LoggerCore{{
				Level:      "info",
				Encoding:   "json",
				Output:     "stdout",
				TimeFormat: "utc",
			}},
		},
	}

	if err = decoder.Decode(&app); err != nil {
		return nil, errors.New("decode configuration: invalid YAML or unknown field")
	}

	if err = validate.Struct(app); err != nil {
		return nil, fmt.Errorf("validate configuration: %w", err)
	}

	return &app, nil
}

type App struct {
	Database Database `yaml:"database"`
	HTTP     HTTP     `yaml:"http"`
	Logger   Logger   `yaml:"logger"`
}

type Database struct {
	URL            string        `validate:"required"                yaml:"url"`
	User           string        `validate:"required"                yaml:"user"`
	Password       string        `validate:"required"                yaml:"password"`
	ConnectTimeout time.Duration `validate:"gt=0"                    yaml:"connect_timeout"`
	MaxConns       int32         `validate:"gt=0"                    yaml:"max_conns"`
	MinConns       int32         `validate:"gte=0,ltefield=MaxConns" yaml:"min_conns"`
}

type HTTP struct {
	Addr              string        `validate:"hostname_port" yaml:"addr"`
	RequestTimeout    time.Duration `validate:"gt=0"          yaml:"request_timeout"`
	ReadHeaderTimeout time.Duration `validate:"gt=0"          yaml:"read_header_timeout"`
	ReadTimeout       time.Duration `validate:"gt=0"          yaml:"read_timeout"`
	WriteTimeout      time.Duration `validate:"gt=0"          yaml:"write_timeout"`
	IdleTimeout       time.Duration `validate:"gt=0"          yaml:"idle_timeout"`
	ShutdownTimeout   time.Duration `validate:"gt=0"          yaml:"shutdown_timeout"`
}

type Logger struct {
	Cores []LoggerCore `validate:"required,min=1,dive" yaml:"cores"`
}

type LoggerCore struct {
	Level      string `validate:"oneof=debug info warn error dpanic panic fatal" yaml:"level"`
	Encoding   string `validate:"oneof=json console"                             yaml:"encoding"`
	Output     string `validate:"oneof=stdout stderr file"                       yaml:"output"`
	Path       string `validate:"required_if=Output file"                        yaml:"path"`
	TimeFormat string `validate:"oneof=utc local"                                yaml:"time_format"`
}
