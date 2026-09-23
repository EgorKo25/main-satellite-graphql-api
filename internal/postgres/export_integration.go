//go:build integration

package postgres

import "github.com/jackc/pgx/v5/pgxpool"

func NewReadDBForTest(pool *pgxpool.Pool) *DB {
	return &DB{pool: pool}
}
