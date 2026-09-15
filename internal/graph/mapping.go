package graph

import (
	"strconv"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph/model"
)

func outputMain(value *domain.Main) *model.Main {
	if value == nil {
		return nil
	}
	var output model.Satellite
	switch satellite := value.Satellite.(type) {
	case *domain.Tool:
		output = &model.Tool{ID: strconv.FormatInt(satellite.ID, 10), Description1: satellite.Description1,
			CreatedAt: satellite.CreatedAt.UTC(), UpdatedAt: satellite.UpdatedAt.UTC(), DeletedAt: utcPointer(satellite.DeletedAt)}
	case *domain.Table:
		output = &model.Table{ID: strconv.FormatInt(satellite.ID, 10), Description2: satellite.Description2,
			CreatedAt: satellite.CreatedAt.UTC(), UpdatedAt: satellite.UpdatedAt.UTC(), DeletedAt: utcPointer(satellite.DeletedAt)}
	case *domain.Chair:
		output = &model.Chair{ID: strconv.FormatInt(satellite.ID, 10), Description3: satellite.Description3, Type: model.ChairType(satellite.Type),
			CreatedAt: satellite.CreatedAt.UTC(), UpdatedAt: satellite.UpdatedAt.UTC(), DeletedAt: utcPointer(satellite.DeletedAt)}
	}
	return &model.Main{ID: strconv.FormatInt(value.ID, 10), Title: value.Title, Satellite: output,
		CreatedAt: value.CreatedAt.UTC(), UpdatedAt: value.UpdatedAt.UTC(), DeletedAt: utcPointer(value.DeletedAt)}
}

func utcPointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

// A supplied null counts as a supplied field. Counting non-nil pointers would
// incorrectly accept {delete: {id: "1"}, create: null}.
func oneOf(fields ...oneOfField) error {
	count := 0
	for _, field := range fields {
		if field.set {
			count++
			if field.null {
				return domain.BadInput("OneOf requires exactly one supplied, non-null field")
			}
		}
	}
	if count != 1 {
		return domain.BadInput("OneOf requires exactly one supplied, non-null field")
	}
	return nil
}

type oneOfField struct {
	set  bool
	null bool
}

func presence[T any](value graphql.Omittable[*T]) oneOfField {
	return oneOfField{set: value.IsSet(), null: value.Value() == nil}
}

func patch[T any](value graphql.Omittable[*T]) domain.Patch[T] {
	return domain.Patch[T]{Set: value.IsSet(), Value: value.Value()}
}

func createInput(input model.MainCreateInput) (domain.CreateInput, error) {
	selected := input.Satellite
	if err := oneOf(presence(selected.Tool), presence(selected.Table), presence(selected.Chair)); err != nil {
		return domain.CreateInput{}, err
	}
	output := domain.CreateInput{Title: input.Title}
	switch {
	case selected.Tool.IsSet():
		output.Kind = domain.Tools
		output.Description = selected.Tool.Value().Description1.Value()
	case selected.Table.IsSet():
		output.Kind = domain.Tables
		output.Description = selected.Table.Value().Description2.Value()
	case selected.Chair.IsSet():
		output.Kind = domain.Chairs
		output.Description = selected.Chair.Value().Description3.Value()
		output.ChairType = domain.ChairType(selected.Chair.Value().Type)
	}
	return output, nil
}

func updateInput(input model.MainUpdateInput) (domain.UpdateInput, error) {
	id, err := domain.ParseID(input.ID)
	if err != nil {
		return domain.UpdateInput{}, err
	}
	output := domain.UpdateInput{ID: id, Title: patch(input.Title)}
	if !input.Satellite.IsSet() {
		return output, nil
	}
	output.Satellite.Set = true
	selected := input.Satellite.Value()
	if selected == nil {
		return output, nil // The service rejects an explicitly null satellite patch.
	}
	if err := oneOf(presence(selected.Tool), presence(selected.Table), presence(selected.Chair)); err != nil {
		return domain.UpdateInput{}, err
	}
	satellite := &domain.SatellitePatch{}
	switch {
	case selected.Tool.IsSet():
		satellite.Kind = domain.Tools
		satellite.Description = patch(selected.Tool.Value().Description1)
	case selected.Table.IsSet():
		satellite.Kind = domain.Tables
		satellite.Description = patch(selected.Table.Value().Description2)
	case selected.Chair.IsSet():
		satellite.Kind = domain.Chairs
		satellite.Description = patch(selected.Chair.Value().Description3)
		chairType := selected.Chair.Value().Type
		satellite.Type.Set = chairType.IsSet()
		if chairType.Value() != nil {
			value := domain.ChairType(*chairType.Value())
			satellite.Type.Value = &value
		}
	}
	output.Satellite.Value = satellite
	return output, nil
}
