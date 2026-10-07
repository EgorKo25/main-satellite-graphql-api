package graph

import (
	"context"

	"github.com/99designs/gqlgen/graphql"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
)

var satelliteTypes = map[domain.Kind]func() domain.SubObject{
	domain.Tools:  func() domain.SubObject { return new(domain.Tool) },
	domain.Tables: func() domain.SubObject { return new(domain.Table) },
	domain.Chairs: func() domain.SubObject { return new(domain.Chair) },
}

//go:generate go tool mockgen -destination=reader_mock_test.go -package=graph -mock_names=mainReader=MockMainReader . mainReader
type mainReader interface {
	List(context.Context, postgres.ListInput) ([]*domain.Main, error)
}

//go:generate go tool mockgen -destination=writer_mock_test.go -package=graph -mock_names=mainWriter=MockMainWriter . mainWriter
type mainWriter interface {
	Create(context.Context, string, map[string]any) (*domain.Main, error)
	Update(context.Context, int64, graphql.Omittable[*string], graphql.Omittable[map[string]any]) (*domain.Main, error)
	Delete(context.Context, int64) error
}

type Resolver struct {
	reader mainReader
	writer mainWriter
}
