//go:build integration

package integration_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSatelliteProjectionPreservesBigint(t *testing.T) {
	for _, test := range []struct {
		branch string
		table  string
		input  object
	}{
		{branch: toolBranch, table: toolsTable, input: object{}},
		{branch: tableBranch, table: tablesTable, input: object{tableDescriptionField: ""}},
		{branch: chairBranch, table: chairsTable, input: object{typeField: chairCDE, chairDescriptionField: "text"}},
	} {
		t.Run(test.branch, func(t *testing.T) {
			testFixture := setup(t)
			testFixture.exec(t, `
			    SELECT setval(pg_get_serial_sequence('main', 'id'), $1, false),
			           setval(pg_get_serial_sequence($2, 'id'), $1, false);
			`, int64(math.MaxInt64), test.table)
			main := testFixture.create(t, test.branch, "large IDs", test.input)
			require.Equal(t, "9223372036854775807", idOf(t, main))
			require.Equal(t, "9223372036854775807", satellite(t, main)["id"])
			require.Equal(t, main[createdAtField], satellite(t, main)[createdAtField])
			require.Equal(t, []any{main}, testFixture.list(t, object{"id": idOf(t, main)}))

			result := testFixture.gql(t, mutate, object{inputArgument: object{updateOperation: object{
				"id": idOf(t, main), titleField: "changed title",
			}}})
			main = updated(t, result)
			require.Equal(t, "9223372036854775807", satellite(t, main)["id"])
			require.Equal(t, []any{main}, testFixture.list(t, object{"id": idOf(t, main)}))

			var id, subID int64

			err := testFixture.pool.QueryRow(t.Context(), `SELECT id, sub_id FROM main;`).Scan(&id, &subID)
			require.NoError(t, err)
			require.Equal(t, int64(math.MaxInt64), id)
			require.Equal(t, int64(math.MaxInt64), subID)
		})
	}
}
