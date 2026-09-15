// Package migrations runs embedded, versioned PostgreSQL migrations with goose.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed *.sql
var Files embed.FS

func Up(ctx context.Context, dsn string) error { return run(ctx, dsn, false) }

// Down reverts one migration. Call it only explicitly on a disposable test DB.
func Down(ctx context.Context, dsn string) error { return run(ctx, dsn, true) }

func run(ctx context.Context, dsn string, down bool) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open migration connection: %w", err)
	}
	defer db.Close()
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("create migration lock: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, Files, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}
	if down {
		_, err = provider.Down(ctx)
	} else {
		_, err = provider.Up(ctx)
	}
	if err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
