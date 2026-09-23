//go:build integration

package integration_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const typenameField = "__typename"

func TestContendingWritesWaitAndRecheckState(t *testing.T) {
	for _, testCase := range []struct{ name, first, second, wantCode, wantTitle, wantType string }{
		{"update_then_delete", updateOperation, deleteOperation, "", updatedValue, chairCDE},
		{"delete_then_update", deleteOperation, updateOperation, "NOT_FOUND", previousValue, chairABC},
		{"delete_then_delete", deleteOperation, deleteOperation, "ALREADY_DELETED", previousValue, chairABC},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			testFixture := setup(t)
			main := testFixture.create(
				t,
				chairBranch,
				previousValue,
				object{chairDescriptionField: previousValue, typeField: chairABC},
			)
			mainID := idOf(t, main)

			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()

			gate, err := testFixture.pool.Acquire(ctx)
			require.NoError(t, err)

			const key int64 = 81824791

			_, err = gate.Exec(ctx, `SELECT pg_advisory_lock($1)`, key)
			if err != nil {
				gate.Release()
				require.NoError(t, err)
			}

			locked := true

			defer func() {
				defer gate.Release()

				if locked {
					cleanupCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
					defer done()

					if _, err := gate.Exec(cleanupCtx, `SELECT pg_advisory_unlock($1)`, key); err != nil {
						closeErr := gate.Conn().Close(cleanupCtx)
						require.NoError(t, errors.Join(err, closeErr), "release connection holding advisory lock")
					}
				}
			}()

			testFixture.exec(
				t,
				fmt.Sprintf(`CREATE FUNCTION test_hold_satellite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM pg_advisory_xact_lock(%d::bigint); RETURN NEW; END $$`, key),
			)
			testFixture.exec(
				t,
				`CREATE TRIGGER test_hold_satellite BEFORE UPDATE ON chairs FOR EACH ROW EXECUTE FUNCTION test_hold_satellite()`,
			)

			type outcome struct {
				result response
				err    error
			}

			inputs := map[string]object{
				deleteOperation: {deleteOperation: object{"id": mainID}},
				updateOperation: {
					updateOperation: object{
						"id":       mainID,
						titleField: updatedValue,
						satelliteField: object{
							chairBranch: object{chairDescriptionField: updatedValue, typeField: chairCDE},
						},
					},
				},
			}
			start := func(input object) <-chan outcome {
				results := make(chan outcome, 1)

				go func() {
					reply, err := testFixture.request(t.Context(), mutate, object{inputArgument: input})
					results <- outcome{result: reply, err: err}
				}()

				return results
			}
			first := start(inputs[testCase.first])

			waitForDBCondition(t, ctx, testFixture, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
WHERE datname=current_database() AND wait_event_type='Lock' AND wait_event='advisory')`)

			second := start(inputs[testCase.second])

			waitForDBCondition(t, ctx, testFixture, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
WHERE datname=current_database() AND wait_event_type='Lock'
AND wait_event IN ('transactionid','tuple') AND query LIKE '%FROM main WHERE id = $1 FOR UPDATE%')`)
			visible := testFixture.list(t, object{"id": mainID})
			require.Len(t, visible, 1, "uncommitted deletion visible")
			visibleMain, validType := visible[0].(object)
			require.True(t, validType)
			require.Equal(t, previousValue, visibleMain[titleField])
			require.Equal(t, previousValue, satellite(t, visibleMain)[chairDescriptionField])

			var unlocked bool
			require.NoError(t, gate.QueryRow(ctx, `SELECT pg_advisory_unlock($1)`, key).Scan(&unlocked))
			require.True(t, unlocked)

			locked = false
			await := func(results <-chan outcome) response {
				select {
				case result := <-results:
					require.NoError(t, result.err)

					return result.result
				case <-ctx.Done():
					require.FailNow(t, "contending HTTP operation did not finish", "%v", ctx.Err())

					return response{}
				}
			}
			require.Empty(t, await(first).Errors)

			secondResult := await(second)
			if testCase.wantCode == "" {
				require.Empty(t, secondResult.Errors)
			} else {
				require.NotEmpty(t, secondResult.Errors)
				require.Equal(t, testCase.wantCode, secondResult.Errors[0].Extensions["code"])
			}

			testFixture.assertLink(t, main, chairsTable, true)
			require.Empty(t, testFixture.list(t, object{"id": mainID}))

			var title, description, chairType string
			require.NoError(
				t,
				testFixture.pool.QueryRow(ctx, `SELECT m.title,c.description3,c.type::text
FROM main m JOIN chairs c ON c.main_id=m.id WHERE m.id=$1`, mainID).
					Scan(&title, &description, &chairType),
			)
			require.Equal(
				t,
				[]string{testCase.wantTitle, testCase.wantTitle, testCase.wantType},
				[]string{title, description, chairType},
			)
		})
	}
}

func waitForDBCondition(t *testing.T, ctx context.Context, testFixture *fixture, query string) {
	t.Helper()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		var ready bool
		require.NoError(t, testFixture.pool.QueryRow(ctx, query).Scan(&ready))

		if ready {
			return
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			require.FailNow(t, "PostgreSQL lock state was not reached", "%v", ctx.Err())
		}
	}
}

func TestMutationAliasesHaveIndependentTransactions(t *testing.T) {
	testFixture := setup(t)
	reply := testFixture.gql(t, `mutation {
 first:main(input:{create:{title:"committed first",satellite:{tool:{}}}}){main{id title} deletedId}
 second:main(input:{delete:{id:"9223372036854775807"}}){main{id} deletedId}
}`, nil)
	result := reply
	require.NotEmpty(t, result.Errors)
	require.Equal(t, "NOT_FOUND", result.Errors[0].Extensions["code"])
	require.Nil(t, reply.Data["second"])
	first, validType := reply.Data["first"].(map[string]any)
	require.True(t, validType)
	require.NotNil(t, first[mainField])
	require.Nil(t, first[deletedIDField])
	main, validType := first[mainField].(object)
	require.True(t, validType)
	rows := testFixture.list(t, object{"id": idOf(t, main)})
	require.Len(t, rows, 1)
	main, validType = rows[0].(object)
	require.True(t, validType)
	require.Equal(t, "committed first", main[titleField])
}

func TestStrictInputVariablesNeverWrite(t *testing.T) {
	testFixture := setup(t)
	chair := testFixture.create(t, chairBranch, previousValue, object{typeField: chairABC})
	mainID := idOf(t, chair)

	tests := []struct {
		name      string
		query     string
		variables object
	}{
		{
			name:  "uppercase create enum",
			query: mutate,
			variables: object{inputArgument: object{createOperation: object{
				titleField:     updatedValue,
				satelliteField: object{chairBranch: object{typeField: "ABC"}},
			}}},
		},
		{
			name:  "uppercase update enum",
			query: mutate,
			variables: object{inputArgument: object{updateOperation: object{
				"id":           mainID,
				titleField:     updatedValue,
				satelliteField: object{chairBranch: object{typeField: "CDE"}},
			}}},
		},
		{
			name: "uppercase enum in direct variable",
			query: `mutation($type:ChairType!){
main(input:{create:{title:"changed",satellite:{chair:{type:$type}}}}){main{id}}
}`,
			variables: object{typeField: "ABC"},
		},
		{
			name:  "typename inside create input",
			query: mutate,
			variables: object{inputArgument: object{createOperation: object{
				titleField:     updatedValue,
				typenameField:  "MainCreateInput",
				satelliteField: object{toolBranch: object{}},
			}}},
		},
		{
			name:  "typename inside concrete create input",
			query: mutate,
			variables: object{inputArgument: object{createOperation: object{
				titleField:     updatedValue,
				satelliteField: object{toolBranch: object{typenameField: "ToolCreateInput"}},
			}}},
		},
		{
			name:  "typename inside update input",
			query: mutate,
			variables: object{inputArgument: object{updateOperation: object{
				"id":          mainID,
				titleField:    updatedValue,
				typenameField: "MainUpdateInput",
			}}},
		},
		{
			name:  "typename inside concrete update input",
			query: mutate,
			variables: object{inputArgument: object{updateOperation: object{
				"id":           mainID,
				titleField:     updatedValue,
				satelliteField: object{chairBranch: object{typeField: chairCDE, typenameField: "ChairUpdateInput"}},
			}}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := testFixture.snapshot(t)
			result := testFixture.gql(t, test.query, test.variables)
			require.NotEmpty(t, result.Errors)
			require.Equal(t, "BAD_USER_INPUT", result.Errors[0].Extensions["code"])
			require.JSONEq(t, before, testFixture.snapshot(t))
		})
	}
}

func TestExactDatabaseColumnsAndEnum(t *testing.T) {
	testFixture := setup(t)
	common := map[string]string{
		"id": requiredBigint, "created_at": "timestamp with time zone:NO",
		"update_at": "timestamp with time zone:NO", "deleted_at": "timestamp with time zone:YES",
	}

	want := map[string]map[string]string{
		mainField:   {titleField: "text:NO", "sub_id": requiredBigint, "sub_obj": "text:NO"},
		toolsTable:  {mainIDColumn: requiredBigint, toolDescriptionField: nullableText},
		tablesTable: {mainIDColumn: requiredBigint, tableDescriptionField: nullableText},
		chairsTable: {mainIDColumn: requiredBigint, chairDescriptionField: nullableText, typeField: "USER-DEFINED:NO"},
	}
	for _, columns := range want {
		maps.Copy(columns, common)
	}

	rows, err := testFixture.pool.Query(t.Context(), `SELECT table_name,column_name,data_type,is_nullable
FROM information_schema.columns WHERE table_schema='public' AND table_name IN ('main','tools','tables','chairs')`)
	require.NoError(t, err)

	defer rows.Close()

	got := make(map[string]map[string]string)

	for rows.Next() {
		var table, column, kind, nullable string
		require.NoError(t, rows.Scan(&table, &column, &kind, &nullable))

		if got[table] == nil {
			got[table] = make(map[string]string)
		}

		got[table][column] = kind + ":" + nullable
	}

	rows.Close()
	require.NoError(t, rows.Err())
	require.Equal(t, want, got)

	var enumValues []string
	require.NoError(
		t,
		testFixture.pool.QueryRow(t.Context(), `SELECT array_agg(e.enumlabel::text ORDER BY e.enumsortorder)
FROM pg_enum e JOIN pg_attribute a ON a.atttypid=e.enumtypid
WHERE a.attrelid='chairs'::regclass AND a.attname='type'`).Scan(&enumValues),
	)
	require.Equal(t, []string{chairABC, chairCDE}, enumValues)
}
