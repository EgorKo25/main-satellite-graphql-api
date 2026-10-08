package graph

import (
	"context"
	"errors"

	"github.com/99designs/gqlgen/graphql"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/logger"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
	"github.com/samber/lo"
)

func presentResolverErrors(ctx context.Context, next graphql.Resolver) (any, error) {
	result, err := next(ctx)
	if err == nil {
		return result, nil
	}

	code, message := lo.Switch[bool, lo.Tuple2[string, string]](true).
		Case(errors.Is(err, postgres.ErrInvalidInput), lo.T2(badUserInput, "invalid input")).
		Case(errors.Is(err, postgres.ErrNotFound), lo.T2("NOT_FOUND", "Main was not found")).
		Case(errors.Is(err, postgres.ErrAlreadyDeleted), lo.T2("ALREADY_DELETED", "Main is already deleted")).
		Case(errors.Is(err, postgres.ErrSatelliteTypeMismatch),
			lo.T2("SATELLITE_TYPE_MISMATCH", "satellite type cannot be changed")).
		DefaultF(func() lo.Tuple2[string, string] {
			logger.Get("graphql").Error("GraphQL operation failed", err)

			return lo.T2(internalServerError, "internal server error")
		}).Unpack()

	presented := graphql.DefaultErrorPresenter(ctx, err)
	presented.Message = message
	presented.Extensions = map[string]any{codeExtension: code}

	return result, presented
}
