package config

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/creasty/defaults"
	"github.com/go-playground/validator/v10"
	"github.com/spf13/afero"
	"go.yaml.in/yaml/v3"
)

var validate = validator.New(validator.WithRequiredStructEnabled())

func Load(fs afero.Fs, path string) (*App, error) {
	contents, err := afero.ReadFile(fs, filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)

	app := new(App)

	if err = defaults.Set(app); err != nil {
		return nil, fmt.Errorf("set configuration defaults: %w", err)
	}

	if err = decoder.Decode(app); err != nil {
		return nil, errors.New("decode configuration: invalid YAML or unknown field")
	}

	if err = validate.Struct(app); err != nil {
		return nil, fmt.Errorf("validate configuration: %w", err)
	}

	return app, nil
}

type App struct {
	Database `yaml:"database"`
	HTTP     `yaml:"http"`
	Logger   `yaml:"logger"`
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
	Addr              string        `default:"0.0.0.0:8080" validate:"hostname_port" yaml:"addr"`
	RequestTimeout    time.Duration `default:"10s"          validate:"gt=0"          yaml:"request_timeout"`
	ReadHeaderTimeout time.Duration `default:"5s"           validate:"gt=0"          yaml:"read_header_timeout"`
	ReadTimeout       time.Duration `default:"10s"          validate:"gt=0"          yaml:"read_timeout"`
	WriteTimeout      time.Duration `default:"15s"          validate:"gt=0"          yaml:"write_timeout"`
	IdleTimeout       time.Duration `default:"1m"           validate:"gt=0"          yaml:"idle_timeout"`
	ShutdownTimeout   time.Duration `default:"15s"          validate:"gt=0"          yaml:"shutdown_timeout"`
}

type Logger struct {
	Cores []LoggerCore `default:"[{}]" validate:"required,min=1,dive" yaml:"cores"`
}

type LoggerCore struct {
	Level      string `default:"info"   validate:"oneof=debug info warn error dpanic panic fatal" yaml:"level"`
	Encoding   string `default:"json"   validate:"oneof=json console"                             yaml:"encoding"`
	Output     string `default:"stdout" validate:"oneof=stdout stderr file"                       yaml:"output"`
	TimeFormat string `default:"utc"    validate:"oneof=utc local"                                yaml:"time_format"`

	Path string `validate:"required_if=Output file" yaml:"path"`
}
