package postgres

import (
	"errors"
	"fmt"

	"github.com/99designs/gqlgen/graphql"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
)

var (
	ErrInvalidInput          = errors.New("invalid input")
	ErrNotFound              = errors.New("main was not found")
	ErrAlreadyDeleted        = errors.New("main is already deleted")
	ErrSatelliteTypeMismatch = errors.New("satellite type cannot be changed")
)

type ListInput struct {
	ID     *int64
	Limit  int `validate:"gte=1,lte=100"`
	Offset int `validate:"gte=0"`
}

func satelliteInput(satellite map[string]any) (domain.Kind, map[string]any, error) {
	for branch, value := range satellite {
		kind, known := satelliteKinds[branch]
		if !known {
			return "", nil, fmt.Errorf("%w: unknown satellite kind %q", ErrInvalidInput, branch)
		}

		fields, valid := value.(map[string]any)
		if !valid || fields == nil {
			return "", nil, fmt.Errorf("%w: satellite cannot be null", ErrInvalidInput)
		}

		return kind, fields, nil
	}

	return "", nil, fmt.Errorf("%w: satellite requires one selected type", ErrInvalidInput)
}

func updateInput(
	title graphql.Omittable[*string],
	satellite graphql.Omittable[map[string]any],
) (domain.Kind, map[string]any, error) {
	if title.IsSet() && title.Value() == nil {
		return "", nil, fmt.Errorf("%w: title cannot be null", ErrInvalidInput)
	}

	if !satellite.IsSet() {
		if !title.IsSet() {
			return "", nil, fmt.Errorf("%w: update must change at least one field", ErrInvalidInput)
		}

		return "", nil, nil
	}

	kind, fields, err := satelliteInput(satellite.Value())
	if err != nil {
		return "", nil, err
	}

	if len(fields) == 0 {
		return "", nil, fmt.Errorf("%w: satellite update must change at least one field", ErrInvalidInput)
	}

	if value, present := fields["type"]; present {
		chairType, ok := value.(*domain.ChairType)
		if !ok || chairType == nil {
			return "", nil, fmt.Errorf("%w: chair type cannot be null", ErrInvalidInput)
		}
	}

	return kind, fields, nil
}
