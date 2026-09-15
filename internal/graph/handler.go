package graph

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph/generated"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/service"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

func NewHandler(service *service.Service, logger *slog.Logger) http.Handler {
	return newHandler(service, logger)
}

func newHandler(service mainService, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	schema := generated.NewExecutableSchema(generated.Config{Resolvers: &Resolver{service: service}})
	server := handler.New(schema)
	server.AddTransport(transport.Options{})
	server.AddTransport(transport.GET{})
	server.AddTransport(transport.POST{})
	server.Use(extension.Introspection{})
	server.Use(extension.FixedComplexityLimit(1000))
	server.AroundOperations(func(ctx context.Context, next graphql.OperationHandler) graphql.ResponseHandler {
		operation := graphql.GetOperationContext(ctx)
		for _, definition := range operation.Operation.VariableDefinitions {
			if err := validateOneOfVariable(schema.Schema(), definition.Type, operation.Variables[definition.Variable]); err != nil {
				return graphql.OneShot(&graphql.Response{Errors: gqlerror.List{{
					Message: err.Error(), Path: ast.Path{ast.PathName("variable"), ast.PathName(definition.Variable)},
					Extensions: map[string]any{"code": domain.BadUserInput},
				}}})
			}
		}
		return next(ctx)
	})
	server.SetErrorPresenter(errorPresenter(logger))
	server.SetRecoverFunc(func(ctx context.Context, recovered any) error {
		logger.ErrorContext(ctx, "panic while executing GraphQL request", "panic", recovered)
		return &domain.Error{Code: domain.InternalServerError, Message: "internal server error"}
	})
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(writer, request.Body, 1<<20)
		server.ServeHTTP(writer, request)
	})
}

func errorPresenter(logger *slog.Logger) graphql.ErrorPresenterFunc {
	return func(ctx context.Context, err error) *gqlerror.Error {
		presented := graphql.DefaultErrorPresenter(ctx, err)
		var applicationError *domain.Error
		if errors.As(err, &applicationError) && applicationError.Code != "INTERNAL_SERVER_ERROR" {
			presented.Message = applicationError.Message
			presented.Extensions = map[string]any{"code": applicationError.Code}
			return presented
		}
		var validationError *gqlerror.Error
		if applicationError == nil && errors.As(err, &validationError) {
			return presented
		}
		if applicationError != nil && applicationError.Cause != nil {
			logger.ErrorContext(ctx, "GraphQL operation failed", "error", err, "cause", applicationError.Cause)
		} else {
			logger.ErrorContext(ctx, "GraphQL operation failed", "error", err)
		}
		presented.Message = "internal server error"
		presented.Extensions = map[string]any{"code": "INTERNAL_SERVER_ERROR"}
		return presented
	}
}

// Errors from parsing/coercion are safe library errors. Unexpected service
// errors are marked explicitly so a gqlerror wrapper cannot expose their cause.
func serviceError(err error) error {
	var applicationError *domain.Error
	if errors.As(err, &applicationError) {
		return err
	}
	return &domain.Error{Code: domain.InternalServerError, Message: "internal server error", Cause: err}
}

// gqlparser v2.5.37 validates OneOf literals, but its variable coercion does not
// enforce OneOf cardinality. Check coerced maps before executing any resolver;
// this also prevents earlier mutation aliases from writing on invalid input.
func validateOneOfVariable(schema *ast.Schema, typ *ast.Type, value any) error {
	if value == nil {
		return nil // Non-null requirements are handled by gqlparser.
	}
	if typ.Elem != nil {
		if values, ok := value.([]any); ok {
			for _, item := range values {
				if err := validateOneOfVariable(schema, typ.Elem, item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	definition := schema.Types[typ.NamedType]
	if definition == nil || definition.Kind != ast.InputObject {
		return nil
	}
	fields, ok := value.(map[string]any)
	if !ok {
		return nil // Input object shape has already been validated by gqlparser.
	}
	if definition.Directives.ForName("oneOf") != nil {
		if len(fields) != 1 {
			return domain.BadInput(definition.Name + " requires exactly one supplied, non-null field")
		}
		for _, selected := range fields {
			if selected == nil {
				return domain.BadInput(definition.Name + " requires exactly one supplied, non-null field")
			}
		}
	}
	for name, item := range fields {
		if field := definition.Fields.ForName(name); field != nil {
			if err := validateOneOfVariable(schema, field.Type, item); err != nil {
				return err
			}
		}
	}
	return nil
}
