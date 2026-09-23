package graph

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph/generated"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

const (
	codeExtension       = "code"
	badUserInput        = "BAD_USER_INPUT"
	internalServerError = "INTERNAL_SERVER_ERROR"
)

func NewHandler(database mainDatabase, logger *slog.Logger) http.Handler {
	configuration := generated.Config{Resolvers: &Resolver{database: database}}
	configuration.Complexity.Query.Main = func(childComplexity int, _ *int64, limit, _ int32) int {
		if limit < 1 || limit > 100 {
			return 1 + childComplexity
		}

		return 1 + int(limit)*max(1, childComplexity)
	}
	schema := generated.NewExecutableSchema(configuration)
	server := handler.New(schema)
	server.AddTransport(transport.Options{})
	server.AddTransport(transport.GET{})
	server.AddTransport(transport.POST{})
	server.Use(extension.Introspection{})
	server.Use(extension.FixedComplexityLimit(10000))
	server.AroundOperations(func(ctx context.Context, next graphql.OperationHandler) graphql.ResponseHandler {
		operation := graphql.GetOperationContext(ctx)
		for _, definition := range operation.Operation.VariableDefinitions {
			if err := validateVariable(
				schema.Schema(),
				definition.Type,
				operation.Variables[definition.Variable],
			); err != nil {
				return graphql.OneShot(&graphql.Response{Errors: gqlerror.List{{
					Message:    err.Error(),
					Path:       ast.Path{ast.PathName("variable"), ast.PathName(definition.Variable)},
					Extensions: map[string]any{codeExtension: badUserInput},
				}}})
			}
		}

		return next(ctx)
	})
	server.AroundFields(presentResolverErrors(logger))
	server.SetRecoverFunc(func(ctx context.Context, recovered any) error {
		logger.ErrorContext(ctx, "panic while executing GraphQL request", "panic", recovered)

		return &gqlerror.Error{
			Message:    "internal server error",
			Extensions: map[string]any{codeExtension: internalServerError},
		}
	})

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(writer, request.Body, 1<<20)
		server.ServeHTTP(writer, request)
	})
}

func presentResolverErrors(logger *slog.Logger) graphql.FieldMiddleware {
	return func(ctx context.Context, next graphql.Resolver) (any, error) {
		result, err := next(ctx)
		if err == nil {
			return result, nil
		}

		var (
			code    = internalServerError
			message = "internal server error"
		)

		switch {
		case errors.Is(err, postgres.ErrInvalidInput):
			code, message = badUserInput, "invalid input"
		case errors.Is(err, postgres.ErrNotFound):
			code, message = "NOT_FOUND", "Main was not found"
		case errors.Is(err, postgres.ErrAlreadyDeleted):
			code, message = "ALREADY_DELETED", "Main is already deleted"
		case errors.Is(err, postgres.ErrSatelliteTypeMismatch):
			code, message = "SATELLITE_TYPE_MISMATCH", "satellite type cannot be changed"
		default:
			logger.ErrorContext(ctx, "GraphQL operation failed", "error", err)
		}

		presented := graphql.DefaultErrorPresenter(ctx, err)
		presented.Message = message
		presented.Extensions = map[string]any{codeExtension: code}

		return result, presented
	}
}

func validateVariable(schema *ast.Schema, typ *ast.Type, value any) error {
	if value == nil {
		return nil
	}

	definition := schema.Types[typ.NamedType]
	if definition == nil {
		return nil
	}

	switch definition.Kind {
	case ast.Enum:
		name, ok := value.(string)
		if !ok || definition.EnumValues.ForName(name) == nil {
			return fmt.Errorf("invalid value for enum %s", definition.Name)
		}
	case ast.InputObject:
		fields, ok := value.(map[string]any)
		if ok {
			return validateInputVariable(schema, definition, fields)
		}
	}

	return nil
}

func validateInputVariable(schema *ast.Schema, definition *ast.Definition, fields map[string]any) error {
	if definition.Directives.ForName("oneOf") != nil {
		if len(fields) != 1 {
			return fmt.Errorf("%s requires exactly one supplied, non-null field", definition.Name)
		}

		for _, selected := range fields {
			if selected == nil {
				return fmt.Errorf("%s requires exactly one supplied, non-null field", definition.Name)
			}
		}
	}

	for name, item := range fields {
		field := definition.Fields.ForName(name)
		if field == nil {
			return fmt.Errorf("unknown field %s in %s", name, definition.Name)
		}

		if err := validateVariable(schema, field.Type, item); err != nil {
			return err
		}
	}

	return nil
}
