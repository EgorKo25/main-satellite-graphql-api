package domain

import (
	"errors"
	"strconv"
	"time"
)

type Kind string

const (
	Tools  Kind = "tools"
	Tables Kind = "tables"
	Chairs Kind = "chairs"
)

type ChairType string

const (
	ABC ChairType = "abc"
	CDE ChairType = "cde"
)

type Main struct {
	ID        int64
	Title     string
	SubID     int64
	SubObj    Kind
	Satellite Satellite
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

type Satellite struct {
	ID          int64
	MainID      int64
	Kind        Kind
	Description *string
	Type        ChairType
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

type Patch[T any] struct {
	Set   bool
	Value *T
}

type CreateInput struct {
	Title       string
	Kind        Kind
	Description *string
	ChairType   ChairType
}

type SatellitePatch struct {
	Kind        Kind
	Description Patch[string]
	Type        Patch[ChairType]
}

type UpdateInput struct {
	ID        int64
	Title     Patch[string]
	Satellite Patch[SatellitePatch]
}

type ListInput struct {
	ID     *int64
	Limit  int
	Offset int
}

const (
	BadUserInput          = "BAD_USER_INPUT"
	NotFound              = "NOT_FOUND"
	AlreadyDeleted        = "ALREADY_DELETED"
	SatelliteTypeMismatch = "SATELLITE_TYPE_MISMATCH"
	InternalServerError   = "INTERNAL_SERVER_ERROR"
)

type Error struct {
	Code    string
	Message string
	Cause   error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }

func ErrorCode(err error) string {
	var domainError *Error
	if errors.As(err, &domainError) {
		return domainError.Code
	}
	return InternalServerError
}

func BadInput(message string) error { return &Error{Code: BadUserInput, Message: message} }

func ParseID(value string) (int64, error) {
	if value == "" {
		return 0, BadInput("id must be a positive BIGINT")
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, BadInput("id must be a positive BIGINT")
		}
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, BadInput("id must be a positive BIGINT")
	}
	return id, nil
}

func ValidateList(input ListInput) error {
	if input.ID != nil && *input.ID <= 0 {
		return BadInput("id must be positive")
	}
	if input.Limit < 1 || input.Limit > 100 {
		return BadInput("limit must be between 1 and 100")
	}
	if input.Offset < 0 {
		return BadInput("offset must be nonnegative")
	}
	return nil
}

func ValidateCreate(input CreateInput) error {
	if !validKind(input.Kind) {
		return BadInput("a satellite type must be selected")
	}
	if input.Kind == Chairs {
		if !validChairType(input.ChairType) {
			return BadInput("chair.type must be abc or cde")
		}
	} else if input.ChairType != "" {
		return BadInput("type is only available for chairs")
	}
	return nil
}

func ValidateUpdate(input UpdateInput) error {
	if input.ID <= 0 {
		return BadInput("id must be positive")
	}
	if !input.Title.Set && !input.Satellite.Set {
		return BadInput("update must change at least one field")
	}
	if input.Title.Set && input.Title.Value == nil {
		return BadInput("title cannot be null")
	}
	if !input.Satellite.Set {
		return nil
	}
	if input.Satellite.Value == nil {
		return BadInput("satellite cannot be null")
	}
	satellite := input.Satellite.Value
	if !validKind(satellite.Kind) {
		return BadInput("a satellite type must be selected")
	}
	if !satellite.Description.Set && !satellite.Type.Set {
		return BadInput("satellite patch must change at least one field")
	}
	if satellite.Type.Set {
		if satellite.Kind != Chairs {
			return BadInput("type is only available for chairs")
		}
		if satellite.Type.Value == nil {
			return BadInput("chair.type cannot be null")
		}
		if !validChairType(*satellite.Type.Value) {
			return BadInput("chair.type must be abc or cde")
		}
	}
	return nil
}

func validKind(kind Kind) bool            { return kind == Tools || kind == Tables || kind == Chairs }
func validChairType(value ChairType) bool { return value == ABC || value == CDE }
