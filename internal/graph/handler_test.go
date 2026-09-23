package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

const (
	toolBranch      = "tool"
	tableBranch     = "table"
	chairBranch     = "chair"
	toolDescription = "description1"
)

func sampleMain(kind domain.Kind) *domain.Main {
	timestamp := time.Date(2026, 9, 15, 13, 42, 10, 123000, time.FixedZone("MSK", 3*60*60))
	common := domain.Satellite{
		ID: 2, MainID: 9223372036854775807, CreatedAt: timestamp, UpdatedAt: timestamp,
	}

	var satellite domain.SubObject

	switch kind {
	case domain.Tools:
		satellite = &domain.Tool{Satellite: common}
	case domain.Tables:
		satellite = &domain.Table{Satellite: common}
	case domain.Chairs:
		satellite = &domain.Chair{Satellite: common, Type: domain.ABC}
	}

	return &domain.Main{ID: 9223372036854775807, Title: "sample", SubID: 2, SubObj: kind,
		CreatedAt: timestamp, UpdatedAt: timestamp, Satellite: satellite}
}

type httpResult struct {
	Data   map[string]json.RawMessage `json:"data"`
	Errors []struct {
		Message    string         `json:"message"`
		Extensions map[string]any `json:"extensions"`
	} `json:"errors"`
}

func requestGraphQL(t *testing.T, handler http.Handler, query string, variables map[string]any) httpResult {
	t.Helper()

	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	require.NoError(t, err)

	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/graphql", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	var result httpResult
	require.NoError(
		t,
		json.Unmarshal(recorder.Body.Bytes(), &result),
		"decode HTTP %d: %s",
		recorder.Code,
		recorder.Body.String(),
	)

	return result
}

func testHandler(service mainDatabase) http.Handler {
	return NewHandler(service, slog.New(slog.DiscardHandler))
}

func jsonObject(t *testing.T, value string) map[string]any {
	t.Helper()

	var decoded map[string]any

	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()

	require.NoError(t, decoder.Decode(&decoded))

	return decoded
}

func TestOneOfAllContainersLiteralsAndVariables(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, literal, variable string }{
		{"root empty", `{}`, `{}`},
		{
			"root multiple",
			`{delete:{id:"1"},create:{title:"x",satellite:{tool:{}}}}`,
			`{"delete":{"id":"1"},"create":{"title":"x","satellite":{"tool":{}}}}`,
		},
		{"root null", `{delete:null}`, `{"delete":null}`},
		{"root extra null", `{delete:{id:"1"},create:null}`, `{"delete":{"id":"1"},"create":null}`},
		{"create empty", `{create:{title:"x",satellite:{}}}`, `{"create":{"title":"x","satellite":{}}}`},
		{
			"create multiple",
			`{create:{title:"x",satellite:{tool:{},table:{}}}}`,
			`{"create":{"title":"x","satellite":{"tool":{},"table":{}}}}`,
		},
		{
			"create null",
			`{create:{title:"x",satellite:{tool:null}}}`,
			`{"create":{"title":"x","satellite":{"tool":null}}}`,
		},
		{
			"create extra null",
			`{create:{title:"x",satellite:{tool:{},chair:null}}}`,
			`{"create":{"title":"x","satellite":{"tool":{},"chair":null}}}`,
		},
		{"update empty", `{update:{id:"1",satellite:{}}}`, `{"update":{"id":"1","satellite":{}}}`},
		{
			"update multiple",
			`{update:{id:"1",satellite:{tool:{description1:"a"},table:{description2:"b"}}}}`,
			`{"update":{"id":"1","satellite":{"tool":{"description1":"a"},"table":{"description2":"b"}}}}`,
		},
		{
			"update null",
			`{update:{id:"1",satellite:{tool:null}}}`,
			`{"update":{"id":"1","satellite":{"tool":null}}}`,
		},
		{
			"update extra null",
			`{update:{id:"1",satellite:{tool:{description1:null},table:null}}}`,
			`{"update":{"id":"1","satellite":{"tool":{"description1":null},"table":null}}}`,
		},
	}
	for _, test := range cases {
		for _, variables := range []bool{false, true} {
			mode := "literal"
			if variables {
				mode = "variables"
			}

			t.Run(test.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				mock := NewMockMainDatabase(gomock.NewController(t))
				query := `mutation { main(input:` + test.literal + `) { deletedId } }`

				var input map[string]any

				if variables {
					query = `mutation($input:MainMutationInput!){main(input:$input){deletedId}}`
					input = map[string]any{"input": jsonObject(t, test.variable)}
				}

				result := requestGraphQL(t, testHandler(mock), query, input)
				require.NotEmpty(t, result.Errors)

				for _, err := range result.Errors {
					require.NotEqual(t, internalServerError, err.Extensions["code"])
				}
			})
		}
	}
}

func TestOneOfValidationBeforeAnyMutationAlias(t *testing.T) {
	t.Parallel()
	mock := NewMockMainDatabase(gomock.NewController(t))
	result := requestGraphQL(t, testHandler(mock), `mutation($bad:MainMutationInput!){
		first:main(input:{create:{title:"first",satellite:{tool:{}}}}){main{id}}
		second:main(input:$bad){deletedId}
	}`, map[string]any{"bad": map[string]any{"delete": map[string]any{"id": "1"}, "create": nil}})
	require.NotEmpty(t, result.Errors)

	for _, err := range result.Errors {
		require.NotEqual(t, internalServerError, err.Extensions["code"])
	}
}

func TestOneOfDirectBranchVariables(t *testing.T) {
	t.Parallel()

	const (
		nullBranch  = "null"
		valueBranch = "provided"
	)

	tests := []struct {
		name, typ, input string
		valid            any
		expect           func(*MockMainDatabase)
	}{
		{
			name: "delete", typ: "MainDeleteInput", input: "{delete:$branch}",
			valid: map[string]any{"id": "1"},
			expect: func(mock *MockMainDatabase) {
				mock.EXPECT().Delete(gomock.Any(), int64(1)).Return(nil)
			},
		},
		{
			name: "create", typ: "ToolCreateInput", input: "{create:{title:\"\",satellite:{tool:$branch}}}",
			valid: map[string]any{},
			expect: func(mock *MockMainDatabase) {
				mock.EXPECT().CreateTool(gomock.Any(), "", postgres.ToolCreate{}).Return(sampleMain(domain.Tools), nil)
			},
		},
		{
			name: "update", typ: "ToolUpdateInput", input: "{update:{id:\"1\",satellite:{tool:$branch}}}",
			valid: map[string]any{toolDescription: nil},
			expect: func(mock *MockMainDatabase) {
				patch := postgres.ToolUpdate{Description1: graphql.OmittableOf[*string](nil)}
				mock.EXPECT().UpdateTool(gomock.Any(), int64(1), graphql.Omittable[*string]{}, patch).
					Return(sampleMain(domain.Tools), nil)
			},
		},
	}
	for _, test := range tests {
		for _, required := range []bool{false, true} {
			for _, state := range []string{"missing", nullBranch, valueBranch} {
				t.Run(fmt.Sprintf("%s/required=%t/%s", test.name, required, state), func(t *testing.T) {
					t.Parallel()
					mock := NewMockMainDatabase(gomock.NewController(t))

					typ := test.typ
					if required {
						typ += "!"
					}

					variables := map[string]any{}
					if state == nullBranch {
						variables["branch"] = nil
					}

					if state == valueBranch {
						variables["branch"] = test.valid
					}

					wantError := !required || state != valueBranch
					if !wantError {
						test.expect(mock)
					}

					result := requestGraphQL(
						t,
						testHandler(mock),
						"mutation($branch:"+typ+"){main(input:"+test.input+"){deletedId}}",
						variables,
					)
					if wantError {
						require.NotEmpty(t, result.Errors)

						for _, err := range result.Errors {
							require.NotEqual(t, internalServerError, err.Extensions["code"])
						}
					} else {
						require.Empty(t, result.Errors)
					}
				})
			}
		}
	}
}

func TestInputMappingPreservesPatchStates(t *testing.T) {
	t.Parallel()

	branches := []struct {
		name, field string
		expect      func(*MockMainDatabase, graphql.Omittable[*string])
	}{
		{
			name:  toolBranch,
			field: toolDescription,
			expect: func(mock *MockMainDatabase, value graphql.Omittable[*string]) {
				mock.EXPECT().UpdateTool(gomock.Any(), int64(1), graphql.Omittable[*string]{},
					postgres.ToolUpdate{Description1: value}).Return(sampleMain(domain.Tools), nil)
			},
		},
		{
			name:  tableBranch,
			field: "description2",
			expect: func(mock *MockMainDatabase, value graphql.Omittable[*string]) {
				mock.EXPECT().UpdateTable(gomock.Any(), int64(1), graphql.Omittable[*string]{},
					postgres.TableUpdate{Description2: value}).Return(sampleMain(domain.Tables), nil)
			},
		},
		{
			name:  chairBranch,
			field: "description3",
			expect: func(mock *MockMainDatabase, value graphql.Omittable[*string]) {
				mock.EXPECT().UpdateChair(gomock.Any(), int64(1), graphql.Omittable[*string]{},
					postgres.ChairUpdate{Description3: value}).Return(sampleMain(domain.Chairs), nil)
			},
		},
	}

	values := []struct {
		name  string
		value *string
	}{
		{name: "null"},
		{name: "empty", value: new("")},
		{name: "text", value: new("value")},
	}
	for _, branch := range branches {
		for _, value := range values {
			t.Run(branch.name+"/"+value.name, func(t *testing.T) {
				t.Parallel()
				mock := NewMockMainDatabase(gomock.NewController(t))
				branch.expect(mock, graphql.OmittableOf(value.value))
				result := requestGraphQL(t, testHandler(mock),
					"mutation($input:MainMutationInput!){main(input:$input){main{id}}}",
					map[string]any{"input": map[string]any{"update": map[string]any{
						"id": "1", "satellite": map[string]any{
							branch.name: map[string]any{branch.field: value.value},
						},
					}}},
				)
				require.Empty(t, result.Errors)
			})
		}
	}
}

func TestTitleOnlyBinding(t *testing.T) {
	t.Parallel()

	for _, title := range []string{"new", ""} {
		t.Run(title, func(t *testing.T) {
			t.Parallel()
			mock := NewMockMainDatabase(gomock.NewController(t))
			mock.EXPECT().UpdateMain(gomock.Any(), int64(1), graphql.OmittableOf(&title)).
				Return(sampleMain(domain.Tools), nil)
			result := requestGraphQL(t, testHandler(mock),
				"mutation($title:String){main(input:{update:{id:1,title:$title}}){main{id}}}",
				map[string]any{"title": title})
			require.Empty(t, result.Errors)
		})
	}
}

func TestPatchVariablePresence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		variables   map[string]any
		description graphql.Omittable[*string]
	}{
		{name: "missing"},
		{name: "null", variables: map[string]any{"description": nil},
			description: graphql.OmittableOf[*string](nil)},
		{name: "provided", variables: map[string]any{"description": "value"},
			description: graphql.OmittableOf(new("value"))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			mock := NewMockMainDatabase(gomock.NewController(t))
			patch := postgres.ChairUpdate{
				Description3: test.description,
				Type:         graphql.OmittableOf(new(domain.CDE)),
			}
			mock.EXPECT().UpdateChair(gomock.Any(), int64(1), graphql.Omittable[*string]{}, patch).
				Return(sampleMain(domain.Chairs), nil)
			result := requestGraphQL(t, testHandler(mock),
				`mutation($description:String){
				main(input:{update:{id:1,satellite:{chair:{type:cde,description3:$description}}}}){main{id}}
			}`,
				test.variables)
			require.Empty(t, result.Errors)
		})
	}
}

func TestGraphQLValidationRejectsBeforeService(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, query string
		variables   map[string]any
	}{
		{name: "missing title", query: `mutation{main(input:{create:{satellite:{tool:{}}}}){deletedId}}`},
		{name: "null title", query: `mutation{main(input:{create:{title:null,satellite:{tool:{}}}}){deletedId}}`},
		{
			name:  "missing chair type",
			query: `mutation{main(input:{create:{title:"x",satellite:{chair:{}}}}){deletedId}}`,
		},
		{
			name:  "null chair type",
			query: `mutation{main(input:{create:{title:"x",satellite:{chair:{type:null}}}}){deletedId}}`,
		},
		{
			name:  "invalid enum",
			query: `mutation{main(input:{create:{title:"x",satellite:{chair:{type:invalid}}}}){deletedId}}`,
		},
		{name: "foreign id", query: `mutation{main(input:{create:{id:"1",title:"x",satellite:{tool:{}}}}){deletedId}}`},
		{name: "sub id", query: `mutation{main(input:{create:{title:"x",sub_id:"1",satellite:{tool:{}}}}){deletedId}}`},
		{
			name:  "satellite owner",
			query: `mutation{main(input:{create:{title:"x",satellite:{tool:{main_id:"1"}}}}){deletedId}}`,
		},
		{name: "deleted timestamp", query: `mutation{main(input:{update:{id:"1",deletedAt:null}}){deletedId}}`},
		{
			name:  "created timestamp",
			query: `mutation{main(input:{update:{id:"1",createdAt:"2026-01-01T00:00:00Z"}}){deletedId}}`,
		},
		{
			name:      "integer overflow variable",
			query:     `query($offset:Int!){main(offset:$offset){id}}`,
			variables: map[string]any{"offset": json.Number("2147483648")},
		},
		{name: "integer overflow literal", query: `{main(offset:2147483648){id}}`},
		{
			name:      "case sensitive enum",
			query:     `mutation($input:MainMutationInput!){main(input:$input){main{id}}}`,
			variables: jsonObject(t, `{"input":{"create":{"title":"x","satellite":{"chair":{"type":"ABC"}}}}}`),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			mock := NewMockMainDatabase(gomock.NewController(t))
			result := requestGraphQL(t, testHandler(mock), test.query, test.variables)
			require.NotEmpty(t, result.Errors)

			for _, err := range result.Errors {
				require.NotEqual(t, internalServerError, err.Extensions["code"])
			}
		})
	}
}

func TestDatabaseErrorsArePresented(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, code string
		err        error
	}{
		{name: "invalid input", code: badUserInput, err: postgres.ErrInvalidInput},
		{name: "not found", code: "NOT_FOUND", err: postgres.ErrNotFound},
		{name: "already deleted", code: "ALREADY_DELETED", err: postgres.ErrAlreadyDeleted},
		{name: "satellite mismatch", code: "SATELLITE_TYPE_MISMATCH", err: postgres.ErrSatelliteTypeMismatch},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			mock := NewMockMainDatabase(gomock.NewController(t))
			mock.EXPECT().Delete(gomock.Any(), int64(1)).Return(fmt.Errorf("operation context: %w", test.err))
			result := requestGraphQL(t, testHandler(mock), `mutation{main(input:{delete:{id:"1"}}){deletedId}}`, nil)
			require.Len(t, result.Errors, 1)
			require.Equal(t, test.code, result.Errors[0].Extensions["code"])
			require.JSONEq(t, "null", string(result.Data["main"]))
		})
	}
}

func TestInvalidIDFormats(t *testing.T) {
	t.Parallel()

	for _, id := range []string{"", "-1", "+1", "1.5", "1e2", " 1", "1 ", "x", "9223372036854775808"} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			mock := NewMockMainDatabase(gomock.NewController(t))
			result := requestGraphQL(t, testHandler(mock), `query($id:ID){main(id:$id){id}}`, map[string]any{"id": id})
			require.Len(t, result.Errors, 1)
			require.Equal(t, badUserInput, result.Errors[0].Extensions["code"])
		})
	}
}

func TestListInputMapping(t *testing.T) {
	t.Parallel()

	mainID := int64(1)
	bigID := int64(9223372036854775807)

	tests := []struct {
		name      string
		variables map[string]any
		want      postgres.ListInput
	}{
		{name: "defaults", want: postgres.ListInput{Limit: 20}},
		{name: "null id", variables: map[string]any{"id": nil}, want: postgres.ListInput{Limit: 20}},
		{name: "id", variables: map[string]any{"id": "1"}, want: postgres.ListInput{ID: &mainID, Limit: 20}},
		{
			name:      "maximum string id",
			variables: map[string]any{"id": "9223372036854775807"},
			want:      postgres.ListInput{ID: &bigID, Limit: 20},
		},
		{
			name:      "maximum numeric id",
			variables: map[string]any{"id": json.Number("9223372036854775807")},
			want:      postgres.ListInput{ID: &bigID, Limit: 20},
		},
		{
			name:      "pagination",
			variables: map[string]any{"limit": 100, "offset": 7},
			want:      postgres.ListInput{Limit: 100, Offset: 7},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			mock := NewMockMainDatabase(gomock.NewController(t))
			mock.EXPECT().List(gomock.Any(), test.want).Return([]*domain.Main{}, nil)

			result := requestGraphQL(
				t,
				testHandler(mock),
				`query($id:ID,$limit:Int! = 20,$offset:Int! = 0){main(id:$id,limit:$limit,offset:$offset){id}}`,
				test.variables,
			)
			require.Empty(t, result.Errors)
			require.JSONEq(t, "[]", string(result.Data["main"]))
		})
	}
}

func TestInputsRejectedBeforeOpeningDatabase(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, query string }{
		{name: "empty update", query: `mutation{main(input:{update:{id:"1"}}){main{id}}}`},
		{name: "null title", query: `mutation{main(input:{update:{id:"1",title:null}}){main{id}}}`},
		{name: "null satellite", query: `mutation{main(input:{update:{id:"1",satellite:null}}){main{id}}}`},
		{name: "empty tool patch", query: `mutation{main(input:{update:{id:"1",satellite:{tool:{}}}}){main{id}}}`},
		{name: "empty table patch", query: `mutation{main(input:{update:{id:"1",satellite:{table:{}}}}){main{id}}}`},
		{name: "empty chair patch", query: `mutation{main(input:{update:{id:"1",satellite:{chair:{}}}}){main{id}}}`},
		{
			name:  "null chair type",
			query: `mutation{main(input:{update:{id:"1",satellite:{chair:{type:null}}}}){main{id}}}`,
		},
		{name: "limit zero", query: `{main(limit:0){id}}`},
		{name: "limit too large", query: `{main(limit:101){id}}`},
		{name: "negative offset", query: `{main(offset:-1){id}}`},
		{name: "zero query id", query: `{main(id:"0"){id}}`},
		{name: "zero update id", query: `mutation{main(input:{update:{id:"0",title:"x"}}){main{id}}}`},
		{name: "zero delete id", query: `mutation{main(input:{delete:{id:"0"}}){deletedId}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result := requestGraphQL(t, testHandler(&postgres.DB{}), test.query, nil)
			require.Len(t, result.Errors, 1)
			require.Equal(t, badUserInput, result.Errors[0].Extensions["code"])
		})
	}
}

func TestSchemaHasOnlyRequiredBusinessRoots(t *testing.T) {
	t.Parallel()

	result := requestGraphQL(
		t,
		testHandler(NewMockMainDatabase(gomock.NewController(t))),
		`{__schema{queryType{fields{name}}mutationType{fields{name}}subscriptionType{name}}}`,
		nil,
	)
	require.Empty(t, result.Errors)

	var schema struct {
		QueryType struct {
			Fields []struct {
				Name string `json:"name"`
			} `json:"fields"`
		} `json:"queryType"`
		MutationType struct {
			Fields []struct {
				Name string `json:"name"`
			} `json:"fields"`
		} `json:"mutationType"`
		SubscriptionType any `json:"subscriptionType"`
	}
	require.NoError(t, json.Unmarshal(result.Data["__schema"], &schema))

	require.Len(t, schema.QueryType.Fields, 1)
	require.Equal(t, "main", schema.QueryType.Fields[0].Name)
	require.Len(t, schema.MutationType.Fields, 1)
	require.Equal(t, "main", schema.MutationType.Fields[0].Name)
	require.Nil(t, schema.SubscriptionType)
}

func TestOneOfSchema(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"MainMutationInput", "SatelliteCreateInput", "SatelliteUpdateInput"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result := requestGraphQL(
				t,
				testHandler(NewMockMainDatabase(gomock.NewController(t))),
				`{__type(name:"`+name+`"){isOneOf inputFields{name defaultValue type{kind}}}}`,
				nil,
			)

			require.Empty(t, result.Errors)

			var typ struct {
				IsOneOf     bool `json:"isOneOf"`
				InputFields []struct {
					Name         string `json:"name"`
					DefaultValue any    `json:"defaultValue"`
					Type         struct {
						Kind string `json:"kind"`
					} `json:"type"`
				} `json:"inputFields"`
			}
			require.NoError(t, json.Unmarshal(result.Data["__type"], &typ))

			require.True(t, typ.IsOneOf)
			require.Len(t, typ.InputFields, 3)

			for _, field := range typ.InputFields {
				require.Nil(t, field.DefaultValue)
				require.NotEqual(t, "NON_NULL", field.Type.Kind)
			}
		})
	}
}
func TestCreateInputAndOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind, typename, contents, fragment string
		expect                             func(*MockMainDatabase, *domain.Main)
	}{
		{kind: toolBranch, typename: "Tool", contents: "{}", fragment: toolDescription,
			expect: func(mock *MockMainDatabase, value *domain.Main) {
				mock.EXPECT().CreateTool(gomock.Any(), "", postgres.ToolCreate{}).Return(value, nil)
			}},
		{kind: tableBranch, typename: "Table", contents: "{}", fragment: "description2",
			expect: func(mock *MockMainDatabase, value *domain.Main) {
				mock.EXPECT().CreateTable(gomock.Any(), "", postgres.TableCreate{}).Return(value, nil)
			}},
		{kind: chairBranch, typename: "Chair", contents: "{type:abc}", fragment: "description3 type",
			expect: func(mock *MockMainDatabase, value *domain.Main) {
				mock.EXPECT().CreateChair(gomock.Any(), "", postgres.ChairCreate{Type: domain.ABC}).Return(value, nil)
			}},
	}
	for _, test := range tests {
		t.Run(test.kind, func(t *testing.T) {
			t.Parallel()
			mock := NewMockMainDatabase(gomock.NewController(t))
			kind := map[string]domain.Kind{
				toolBranch:  domain.Tools,
				tableBranch: domain.Tables,
				chairBranch: domain.Chairs,
			}[test.kind]
			value := sampleMain(kind)
			test.expect(mock, value)

			result := requestGraphQL(
				t,
				testHandler(mock),
				`mutation {
					main(input:{create:{title:"",satellite:{`+test.kind+`:`+test.contents+`}}}) {
						deletedId
						main {
							id title createdAt updatedAt deletedAt
							satellite {
								__typename
								... on `+test.typename+` { id `+test.fragment+` createdAt updatedAt deletedAt }
							}
						}
					}
				}`,
				nil,
			)
			require.Empty(t, result.Errors)

			var payload struct {
				DeletedID *string `json:"deletedId"`
				Main      struct {
					ID        string         `json:"id"`
					Title     string         `json:"title"`
					CreatedAt string         `json:"createdAt"`
					UpdatedAt string         `json:"updatedAt"`
					DeletedAt *string        `json:"deletedAt"`
					Satellite map[string]any `json:"satellite"`
				} `json:"main"`
			}
			require.NoError(t, json.Unmarshal(result.Data["main"], &payload))

			require.Nil(t, payload.DeletedID)
			require.Equal(t, "9223372036854775807", payload.Main.ID)
			require.Equal(t, value.Title, payload.Main.Title)
			require.Equal(t, test.typename, payload.Main.Satellite["__typename"])
			require.Equal(t, "2", payload.Main.Satellite["id"])
			require.Equal(t, "2026-09-15T10:42:10.000123Z", payload.Main.CreatedAt)
			require.Equal(t, payload.Main.CreatedAt, payload.Main.UpdatedAt)
			require.Nil(t, payload.Main.DeletedAt)
		})
	}
}

func TestInternalErrorsAreSanitizedAndLogged(t *testing.T) {
	t.Parallel()

	cause := errors.New("postgres://secret@host SELECT sensitive")

	tests := []struct {
		name string
		err  error
	}{
		{name: "plain", err: cause},
		{name: "wrapped", err: fmt.Errorf("list database records: %w", cause)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			mock := NewMockMainDatabase(gomock.NewController(t))
			mock.EXPECT().List(gomock.Any(), postgres.ListInput{Limit: 20}).Return(nil, test.err)

			var log bytes.Buffer

			result := requestGraphQL(t, NewHandler(mock, slog.New(slog.NewTextHandler(&log, nil))), "{main{id}}", nil)
			require.Len(t, result.Errors, 1)
			require.Equal(t, "internal server error", result.Errors[0].Message)
			require.Equal(t, internalServerError, result.Errors[0].Extensions["code"])

			require.Contains(t, log.String(), test.err.Error())
		})
	}
}

func TestResolverPanicIsSanitizedAndLogged(t *testing.T) {
	t.Parallel()
	mock := NewMockMainDatabase(gomock.NewController(t))
	mock.EXPECT().
		List(gomock.Any(), postgres.ListInput{Limit: 20}).
		DoAndReturn(func(context.Context, postgres.ListInput) ([]*domain.Main, error) {
			panic("private panic")
		})

	var log bytes.Buffer

	result := requestGraphQL(t, NewHandler(mock, slog.New(slog.NewTextHandler(&log, nil))), "{main{id}}", nil)
	require.Len(t, result.Errors, 1)
	require.Equal(t, "internal server error", result.Errors[0].Message)
	require.Equal(t, internalServerError, result.Errors[0].Extensions["code"])

	require.Contains(t, log.String(), "private panic")
}

func TestListComplexityIncludesPageSizeAndAliases(t *testing.T) {
	t.Parallel()
	mock := NewMockMainDatabase(gomock.NewController(t))

	var query strings.Builder
	query.WriteString("{")

	for i := range 101 {
		fmt.Fprintf(&query, "page%d:main(limit:100){id}", i)
	}

	query.WriteString("}")
	result := requestGraphQL(t, testHandler(mock), query.String(), nil)
	require.NotEmpty(t, result.Errors)

	for _, err := range result.Errors {
		require.NotEqual(t, internalServerError, err.Extensions["code"])
	}
}

func TestMaximumPageAllowsCompleteSelection(t *testing.T) {
	t.Parallel()
	mock := NewMockMainDatabase(gomock.NewController(t))
	mock.EXPECT().List(gomock.Any(), postgres.ListInput{Limit: 100}).Return([]*domain.Main{}, nil)

	result := requestGraphQL(t, testHandler(mock), `{
		main(limit:100) {
			id title createdAt updatedAt deletedAt
			satellite {
				__typename
				... on Tool { id description1 createdAt updatedAt deletedAt }
				... on Table { id description2 createdAt updatedAt deletedAt }
				... on Chair { id description3 type createdAt updatedAt deletedAt }
			}
		}
	}`, nil)
	require.Empty(t, result.Errors)
	require.JSONEq(t, "[]", string(result.Data["main"]))
}
