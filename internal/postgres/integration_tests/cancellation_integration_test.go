//go:build integration

package integrationtests_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestCanceledUpdateRollsBackWholeAggregate(t *testing.T) {
	t.Parallel()

	database, pool := setupDatabase(t)

	ctx, finish := context.WithTimeout(t.Context(), 30*time.Second)
	defer finish()

	created, err := database.Create(ctx, initialTitle, map[string]any{chairName: map[string]any{
		chairDescriptionField: new("retained"),
		chairTypeField:        domain.ABC,
	}})
	require.NoError(t, err)

	var beforeMain, beforeChair, afterMain, afterChair string

	err = pool.QueryRow(ctx, `
	    SELECT row_to_json(m)::text, row_to_json(c)::text
	    FROM main m JOIN chairs c ON c.id = m.sub_id AND c.main_id = m.id
	    WHERE m.id = $1;
	`, created.ID).Scan(&beforeMain, &beforeChair)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
	    CREATE FUNCTION wait_for_cancellation() RETURNS trigger LANGUAGE plpgsql AS $$
	    BEGIN
	        IF NOT EXISTS (SELECT 1 FROM main WHERE id = NEW.main_id AND title = 'changed') THEN
	            RAISE EXCEPTION 'Main must already be updated before the satellite write';
	        END IF;
	        PERFORM pg_advisory_xact_lock(42871);
	        RETURN NEW;
	    END;
	    $$;
	    CREATE TRIGGER wait_for_cancellation BEFORE UPDATE ON chairs
	    FOR EACH ROW EXECUTE FUNCTION wait_for_cancellation();
	`)
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

	_, err = gate.Exec(ctx, `SELECT pg_advisory_xact_lock(42871);`)
	require.NoError(t, err)

	writeCtx, cancel := context.WithCancel(ctx)
	completed := make(chan struct{})

	var (
		result    *domain.Main
		updateErr error
	)

	go func() {
		defer close(completed)

		result, updateErr = database.Update(writeCtx, created.ID, graphql.OmittableOf(new(changedTitle)),
			graphql.OmittableOf(map[string]any{chairName: map[string]any{
				chairDescriptionField: (*string)(nil),
				chairTypeField:        new(domain.CDE),
			}}))
	}()

	t.Cleanup(func() {
		cancel()

		cleanupCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()

		select {
		case <-completed:
		case <-cleanupCtx.Done():
			require.NoError(t, cleanupCtx.Err(), "canceled update must release its goroutine")
		}
	})

	for {
		var waiting bool

		err = pool.QueryRow(ctx, `
		    SELECT EXISTS (
		        SELECT 1 FROM pg_stat_activity
		        WHERE datname = current_database() AND wait_event_type = 'Lock' AND wait_event = 'advisory'
		    );
		`).Scan(&waiting)
		require.NoError(t, err)

		if waiting {
			break
		}
	}

	cancel()

	select {
	case <-completed:
	case <-ctx.Done():
		require.NoError(t, ctx.Err(), "canceled update must return before releasing the gate")
	}

	require.ErrorIs(t, updateErr, context.Canceled)
	require.Nil(t, result)
	require.NoError(t, gate.Commit(ctx))

	err = pool.QueryRow(ctx, `
	    SELECT row_to_json(m)::text, row_to_json(c)::text
	    FROM main m JOIN chairs c ON c.id = m.sub_id AND c.main_id = m.id
	    WHERE m.id = $1 FOR UPDATE OF m, c;
	`, created.ID).Scan(&afterMain, &afterChair)
	require.NoError(t, err)
	require.JSONEq(t, beforeMain, afterMain)
	require.JSONEq(t, beforeChair, afterChair)

	updated, err := database.Update(ctx, created.ID, graphql.OmittableOf(new(changedTitle)),
		graphql.OmittableOf(map[string]any{chairName: map[string]any{chairTypeField: new(domain.CDE)}}))
	require.NoError(t, err)
	require.Equal(t, changedTitle, updated.Title)

	var chair domain.Chair

	require.NoError(t, json.Unmarshal(updated.SatelliteData, &chair))
	require.Equal(t, domain.CDE, chair.Type)
	require.Equal(t, new("retained"), chair.Description3)

	listed, err := database.List(ctx, postgres.ListInput{ID: &created.ID, Limit: 20})
	require.NoError(t, err)
	require.Equal(t, []*domain.Main{updated}, listed)
}
