package postgres

import (
	"errors"
	"fmt"

	"github.com/99designs/gqlgen/graphql"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/go-playground/validator/v10"
)

var (
	ErrInvalidInput          = errors.New("invalid input")
	ErrNotFound              = errors.New("main was not found")
	ErrAlreadyDeleted        = errors.New("main is already deleted")
	ErrSatelliteTypeMismatch = errors.New("satellite type cannot be changed")
	inputValidator           = validator.New(validator.WithRequiredStructEnabled())
)

type ListInput struct {
	ID     *int64
	Limit  int `validate:"gte=1,lte=100"`
	Offset int `validate:"gte=0"`
}

func (input ListInput) Validate() error {
	if err := inputValidator.Struct(input); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	return nil
}

type ToolCreate struct {
	Description1 *string
}

type TableCreate struct {
	Description2 *string
}

type ChairCreate struct {
	Description3 *string
	Type         domain.ChairType
}

type ToolUpdate struct {
	Description1 graphql.Omittable[*string]
}

func (input ToolUpdate) Validate() error {
	if err := inputValidator.Var(input.Description1.IsSet(), "required"); err != nil {
		return fmt.Errorf("%w: tool update must change description1", ErrInvalidInput)
	}

	return nil
}

type TableUpdate struct {
	Description2 graphql.Omittable[*string]
}

func (input TableUpdate) Validate() error {
	if err := inputValidator.Var(input.Description2.IsSet(), "required"); err != nil {
		return fmt.Errorf("%w: table update must change description2", ErrInvalidInput)
	}

	return nil
}

type ChairUpdate struct {
	Description3 graphql.Omittable[*string]
	Type         graphql.Omittable[*domain.ChairType]
}

func (input ChairUpdate) Validate() error {
	if err := inputValidator.Var(input.Description3.IsSet() || input.Type.IsSet(), "required"); err != nil {
		return fmt.Errorf("%w: chair update must change at least one field", ErrInvalidInput)
	}

	if input.Type.IsSet() {
		if err := inputValidator.Var(input.Type.Value(), "required"); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidInput, err)
		}
	}

	return nil
}
