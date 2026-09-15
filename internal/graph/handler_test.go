package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
)

type serviceSpy struct {
	calls      int
	create     domain.CreateInput
	update     domain.UpdateInput
	list       domain.ListInput
	err        error
	panicValue any
}

func (s *serviceSpy) List(_ context.Context, input domain.ListInput) ([]*domain.Main, error) {
	s.calls++
	s.list = input
	if s.panicValue != nil {
		panic(s.panicValue)
	}
	if s.err != nil {
		return nil, s.err
	}
	return []*domain.Main{}, domain.ValidateList(input)
}

func (s *serviceSpy) Create(_ context.Context, input domain.CreateInput) (*domain.Main, error) {
	s.calls++
	s.create = input
	if s.err != nil {
		return nil, s.err
	}
	if err := domain.ValidateCreate(input); err != nil {
		return nil, err
	}
	value := sampleMain(input.Kind)
	value.Title = input.Title
	switch satellite := value.Satellite.(type) {
	case *domain.Tool:
		satellite.Description1 = input.Description
	case *domain.Table:
		satellite.Description2 = input.Description
	case *domain.Chair:
		satellite.Description3 = input.Description
		satellite.Type = input.ChairType
	}
	return value, nil
}

func (s *serviceSpy) Update(_ context.Context, input domain.UpdateInput) (*domain.Main, error) {
	s.calls++
	s.update = input
	if s.err != nil {
		return nil, s.err
	}
	if err := domain.ValidateUpdate(input); err != nil {
		return nil, err
	}
	return sampleMain(domain.Tools), nil
}

func (s *serviceSpy) Delete(_ context.Context, id int64) (int64, error) {
	s.calls++
	return id, s.err
}

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
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var result httpResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode HTTP %d: %v: %s", recorder.Code, err, recorder.Body.String())
	}
	return result
}

func testHandler(service *serviceSpy) http.Handler {
	return newHandler(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func jsonObject(t *testing.T, value string) map[string]any {
	t.Helper()
	var decoded map[string]any
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func expectRejected(t *testing.T, result httpResult) {
	t.Helper()
	if len(result.Errors) == 0 {
		t.Fatalf("expected GraphQL errors, got %+v", result)
	}
	for _, err := range result.Errors {
		if err.Extensions["code"] == domain.InternalServerError {
			t.Fatalf("invalid input caused an internal error: %+v", result.Errors)
		}
	}
}

func TestOneOfAllContainersLiteralsAndVariables(t *testing.T) {
	cases := []struct{ name, literal, variable string }{
		{"root empty", `{}`, `{}`},
		{"root multiple", `{delete:{id:"1"},create:{title:"x",satellite:{tool:{}}}}`, `{"delete":{"id":"1"},"create":{"title":"x","satellite":{"tool":{}}}}`},
		{"root null", `{delete:null}`, `{"delete":null}`},
		{"root extra null", `{delete:{id:"1"},create:null}`, `{"delete":{"id":"1"},"create":null}`},
		{"create empty", `{create:{title:"x",satellite:{}}}`, `{"create":{"title":"x","satellite":{}}}`},
		{"create multiple", `{create:{title:"x",satellite:{tool:{},table:{}}}}`, `{"create":{"title":"x","satellite":{"tool":{},"table":{}}}}`},
		{"create null", `{create:{title:"x",satellite:{tool:null}}}`, `{"create":{"title":"x","satellite":{"tool":null}}}`},
		{"create extra null", `{create:{title:"x",satellite:{tool:{},chair:null}}}`, `{"create":{"title":"x","satellite":{"tool":{},"chair":null}}}`},
		{"update empty", `{update:{id:"1",satellite:{}}}`, `{"update":{"id":"1","satellite":{}}}`},
		{"update multiple", `{update:{id:"1",satellite:{tool:{description1:"a"},table:{description2:"b"}}}}`, `{"update":{"id":"1","satellite":{"tool":{"description1":"a"},"table":{"description2":"b"}}}}`},
		{"update null", `{update:{id:"1",satellite:{tool:null}}}`, `{"update":{"id":"1","satellite":{"tool":null}}}`},
		{"update extra null", `{update:{id:"1",satellite:{tool:{description1:null},table:null}}}`, `{"update":{"id":"1","satellite":{"tool":{"description1":null},"table":null}}}`},
	}
	for _, test := range cases {
		for _, variables := range []bool{false, true} {
			mode := "literal"
			if variables {
				mode = "variables"
			}
			t.Run(test.name+"/"+mode, func(t *testing.T) {
				spy := &serviceSpy{}
				query := `mutation { main(input:` + test.literal + `) { deletedId } }`
				var input map[string]any
				if variables {
					query = `mutation($input:MainMutationInput!){main(input:$input){deletedId}}`
					input = map[string]any{"input": jsonObject(t, test.variable)}
				}
				expectRejected(t, requestGraphQL(t, testHandler(spy), query, input))
				if spy.calls != 0 {
					t.Fatalf("invalid OneOf reached service %d times", spy.calls)
				}
			})
		}
	}
}

func TestOneOfValidationBeforeAnyMutationAlias(t *testing.T) {
	spy := &serviceSpy{}
	result := requestGraphQL(t, testHandler(spy), `mutation($bad:MainMutationInput!){
		first:main(input:{create:{title:"first",satellite:{tool:{}}}}){main{id}}
		second:main(input:$bad){deletedId}
	}`, map[string]any{"bad": map[string]any{"delete": map[string]any{"id": "1"}, "create": nil}})
	expectRejected(t, result)
	if spy.calls != 0 {
		t.Fatalf("an earlier alias executed before invalid OneOf rejection: %d", spy.calls)
	}
}

func TestOneOfDirectBranchVariables(t *testing.T) {
	cases := []struct {
		name, typ, input string
		valid            any
	}{
		{"root", "MainDeleteInput", `{delete:$branch}`, map[string]any{"id": "1"}},
		{"create", "ToolCreateInput", `{create:{title:"",satellite:{tool:$branch}}}`, map[string]any{}},
		{"update", "ToolUpdateInput", `{update:{id:"1",satellite:{tool:$branch}}}`, map[string]any{"description1": nil}},
	}
	for _, test := range cases {
		for _, required := range []bool{false, true} {
			for _, state := range []string{"missing", "null", "value"} {
				t.Run(test.name+"/"+map[bool]string{false: "nullable", true: "non-null"}[required]+"/"+state, func(t *testing.T) {
					typ := test.typ
					if required {
						typ += "!"
					}
					variables := map[string]any{}
					if state == "null" {
						variables["branch"] = nil
					}
					if state == "value" {
						variables["branch"] = test.valid
					}
					spy := &serviceSpy{}
					result := requestGraphQL(t, testHandler(spy), `mutation($branch:`+typ+`){main(input:`+test.input+`){deletedId}}`, variables)
					if required && state == "value" {
						if len(result.Errors) != 0 || spy.calls != 1 {
							t.Fatalf("valid OneOf rejected: %+v, calls=%d", result.Errors, spy.calls)
						}
					} else {
						expectRejected(t, result)
						if spy.calls != 0 {
							t.Fatal("invalid branch variable reached service")
						}
					}
				})
			}
		}
	}
}

func TestInputMappingPreservesPatchStates(t *testing.T) {
	cases := []struct {
		name, fields                                                     string
		titleSet, satelliteSet, descriptionSet, descriptionNull, typeSet bool
		description                                                      string
	}{
		{"title only", `"title":"new"`, true, false, false, false, false, ""},
		{"description value", `"satellite":{"tool":{"description1":"value"}}`, false, true, true, false, false, "value"},
		{"description null", `"satellite":{"tool":{"description1":null}}`, false, true, true, true, false, ""},
		{"description empty", `"satellite":{"table":{"description2":""}}`, false, true, true, false, false, ""},
		{"chair type only", `"satellite":{"chair":{"type":"cde"}}`, false, true, false, false, true, ""},
		{"chair description", `"satellite":{"chair":{"description3":"seat"}}`, false, true, true, false, false, "seat"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			spy := &serviceSpy{}
			input := jsonObject(t, `{"update":{"id":"1",`+test.fields+`}}`)
			result := requestGraphQL(t, testHandler(spy), `mutation($input:MainMutationInput!){main(input:$input){main{id}}}`, map[string]any{"input": input})
			if len(result.Errors) != 0 {
				t.Fatalf("unexpected errors: %+v", result.Errors)
			}
			if spy.update.Title.Set != test.titleSet || spy.update.Satellite.Set != test.satelliteSet {
				t.Fatalf("lost patch presence: %+v", spy.update)
			}
			if test.satelliteSet {
				patch := spy.update.Satellite.Value
				if patch == nil || patch.Description.Set != test.descriptionSet || patch.Type.Set != test.typeSet {
					t.Fatalf("lost satellite patch presence: %+v", patch)
				}
				if test.descriptionSet {
					if (patch.Description.Value == nil) != test.descriptionNull {
						t.Fatalf("lost explicit null: %+v", patch.Description)
					}
					if !test.descriptionNull && *patch.Description.Value != test.description {
						t.Fatalf("description = %q", *patch.Description.Value)
					}
				}
			}
		})
	}
}

func TestMissingVariableInsidePatchStaysAbsent(t *testing.T) {
	spy := &serviceSpy{}
	result := requestGraphQL(t, testHandler(spy), `mutation($description:String){main(input:{update:{id:"1",satellite:{chair:{type:cde,description3:$description}}}}){main{id}}}`, nil)
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", result.Errors)
	}
	if spy.update.Satellite.Value.Description.Set {
		t.Fatal("missing variable was coerced into explicit null")
	}
}

func TestRequiredAndForbiddenInputs(t *testing.T) {
	inputs := []string{
		`{create:{satellite:{tool:{}}}}`,
		`{create:{title:null,satellite:{tool:{}}}}`,
		`{create:{title:"x",satellite:{chair:{}}}}`,
		`{create:{title:"x",satellite:{chair:{type:null}}}}`,
		`{create:{title:"x",satellite:{chair:{type:invalid}}}}`,
		`{create:{id:"1",title:"x",satellite:{tool:{}}}}`,
		`{create:{title:"x",sub_id:"1",satellite:{tool:{}}}}`,
		`{create:{title:"x",satellite:{tool:{main_id:"1"}}}}`,
		`{update:{id:"1",deletedAt:null}}`,
		`{update:{id:"1",createdAt:"2026-01-01T00:00:00Z"}}`,
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			spy := &serviceSpy{}
			expectRejected(t, requestGraphQL(t, testHandler(spy), `mutation{main(input:`+input+`){deletedId}}`, nil))
			if spy.calls != 0 {
				t.Fatal("invalid schema input reached service")
			}
		})
	}
}

func TestInvalidPatchAndArguments(t *testing.T) {
	queries := []string{
		`mutation{main(input:{update:{id:"1"}}){main{id}}}`,
		`mutation{main(input:{update:{id:"1",title:null}}){main{id}}}`,
		`mutation{main(input:{update:{id:"1",satellite:null}}){main{id}}}`,
		`mutation{main(input:{update:{id:"1",satellite:{tool:{}}}}){main{id}}}`,
		`mutation{main(input:{update:{id:"1",satellite:{table:{}}}}){main{id}}}`,
		`mutation{main(input:{update:{id:"1",satellite:{chair:{}}}}){main{id}}}`,
		`mutation{main(input:{update:{id:"1",satellite:{chair:{type:null}}}}){main{id}}}`,
		`{main(limit:0){id}}`, `{main(limit:101){id}}`, `{main(offset:-1){id}}`,
	}
	for _, query := range queries {
		t.Run(query, func(t *testing.T) {
			result := requestGraphQL(t, testHandler(&serviceSpy{}), query, nil)
			expectRejected(t, result)
			if result.Errors[0].Extensions["code"] != domain.BadUserInput {
				t.Fatalf("wrong error: %+v", result.Errors)
			}
		})
	}
}

func TestIDFormatAndRange(t *testing.T) {
	for _, id := range []string{"", "0", "-1", "+1", "1.5", "1e2", " 1", "1 ", "x", "9223372036854775808"} {
		t.Run(id, func(t *testing.T) {
			spy := &serviceSpy{}
			result := requestGraphQL(t, testHandler(spy), `query($id:ID){main(id:$id){id}}`, map[string]any{"id": id})
			expectRejected(t, result)
			if result.Errors[0].Extensions["code"] != domain.BadUserInput || spy.calls != 0 {
				t.Fatalf("bad ID reached service: %+v", result)
			}
		})
	}
	for _, id := range []any{nil, "1", "9223372036854775807", json.Number("9223372036854775807")} {
		spy := &serviceSpy{}
		result := requestGraphQL(t, testHandler(spy), `query($id:ID){main(id:$id){id}}`, map[string]any{"id": id})
		if len(result.Errors) != 0 {
			t.Fatalf("valid ID %v rejected: %+v", id, result.Errors)
		}
		if id == nil && spy.list.ID != nil {
			t.Fatal("null id must mean no filter")
		}
		if spy.list.Limit != 20 || spy.list.Offset != 0 {
			t.Fatalf("wrong query defaults: %+v", spy.list)
		}
		if string(result.Data["main"]) != "[]" {
			t.Fatalf("empty list must serialize as []: %s", result.Data["main"])
		}
	}
}

func TestInputCoercionErrorsRemainClientErrors(t *testing.T) {
	tests := []struct {
		query     string
		variables map[string]any
	}{
		{`query($offset:Int!){main(offset:$offset){id}}`, map[string]any{"offset": json.Number("2147483648")}},
		{`{main(offset:2147483648){id}}`, nil},
		{`mutation($input:MainMutationInput!){main(input:$input){main{id}}}`, jsonObject(t, `{"input":{"create":{"title":"x","satellite":{"chair":{"type":"ABC"}}}}}`)},
	}
	for _, test := range tests {
		spy := &serviceSpy{}
		expectRejected(t, requestGraphQL(t, testHandler(spy), test.query, test.variables))
		if spy.calls != 0 {
			t.Fatal("invalid scalar or enum reached service")
		}
	}
}

func TestSchemaHasOnlyRequiredBusinessRoots(t *testing.T) {
	result := requestGraphQL(t, testHandler(&serviceSpy{}), `{__schema{queryType{fields{name}}mutationType{fields{name}}subscriptionType{name}}}`, nil)
	if len(result.Errors) != 0 {
		t.Fatal(result.Errors)
	}
	var schema struct {
		QueryType        struct{ Fields []struct{ Name string } }
		MutationType     struct{ Fields []struct{ Name string } }
		SubscriptionType any
	}
	if err := json.Unmarshal(result.Data["__schema"], &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.QueryType.Fields) != 1 || schema.QueryType.Fields[0].Name != "main" || len(schema.MutationType.Fields) != 1 || schema.MutationType.Fields[0].Name != "main" || schema.SubscriptionType != nil {
		t.Fatalf("unexpected roots: %+v", schema)
	}
	for _, name := range []string{"MainMutationInput", "SatelliteCreateInput", "SatelliteUpdateInput"} {
		result := requestGraphQL(t, testHandler(&serviceSpy{}), `{__type(name:"`+name+`"){isOneOf inputFields{name defaultValue type{kind}}}}`, nil)
		var typ struct {
			IsOneOf     bool
			InputFields []struct {
				Name         string
				DefaultValue any
				Type         struct{ Kind string }
			}
		}
		if err := json.Unmarshal(result.Data["__type"], &typ); err != nil {
			t.Fatal(err)
		}
		if !typ.IsOneOf || len(typ.InputFields) != 3 {
			t.Fatalf("wrong OneOf introspection: %+v", typ)
		}
		for _, field := range typ.InputFields {
			if field.DefaultValue != nil || field.Type.Kind == "NON_NULL" {
				t.Fatalf("OneOf field cannot be required or defaulted: %+v", field)
			}
		}
	}
}

func TestCreateOutputUsesUnionStringIDsAndUTC(t *testing.T) {
	for _, test := range []struct{ kind, typename, contents, fragment string }{
		{"tool", "Tool", `{}`, `description1`}, {"table", "Table", `{}`, `description2`}, {"chair", "Chair", `{type:abc}`, `description3 type`},
	} {
		t.Run(test.kind, func(t *testing.T) {
			spy := &serviceSpy{}
			result := requestGraphQL(t, testHandler(spy), `mutation{main(input:{create:{title:"",satellite:{`+test.kind+`:`+test.contents+`}}}){deletedId main{id title createdAt updatedAt deletedAt satellite{__typename ... on `+test.typename+` { id `+test.fragment+` createdAt updatedAt deletedAt }}}}}`, nil)
			if len(result.Errors) != 0 {
				t.Fatal(result.Errors)
			}
			var payload struct {
				DeletedID *string
				Main      struct {
					ID        string
					Title     string
					CreatedAt string
					UpdatedAt string
					DeletedAt *string
					Satellite map[string]any
				}
			}
			if err := json.Unmarshal(result.Data["main"], &payload); err != nil {
				t.Fatal(err)
			}
			if payload.DeletedID != nil || payload.Main.ID != "9223372036854775807" || payload.Main.Title != "" || payload.Main.Satellite["__typename"] != test.typename || payload.Main.Satellite["id"] != "2" {
				t.Fatalf("bad payload: %+v", payload)
			}
			if !strings.HasSuffix(payload.Main.CreatedAt, "Z") || payload.Main.CreatedAt != payload.Main.UpdatedAt || payload.Main.DeletedAt != nil {
				t.Fatalf("timestamps must be UTC RFC3339: %+v", payload.Main)
			}
			if spy.create.Description != nil {
				t.Fatal("missing description must map to SQL NULL")
			}
		})
	}
}

func TestInternalErrorsAreSanitizedAndLogged(t *testing.T) {
	for _, original := range []error{errors.New("postgres://secret@host SELECT sensitive"), &domain.Error{Code: domain.InternalServerError, Message: "database operation failed", Cause: errors.New("private SQL cause")}} {
		var log bytes.Buffer
		handler := newHandler(&serviceSpy{err: original}, slog.New(slog.NewTextHandler(&log, nil)))
		result := requestGraphQL(t, handler, `{main{id}}`, nil)
		if len(result.Errors) != 1 || result.Errors[0].Message != "internal server error" || result.Errors[0].Extensions["code"] != domain.InternalServerError {
			t.Fatalf("internal error leaked or lost code: %+v", result)
		}
		if !strings.Contains(log.String(), original.Error()) {
			t.Fatalf("internal cause not logged: %s", log.String())
		}
		if cause := errors.Unwrap(original); cause != nil && !strings.Contains(log.String(), cause.Error()) {
			t.Fatalf("DB cause not logged: %s", log.String())
		}
	}
	result := requestGraphQL(t, testHandler(&serviceSpy{panicValue: "private panic"}), `{main{id}}`, nil)
	if len(result.Errors) != 1 || result.Errors[0].Message != "internal server error" || result.Errors[0].Extensions["code"] != domain.InternalServerError {
		t.Fatalf("panic leaked or lost code: %+v", result)
	}
}
