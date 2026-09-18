package graph

import (
	"context"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/service"
)

//go:generate go tool mockgen -source=resolver.go -destination=service_mock_test.go -package=graph -mock_names=mainService=MockMainService
type mainService interface {
	List(context.Context, service.ListInput) ([]*domain.Main, error)
	Create(context.Context, service.CreateInput) (*domain.Main, error)
	Update(context.Context, service.UpdateInput) (*domain.Main, error)
	Delete(context.Context, int64) (int64, error)
}

type Resolver struct {
	service mainService
}
