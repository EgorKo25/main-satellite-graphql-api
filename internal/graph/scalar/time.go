package scalar

import (
	"fmt"
	"time"

	"github.com/99designs/gqlgen/graphql"
)

func MarshalTime(value time.Time) graphql.Marshaler {
	return graphql.MarshalTime(value.UTC())
}

func UnmarshalTime(value any) (time.Time, error) {
	parsed, err := graphql.UnmarshalTime(value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse Time: %w", err)
	}

	return parsed.UTC(), nil
}
