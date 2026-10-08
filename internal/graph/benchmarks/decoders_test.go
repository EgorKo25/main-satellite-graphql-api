package benchmarks_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph/benchmarks"
	"github.com/mailru/easyjson"
	"github.com/samber/lo"
	"github.com/valyala/fastjson"
)

var factories = map[domain.Kind]func() domain.SubObject{
	domain.Tools:  func() domain.SubObject { return new(domain.Tool) },
	domain.Tables: func() domain.SubObject { return new(domain.Table) },
	domain.Chairs: func() domain.SubObject { return new(domain.Chair) },
}

func factoryMap(kind domain.Kind) domain.SubObject {
	factory, ok := factories[kind]
	if !ok {
		return nil
	}

	return factory()
}

func factorySwitch(kind domain.Kind) domain.SubObject {
	switch kind {
	case domain.Tools:
		return new(domain.Tool)
	case domain.Tables:
		return new(domain.Table)
	case domain.Chairs:
		return new(domain.Chair)
	default:
		return nil
	}
}

func factoryLO(kind domain.Kind) domain.SubObject {
	return lo.Switch[domain.Kind, domain.SubObject](kind).
		CaseF(domain.Tools, func() domain.SubObject { return new(domain.Tool) }).
		CaseF(domain.Tables, func() domain.SubObject { return new(domain.Table) }).
		CaseF(domain.Chairs, func() domain.SubObject { return new(domain.Chair) }).
		Default(nil)
}

func decodeStandard(kind domain.Kind, data []byte) (domain.SubObject, error) {
	value := factoryMap(kind)
	if value == nil {
		return nil, fmt.Errorf("unknown kind %q", kind)
	}

	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("decode satellite: %w", err)
	}

	if value == nil {
		return nil, errors.New("missing satellite")
	}

	return value, nil
}

func decodeEasy(kind domain.Kind, data []byte) (domain.SubObject, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return nil, errors.New("expected satellite object")
	}

	var (
		value domain.SubObject
		err   error
	)

	switch kind {
	case domain.Tools:
		target := new(benchmarks.Tool)
		err = easyjson.Unmarshal(data, target)
		value = (*domain.Tool)(target)
	case domain.Tables:
		target := new(benchmarks.Table)
		err = easyjson.Unmarshal(data, target)
		value = (*domain.Table)(target)
	case domain.Chairs:
		target := new(benchmarks.Chair)
		err = easyjson.Unmarshal(data, target)
		value = (*domain.Chair)(target)
	default:
		return nil, fmt.Errorf("unknown kind %q", kind)
	}

	if err != nil {
		return nil, fmt.Errorf("decode satellite: %w", err)
	}

	return value, nil
}

func decodeFast(parser *fastjson.Parser, kind domain.Kind, data []byte) (domain.SubObject, error) {
	root, err := parser.ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parse satellite: %w", err)
	}

	if root.Type() != fastjson.TypeObject {
		return nil, errors.New("expected satellite object")
	}

	var (
		value       domain.SubObject
		metadata    *domain.Satellite
		description **string
		field       string
	)

	switch kind {
	case domain.Tools:
		tool := new(domain.Tool)
		value, metadata, description, field = tool, &tool.Satellite, &tool.Description1, "description1"
	case domain.Tables:
		table := new(domain.Table)
		value, metadata, description, field = table, &table.Satellite, &table.Description2, "description2"
	case domain.Chairs:
		chair := new(domain.Chair)
		value, metadata, description, field = chair, &chair.Satellite, &chair.Description3, "description3"

		if typ := root.Get("type"); typ != nil && typ.Type() != fastjson.TypeNull {
			text, typeErr := typ.StringBytes()
			if typeErr != nil {
				return nil, fmt.Errorf("decode chair type: %w", typeErr)
			}

			chair.Type = domain.ChairType(text)
		}
	default:
		return nil, fmt.Errorf("unknown kind %q", kind)
	}

	if err = decodeMetadata(root, metadata); err != nil {
		return nil, err
	}

	if text := root.Get(field); text != nil && text.Type() != fastjson.TypeNull {
		decoded, textErr := text.StringBytes()
		if textErr != nil {
			return nil, fmt.Errorf("decode description: %w", textErr)
		}

		*description = new(string(decoded))
	}

	return value, nil
}

func decodeMetadata(root *fastjson.Value, target *domain.Satellite) error {
	for _, field := range []struct {
		name   string
		target *int64
	}{{"id", &target.ID}, {"main_id", &target.MainID}} {
		if value := root.Get(field.name); value != nil && value.Type() != fastjson.TypeNull {
			integer, err := value.Int64()
			if err != nil {
				return fmt.Errorf("decode %s: %w", field.name, err)
			}

			*field.target = integer
		}
	}

	for _, field := range []struct {
		name   string
		target *time.Time
	}{{"created_at", &target.CreatedAt}, {"update_at", &target.UpdatedAt}} {
		if value := root.Get(field.name); value != nil && value.Type() != fastjson.TypeNull {
			text, err := value.StringBytes()
			if err != nil {
				return fmt.Errorf("decode %s: %w", field.name, err)
			}

			if *field.target, err = time.Parse(time.RFC3339Nano, string(text)); err != nil {
				return fmt.Errorf("parse %s: %w", field.name, err)
			}
		}
	}

	if value := root.Get("deleted_at"); value != nil && value.Type() != fastjson.TypeNull {
		text, err := value.StringBytes()
		if err != nil {
			return fmt.Errorf("decode deleted_at: %w", err)
		}

		deleted, err := time.Parse(time.RFC3339Nano, string(text))
		if err != nil {
			return fmt.Errorf("parse deleted_at: %w", err)
		}

		target.DeletedAt = &deleted
	}

	return nil
}

type decoder struct {
	name   string
	decode func(domain.Kind, []byte) (domain.SubObject, error)
}

func decoders() []decoder {
	var parser fastjson.Parser

	return []decoder{
		{name: "standard", decode: decodeStandard},
		{name: "easyjson", decode: decodeEasy},
		{name: "fastjson_reuse", decode: func(kind domain.Kind, data []byte) (domain.SubObject, error) {
			return decodeFast(&parser, kind, data)
		}},
		{name: "fastjson_fresh", decode: func(kind domain.Kind, data []byte) (domain.SubObject, error) {
			var local fastjson.Parser

			return decodeFast(&local, kind, data)
		}},
	}
}
