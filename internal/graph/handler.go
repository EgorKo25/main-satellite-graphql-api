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
	"github.com/EgorKo25/main-satellite-graphql-api/internal/service"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

const (
	codeExtension       = "code"
	badUserInput        = "BAD_USER_INPUT"
	internalServerError = "INTERNAL_SERVER_ERROR"
)

func NewHandler(operations mainService, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	configuration := generated.Config{Resolvers: &Resolver{service: operations}}
	configuration.Complexity.Query.Main = func(childComplexity int, _ *string, limit, _ int32) int {
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
			if err := validateOneOfVariable(
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

		code, message := internalServerError, "internal server error"

		switch {
		case errors.Is(err, service.ErrInvalidInput):
			code, message = badUserInput, "invalid input"
		case errors.Is(err, service.ErrNotFound):
			code, message = "NOT_FOUND", "Main was not found"
		case errors.Is(err, service.ErrAlreadyDeleted):
			code, message = "ALREADY_DELETED", "Main is already deleted"
		case errors.Is(err, service.ErrSatelliteTypeMismatch):
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

func validateOneOfVariable(schema *ast.Schema, typ *ast.Type, value any) error {
	definition := schema.Types[typ.NamedType]
	if definition == nil || definition.Kind != ast.InputObject {
		return nil
	}

	fields, ok := value.(map[string]any)
	if !ok {
		return nil
	}

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
		if err := validateOneOfVariable(schema, field.Type, item); err != nil {
			return err
		}
	}

	return nil
}
