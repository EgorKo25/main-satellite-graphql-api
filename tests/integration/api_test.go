//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/service"
	"github.com/EgorKo25/main-satellite-graphql-api/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type object = map[string]any

const fields = `id title createdAt updatedAt deletedAt satellite {
 __typename
 ... on Tool { id description1 createdAt updatedAt deletedAt }
 ... on Table { id description2 createdAt updatedAt deletedAt }
 ... on Chair { id description3 type createdAt updatedAt deletedAt }
}`
const mutate = `mutation($input: MainMutationInput!) { main(input:$input) { main { ` + fields + ` } deletedId } }`
const read = `query($id:ID,$limit:Int! = 20,$offset:Int! = 0) { main(id:$id,limit:$limit,offset:$offset) { ` + fields + ` } }`

type response struct {
	Data   object `json:"data"`
	Errors []struct {
		Message    string `json:"message"`
		Extensions object `json:"extensions"`
	} `json:"errors"`
}

type traceCounter struct{ queries atomic.Int64 }

func (c *traceCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.queries.Add(1)
	return ctx
}
func (*traceCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

type fixture struct {
	pool   *pgxpool.Pool
	server *httptest.Server
	trace  *traceCounter
	dsn    string
}

// Every fixture owns a freshly created database. The supplied admin database is
// used only to CREATE/DROP that uniquely named database, never to run migrations.
func setup(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("integration tests require TEST_DATABASE_URL pointing to a dedicated PostgreSQL test instance")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("gql_test_%d_%d", os.Getpid(), time.Now().UnixNano())
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		admin.Close(ctx)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		if _, err := admin.Exec(cleanupCtx, "DROP DATABASE "+quoted); err != nil {
			t.Errorf("drop owned test database: %v", err)
		}
		if err := admin.Close(cleanupCtx); err != nil {
			t.Errorf("close test admin: %v", err)
		}
	})
	testDSN := dsn + " dbname=" + name
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		parsed.Path = "/" + name
		query := parsed.Query()
		query.Del("database")
		query.Del("dbname")
		parsed.RawQuery = query.Encode()
		testDSN = parsed.String()
	}
	cfg, err := pgxpool.ParseConfig(testDSN)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Database != name {
		t.Fatal("test DSN must target the owned database")
	}
	if err := migrations.Up(ctx, testDSN); err != nil {
		t.Fatal(err)
	}
	trace := new(traceCounter)
	cfg.ConnConfig.Tracer = trace
	cfg.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	server := httptest.NewServer(graph.NewHandler(service.New(postgres.New(pool)), slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(server.Close)
	return &fixture{pool: pool, server: server, trace: trace, dsn: testDSN}
}

func (f *fixture) request(query string, variables object) (response, error) {
	body, err := json.Marshal(object{"query": query, "variables": variables})
	if err != nil {
		return response{}, err
	}
	res, err := f.server.Client().Post(f.server.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		return response{}, err
	}
	defer res.Body.Close()
	var out response
	err = json.NewDecoder(res.Body).Decode(&out)
	return out, err
}
func (f *fixture) gql(t *testing.T, query string, variables object) response {
	t.Helper()
	r, err := f.request(query, variables)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func requireOK(t *testing.T, r response) object {
	t.Helper()
	if len(r.Errors) > 0 {
		t.Fatalf("unexpected GraphQL errors: %+v", r.Errors)
	}
	return r.Data
}
func requireError(t *testing.T, r response, code string) {
	t.Helper()
	if len(r.Errors) == 0 {
		t.Fatalf("expected error %q, got %+v", code, r.Data)
	}
	if code != "" && r.Errors[0].Extensions["code"] != code {
		t.Fatalf("want code %s, got %+v", code, r.Errors)
	}
}
func (f *fixture) create(t *testing.T, kind, title string, patch object) object {
	t.Helper()
	r := f.gql(t, mutate, object{"input": object{"create": object{"title": title, "satellite": object{kind: patch}}}})
	p := requireOK(t, r)["main"].(map[string]any)
	if p["deletedId"] != nil {
		t.Fatalf("create deletedId: %v", p)
	}
	return p["main"].(map[string]any)
}
func (f *fixture) update(t *testing.T, patch object) response {
	t.Helper()
	return f.gql(t, mutate, object{"input": object{"update": patch}})
}
func (f *fixture) remove(t *testing.T, id string) response {
	t.Helper()
	return f.gql(t, mutate, object{"input": object{"delete": object{"id": id}}})
}
func (f *fixture) list(t *testing.T, vars object) []any {
	t.Helper()
	return requireOK(t, f.gql(t, read, vars))["main"].([]any)
}
func idOf(m object) string      { return m["id"].(string) }
func satellite(m object) object { return m["satellite"].(map[string]any) }
func updated(t *testing.T, r response) object {
	t.Helper()
	p := requireOK(t, r)["main"].(map[string]any)
	if p["deletedId"] != nil {
		t.Fatal(p)
	}
	return p["main"].(map[string]any)
}
func (f *fixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}
func (f *fixture) snapshot(t *testing.T) string {
	t.Helper()
	var value string
	err := f.pool.QueryRow(context.Background(), `SELECT jsonb_build_object(
 'main',(SELECT jsonb_agg(m ORDER BY id) FROM main m),
 'tools',(SELECT jsonb_agg(s ORDER BY id) FROM tools s),
 'tables',(SELECT jsonb_agg(s ORDER BY id) FROM tables s),
 'chairs',(SELECT jsonb_agg(s ORDER BY id) FROM chairs s))::text`).Scan(&value)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func (f *fixture) unchanged(t *testing.T, before string) {
	t.Helper()
	if after := f.snapshot(t); before != after {
		t.Fatalf("failed operation changed DB\nbefore: %s\nafter: %s", before, after)
	}
}

func (f *fixture) assertLink(t *testing.T, m object, table string, deleted bool) {
	t.Helper()
	var subID, mainID, satelliteID int64
	var kind string
	var mainDeleted, satDeleted *time.Time
	var mainUpdated, satUpdated time.Time
	var others int
	// table is chosen exclusively by the test's fixed, internal cases.
	if table != "tools" && table != "tables" && table != "chairs" {
		t.Fatal("invalid test table")
	}
	sql := `SELECT m.sub_id,m.sub_obj,s.id,s.main_id,m.deleted_at,s.deleted_at,m.update_at,s.update_at,
 (SELECT count(*) FROM tools WHERE main_id=m.id)+(SELECT count(*) FROM tables WHERE main_id=m.id)+(SELECT count(*) FROM chairs WHERE main_id=m.id)
 FROM main m JOIN ` + table + ` s ON s.main_id=m.id WHERE m.id=$1`
	err := f.pool.QueryRow(context.Background(), sql, idOf(m)).Scan(&subID, &kind, &satelliteID, &mainID, &mainDeleted, &satDeleted, &mainUpdated, &satUpdated, &others)
	if err != nil {
		t.Fatal(err)
	}
	if kind != table || subID != satelliteID || strconv.FormatInt(mainID, 10) != idOf(m) || strconv.FormatInt(satelliteID, 10) != idOf(satellite(m)) || others != 1 {
		t.Fatalf("broken link: kind=%s sub=%d satellite=%d main=%d count=%d", kind, subID, satelliteID, mainID, others)
	}
	if deleted {
		if mainDeleted == nil || satDeleted == nil || !mainDeleted.Equal(*satDeleted) || !mainUpdated.Equal(*mainDeleted) || !satUpdated.Equal(*satDeleted) {
			t.Fatalf("inconsistent deletion timestamps: %v %v %v %v", mainDeleted, satDeleted, mainUpdated, satUpdated)
		}
	} else if mainDeleted != nil || satDeleted != nil {
		t.Fatal("unexpected deleted row")
	}
}

func TestCreateReadAndPagination(t *testing.T) {
	f := setup(t)
	cases := []struct {
		kind, table, typename, description string
		input                              object
	}{{"tool", "tools", "Tool", "description1", object{}}, {"table", "tables", "Table", "description2", object{"description2": nil}}, {"chair", "chairs", "Chair", "description3", object{"type": "abc"}}}
	var created []object
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			m := f.create(t, tc.kind, "", tc.input)
			created = append(created, m)
			if m["title"] != "" || m["deletedAt"] != nil || satellite(m)["__typename"] != tc.typename || satellite(m)[tc.description] != nil {
				t.Fatal(m)
			}
			for _, obj := range []object{m, satellite(m)} {
				for _, key := range []string{"createdAt", "updatedAt"} {
					value := obj[key].(string)
					parsed, err := time.Parse(time.RFC3339Nano, value)
					if err != nil || parsed.Location() != time.UTC || !strings.HasSuffix(value, "Z") {
						t.Fatalf("invalid UTC timestamp %q: %v", value, err)
					}
				}
			}
			f.assertLink(t, m, tc.table, false)
			var description *string
			if err := f.pool.QueryRow(context.Background(), "SELECT "+tc.description+" FROM "+tc.table+" WHERE main_id=$1", idOf(m)).Scan(&description); err != nil {
				t.Fatal(err)
			}
			if description != nil {
				t.Fatalf("description should be SQL NULL: %v", description)
			}
		})
	}
	all := f.list(t, nil)
	if len(all) != 3 {
		t.Fatal(all)
	}
	for i, m := range all {
		if idOf(m.(map[string]any)) != idOf(created[i]) {
			t.Fatalf("order: %v", all)
		}
	}
	for _, tc := range []struct {
		vars object
		want int
	}{{object{"id": nil}, 3}, {object{"id": idOf(created[1])}, 1}, {object{"id": "9223372036854775807"}, 0}, {object{"id": idOf(created[1]), "offset": 1}, 0}, {object{"limit": 1, "offset": 1}, 1}, {object{"limit": 100, "offset": 100}, 0}} {
		if got := f.list(t, tc.vars); len(got) != tc.want {
			t.Fatalf("vars %v: want %d got %v", tc.vars, tc.want, got)
		}
	}
	for _, vars := range []object{{"limit": 0}, {"limit": 101}, {"offset": -1}, {"id": "0"}, {"id": "-1"}, {"id": "+1"}, {"id": "1.5"}, {"id": " 1"}, {"id": "9223372036854775808"}, {"id": ""}, {"id": "abc"}} {
		requireError(t, f.gql(t, read, vars), "BAD_USER_INPUT")
	}
	// The same single query loads 1 and 20 results including all satellite fragments.
	for i := 0; i < 22; i++ {
		f.create(t, "tool", "bulk", object{})
	}
	for _, limit := range []int{1, 20} {
		f.trace.queries.Store(0)
		rows := f.list(t, object{"limit": limit})
		count := f.trace.queries.Load()
		if len(rows) != limit || count != 1 {
			t.Fatalf("limit %d: rows=%d SQL queries=%d", limit, len(rows), count)
		}
	}
}

func TestPartialUpdates(t *testing.T) {
	f := setup(t)
	for _, tc := range []struct{ kind, table, desc string }{{"tool", "tools", "description1"}, {"table", "tables", "description2"}, {"chair", "chairs", "description3"}} {
		t.Run(tc.kind, func(t *testing.T) {
			initial := object{tc.desc: "original"}
			if tc.kind == "chair" {
				initial["type"] = "abc"
			}
			m := f.create(t, tc.kind, "before", initial)
			id := idOf(m)
			x := updated(t, f.update(t, object{"id": id, "title": "after"}))
			if x["title"] != "after" || satellite(x)[tc.desc] != "original" || x["createdAt"] != m["createdAt"] || satellite(x)["updatedAt"] != satellite(m)["updatedAt"] {
				t.Fatal(x)
			}
			for _, value := range []any{"replacement", "", nil} {
				before := x
				x = updated(t, f.update(t, object{"id": id, "satellite": object{tc.kind: object{tc.desc: value}}}))
				if x["title"] != "after" || satellite(x)[tc.desc] != value || x["createdAt"] != m["createdAt"] || satellite(x)["createdAt"] != satellite(m)["createdAt"] {
					t.Fatal(x)
				}
				if x["updatedAt"] == before["updatedAt"] {
					t.Fatal("satellite update did not advance main.update_at")
				}
				var actual *string
				if err := f.pool.QueryRow(context.Background(), "SELECT "+tc.desc+" FROM "+tc.table+" WHERE main_id=$1", id).Scan(&actual); err != nil {
					t.Fatal(err)
				}
				if value == nil {
					if actual != nil {
						t.Fatal("NULL not persisted")
					}
				} else if actual == nil || *actual != value {
					t.Fatalf("description mismatch %v %v", actual, value)
				}
			}
			if tc.kind == "chair" {
				x = updated(t, f.update(t, object{"id": id, "satellite": object{"chair": object{"description3": "keep"}}}))
				for _, typ := range []string{"cde", "abc"} {
					x = updated(t, f.update(t, object{"id": id, "satellite": object{"chair": object{"type": typ}}}))
					if satellite(x)["type"] != typ || satellite(x)[tc.desc] != "keep" {
						t.Fatal(x)
					}
				}
			}
			f.assertLink(t, x, tc.table, false)
			for _, patch := range []object{{"id": id}, {"id": id, "title": nil}, {"id": id, "satellite": nil}, {"id": id, "satellite": object{tc.kind: object{}}}, {"id": id, "title": "must rollback", "satellite": object{tc.kind: object{}}}} {
				before := f.snapshot(t)
				requireError(t, f.update(t, patch), "BAD_USER_INPUT")
				f.unchanged(t, before)
			}
			other := "chair"
			otherPatch := object{"type": "abc"}
			if tc.kind == "chair" {
				other = "tool"
				otherPatch = object{"description1": "wrong"}
			}
			before := f.snapshot(t)
			requireError(t, f.update(t, object{"id": id, "title": "must rollback", "satellite": object{other: otherPatch}}), "SATELLITE_TYPE_MISMATCH")
			f.unchanged(t, before)
		})
	}
	chair := f.create(t, "chair", "chair", object{"type": "abc"})
	before := f.snapshot(t)
	requireError(t, f.update(t, object{"id": idOf(chair), "satellite": object{"chair": object{"type": nil}}}), "BAD_USER_INPUT")
	f.unchanged(t, before)
	requireError(t, f.update(t, object{"id": "9223372036854775807", "title": "unknown"}), "NOT_FOUND")
}

func TestSoftDelete(t *testing.T) {
	f := setup(t)
	for _, tc := range []struct{ kind, table string }{{"tool", "tools"}, {"table", "tables"}, {"chair", "chairs"}} {
		t.Run(tc.kind, func(t *testing.T) {
			input := object{}
			if tc.kind == "chair" {
				input["type"] = "abc"
			}
			m := f.create(t, tc.kind, "delete", input)
			id := idOf(m)
			payload := requireOK(t, f.remove(t, id))["main"].(map[string]any)
			if payload["main"] != nil || payload["deletedId"] != id {
				t.Fatal(payload)
			}
			f.assertLink(t, m, tc.table, true)
			if len(f.list(t, object{"id": id})) != 0 {
				t.Fatal("deleted id returned")
			}
			before := f.snapshot(t)
			requireError(t, f.remove(t, id), "ALREADY_DELETED")
			f.unchanged(t, before)
			requireError(t, f.update(t, object{"id": id, "title": "restore"}), "NOT_FOUND")
			f.unchanged(t, before)
		})
	}
	if len(f.list(t, nil)) != 0 {
		t.Fatal("deleted rows listed")
	}
	requireError(t, f.remove(t, "9223372036854775807"), "NOT_FOUND")
}

func TestInvalidGraphQLNeverWrites(t *testing.T) {
	f := setup(t)
	m := f.create(t, "tool", "unchanged", object{})
	id := idOf(m)
	// Each OneOf position gets all four invalid presence/null cases, separately
	// for literals and variables. DB snapshots include all rows and timestamps.
	root := []object{{}, {"delete": object{"id": id}, "create": object{"title": "x", "satellite": object{"tool": object{}}}}, {"delete": nil}, {"delete": object{"id": id}, "create": nil}}
	createSat := []object{{}, {"tool": object{}, "table": object{}}, {"tool": nil}, {"tool": object{}, "table": nil}}
	updateSat := []object{{}, {"tool": object{"description1": "x"}, "chair": object{"type": "abc"}}, {"tool": nil}, {"tool": object{"description1": "x"}, "table": nil}}
	var inputs []object
	inputs = append(inputs, root...)
	for _, s := range createSat {
		inputs = append(inputs, object{"create": object{"title": "invalid", "satellite": s}})
	}
	for _, s := range updateSat {
		inputs = append(inputs, object{"update": object{"id": id, "title": "invalid", "satellite": s}})
	}
	for i, input := range inputs {
		t.Run(fmt.Sprintf("variables_%d", i), func(t *testing.T) {
			before := f.snapshot(t)
			requireError(t, f.gql(t, mutate, object{"input": input}), "")
			f.unchanged(t, before)
		})
	}
	literals := []string{
		`{}`, `{delete:{id:"` + id + `"},create:{title:"x",satellite:{tool:{}}}}`, `{delete:null}`, `{delete:{id:"` + id + `"},create:null}`,
		`{create:{title:"x",satellite:{}}}`, `{create:{title:"x",satellite:{tool:{},table:{}}}}`, `{create:{title:"x",satellite:{tool:null}}}`, `{create:{title:"x",satellite:{tool:{},table:null}}}`,
		`{update:{id:"` + id + `",satellite:{}}}`, `{update:{id:"` + id + `",satellite:{tool:{description1:"x"},chair:{type:abc}}}}`, `{update:{id:"` + id + `",satellite:{tool:null}}}`, `{update:{id:"` + id + `",satellite:{tool:{description1:"x"},table:null}}}`,
	}
	for i, input := range literals {
		t.Run(fmt.Sprintf("literal_%d", i), func(t *testing.T) {
			before := f.snapshot(t)
			requireError(t, f.gql(t, `mutation {main(input:`+input+`){deletedId}}`, nil), "")
			f.unchanged(t, before)
		})
	}
	invalidInputs := []object{
		{"create": object{"satellite": object{"tool": object{}}}},
		{"create": object{"title": nil, "satellite": object{"tool": object{}}}},
		{"create": object{"title": "x", "satellite": object{"chair": object{}}}},
		{"create": object{"title": "x", "satellite": object{"chair": object{"type": nil}}}},
		{"create": object{"title": "x", "satellite": object{"chair": object{"type": "invalid"}}}},
		{"create": object{"id": "5", "title": "x", "satellite": object{"tool": object{}}}},
		{"create": object{"title": "x", "sub_id": "1", "satellite": object{"tool": object{}}}},
		{"create": object{"title": "x", "sub_obj": "tools", "satellite": object{"tool": object{}}}},
		{"create": object{"title": "x", "createdAt": "2026-01-01T00:00:00Z", "satellite": object{"tool": object{}}}},
		{"create": object{"title": "x", "satellite": object{"tool": object{"main_id": id}}}},
		{"update": object{"id": id, "deletedAt": nil}},
		{"update": object{"id": id, "satellite": object{"tool": object{"id": "999"}}}},
		{"update": object{"id": id, "satellite": object{"chair": object{"type": "invalid"}}}},
	}
	for i, input := range invalidInputs {
		t.Run(fmt.Sprintf("unknown_required_enum_%d", i), func(t *testing.T) {
			before := f.snapshot(t)
			requireError(t, f.gql(t, mutate, object{"input": input}), "")
			f.unchanged(t, before)
		})
	}
	// Variables used directly as a chosen OneOf member must be non-null. Both
	// nullable declarations and null/missing non-null variables are rejected.
	for _, declaration := range []string{"MainDeleteInput", "MainDeleteInput!"} {
		for _, vars := range []object{nil, {"branch": nil}} {
			before := f.snapshot(t)
			requireError(t, f.gql(t, `mutation($branch:`+declaration+`){main(input:{delete:$branch}){deletedId}}`, vars), "")
			f.unchanged(t, before)
		}
	}
	for _, tc := range []struct{ typ, query string }{{"ToolCreateInput", `mutation($branch:%s){main(input:{create:{title:"x",satellite:{tool:$branch}}}){deletedId}}`}, {"ToolUpdateInput", `mutation($branch:%s){main(input:{update:{id:"` + id + `",satellite:{tool:$branch}}}){deletedId}}`}} {
		for _, suffix := range []string{"", "!"} {
			for _, vars := range []object{nil, {"branch": nil}} {
				before := f.snapshot(t)
				requireError(t, f.gql(t, fmt.Sprintf(tc.query, tc.typ+suffix), vars), "")
				f.unchanged(t, before)
			}
		}
	}
	// Invalid variable in the second alias must be rejected before the first write.
	before := f.snapshot(t)
	r := f.gql(t, `mutation($bad:MainMutationInput!){first:main(input:{create:{title:"no write",satellite:{tool:{}}}}){deletedId} second:main(input:$bad){deletedId}}`, object{"bad": object{"delete": object{"id": id}, "create": nil}})
	requireError(t, r, "")
	f.unchanged(t, before)
}

func TestSchemaAndBrokenRelationships(t *testing.T) {
	f := setup(t)
	r := requireOK(t, f.gql(t, `{__schema{queryType{fields{name}} mutationType{fields{name}} subscriptionType{name}} root:__type(name:"MainMutationInput"){isOneOf inputFields{name defaultValue type{kind}}} create:__type(name:"SatelliteCreateInput"){isOneOf} update:__type(name:"SatelliteUpdateInput"){isOneOf}}`, nil))
	schema := r["__schema"].(map[string]any)
	for _, typ := range []string{"queryType", "mutationType"} {
		got := schema[typ].(map[string]any)["fields"].([]any)
		if len(got) != 1 || got[0].(map[string]any)["name"] != "main" {
			t.Fatal(schema)
		}
	}
	if schema["subscriptionType"] != nil {
		t.Fatal(schema)
	}
	for _, typ := range []string{"root", "create", "update"} {
		if r[typ].(map[string]any)["isOneOf"] != true {
			t.Fatalf("%s not OneOf", typ)
		}
	}
	for _, field := range r["root"].(map[string]any)["inputFields"].([]any) {
		m := field.(map[string]any)
		if m["defaultValue"] != nil || m["type"].(map[string]any)["kind"] == "NON_NULL" {
			t.Fatal(m)
		}
	}
	for _, query := range []string{`{tool(id:"1"){id}}`, `{chair(id:"1"){id}}`, `mutation{createTool{ id }}`} {
		requireError(t, f.gql(t, query, nil), "")
	}
	m := f.create(t, "tool", "broken", object{})
	f.exec(t, `UPDATE main SET sub_id=sub_id+1000 WHERE id=$1`, idOf(m))
	requireError(t, f.gql(t, read, object{"id": idOf(m)}), "INTERNAL_SERVER_ERROR")
	f.exec(t, `UPDATE main SET sub_id=(SELECT id FROM tools WHERE main_id=main.id) WHERE id=$1`, idOf(m))
	f.exec(t, `UPDATE tools SET deleted_at=clock_timestamp() WHERE main_id=$1`, idOf(m))
	requireError(t, f.gql(t, read, object{"id": idOf(m)}), "INTERNAL_SERVER_ERROR")
	f.exec(t, `UPDATE tools SET deleted_at=NULL WHERE main_id=$1`, idOf(m))
	f.exec(t, `INSERT INTO tables(main_id,description2) VALUES($1,'foreign satellite')`, idOf(m))
	requireError(t, f.gql(t, read, object{"id": idOf(m)}), "INTERNAL_SERVER_ERROR")
}

func TestRollbackAndCommitFailures(t *testing.T) {
	f := setup(t)
	for _, tc := range []struct{ kind, table, desc string }{{"tool", "tools", "description1"}, {"table", "tables", "description2"}, {"chair", "chairs", "description3"}} {
		t.Run(tc.kind, func(t *testing.T) {
			input := object{tc.desc: "initial"}
			if tc.kind == "chair" {
				input["type"] = "abc"
			}
			m := f.create(t, tc.kind, "initial", input)
			// These temporary trigger functions exist only in this fixture's database.
			f.exec(t, `CREATE FUNCTION test_fail_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test private SQL failure, must not leak'; END $$`)
			f.exec(t, `CREATE TRIGGER test_fail BEFORE INSERT OR UPDATE ON `+tc.table+` FOR EACH ROW EXECUTE FUNCTION test_fail_write()`)
			before := f.snapshot(t)
			requests := []object{{"create": object{"title": "rollback", "satellite": object{tc.kind: input}}}, {"update": object{"id": idOf(m), "title": "rollback", "satellite": object{tc.kind: object{tc.desc: "rollback"}}}}, {"delete": object{"id": idOf(m)}}}
			for _, input := range requests {
				r := f.gql(t, mutate, object{"input": input})
				requireError(t, r, "INTERNAL_SERVER_ERROR")
				if strings.Contains(r.Errors[0].Message, "private SQL") {
					t.Fatal("internal error leaked")
				}
				f.unchanged(t, before)
			}
			f.exec(t, `DROP TRIGGER test_fail ON `+tc.table)
			f.exec(t, `DROP FUNCTION test_fail_write()`)
			f.assertLink(t, m, tc.table, false)
		})
	}
	m := f.create(t, "tool", "commit failure", object{})
	f.exec(t, `CREATE FUNCTION test_fail_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'deferred failure at commit'; END $$`)
	f.exec(t, `CREATE CONSTRAINT TRIGGER test_fail_commit AFTER INSERT OR UPDATE ON tools DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION test_fail_commit()`)
	before := f.snapshot(t)
	for _, input := range []object{{"create": object{"title": "commit rollback", "satellite": object{"tool": object{}}}}, {"update": object{"id": idOf(m), "satellite": object{"tool": object{"description1": "rollback"}}}}, {"delete": object{"id": idOf(m)}}} {
		requireError(t, f.gql(t, mutate, object{"input": input}), "INTERNAL_SERVER_ERROR")
		f.unchanged(t, before)
	}
}

func TestConcurrentWrites(t *testing.T) {
	f := setup(t)
	t.Run("double_delete", func(t *testing.T) {
		m := f.create(t, "tool", "race", object{})
		id := idOf(m)
		var wg sync.WaitGroup
		results := make(chan response, 2)
		errs := make(chan error, 2)
		start := make(chan struct{})
		for i := 0; i < 2; i++ {
			wg.Go(func() {
				<-start
				r, err := f.request(mutate, object{"input": object{"delete": object{"id": id}}})
				results <- r
				errs <- err
			})
		}
		close(start)
		wg.Wait()
		close(results)
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		success, failed := 0, 0
		for r := range results {
			if len(r.Errors) == 0 {
				success++
			} else {
				requireError(t, r, "ALREADY_DELETED")
				failed++
			}
		}
		if success != 1 || failed != 1 {
			t.Fatalf("success=%d failed=%d", success, failed)
		}
		f.assertLink(t, m, "tools", true)
	})
	for i := 0; i < 10; i++ {
		t.Run(fmt.Sprintf("update_delete_%d", i), func(t *testing.T) {
			m := f.create(t, "chair", "old", object{"description3": "old", "type": "abc"})
			id := idOf(m)
			start := make(chan struct{})
			var wg sync.WaitGroup
			var ur, dr response
			var ue, de error
			wg.Go(func() {
				<-start
				ur, ue = f.request(mutate, object{"input": object{"update": object{"id": id, "title": "new", "satellite": object{"chair": object{"description3": "new", "type": "cde"}}}}})
			})
			wg.Go(func() { <-start; dr, de = f.request(mutate, object{"input": object{"delete": object{"id": id}}}) })
			close(start)
			wg.Wait()
			if ue != nil || de != nil {
				t.Fatalf("request errors %v %v", ue, de)
			}
			requireOK(t, dr)
			var title, desc, typ string
			if err := f.pool.QueryRow(context.Background(), `SELECT m.title,c.description3,c.type::text FROM main m JOIN chairs c ON c.main_id=m.id WHERE m.id=$1`, id).Scan(&title, &desc, &typ); err != nil {
				t.Fatal(err)
			}
			if len(ur.Errors) == 0 {
				if title != "new" || desc != "new" || typ != "cde" {
					t.Fatalf("partial update %s/%s/%s", title, desc, typ)
				}
			} else {
				requireError(t, ur, "NOT_FOUND")
				if title != "old" || desc != "old" || typ != "abc" {
					t.Fatalf("failed update wrote %s/%s/%s", title, desc, typ)
				}
			}
			f.assertLink(t, m, "chairs", true)
			if len(f.list(t, object{"id": id})) != 0 {
				t.Fatal("resurrected row")
			}
		})
	}
}

func TestMigrationsAndDatabaseConstraints(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if err := migrations.Up(ctx, f.dsn); err != nil {
		t.Fatalf("repeat up: %v", err)
	}
	if err := migrations.Down(ctx, f.dsn); err != nil {
		t.Fatal(err)
	}
	var table *string
	if err := f.pool.QueryRow(ctx, `SELECT to_regclass('main')::text`).Scan(&table); err != nil {
		t.Fatal(err)
	}
	if table != nil {
		t.Fatal("down left main table")
	}
	if err := migrations.Up(ctx, f.dsn); err != nil {
		t.Fatal(err)
	}
	rows, err := f.pool.Query(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema='public' ORDER BY table_name`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"chairs", "goose_db_version", "main", "tables", "tools"}) {
		t.Fatal(names)
	}
	m := f.create(t, "tool", "constraints", object{})
	for _, sql := range []string{`INSERT INTO tools(main_id) VALUES(9223372036854775807)`, `INSERT INTO tools(main_id) VALUES(` + idOf(m) + `)`, `UPDATE main SET sub_obj='invalid' WHERE id=` + idOf(m), `INSERT INTO chairs(main_id,type) VALUES(` + idOf(m) + `,'invalid')`, `UPDATE main SET title=NULL WHERE id=` + idOf(m)} {
		if _, err := f.pool.Exec(ctx, sql); err == nil {
			t.Fatalf("constraint accepted %s", sql)
		}
	}
	// HTTP endpoint accepts the GraphQL transport, with no REST CRUD route implied.
	res, err := f.server.Client().Get(f.server.URL + "?query=" + "%7Bmain%7Bid%7D%7D")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatal(res.Status)
	}
}
