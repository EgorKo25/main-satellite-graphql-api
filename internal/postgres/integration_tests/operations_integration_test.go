//go:build integration

package integrationtests_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

const (
	initialTitle = "initial"
	changedTitle = "changed"
)

func TestSatelliteLifecycle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind        domain.Kind
		description string
		create      func(context.Context, *postgres.DB) (*domain.Main, error)
		update      func(context.Context, *postgres.DB, int64, graphql.Omittable[*string]) (*domain.Main, error)
	}{
		{
			kind:        domain.Tools,
			description: "description1",
			create: func(ctx context.Context, database *postgres.DB) (*domain.Main, error) {
				return database.CreateTool(ctx, initialTitle, postgres.ToolCreate{})
			},
			update: func(
				ctx context.Context,
				database *postgres.DB,
				id int64,
				value graphql.Omittable[*string],
			) (*domain.Main, error) {
				return database.UpdateTool(
					ctx,
					id,
					graphql.Omittable[*string]{},
					postgres.ToolUpdate{Description1: value},
				)
			},
		},
		{
			kind:        domain.Tables,
			description: "description2",
			create: func(ctx context.Context, database *postgres.DB) (*domain.Main, error) {
				return database.CreateTable(ctx, initialTitle, postgres.TableCreate{})
			},
			update: func(
				ctx context.Context,
				database *postgres.DB,
				id int64,
				value graphql.Omittable[*string],
			) (*domain.Main, error) {
				return database.UpdateTable(
					ctx,
					id,
					graphql.Omittable[*string]{},
					postgres.TableUpdate{Description2: value},
				)
			},
		},
		{
			kind:        domain.Chairs,
			description: "description3",
			create: func(ctx context.Context, database *postgres.DB) (*domain.Main, error) {
				return database.CreateChair(ctx, initialTitle, postgres.ChairCreate{Type: domain.ABC})
			},
			update: func(
				ctx context.Context,
				database *postgres.DB,
				id int64,
				value graphql.Omittable[*string],
			) (*domain.Main, error) {
				return database.UpdateChair(
					ctx,
					id,
					graphql.Omittable[*string]{},
					postgres.ChairUpdate{Description3: value},
				)
			},
		},
	}

	for _, test := range tests {
		t.Run(string(test.kind), func(t *testing.T) {
			t.Parallel()

			database, pool := setupDatabase(t)
			ctx := t.Context()
			created, err := test.create(ctx, database)
			require.NoError(t, err)
			require.Positive(t, created.ID)
			require.Equal(t, test.kind, created.SubObj)
			require.Equal(t, test.kind, created.Satellite.Kind())
			require.Equal(t, created.SubID, created.Satellite.Metadata().ID)
			require.Equal(t, created.ID, created.Satellite.Metadata().MainID)
			require.Equal(t, created.CreatedAt, created.Satellite.Metadata().CreatedAt)

			var (
				storedID, storedMainID int64
				storedDescription      *string
				satelliteCount         int
			)

			err = pool.QueryRow(ctx, fmt.Sprintf(`SELECT id, main_id, %s FROM %s WHERE main_id = $1`,
				pgx.Identifier{test.description}.Sanitize(), pgx.Identifier{string(test.kind)}.Sanitize()),
				created.ID).Scan(&storedID, &storedMainID, &storedDescription)
			require.NoError(t, err)
			require.Equal(t, created.SubID, storedID)
			require.Equal(t, created.ID, storedMainID)
			require.Nil(t, storedDescription)

			err = pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM tools WHERE main_id = $1) +
 (SELECT count(*) FROM tables WHERE main_id = $1) +
 (SELECT count(*) FROM chairs WHERE main_id = $1)`, created.ID).Scan(&satelliteCount)
			require.NoError(t, err)
			require.Equal(t, 1, satelliteCount)

			updated, err := test.update(ctx, database, created.ID, graphql.OmittableOf(new("first")))
			require.NoError(t, err)

			beforeTitleUpdate := updated.Satellite.Metadata().UpdatedAt
			updated, err = database.UpdateMain(ctx, created.ID, graphql.OmittableOf(new(changedTitle)))
			require.NoError(t, err)
			require.Equal(t, changedTitle, updated.Title)
			require.Equal(t, created.CreatedAt, updated.CreatedAt)
			require.Equal(t, beforeTitleUpdate, updated.Satellite.Metadata().UpdatedAt)

			err = pool.QueryRow(ctx, fmt.Sprintf(`SELECT %s FROM %s WHERE main_id = $1`,
				pgx.Identifier{test.description}.Sanitize(), pgx.Identifier{string(test.kind)}.Sanitize()),
				created.ID).Scan(&storedDescription)
			require.NoError(t, err)
			require.Equal(t, new("first"), storedDescription)

			for _, description := range []*string{new(""), nil} {
				updated, err = test.update(ctx, database, created.ID, graphql.OmittableOf(description))
				require.NoError(t, err)
				require.Equal(t, changedTitle, updated.Title)
				require.Equal(t, created.CreatedAt, updated.CreatedAt)
				require.Equal(t, created.Satellite.Metadata().CreatedAt, updated.Satellite.Metadata().CreatedAt)
				require.True(t, updated.UpdatedAt.After(created.UpdatedAt))
				require.Equal(t, updated.UpdatedAt, updated.Satellite.Metadata().UpdatedAt)

				err = pool.QueryRow(ctx, fmt.Sprintf(`SELECT %s FROM %s WHERE main_id = $1`,
					pgx.Identifier{test.description}.Sanitize(), pgx.Identifier{string(test.kind)}.Sanitize()),
					created.ID).Scan(&storedDescription)
				require.NoError(t, err)
				require.Equal(t, description, storedDescription)
			}

			listed, err := database.List(ctx, postgres.ListInput{ID: &created.ID, Limit: 20})
			require.NoError(t, err)
			require.Equal(t, []*domain.Main{updated}, listed)
			require.NoError(t, database.Delete(ctx, created.ID))

			var mainDeleted, satelliteDeleted, mainUpdated, satelliteUpdated time.Time

			err = pool.QueryRow(ctx, fmt.Sprintf(`SELECT m.deleted_at, s.deleted_at, m.update_at, s.update_at
FROM main AS m JOIN %s AS s ON s.id = m.sub_id AND s.main_id = m.id
WHERE m.id = $1`, pgx.Identifier{string(test.kind)}.Sanitize()), created.ID).
				Scan(&mainDeleted, &satelliteDeleted, &mainUpdated, &satelliteUpdated)
			require.NoError(t, err)
			require.False(t, mainDeleted.IsZero())
			require.Equal(t, mainDeleted, satelliteDeleted)
			require.Equal(t, mainDeleted, mainUpdated)
			require.Equal(t, mainDeleted, satelliteUpdated)
			require.ErrorIs(t, database.Delete(ctx, created.ID), postgres.ErrAlreadyDeleted)

			updated, err = test.update(ctx, database, created.ID, graphql.OmittableOf(new("forbidden")))
			require.ErrorIs(t, err, postgres.ErrNotFound)
			require.Nil(t, updated)

			listed, err = database.List(ctx, postgres.ListInput{ID: &created.ID, Limit: 20})
			require.NoError(t, err)
			require.Empty(t, listed)
		})
	}
}

func TestChairTypeOnlyUpdate(t *testing.T) {
	t.Parallel()

	database, pool := setupDatabase(t)
	created, err := database.CreateChair(t.Context(), "", postgres.ChairCreate{
		Description3: new("retained"),
		Type:         domain.ABC,
	})
	require.NoError(t, err)
	require.Empty(t, created.Title)

	for _, chairType := range []domain.ChairType{domain.CDE, domain.ABC} {
		updated, updateErr := database.UpdateChair(t.Context(), created.ID, graphql.Omittable[*string]{},
			postgres.ChairUpdate{Type: graphql.OmittableOf(&chairType)})
		require.NoError(t, updateErr)
		require.Equal(t, domain.Chairs, updated.SubObj)

		var (
			storedDescription string
			storedType        domain.ChairType
		)

		err = pool.QueryRow(t.Context(), `SELECT description3, type FROM chairs WHERE main_id = $1`, created.ID).
			Scan(&storedDescription, &storedType)
		require.NoError(t, err)
		require.Equal(t, "retained", storedDescription)
		require.Equal(t, chairType, storedType)
	}
}

func TestUpdateFailurePreservesWholeAggregate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		update func(context.Context, *postgres.DB, int64) (*domain.Main, error)
		want   error
	}{
		{
			name: "different satellite kind",
			update: func(ctx context.Context, database *postgres.DB, id int64) (*domain.Main, error) {
				return database.UpdateTool(ctx, id, graphql.OmittableOf(new(changedTitle)),
					postgres.ToolUpdate{Description1: graphql.OmittableOf(new("different"))})
			},
			want: postgres.ErrSatelliteTypeMismatch,
		},
		{
			name: "empty satellite patch with changed title",
			update: func(ctx context.Context, database *postgres.DB, id int64) (*domain.Main, error) {
				return database.UpdateChair(ctx, id, graphql.OmittableOf(new(changedTitle)), postgres.ChairUpdate{})
			},
			want: postgres.ErrInvalidInput,
		},
		{
			name: "null chair type with changed title",
			update: func(ctx context.Context, database *postgres.DB, id int64) (*domain.Main, error) {
				return database.UpdateChair(ctx, id, graphql.OmittableOf(new(changedTitle)),
					postgres.ChairUpdate{Type: graphql.OmittableOf[*domain.ChairType](nil)})
			},
			want: postgres.ErrInvalidInput,
		},
		{
			name: "null title with changed description",
			update: func(ctx context.Context, database *postgres.DB, id int64) (*domain.Main, error) {
				return database.UpdateChair(ctx, id, graphql.OmittableOf[*string](nil),
					postgres.ChairUpdate{Description3: graphql.OmittableOf(new("different"))})
			},
			want: postgres.ErrInvalidInput,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			database, _ := setupDatabase(t)
			created, err := database.CreateChair(t.Context(), initialTitle, postgres.ChairCreate{
				Description3: new("retained"),
				Type:         domain.ABC,
			})
			require.NoError(t, err)

			updated, err := test.update(t.Context(), database, created.ID)
			require.ErrorIs(t, err, test.want)
			require.Nil(t, updated)

			stored, err := database.List(t.Context(), postgres.ListInput{ID: &created.ID, Limit: 20})
			require.NoError(t, err)
			require.Equal(t, []*domain.Main{created}, stored)
		})
	}
}

func TestMixedListFilteringAndPagination(t *testing.T) {
	t.Parallel()

	database, _ := setupDatabase(t)
	tool, err := database.CreateTool(t.Context(), "tool", postgres.ToolCreate{})
	require.NoError(t, err)

	table, err := database.CreateTable(t.Context(), "table", postgres.TableCreate{})
	require.NoError(t, err)

	chair, err := database.CreateChair(t.Context(), "chair", postgres.ChairCreate{Type: domain.ABC})
	require.NoError(t, err)

	tests := []struct {
		name  string
		input postgres.ListInput
		want  []*domain.Main
	}{
		{
			name:  "mixed list ordered by ID",
			input: postgres.ListInput{Limit: 20},
			want:  []*domain.Main{tool, table, chair},
		},
		{name: "pagination", input: postgres.ListInput{Limit: 1, Offset: 1}, want: []*domain.Main{table}},
		{name: "filter ID", input: postgres.ListInput{ID: &chair.ID, Limit: 20}, want: []*domain.Main{chair}},
		{
			name:  "offset after filter",
			input: postgres.ListInput{ID: &chair.ID, Limit: 20, Offset: 1},
			want:  []*domain.Main{},
		},
		{name: "unknown ID", input: postgres.ListInput{ID: new(chair.ID + 1), Limit: 20}, want: []*domain.Main{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			items, listErr := database.List(t.Context(), test.input)
			require.NoError(t, listErr)
			require.Equal(t, test.want, items)
		})
	}
}

func TestWriteFailureRollsBack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		trigger string
		write   func(context.Context, *postgres.DB, int64) (*domain.Main, error)
	}{
		{
			name:    "create second write",
			trigger: `CREATE TRIGGER fail_write BEFORE INSERT ON chairs FOR EACH ROW EXECUTE FUNCTION reject_write()`,
			write: func(ctx context.Context, database *postgres.DB, _ int64) (*domain.Main, error) {
				return database.CreateChair(ctx, changedTitle, postgres.ChairCreate{Type: domain.CDE})
			},
		},
		{
			name:    "update second write",
			trigger: `CREATE TRIGGER fail_write BEFORE UPDATE ON chairs FOR EACH ROW EXECUTE FUNCTION reject_write()`,
			write: func(ctx context.Context, database *postgres.DB, id int64) (*domain.Main, error) {
				return database.UpdateChair(ctx, id, graphql.OmittableOf(new(changedTitle)),
					postgres.ChairUpdate{Type: graphql.OmittableOf(new(domain.CDE))})
			},
		},
		{
			name:    "delete second write",
			trigger: `CREATE TRIGGER fail_write BEFORE UPDATE ON chairs FOR EACH ROW EXECUTE FUNCTION reject_write()`,
			write: func(ctx context.Context, database *postgres.DB, id int64) (*domain.Main, error) {
				return nil, database.Delete(ctx, id)
			},
		},
		{
			name: "commit failure",
			trigger: `CREATE CONSTRAINT TRIGGER fail_write AFTER INSERT ON chairs
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_write()`,
			write: func(ctx context.Context, database *postgres.DB, _ int64) (*domain.Main, error) {
				return database.CreateChair(ctx, changedTitle, postgres.ChairCreate{Type: domain.CDE})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			database, pool := setupDatabase(t)
			ctx := t.Context()
			created, err := database.CreateChair(ctx, initialTitle, postgres.ChairCreate{
				Description3: new("retained"),
				Type:         domain.ABC,
			})
			require.NoError(t, err)

			var beforeMain, beforeChair, afterMain, afterChair string

			err = pool.QueryRow(ctx, `SELECT
 (SELECT jsonb_agg(to_jsonb(m) ORDER BY id)::text FROM main AS m),
 (SELECT jsonb_agg(to_jsonb(c) ORDER BY id)::text FROM chairs AS c)`).Scan(&beforeMain, &beforeChair)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `CREATE FUNCTION reject_write() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'injected write failure' USING ERRCODE = '23514'; END $$`)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, test.trigger)
			require.NoError(t, err)

			result, err := test.write(ctx, database, created.ID)
			require.Error(t, err)
			require.Nil(t, result)

			var databaseError *pgconn.PgError

			require.ErrorAs(t, err, &databaseError)
			require.Equal(t, "23514", databaseError.Code)

			err = pool.QueryRow(ctx, `SELECT
 (SELECT jsonb_agg(to_jsonb(m) ORDER BY id)::text FROM main AS m),
 (SELECT jsonb_agg(to_jsonb(c) ORDER BY id)::text FROM chairs AS c)`).Scan(&afterMain, &afterChair)
			require.NoError(t, err)
			require.JSONEq(t, beforeMain, afterMain)
			require.JSONEq(t, beforeChair, afterChair)
		})
	}
}

func TestConcurrentMainWrites(t *testing.T) {
	t.Parallel()

	type version struct {
		title     string
		chairType domain.ChairType
	}

	tests := []struct {
		name       string
		second     func(context.Context, *postgres.DB, int64) error
		firstError error
		lastError  error
		successes  []int
		versions   []version
	}{
		{
			name: "double delete",
			second: func(ctx context.Context, database *postgres.DB, id int64) error {
				return database.Delete(ctx, id)
			},
			firstError: postgres.ErrAlreadyDeleted,
			lastError:  postgres.ErrAlreadyDeleted,
			successes:  []int{1},
			versions:   []version{{initialTitle, domain.ABC}},
		},
		{
			name: "delete and update",
			second: func(ctx context.Context, database *postgres.DB, id int64) error {
				_, err := database.UpdateChair(ctx, id, graphql.OmittableOf(new(changedTitle)),
					postgres.ChairUpdate{Type: graphql.OmittableOf(new(domain.CDE))})
				if err != nil {
					return fmt.Errorf("concurrent chair update: %w", err)
				}

				return nil
			},
			lastError: postgres.ErrNotFound,
			successes: []int{1, 2},
			versions:  []version{{initialTitle, domain.ABC}, {changedTitle, domain.CDE}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			database, pool := setupDatabase(t)

			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()

			created, err := database.CreateChair(ctx, initialTitle, postgres.ChairCreate{Type: domain.ABC})
			require.NoError(t, err)

			gate, err := pool.Begin(ctx)
			require.NoError(t, err)
			t.Cleanup(func() {
				cleanupCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
				defer done()

				rollbackErr := gate.Rollback(cleanupCtx)
				if !errors.Is(rollbackErr, pgx.ErrTxClosed) {
					require.NoError(t, rollbackErr)
				}
			})

			var lockedID int64

			err = gate.QueryRow(ctx, `SELECT id FROM main WHERE id = $1 FOR UPDATE`, created.ID).Scan(&lockedID)
			require.NoError(t, err)
			require.Equal(t, created.ID, lockedID)

			type outcome struct {
				index int
				err   error
			}

			completed := make(chan outcome, 2)
			go func() { completed <- outcome{index: 0, err: database.Delete(ctx, created.ID)} }()
			go func() { completed <- outcome{index: 1, err: test.second(ctx, database, created.ID)} }()

			for {
				var waiting int

				err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database() AND wait_event_type = 'Lock'
AND wait_event IN ('transactionid', 'tuple') AND query LIKE '%FOR UPDATE%'`).Scan(&waiting)
				require.NoError(t, err)

				if waiting == 2 {
					break
				}
			}

			require.NoError(t, gate.Commit(ctx))

			var (
				failures  [2]error
				successes int
			)

			for range 2 {
				select {
				case result := <-completed:
					failures[result.index] = result.err
					if result.err == nil {
						successes++
					}
				case <-ctx.Done():
					require.NoError(t, ctx.Err())
				}
			}

			require.Contains(t, test.successes, successes)

			for index, failure := range failures {
				if failure != nil {
					require.ErrorIs(t, failure, [2]error{test.firstError, test.lastError}[index])
				}
			}

			var (
				stored                        version
				mainDeleted, satelliteDeleted time.Time
				mainUpdated, satelliteUpdated time.Time
			)

			err = pool.QueryRow(ctx, `SELECT m.title, c.type, m.deleted_at, c.deleted_at, m.update_at, c.update_at
FROM main AS m JOIN chairs AS c ON c.id = m.sub_id AND c.main_id = m.id WHERE m.id = $1`, created.ID).
				Scan(&stored.title, &stored.chairType, &mainDeleted, &satelliteDeleted, &mainUpdated, &satelliteUpdated)
			require.NoError(t, err)
			require.Contains(t, test.versions, stored)
			require.Equal(t, mainDeleted, satelliteDeleted)
			require.Equal(t, mainDeleted, mainUpdated)
			require.Equal(t, mainDeleted, satelliteUpdated)

			items, err := database.List(ctx, postgres.ListInput{ID: &created.ID, Limit: 20})
			require.NoError(t, err)
			require.Empty(t, items)
		})
	}
}
