//go:build integration

package postgres

import (
	"github.com/go-playground/validator/v10"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewReadDBForTest(pool *pgxpool.Pool) *DB {
	return &DB{pool: pool, validator: validator.New(validator.WithRequiredStructEnabled())}
}
