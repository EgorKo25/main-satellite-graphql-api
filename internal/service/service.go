package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
	"github.com/jackc/pgx/v5"
)

func New(store *postgres.Store) *Service { return &Service{store: store} }

type Service struct{ store *postgres.Store }

func (s *Service) List(ctx context.Context, input domain.ListInput) ([]*domain.Main, error) {
	if err := domain.ValidateList(input); err != nil {
		return nil, err
	}
	result, err := s.store.List(ctx, input)
	if err != nil {
		return nil, internal("list Main", err)
	}
	return result, nil
}

func (s *Service) Create(ctx context.Context, input domain.CreateInput) (*domain.Main, error) {
	if err := domain.ValidateCreate(input); err != nil {
		return nil, err
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return nil, internal("begin create", err)
	}
	defer rollback(tx)
	subID, err := s.store.NextSatelliteID(ctx, tx, input.Kind)
	if err != nil {
		return nil, internal("allocate satellite id", err)
	}
	now, err := s.store.Now(ctx, tx)
	if err != nil {
		return nil, internal("get create timestamp", err)
	}
	mainID, err := s.store.InsertMain(ctx, tx, input, subID, now)
	if err != nil {
		return nil, internal("insert Main", err)
	}
	if err = s.store.InsertSatellite(ctx, tx, input, mainID, subID, now); err != nil {
		return nil, internal("insert satellite", err)
	}
	result, err := s.store.ReadTx(ctx, tx, mainID)
	if err != nil {
		return nil, internal("read created aggregate", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, internal("commit create", err)
	}
	return result, nil
}

func (s *Service) Update(ctx context.Context, input domain.UpdateInput) (*domain.Main, error) {
	if err := domain.ValidateUpdate(input); err != nil {
		return nil, err
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return nil, internal("begin update", err)
	}
	defer rollback(tx)
	main, err := s.lockActive(ctx, tx, input.ID, false)
	if err != nil {
		return nil, err
	}
	if input.Satellite.Set && input.Satellite.Value.Kind != main.SubObj {
		return nil, &domain.Error{Code: domain.SatelliteTypeMismatch, Message: "satellite type cannot be changed"}
	}
	if input.Title.Set {
		main.Title = *input.Title.Value
	}
	if input.Satellite.Set {
		patch := input.Satellite.Value
		if patch.Description.Set {
			main.Satellite.Description = patch.Description.Value
		}
		if patch.Type.Set {
			main.Satellite.Type = *patch.Type.Value
		}
	}
	now, err := s.store.Now(ctx, tx)
	if err != nil {
		return nil, internal("get update timestamp", err)
	}
	if err = s.store.UpdateMain(ctx, tx, main, now); err != nil {
		return nil, internal("update Main", err)
	}
	if input.Satellite.Set {
		if err = s.store.UpdateSatellite(ctx, tx, main.Satellite, now); err != nil {
			return nil, internal("update satellite", err)
		}
	}
	result, err := s.store.ReadTx(ctx, tx, main.ID)
	if err != nil {
		return nil, internal("read updated aggregate", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, internal("commit update", err)
	}
	return result, nil
}

func (s *Service) Delete(ctx context.Context, id int64) (int64, error) {
	if id <= 0 {
		return 0, domain.BadInput("id must be positive")
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return 0, internal("begin delete", err)
	}
	defer rollback(tx)
	main, err := s.lockActive(ctx, tx, id, true)
	if err != nil {
		return 0, err
	}
	now, err := s.store.Now(ctx, tx)
	if err != nil {
		return 0, internal("get delete timestamp", err)
	}
	if err = s.store.DeleteMain(ctx, tx, id, now); err != nil {
		return 0, internal("soft delete Main", err)
	}
	if err = s.store.DeleteSatellite(ctx, tx, main.Satellite, now); err != nil {
		return 0, internal("soft delete satellite", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, internal("commit delete", err)
	}
	return id, nil
}

func (s *Service) lockActive(ctx context.Context, tx pgx.Tx, id int64, deleting bool) (*domain.Main, error) {
	main, err := s.store.LockMain(ctx, tx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &domain.Error{Code: domain.NotFound, Message: "Main was not found"}
	}
	if err != nil {
		return nil, internal("lock Main", err)
	}
	if main.DeletedAt != nil {
		if deleting {
			return nil, &domain.Error{Code: domain.AlreadyDeleted, Message: "Main is already deleted"}
		}
		return nil, &domain.Error{Code: domain.NotFound, Message: "Main was not found"}
	}
	main, err = s.store.LockSatellite(ctx, tx, main)
	if err != nil {
		return nil, internal("lock aggregate", err)
	}
	return main, nil
}

func internal(operation string, err error) error {
	return &domain.Error{Code: domain.InternalServerError, Message: "internal server error", Cause: fmt.Errorf("%s: %w", operation, err)}
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
