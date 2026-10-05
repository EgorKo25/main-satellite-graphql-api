package graph

import (
	"context"
	"fmt"

	"github.com/99designs/gqlgen/graphql"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph/model"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
)

//go:generate go tool mockgen -destination=reader_mock_test.go -package=graph -mock_names=mainReader=MockMainReader . mainReader
type mainReader interface {
	List(context.Context, postgres.ListInput) ([]*domain.Main, error)
}

//go:generate go tool mockgen -destination=writer_mock_test.go -package=graph -mock_names=mainWriter=MockMainWriter . mainWriter
type mainWriter interface {
	CreateTool(context.Context, string, postgres.ToolCreate) (*domain.Main, error)
	CreateTable(context.Context, string, postgres.TableCreate) (*domain.Main, error)
	CreateChair(context.Context, string, postgres.ChairCreate) (*domain.Main, error)
	UpdateTool(context.Context, int64, graphql.Omittable[*string], postgres.ToolUpdate) (*domain.Main, error)
	UpdateTable(context.Context, int64, graphql.Omittable[*string], postgres.TableUpdate) (*domain.Main, error)
	UpdateChair(context.Context, int64, graphql.Omittable[*string], postgres.ChairUpdate) (*domain.Main, error)
	UpdateMain(context.Context, int64, graphql.Omittable[*string]) (*domain.Main, error)
	Delete(context.Context, int64) error
}

type Resolver struct {
	reader mainReader
	writer mainWriter
}

func (r *mutationResolver) create(ctx context.Context, input *model.MainCreateInput) (*domain.Main, error) {
	satellite := input.Satellite

	switch {
	case satellite.Tool.IsSet():
		return r.database.CreateTool(ctx, input.Title, *satellite.Tool.Value())
	case satellite.Table.IsSet():
		return r.database.CreateTable(ctx, input.Title, *satellite.Table.Value())
	case satellite.Chair.IsSet():
		return r.database.CreateChair(ctx, input.Title, *satellite.Chair.Value())
	default:
		return nil, fmt.Errorf("%w: satellite requires one selected type", postgres.ErrInvalidInput)
	}
}

func (r *mutationResolver) update(ctx context.Context, input *model.MainUpdateInput) (*domain.Main, error) {
	if !input.Satellite.IsSet() {
		return r.database.UpdateMain(ctx, input.ID, input.Title)
	}

	satellite := input.Satellite.Value()
	if satellite == nil {
		return nil, fmt.Errorf("%w: satellite cannot be null", postgres.ErrInvalidInput)
	}

	switch {
	case satellite.Tool.IsSet():
		return r.database.UpdateTool(ctx, input.ID, input.Title, *satellite.Tool.Value())
	case satellite.Table.IsSet():
		return r.database.UpdateTable(ctx, input.ID, input.Title, *satellite.Table.Value())
	case satellite.Chair.IsSet():
		return r.database.UpdateChair(ctx, input.ID, input.Title, *satellite.Chair.Value())
	default:
		return nil, fmt.Errorf("%w: satellite requires one selected type", postgres.ErrInvalidInput)
	}
}
