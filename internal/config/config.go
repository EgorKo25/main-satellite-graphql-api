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

var (
	App      Application
	validate = validator.New(validator.WithRequiredStructEnabled())
)

type Application struct {
	Database Database `yaml:"database"`
}

type Database struct {
	URL            string        `validate:"required"                yaml:"url"`
	ConnectTimeout time.Duration `validate:"gt=0"                    yaml:"connect_timeout"`
	MaxConns       int32         `validate:"gt=0"                    yaml:"max_conns"`
	MinConns       int32         `validate:"gte=0,ltefield=MaxConns" yaml:"min_conns"`
}

func Load(path string) error {
	contents, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("read configuration: %w", err)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)

	var (
		application Application
		extra       yaml.Node
	)

	if err = decoder.Decode(&application); err != nil {
		return errors.New("decode configuration: invalid YAML or unknown field")
	}

	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("configuration must contain exactly one YAML document")
	}

	if url, present := os.LookupEnv("DATABASE_URL"); present {
		application.Database.URL = url
	}

	if err = validate.Struct(application); err != nil {
		return fmt.Errorf("validate configuration: %w", err)
	}

	App = application

	return nil
}
