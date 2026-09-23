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

	database := &DB{pool: pool}
	database.deleters = map[domain.Kind]func(context.Context, pgx.Tx, *domain.Main, time.Time) error{
		domain.Tools:  database.deleteTool,
		domain.Tables: database.deleteTable,
		domain.Chairs: database.deleteChair,
	}

	return database, nil
}

type DB struct {
	pool     *pgxpool.Pool
	deleters map[domain.Kind]func(context.Context, pgx.Tx, *domain.Main, time.Time) error
}

func (db *DB) Close() {
	db.pool.Close()
}
