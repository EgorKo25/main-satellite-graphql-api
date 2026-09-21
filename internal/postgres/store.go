package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type satelliteHandler struct {
	nextID     func(context.Context, pgx.Tx) (int64, error)
	lock       func(context.Context, pgx.Tx, *domain.Main) error
	create     func(context.Context, pgx.Tx, *domain.Main) error
	update     func(context.Context, pgx.Tx, *domain.Main, time.Time) error
	softDelete func(context.Context, pgx.Tx, *domain.Main, time.Time) error
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{
		pool: pool,
		handlers: map[domain.Kind]satelliteHandler{
			domain.Tools: {
				nextID:     nextToolID,
				lock:       lockTool,
				create:     createTool,
				update:     updateTool,
				softDelete: softDeleteTool,
			},
			domain.Tables: {
				nextID:     nextTableID,
				lock:       lockTable,
				create:     createTable,
				update:     updateTable,
				softDelete: softDeleteTable,
			},
			domain.Chairs: {
				nextID:     nextChairID,
				lock:       lockChair,
				create:     createChair,
				update:     updateChair,
				softDelete: softDeleteChair,
			},
		},
	}
}

type Store struct {
	pool     *pgxpool.Pool
	handlers map[domain.Kind]satelliteHandler
}

func (s *Store) Begin(ctx context.Context) (pgx.Tx, error) {
	transaction, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}

	return transaction, nil
}

func (s *Store) Now(ctx context.Context, transaction pgx.Tx) (time.Time, error) {
	var now time.Time

	if err := transaction.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("read database time: %w", err)
	}

	return now.UTC(), nil
}
