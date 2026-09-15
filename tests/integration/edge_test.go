//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// A test-only trigger holds the first request after its Main write. Unlike a
// scheduler-dependent race, this proves that the competing request waits for
// Main, then rechecks the state committed by the first operation.
func TestContendingWritesWaitAndRecheckState(t *testing.T) {
	for _, tc := range []struct{ name, first, second string }{
		{"update_then_delete", "update", "delete"},
		{"delete_then_update", "delete", "update"},
		{"delete_then_delete", "delete", "delete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			m := f.create(t, "chair", "old", object{"description3": "old", "type": "abc"})
			id := idOf(m)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			gate, err := f.pool.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			const key int64 = 81824791
			if _, err := gate.Exec(ctx, `SELECT pg_advisory_lock($1)`, key); err != nil {
				gate.Release()
				t.Fatal(err)
			}
			locked := true
			defer func() {
				if locked {
					cleanupCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
					defer done()
					_, _ = gate.Exec(cleanupCtx, `SELECT pg_advisory_unlock($1)`, key)
				}
				gate.Release()
			}()
			f.exec(t, fmt.Sprintf(`CREATE FUNCTION test_hold_satellite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM pg_advisory_xact_lock(%d::bigint); RETURN NEW; END $$`, key))
			f.exec(t, `CREATE TRIGGER test_hold_satellite BEFORE UPDATE ON chairs FOR EACH ROW EXECUTE FUNCTION test_hold_satellite()`)
			type outcome struct {
				result response
				err    error
			}
			start := func(operation string) <-chan outcome {
				ch := make(chan outcome, 1)
				go func() {
					input := object{"delete": object{"id": id}}
					if operation == "update" {
						input = object{"update": object{"id": id, "title": "new", "satellite": object{"chair": object{"description3": "new", "type": "cde"}}}}
					}
					r, err := f.request(mutate, object{"input": input})
					ch <- outcome{result: r, err: err}
				}()
				return ch
			}
			first := start(tc.first)
			waitForDBCondition(t, ctx, f, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
WHERE datname=current_database() AND wait_event_type='Lock' AND wait_event='advisory')`)
			second := start(tc.second)
			waitForDBCondition(t, ctx, f, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
WHERE datname=current_database() AND wait_event_type='Lock'
AND wait_event IN ('transactionid','tuple') AND query LIKE '%FROM main WHERE id = $1 FOR UPDATE%')`)
			// Neither write may be visible before the first transaction commits.
			visible := f.list(t, object{"id": id})
			if len(visible) != 1 {
				t.Fatalf("uncommitted deletion visible: %v", visible)
			}
			visibleMain := visible[0].(map[string]any)
			if visibleMain["title"] != "old" || satellite(visibleMain)["description3"] != "old" {
				t.Fatalf("uncommitted partial update visible: %v", visibleMain)
			}
			var unlocked bool
			if err := gate.QueryRow(ctx, `SELECT pg_advisory_unlock($1)`, key).Scan(&unlocked); err != nil || !unlocked {
				t.Fatalf("release gate: unlocked=%v err=%v", unlocked, err)
			}
			locked = false
			await := func(ch <-chan outcome) response {
				select {
				case result := <-ch:
					if result.err != nil {
						t.Fatal(result.err)
					}
					return result.result
				case <-ctx.Done():
					t.Fatal("contending HTTP operation did not finish:", ctx.Err())
					return response{}
				}
			}
			requireOK(t, await(first))
			secondResult := await(second)
			if tc.first == "update" {
				requireOK(t, secondResult)
			} else if tc.second == "update" {
				requireError(t, secondResult, "NOT_FOUND")
			} else {
				requireError(t, secondResult, "ALREADY_DELETED")
			}
			f.assertLink(t, m, "chairs", true)
			if len(f.list(t, object{"id": id})) != 0 {
				t.Fatal("deleted Main was resurrected")
			}
			var title, description, chairType string
			if err := f.pool.QueryRow(ctx, `SELECT m.title,c.description3,c.type::text FROM main m JOIN chairs c ON c.main_id=m.id WHERE m.id=$1`, id).Scan(&title, &description, &chairType); err != nil {
				t.Fatal(err)
			}
			wantTitle, wantType := "old", "abc"
			if tc.first == "update" {
				wantTitle, wantType = "new", "cde"
			}
			if title != wantTitle || description != wantTitle || chairType != wantType {
				t.Fatalf("partial or unexpected update: %s/%s/%s", title, description, chairType)
			}
		})
	}
}

func waitForDBCondition(t *testing.T, ctx context.Context, f *fixture, query string) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var ready bool
		if err := f.pool.QueryRow(ctx, query).Scan(&ready); err != nil {
			t.Fatal(err)
		}
		if ready {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("PostgreSQL lock state was not reached: %v", ctx.Err())
		}
	}
}

func TestMutationAliasesHaveIndependentTransactions(t *testing.T) {
	f := setup(t)
	r := f.gql(t, `mutation {
 first:main(input:{create:{title:"committed first",satellite:{tool:{}}}}){main{id title} deletedId}
 second:main(input:{delete:{id:"9223372036854775807"}}){main{id} deletedId}
}`, nil)
	requireError(t, r, "NOT_FOUND")
	if r.Data["second"] != nil {
		t.Fatalf("failed mutation returned payload: %v", r.Data["second"])
	}
	first, ok := r.Data["first"].(map[string]any)
	if !ok || first["main"] == nil || first["deletedId"] != nil {
		t.Fatalf("first mutation did not return its committed payload: %v", r.Data)
	}
	m := first["main"].(map[string]any)
	rows := f.list(t, object{"id": idOf(m)})
	if len(rows) != 1 || rows[0].(map[string]any)["title"] != "committed first" {
		t.Fatalf("a later business error rolled back a previous root mutation: %v", rows)
	}
}

func TestExactDatabaseColumnsAndEnum(t *testing.T) {
	f := setup(t)
	common := map[string]string{
		"id": "bigint:NO", "created_at": "timestamp with time zone:NO",
		"update_at": "timestamp with time zone:NO", "deleted_at": "timestamp with time zone:YES",
	}
	want := map[string]map[string]string{
		"main":   {"title": "text:NO", "sub_id": "bigint:NO", "sub_obj": "text:NO"},
		"tools":  {"main_id": "bigint:NO", "description1": "text:YES"},
		"tables": {"main_id": "bigint:NO", "description2": "text:YES"},
		"chairs": {"main_id": "bigint:NO", "description3": "text:YES", "type": "USER-DEFINED:NO"},
	}
	for _, columns := range want {
		for key, value := range common {
			columns[key] = value
		}
	}
	rows, err := f.pool.Query(context.Background(), `SELECT table_name,column_name,data_type,is_nullable
FROM information_schema.columns WHERE table_schema='public' AND table_name IN ('main','tools','tables','chairs')`)
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]map[string]string)
	for rows.Next() {
		var table, column, kind, nullable string
		if err := rows.Scan(&table, &column, &kind, &nullable); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if got[table] == nil {
			got[table] = make(map[string]string)
		}
		got[table][column] = kind + ":" + nullable
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("database columns differ from the required contract\nwant=%v\ngot=%v", want, got)
	}
	var enumValues []string
	if err := f.pool.QueryRow(context.Background(), `SELECT array_agg(e.enumlabel::text ORDER BY e.enumsortorder)
FROM pg_enum e JOIN pg_attribute a ON a.atttypid=e.enumtypid
WHERE a.attrelid='chairs'::regclass AND a.attname='type'`).Scan(&enumValues); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(enumValues, []string{"abc", "cde"}) {
		t.Fatalf("chairs.type must be PostgreSQL enum abc,cde: %v", enumValues)
	}
}
