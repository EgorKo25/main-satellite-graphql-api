package graph

import (
	"context"
	"fmt"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph/generated"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph/model"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
	"github.com/samber/lo"
)

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
	payload, err := lo.Switch[bool, lo.Tuple2[*model.MainMutationPayload, error]](true).
		CaseF(input.Create.IsSet(), func() lo.Tuple2[*model.MainMutationPayload, error] {
			create := input.Create.Value()
			main, createErr := r.database.Create(ctx, create.Title, create.Satellite)

			return lo.T2(&model.MainMutationPayload{Main: main}, createErr)
		}).
		CaseF(input.Update.IsSet(), func() lo.Tuple2[*model.MainMutationPayload, error] {
			update := input.Update.Value()
			main, updateErr := r.database.Update(ctx, update.ID, update.Title, update.Satellite)

			return lo.T2(&model.MainMutationPayload{Main: main}, updateErr)
		}).
		CaseF(input.Delete.IsSet(), func() lo.Tuple2[*model.MainMutationPayload, error] {
			id := input.Delete.Value().ID

			return lo.T2(&model.MainMutationPayload{DeletedID: &id}, r.database.Delete(ctx, id))
		}).
		Default(lo.T2[*model.MainMutationPayload](nil, postgres.ErrInvalidInput)).Unpack()
	if err != nil {
		return nil, fmt.Errorf("mutate Main: %w", err)
	}

	return payload, nil
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
