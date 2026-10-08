package graph

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

const jsonNull = "null"

func TestSatelliteProjection(t *testing.T) {
	t.Parallel()

	types := []struct {
		kind     domain.Kind
		typename string
		field    string
		extra    string
		fragment string
	}{
		{kind: domain.Tools, typename: "Tool", field: toolDescription},
		{kind: domain.Tables, typename: "Table", field: tableDescription},
		{kind: domain.Chairs, typename: "Chair", field: chairDescription, extra: `,"type":"cde"`, fragment: "type"},
	}
	descriptions := []struct {
		name  string
		value string
	}{
		{name: "SQL NULL", value: jsonNull},
		{name: "empty string", value: `""`},
		{name: "text", value: `"line\n\"quoted\" \\"`},
	}

	for _, typ := range types {
		for _, description := range descriptions {
			t.Run(typ.typename+"/"+description.name, func(t *testing.T) {
				t.Parallel()

				controller := gomock.NewController(t)
				reader := NewMockMainReader(controller)
				writer := NewMockMainWriter(controller)
				data := fmt.Sprintf(`{
					"id":9223372036854775807,"main_id":1,
					"created_at":"2026-09-15T13:42:10.123456+03:00",
					"update_at":"2026-09-16T00:00:00.000001-04:00","deleted_at":null,
					%q:%s%s
				}`, typ.field, description.value, typ.extra)
				main := sampleMain(t, typ.kind)
				main.ID = 1
				require.NoError(t, json.Unmarshal([]byte(data), &main.Satellite))
				reader.EXPECT().List(gomock.Any(), postgres.ListInput{Limit: 20}).Return([]*domain.Main{main}, nil)

				selection := fmt.Sprintf(`{
					__typename ... on %s { id %s %s createdAt updatedAt deletedAt }
				}`, typ.typename, typ.field, typ.fragment)
				result := requestGraphQL(t, NewHandler(reader, writer), `{
					main {
						id first:satellite `+selection+` second:satellite `+selection+`
						omitted:satellite @skip(if:true) { __typename }
					}
				}`, nil)
				require.Empty(t, result.Errors)

				want := fmt.Sprintf(`{
					"__typename":%q,"id":"9223372036854775807",
					"createdAt":"2026-09-15T10:42:10.123456Z",
					"updatedAt":"2026-09-16T04:00:00.000001Z","deletedAt":null,
					%q:%s%s
				}`, typ.typename, typ.field, description.value, typ.extra)
				require.JSONEq(t, `[{"id":"1","first":`+want+`,"second":`+want+`}]`, string(result.Data["main"]))

				serialized, err := json.Marshal(main.Satellite)
				require.NoError(t, err)
				require.JSONEq(t, data, string(serialized))
			})
		}
	}
}
