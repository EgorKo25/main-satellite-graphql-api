package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/config"
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

func New(ctx context.Context, cfg config.Database) (*DB, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, errors.New("parse database configuration: invalid connection string")
	}

	poolConfig.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	poolConfig.MaxConns = cfg.MaxConns
	poolConfig.MinConns = cfg.MinConns

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()

	if err = pool.Ping(ctx); err != nil {
		pool.Close()

		return nil, fmt.Errorf("connect to database: %w", err)
	}

	return &DB{
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
	}, nil
}

type DB struct {
	pool     *pgxpool.Pool
	handlers map[domain.Kind]satelliteHandler
}

func (s *DB) Close() {
	s.pool.Close()
}

func (s *DB) Begin(ctx context.Context) (pgx.Tx, error) {
	transaction, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}

	return transaction, nil
}

func (s *DB) Now(ctx context.Context, transaction pgx.Tx) (time.Time, error) {
	var now time.Time

	if err := transaction.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("read database time: %w", err)
	}

	return now.UTC(), nil
}
