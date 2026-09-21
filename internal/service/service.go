package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidInput          = errors.New("invalid input")
	ErrNotFound              = errors.New("main was not found")
	ErrAlreadyDeleted        = errors.New("main is already deleted")
	ErrSatelliteTypeMismatch = errors.New("satellite type cannot be changed")
)

func New(store *postgres.Store) *Service {
	return &Service{store: store}
}

type Service struct {
	store *postgres.Store
}

func (s *Service) List(ctx context.Context, input ListInput) ([]*domain.Main, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	result, err := s.store.List(ctx, input.ID, input.Limit, input.Offset)
	if err != nil {
		return nil, fmt.Errorf("list Main: %w", err)
	}

	return result, nil
}

func (s *Service) Create(ctx context.Context, input CreateInput) (*domain.Main, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	transaction, err := s.store.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin create: %w", err)
	}
	defer rollback(ctx, transaction)

	result, err := s.create(ctx, transaction, input)
	if err != nil {
		return nil, err
	}

	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit create: %w", err)
	}

	return result, nil
}

func (s *Service) create(ctx context.Context, transaction pgx.Tx, input CreateInput) (*domain.Main, error) {
	var (
		main     = &domain.Main{Title: input.Title}
		metadata *domain.Satellite
	)

	switch {
	case input.Satellite.Tool != nil:
		tool := &domain.Tool{Description1: input.Satellite.Tool.Description1}
		main.SubObj, main.Satellite, metadata = domain.Tools, tool, &tool.Satellite
	case input.Satellite.Table != nil:
		table := &domain.Table{Description2: input.Satellite.Table.Description2}
		main.SubObj, main.Satellite, metadata = domain.Tables, table, &table.Satellite
	case input.Satellite.Chair != nil:
		chair := &domain.Chair{Description3: input.Satellite.Chair.Description3, Type: input.Satellite.Chair.Type}
		main.SubObj, main.Satellite, metadata = domain.Chairs, chair, &chair.Satellite
	}

	subID, err := s.store.NextSatelliteID(ctx, transaction, main.SubObj)
	if err != nil {
		return nil, fmt.Errorf("allocate satellite id: %w", err)
	}

	main.SubID = subID

	now, err := s.store.Now(ctx, transaction)
	if err != nil {
		return nil, fmt.Errorf("get create timestamp: %w", err)
	}

	main.CreatedAt, main.UpdatedAt = now, now

	main.ID, err = s.store.InsertMain(ctx, transaction, main)
	if err != nil {
		return nil, fmt.Errorf("insert Main: %w", err)
	}

	*metadata = domain.Satellite{ID: main.SubID, MainID: main.ID, CreatedAt: now, UpdatedAt: now}
	if err := s.store.InsertSatellite(ctx, transaction, main); err != nil {
		return nil, fmt.Errorf("insert satellite: %w", err)
	}

	result, err := s.store.ReadTx(ctx, transaction, main.ID)
	if err != nil {
		return nil, fmt.Errorf("read created aggregate: %w", err)
	}

	return result, nil
}

func (s *Service) Update(ctx context.Context, input UpdateInput) (*domain.Main, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	transaction, err := s.store.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin update: %w", err)
	}
	defer rollback(ctx, transaction)

	main, err := s.applyUpdate(ctx, transaction, input)
	if err != nil {
		return nil, err
	}

	now, err := s.store.Now(ctx, transaction)
	if err != nil {
		return nil, fmt.Errorf("get update timestamp: %w", err)
	}

	if err := s.store.UpdateMain(ctx, transaction, main, now); err != nil {
		return nil, fmt.Errorf("update Main: %w", err)
	}

	if input.SatelliteSet {
		if err := s.store.UpdateSatellite(ctx, transaction, main, now); err != nil {
			return nil, fmt.Errorf("update satellite: %w", err)
		}
	}

	result, err := s.store.ReadTx(ctx, transaction, main.ID)
	if err != nil {
		return nil, fmt.Errorf("read updated aggregate: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit update: %w", err)
	}

	return result, nil
}

func (s *Service) applyUpdate(ctx context.Context, transaction pgx.Tx, input UpdateInput) (*domain.Main, error) {
	main, err := s.store.LockMain(ctx, transaction, input.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("lock Main for update: %w", err)
	}

	if main.DeletedAt != nil {
		return nil, ErrNotFound
	}

	main, err = s.store.LockSatellite(ctx, transaction, main)
	if err != nil {
		return nil, fmt.Errorf("lock satellite for update: %w", err)
	}

	if input.TitleSet {
		main.Title = *input.Title
	}

	if input.SatelliteSet {
		if err := input.Satellite.apply(main.Satellite); err != nil {
			return nil, err
		}
	}

	return main, nil
}

func (s *Service) Delete(ctx context.Context, mainID int64) (int64, error) {
	if err := inputValidator.Var(mainID, "gt=0"); err != nil {
		return 0, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	transaction, err := s.store.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin delete: %w", err)
	}
	defer rollback(ctx, transaction)

	if err := s.delete(ctx, transaction, mainID); err != nil {
		return 0, err
	}

	if err := transaction.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit delete: %w", err)
	}

	return mainID, nil
}

func (s *Service) delete(ctx context.Context, transaction pgx.Tx, mainID int64) error {
	main, err := s.store.LockMain(ctx, transaction, mainID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}

	if err != nil {
		return fmt.Errorf("lock Main for delete: %w", err)
	}

	if main.DeletedAt != nil {
		return ErrAlreadyDeleted
	}

	main, err = s.store.LockSatellite(ctx, transaction, main)
	if err != nil {
		return fmt.Errorf("lock satellite for delete: %w", err)
	}

	now, err := s.store.Now(ctx, transaction)
	if err != nil {
		return fmt.Errorf("get delete timestamp: %w", err)
	}

	if err := s.store.DeleteMain(ctx, transaction, mainID, now); err != nil {
		return fmt.Errorf("soft delete Main: %w", err)
	}

	if err := s.store.DeleteSatellite(ctx, transaction, main, now); err != nil {
		return fmt.Errorf("soft delete satellite: %w", err)
	}

	return nil
}

func rollback(ctx context.Context, transaction pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := transaction.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		slog.ErrorContext(ctx, "rollback failed", "error", err)
	}
}
