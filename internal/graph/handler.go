package graph

import (
	"context"
	"net/http"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph/generated"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/logger"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

const (
	codeExtension       = "code"
	badUserInput        = "BAD_USER_INPUT"
	internalServerError = "INTERNAL_SERVER_ERROR"
)

func NewHandler(reader mainReader, writer mainWriter) http.Handler {
	log := logger.Get("graphql")
	configuration := generated.Config{Resolvers: &Resolver{reader: reader, writer: writer}}
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
	server.AroundFields(presentResolverErrors)
	server.SetRecoverFunc(func(_ context.Context, recovered any) error {
		log.Error("panic while executing GraphQL request", nil, logger.Any("panic", recovered))

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
