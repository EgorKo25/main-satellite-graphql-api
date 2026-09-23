package graph

import (
	"context"
	"fmt"

	"github.com/99designs/gqlgen/graphql"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph/model"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
)

//go:generate go tool mockgen -source=resolver.go -destination=database_mock_test.go -package=graph -mock_names=mainDatabase=MockMainDatabase
type mainDatabase interface {
	List(context.Context, postgres.ListInput) ([]*domain.Main, error)
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
	database mainDatabase
}

func (r *Resolver) create(ctx context.Context, input *model.MainCreateInput) (*domain.Main, error) {
	var (
		main *domain.Main
		err  error
	)

	switch {
	case input.Satellite.Tool.IsSet():
		main, err = r.database.CreateTool(ctx, input.Title, *input.Satellite.Tool.Value())
	case input.Satellite.Table.IsSet():
		main, err = r.database.CreateTable(ctx, input.Title, *input.Satellite.Table.Value())
	case input.Satellite.Chair.IsSet():
		main, err = r.database.CreateChair(ctx, input.Title, *input.Satellite.Chair.Value())
	default:
		return nil, fmt.Errorf("%w: satellite requires one selected type", postgres.ErrInvalidInput)
	}

	if err != nil {
		return nil, fmt.Errorf("create Main: %w", err)
	}

	return main, nil
}

func (r *Resolver) update(ctx context.Context, input *model.MainUpdateInput) (*domain.Main, error) {
	if !input.Satellite.IsSet() {
		main, err := r.database.UpdateMain(ctx, input.ID, input.Title)
		if err != nil {
			return nil, fmt.Errorf("update Main title: %w", err)
		}

		return main, nil
	}

	satellite := input.Satellite.Value()
	if satellite == nil {
		return nil, fmt.Errorf("%w: satellite cannot be null", postgres.ErrInvalidInput)
	}

	var (
		main *domain.Main
		err  error
	)

	switch {
	case satellite.Tool.IsSet():
		main, err = r.database.UpdateTool(ctx, input.ID, input.Title, *satellite.Tool.Value())
	case satellite.Table.IsSet():
		main, err = r.database.UpdateTable(ctx, input.ID, input.Title, *satellite.Table.Value())
	case satellite.Chair.IsSet():
		main, err = r.database.UpdateChair(ctx, input.ID, input.Title, *satellite.Chair.Value())
	default:
		return nil, fmt.Errorf("%w: satellite requires one selected type", postgres.ErrInvalidInput)
	}

	if err != nil {
		return nil, fmt.Errorf("update Main satellite: %w", err)
	}

	return main, nil
}
