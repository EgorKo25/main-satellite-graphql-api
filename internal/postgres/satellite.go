package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *DB) NextSatelliteID(ctx context.Context, transaction pgx.Tx, kind domain.Kind) (int64, error) {
	handler, ok := s.handlers[kind]
	if !ok {
		return 0, fmt.Errorf("allocate satellite ID: unknown satellite kind %q", kind)
	}

	return handler.nextID(ctx, transaction)
}

func (s *DB) LockSatellite(ctx context.Context, transaction pgx.Tx, main *domain.Main) (*domain.Main, error) {
	handler, ok := s.handlers[main.SubObj]
	if !ok {
		return nil, fmt.Errorf("lock satellite: unknown satellite kind %q", main.SubObj)
	}

	if err := handler.lock(ctx, transaction, main); err != nil {
		return nil, err
	}

	return s.ReadTx(ctx, transaction, main.ID)
}

func (s *DB) InsertSatellite(ctx context.Context, transaction pgx.Tx, main *domain.Main) error {
	handler, ok := s.handlers[main.SubObj]
	if !ok {
		return fmt.Errorf("insert satellite: unknown satellite kind %q", main.SubObj)
	}

	return handler.create(ctx, transaction, main)
}

func (s *DB) UpdateSatellite(ctx context.Context, transaction pgx.Tx, main *domain.Main, now time.Time) error {
	handler, ok := s.handlers[main.SubObj]
	if !ok {
		return fmt.Errorf("update satellite: unknown satellite kind %q", main.SubObj)
	}

	return handler.update(ctx, transaction, main, now)
}

func (s *DB) DeleteSatellite(ctx context.Context, transaction pgx.Tx, main *domain.Main, now time.Time) error {
	handler, ok := s.handlers[main.SubObj]
	if !ok {
		return fmt.Errorf("delete satellite: unknown satellite kind %q", main.SubObj)
	}

	return handler.softDelete(ctx, transaction, main, now)
}

func exactlyOne(tag pgconn.CommandTag, err error) error {
	if err != nil {
		return err
	}

	if tag.RowsAffected() != 1 {
		return fmt.Errorf("expected one changed row, got %d", tag.RowsAffected())
	}

	return nil
}
