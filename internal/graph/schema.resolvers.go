package graph

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph/generated"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph/model"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
)

func (r *Resolver) Main() generated.MainResolver { return &mainResolver{} }

type mainResolver struct{}

func (r *mainResolver) Satellite(_ context.Context, obj *domain.Main) (domain.SubObject, error) {
	factory, ok := satelliteTypes[obj.SubObj]
	if !ok {
		return nil, fmt.Errorf("unknown satellite kind %q", obj.SubObj)
	}

	satellite := factory()
	if err := json.Unmarshal(obj.SatelliteData, &satellite); err != nil {
		return nil, fmt.Errorf("decode %s: %w", obj.SubObj, err)
	}

	if satellite == nil {
		return nil, fmt.Errorf("main %d has no satellite data", obj.ID)
	}

	return satellite, nil
}

func (r *Resolver) Mutation() generated.MutationResolver {
	return &mutationResolver{database: r.writer}
}

type mutationResolver struct {
	database mainWriter
}

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
		create := input.Create.Value()
		main, err = r.database.Create(ctx, create.Title, create.Satellite)
	case input.Update.IsSet():
		update := input.Update.Value()
		main, err = r.database.Update(ctx, update.ID, update.Title, update.Satellite)
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

func (r *Resolver) Query() generated.QueryResolver { return &queryResolver{database: r.reader} }

type queryResolver struct {
	database mainReader
}

func (r *queryResolver) Main(ctx context.Context, id *int64, limit int32, offset int32) ([]*domain.Main, error) {
	items, err := r.database.List(ctx, postgres.ListInput{ID: id, Limit: int(limit), Offset: int(offset)})
	if err != nil {
		return nil, fmt.Errorf("list Main: %w", err)
	}

	return items, nil
}
