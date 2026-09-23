package scalar

import (
	"strconv"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

func MarshalID(value int64) graphql.Marshaler {
	return graphql.MarshalID(strconv.FormatInt(value, 10))
}

func UnmarshalID(value any) (int64, error) {
	text, err := graphql.UnmarshalID(value)
	if err != nil {
		return 0, &gqlerror.Error{
			Message:    "invalid ID",
			Extensions: map[string]any{"code": "BAD_USER_INPUT"},
		}
	}

	identifier, err := strconv.ParseUint(text, 10, 63)
	if err != nil || identifier == 0 {
		return 0, &gqlerror.Error{
			Message:    "invalid ID",
			Extensions: map[string]any{"code": "BAD_USER_INPUT"},
		}
	}

	return int64(identifier), nil
}
