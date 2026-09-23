package graph

import (
	"context"
	"fmt"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph/generated"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph/model"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
)

func (r *Resolver) Mutation() generated.MutationResolver { return &mutationResolver{r} }

type mutationResolver struct{ *Resolver }

func (r *mutationResolver) Main(
	ctx context.Context,
	input model.MainMutationInput,
) (*model.MainMutationPayload, error) {
	var (
		main *domain.Main
		err  error
	)

	switch {
	case input.Create.IsSet():
		main, err = r.create(ctx, input.Create.Value())
	case input.Update.IsSet():
		main, err = r.update(ctx, input.Update.Value())
	case input.Delete.IsSet():
		id := input.Delete.Value().ID
		if err = r.database.Delete(ctx, id); err != nil {
			return nil, fmt.Errorf("delete Main: %w", err)
		}

		return &model.MainMutationPayload{DeletedID: &id}, nil
	default:
		return nil, fmt.Errorf("%w: mutation requires one selected operation", postgres.ErrInvalidInput)
	}

	if err != nil {
		return nil, fmt.Errorf("mutate Main: %w", err)
	}

	return &model.MainMutationPayload{Main: main}, nil
}

func (r *Resolver) Query() generated.QueryResolver { return &queryResolver{r} }

type queryResolver struct{ *Resolver }

func (r *queryResolver) Main(ctx context.Context, id *int64, limit int32, offset int32) ([]*domain.Main, error) {
	items, err := r.database.List(ctx, postgres.ListInput{ID: id, Limit: int(limit), Offset: int(offset)})
	if err != nil {
		return nil, fmt.Errorf("list Main: %w", err)
	}

	return items, nil
}
