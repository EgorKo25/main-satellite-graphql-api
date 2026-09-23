package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
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

	var (
		app = App{HTTP: HTTP{
			Addr:              "0.0.0.0:8080",
			RequestTimeout:    10 * time.Second,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      15 * time.Second,
			IdleTimeout:       time.Minute,
			ShutdownTimeout:   15 * time.Second,
		}}
		extra yaml.Node
	)

	if err = decoder.Decode(&app); err != nil {
		return nil, errors.New("decode configuration: invalid YAML or unknown field")
	}

	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("configuration must contain exactly one YAML document")
	}

	if url, present := os.LookupEnv("DATABASE_URL"); present {
		app.Database.URL = url
	}

	if err = validate.Struct(app); err != nil {
		return nil, fmt.Errorf("validate configuration: %w", err)
	}

	return &app, nil
}

type App struct {
	Database Database `yaml:"database"`
	HTTP     HTTP     `yaml:"http"`
}

type Database struct {
	URL            string        `validate:"required"                yaml:"url"`
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
