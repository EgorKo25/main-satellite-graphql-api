package service

import (
	"fmt"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/go-playground/validator/v10"
)

var inputValidator = validator.New(validator.WithRequiredStructEnabled())

type ListInput struct {
	ID     *int64 `validate:"omitnil,gt=0"`
	Limit  int    `validate:"gte=1,lte=100"`
	Offset int    `validate:"gte=0"`
}

func (input ListInput) Validate() error {
	if err := inputValidator.Struct(input); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	return nil
}

type CreateInput struct {
	Title     string
	Satellite SatelliteCreateInput
}

func (input CreateInput) Validate() error {
	if err := inputValidator.Struct(input); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	return nil
}

type SatelliteCreateInput struct {
	Tool  *ToolCreateInput  `validate:"required_without_all=Table Chair,excluded_with=Table Chair"`
	Table *TableCreateInput `validate:"required_without_all=Tool Chair,excluded_with=Tool Chair"`
	Chair *ChairCreateInput `validate:"required_without_all=Tool Table,excluded_with=Tool Table"`
}

type ToolCreateInput struct {
	Description1 *string
}

type TableCreateInput struct {
	Description2 *string
}

type ChairCreateInput struct {
	Description3 *string
	Type         domain.ChairType `validate:"oneof=abc cde"`
}

type UpdateInput struct {
	ID           int64                 `validate:"gt=0"`
	Title        *string               `validate:"required_if=TitleSet true"`
	TitleSet     bool                  `validate:"required_without=SatelliteSet"`
	Satellite    *SatelliteUpdateInput `validate:"required_if=SatelliteSet true"`
	SatelliteSet bool
}

func (input UpdateInput) Validate() error {
	if err := inputValidator.Struct(input); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	return nil
}

type SatelliteUpdateInput struct {
	Tool  *ToolUpdateInput  `validate:"required_without_all=Table Chair,excluded_with=Table Chair"`
	Table *TableUpdateInput `validate:"required_without_all=Tool Chair,excluded_with=Tool Chair"`
	Chair *ChairUpdateInput `validate:"required_without_all=Tool Table,excluded_with=Tool Table"`
}

func (input SatelliteUpdateInput) apply(satellite domain.SubObject) error {
	switch satellite := satellite.(type) {
	case *domain.Tool:
		if input.Tool == nil {
			return ErrSatelliteTypeMismatch
		}

		satellite.Description1 = input.Tool.Description1
	case *domain.Table:
		if input.Table == nil {
			return ErrSatelliteTypeMismatch
		}

		satellite.Description2 = input.Table.Description2
	case *domain.Chair:
		if input.Chair == nil {
			return ErrSatelliteTypeMismatch
		}

		if input.Chair.Description3Set {
			satellite.Description3 = input.Chair.Description3
		}

		if input.Chair.TypeSet {
			satellite.Type = *input.Chair.Type
		}
	default:
		return fmt.Errorf("update satellite: unexpected type %T", satellite)
	}

	return nil
}

type ToolUpdateInput struct {
	Description1    *string
	Description1Set bool `validate:"required"`
}

type TableUpdateInput struct {
	Description2    *string
	Description2Set bool `validate:"required"`
}

type ChairUpdateInput struct {
	Description3    *string
	Description3Set bool              `validate:"required_without=TypeSet"`
	Type            *domain.ChairType `validate:"required_if=TypeSet true,omitnil,oneof=abc cde"`
	TypeSet         bool
}
