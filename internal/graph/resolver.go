package graph

import (
	"context"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
)

// mainService is the GraphQL boundary. PostgreSQL transactions belong to the service.
type mainService interface {
	List(context.Context, domain.ListInput) ([]*domain.Main, error)
	Create(context.Context, domain.CreateInput) (*domain.Main, error)
	Update(context.Context, domain.UpdateInput) (*domain.Main, error)
	Delete(context.Context, int64) (int64, error)
}

type Resolver struct {
	service mainService
}
