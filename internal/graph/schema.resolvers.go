package graph

import (
	"context"
	"fmt"
	"strconv"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph/generated"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph/model"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/service"
)

func (r *Resolver) Mutation() generated.MutationResolver { return &mutationResolver{r} }

type mutationResolver struct{ *Resolver }

func (r *mutationResolver) Main(
	ctx context.Context,
	input model.MainMutationInput,
) (*model.MainMutationPayload, error) {
	switch {
	case input.Create.IsSet():
		value, err := r.service.Create(ctx, createInput(input.Create.Value()))
		if err != nil {
			return nil, fmt.Errorf("create Main: %w", err)
		}

		return &model.MainMutationPayload{Main: outputMain(value)}, nil
	case input.Update.IsSet():
		update := input.Update.Value()

		id, err := parseID(update.ID)
		if err != nil {
			return nil, err
		}

		value, err := r.service.Update(ctx, updateInput(id, update))
		if err != nil {
			return nil, fmt.Errorf("update Main: %w", err)
		}

		return &model.MainMutationPayload{Main: outputMain(value)}, nil
	default:
		id, err := parseID(input.Delete.Value().ID)
		if err != nil {
			return nil, err
		}

		deleted, err := r.service.Delete(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("delete Main: %w", err)
		}

		output := strconv.FormatInt(deleted, 10)

		return &model.MainMutationPayload{DeletedID: &output}, nil
	}
}

func (r *Resolver) Query() generated.QueryResolver { return &queryResolver{r} }

type queryResolver struct{ *Resolver }

func (r *queryResolver) Main(ctx context.Context, id *string, limit int32, offset int32) ([]*model.Main, error) {
	input := service.ListInput{Limit: int(limit), Offset: int(offset)}

	if id != nil {
		parsed, err := parseID(*id)
		if err != nil {
			return nil, err
		}

		input.ID = &parsed
	}

	values, err := r.service.List(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("list Main: %w", err)
	}

	output := make([]*model.Main, 0, len(values))
	for _, value := range values {
		output = append(output, outputMain(value))
	}

	return output, nil
}
