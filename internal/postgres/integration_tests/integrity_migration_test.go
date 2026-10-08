//go:build integration

package integrationtests_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

const (
	createOperation = "create"
	updateOperation = "update"
	deleteOperation = "delete"
)

func TestDeferredSatelliteIntegrity(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		sql  string
	}{
		{
			name: "missing main satellite",
			sql:  "INSERT INTO main (id, title, sub_id, sub_obj) VALUES (999, 'broken', 999, 'tools');",
		},
		{name: "wrong ID", sql: "UPDATE main SET sub_id = 999 WHERE id = 1;"},
		{name: "wrong kind", sql: "UPDATE main SET sub_obj = 'chairs' WHERE id = 1;"},
		{name: "wrong backlink", sql: "UPDATE tools SET main_id = 2 WHERE main_id = 1;"},
		{name: "missing tool", sql: "DELETE FROM tools WHERE main_id = 1;"},
		{name: "missing table", sql: "DELETE FROM tables WHERE main_id = 2;"},
		{name: "missing chair", sql: "DELETE FROM chairs WHERE main_id = 3;"},
		{name: "extra tool", sql: "INSERT INTO tools (id, main_id) VALUES (999, 2);"},
		{name: "extra table", sql: "INSERT INTO tables (id, main_id) VALUES (999, 1);"},
		{name: "extra chair", sql: "INSERT INTO chairs (id, main_id, type) VALUES (999, 1, 'abc');"},
		{name: "main deleted alone", sql: "UPDATE main SET deleted_at = now() WHERE id = 1;"},
		{name: "tool deleted alone", sql: "UPDATE tools SET deleted_at = now() WHERE main_id = 1;"},
		{name: "table deleted alone", sql: "UPDATE tables SET deleted_at = now() WHERE main_id = 2;"},
		{name: "chair deleted alone", sql: "UPDATE chairs SET deleted_at = now() WHERE main_id = 3;"},
		{name: "different deletion times", sql: `
		    UPDATE main SET deleted_at = '2026-10-08 12:00:00+00' WHERE id = 1;
		    UPDATE tools SET deleted_at = '2026-10-08 12:00:01+00' WHERE main_id = 1;
		`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			database, pool := setupDatabase(t)
			seedReadDatabase(t, pool, 3, 32)
			before, err := database.List(t.Context(), postgres.ListInput{Limit: 20})
			require.NoError(t, err)

			transaction, err := pool.Begin(t.Context())
			require.NoError(t, err)

			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				if rollbackErr := transaction.Rollback(ctx); !errors.Is(rollbackErr, pgx.ErrTxClosed) {
					require.NoError(t, rollbackErr)
				}
			})

			_, err = transaction.Exec(t.Context(), testCase.sql)
			require.NoError(t, err, "constraint is deferred until commit")

			err = transaction.Commit(t.Context())

			var constraintError *pgconn.PgError

			require.ErrorAs(t, err, &constraintError)
			require.Equal(t, "23514", constraintError.Code)
			require.Equal(t, "main_satellite_integrity", constraintError.ConstraintName)

			after, err := database.List(t.Context(), postgres.ListInput{Limit: 20})
			require.NoError(t, err)
			require.Empty(t, cmp.Diff(before, after))

			var counts []int64

			err = pool.QueryRow(t.Context(), `
			    SELECT ARRAY[(SELECT count(*) FROM main), (SELECT count(*) FROM tools),
			        (SELECT count(*) FROM tables), (SELECT count(*) FROM chairs)];
			`).Scan(&counts)
			require.NoError(t, err)
			require.Equal(t, []int64{3, 1, 1, 1}, counts)
		})
	}
}

func TestIntegrityMigrationExistingData(t *testing.T) {
	t.Parallel()

	for _, broken := range []bool{false, true} {
		t.Run(fmt.Sprintf("broken_%t", broken), func(t *testing.T) {
			t.Parallel()

			database, pool := setupDatabase(t)
			created, err := database.Create(t.Context(), initialTitle, map[string]any{toolName: map[string]any{}})
			require.NoError(t, err)

			migrationDB := stdlib.OpenDB(*pool.Config().ConnConfig)

			t.Cleanup(func() { require.NoError(t, migrationDB.Close()) })

			migrations, err := goose.NewProvider(goose.DialectPostgres, migrationDB, os.DirFS("../../../migrations"))
			require.NoError(t, err)

			_, err = migrations.Down(t.Context())
			require.NoError(t, err)

			if broken {
				_, err = pool.Exec(t.Context(), "UPDATE main SET sub_id = 999 WHERE id = $1;", created.ID)
				require.NoError(t, err)
			}

			_, err = migrations.Up(t.Context())

			if broken {
				var constraintError *pgconn.PgError

				require.ErrorAs(t, err, &constraintError)
				require.Equal(t, "23514", constraintError.Code)

				version, versionErr := migrations.GetDBVersion(t.Context())
				require.NoError(t, versionErr)
				require.Equal(t, int64(1), version)

				_, err = pool.Exec(t.Context(), "UPDATE main SET sub_id = $2 WHERE id = $1;", created.ID, created.SubID)
				require.NoError(t, err)
				_, err = migrations.Up(t.Context())
			}

			require.NoError(t, err)

			items, err := database.List(t.Context(), postgres.ListInput{Limit: 20})
			require.NoError(t, err)
			require.Empty(t, cmp.Diff([]*domain.Main{created}, items))
		})
	}
}

func TestConcurrentDirectSatelliteWrite(t *testing.T) {
	t.Parallel()

	for _, isolation := range []pgx.TxIsoLevel{pgx.ReadCommitted, pgx.RepeatableRead, pgx.Serializable} {
		t.Run(string(isolation), func(t *testing.T) {
			t.Parallel()

			database, pool := setupDatabase(t)

			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()

			created, err := database.Create(ctx, initialTitle, map[string]any{toolName: map[string]any{}})
			require.NoError(t, err)

			deletion, err := pool.Begin(ctx)
			require.NoError(t, err)

			defer func() { _ = deletion.Rollback(ctx) }()

			_, err = deletion.Exec(ctx, "SELECT id FROM main WHERE id = $1 FOR UPDATE;", created.ID)
			require.NoError(t, err)

			writer, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: isolation})
			require.NoError(t, err)

			_, err = writer.Exec(ctx, "SELECT id FROM main WHERE id = $1;", created.ID)
			require.NoError(t, err)

			writerPID := writer.Conn().PgConn().PID()
			completed := make(chan error, 1)

			go func() {
				_, writeErr := writer.Exec(ctx, "INSERT INTO tables (main_id) VALUES ($1);", created.ID)
				if writeErr == nil {
					writeErr = writer.Commit(ctx)
				}

				cleanupCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer done()

				if rollbackErr := writer.Rollback(cleanupCtx); !errors.Is(rollbackErr, pgx.ErrTxClosed) {
					writeErr = errors.Join(writeErr, rollbackErr)
				}

				completed <- writeErr
			}()

			for {
				var blocked bool

				err = deletion.QueryRow(ctx, "SELECT cardinality(pg_blocking_pids($1)) > 0;", writerPID).Scan(&blocked)
				require.NoError(t, err)

				if blocked {
					break
				}
			}

			now := time.Now().UTC()
			_, err = deletion.Exec(ctx, `
			    UPDATE main SET deleted_at = $2, update_at = $2 WHERE id = $1;
			`, created.ID, now)
			require.NoError(t, err)
			_, err = deletion.Exec(ctx, `
			    UPDATE tools SET deleted_at = $2, update_at = $2 WHERE main_id = $1;
			`, created.ID, now)
			require.NoError(t, err)
			require.NoError(t, deletion.Commit(ctx))

			select {
			case err = <-completed:
			case <-ctx.Done():
				require.NoError(t, ctx.Err())
			}

			var constraintError *pgconn.PgError

			require.ErrorAs(t, err, &constraintError)
			require.Contains(t, []string{"23514", "40001"}, constraintError.Code)

			var (
				consistent bool
				extraCount int
			)

			err = pool.QueryRow(ctx, `
			    SELECT m.deleted_at IS NOT NULL AND m.deleted_at = s.deleted_at,
			        (SELECT count(*) FROM tables WHERE main_id = m.id)
			    FROM main m JOIN tools s ON s.id = m.sub_id AND s.main_id = m.id WHERE m.id = $1;
			`, created.ID).Scan(&consistent, &extraCount)
			require.NoError(t, err)
			require.True(t, consistent)
			require.Zero(t, extraCount)
		})
	}
}

func BenchmarkIntegrityWrites(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		for _, operation := range []string{createOperation, updateOperation, deleteOperation} {
			b.Run(fmt.Sprintf("constraints_%t/%s", enabled, operation), func(b *testing.B) {
				database, pool := setupDatabase(b)
				if !enabled {
					migrationDB := stdlib.OpenDB(*pool.Config().ConnConfig)

					b.Cleanup(func() { require.NoError(b, migrationDB.Close()) })

					migrations, err := goose.NewProvider(
						goose.DialectPostgres, migrationDB, os.DirFS("../../../migrations"),
					)
					require.NoError(b, err)

					_, err = migrations.Down(b.Context())
					require.NoError(b, err)
				}

				ids := make([]int64, b.N)
				if operation != createOperation {
					for index := range ids {
						created, err := database.Create(
							b.Context(),
							initialTitle,
							map[string]any{toolName: map[string]any{}},
						)
						require.NoError(b, err)

						ids[index] = created.ID
					}
				}

				b.ResetTimer()

				for index := range b.N {
					var err error

					switch operation {
					case createOperation:
						_, err = database.Create(b.Context(), initialTitle, map[string]any{toolName: map[string]any{}})
					case updateOperation:
						_, err = database.Update(b.Context(), ids[index], graphql.OmittableOf(new(changedTitle)),
							graphql.Omittable[map[string]any]{})
					case deleteOperation:
						err = database.Delete(b.Context(), ids[index])
					}

					require.NoError(b, err)
				}

				b.StopTimer()
			})
		}
	}
}
