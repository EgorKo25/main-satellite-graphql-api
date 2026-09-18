package graph

import (
	"fmt"
	"strconv"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph/model"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/service"
)

func outputMain(value *domain.Main) *model.Main {
	if value == nil {
		return nil
	}

	var output model.Satellite

	switch satellite := value.Satellite.(type) {
	case *domain.Tool:
		output = &model.Tool{
			ID:           strconv.FormatInt(satellite.ID, 10),
			Description1: satellite.Description1,
			CreatedAt:    satellite.CreatedAt.UTC(),
			UpdatedAt:    satellite.UpdatedAt.UTC(),
			DeletedAt:    utcPointer(satellite.DeletedAt),
		}
	case *domain.Table:
		output = &model.Table{
			ID:           strconv.FormatInt(satellite.ID, 10),
			Description2: satellite.Description2,
			CreatedAt:    satellite.CreatedAt.UTC(),
			UpdatedAt:    satellite.UpdatedAt.UTC(),
			DeletedAt:    utcPointer(satellite.DeletedAt),
		}
	case *domain.Chair:
		output = &model.Chair{
			ID:           strconv.FormatInt(satellite.ID, 10),
			Description3: satellite.Description3,
			Type:         model.ChairType(satellite.Type),
			CreatedAt:    satellite.CreatedAt.UTC(),
			UpdatedAt:    satellite.UpdatedAt.UTC(),
			DeletedAt:    utcPointer(satellite.DeletedAt),
		}
	}

	return &model.Main{
		ID:        strconv.FormatInt(value.ID, 10),
		Title:     value.Title,
		Satellite: output,
		CreatedAt: value.CreatedAt.UTC(),
		UpdatedAt: value.UpdatedAt.UTC(),
		DeletedAt: utcPointer(value.DeletedAt),
	}
}

func utcPointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}

	utc := value.UTC()

	return &utc
}

func parseID(value string) (int64, error) {
	id, err := strconv.ParseUint(value, 10, 63)
	if err != nil {
		return 0, fmt.Errorf("%w: parse id: %w", service.ErrInvalidInput, err)
	}

	return int64(id), nil
}

func createInput(input *model.MainCreateInput) service.CreateInput {
	output := service.CreateInput{Title: input.Title}
	switch {
	case input.Satellite.Tool.IsSet():
		output.Satellite.Tool = &service.ToolCreateInput{
			Description1: input.Satellite.Tool.Value().Description1.Value(),
		}
	case input.Satellite.Table.IsSet():
		output.Satellite.Table = &service.TableCreateInput{
			Description2: input.Satellite.Table.Value().Description2.Value(),
		}
	case input.Satellite.Chair.IsSet():
		chair := input.Satellite.Chair.Value()
		output.Satellite.Chair = &service.ChairCreateInput{
			Description3: chair.Description3.Value(),
			Type:         domain.ChairType(chair.Type),
		}
	}

	return output
}

func updateInput(id int64, input *model.MainUpdateInput) service.UpdateInput {
	output := service.UpdateInput{
		ID:           id,
		Title:        input.Title.Value(),
		TitleSet:     input.Title.IsSet(),
		SatelliteSet: input.Satellite.IsSet(),
	}
	if selected := input.Satellite.Value(); selected != nil {
		output.Satellite = &service.SatelliteUpdateInput{}

		switch {
		case selected.Tool.IsSet():
			tool := selected.Tool.Value()
			output.Satellite.Tool = &service.ToolUpdateInput{
				Description1:    tool.Description1.Value(),
				Description1Set: tool.Description1.IsSet(),
			}
		case selected.Table.IsSet():
			table := selected.Table.Value()
			output.Satellite.Table = &service.TableUpdateInput{
				Description2:    table.Description2.Value(),
				Description2Set: table.Description2.IsSet(),
			}
		case selected.Chair.IsSet():
			chair := selected.Chair.Value()

			output.Satellite.Chair = &service.ChairUpdateInput{
				Description3:    chair.Description3.Value(),
				Description3Set: chair.Description3.IsSet(),
				TypeSet:         chair.Type.IsSet(),
			}
			if chair.Type.Value() != nil {
				chairType := domain.ChairType(*chair.Type.Value())
				output.Satellite.Chair.Type = &chairType
			}
		}
	}

	return output
}
