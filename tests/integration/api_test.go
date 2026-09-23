//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/config"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

type object = map[string]any

const migrationsDir = "../../migrations"

const (
	satelliteField = "satellite"
	toolsTable     = "tools"
	tablesTable    = "tables"
	toolBranch     = "tool"
	tableBranch    = "table"
	typeField      = "type"
	titleField     = "title"
	nullableText   = "text:YES"
	createdAtField = "createdAt"
	updatedAtField = "updatedAt"
	deletedAtField = "deletedAt"
	deletedIDField = "deletedId"
)

const (
	createOperation       = "create"
	updateOperation       = "update"
	deleteOperation       = "delete"
	chairsTable           = "chairs"
	toolDescriptionField  = "description1"
	tableDescriptionField = "description2"
	chairDescriptionField = "description3"
	chairABC              = "abc"
	chairCDE              = "cde"
	offsetArgument        = "offset"
	limitArgument         = "limit"
	originalDescription   = "original"
	mainField             = "main"
	invalidValue          = "invalid"
	mainIDColumn          = "main_id"
	initialDescription    = "initial"
	rollbackValue         = "rollback"
	previousValue         = "old"
	updatedValue          = "new"
	chairBranch           = "chair"
	inputArgument         = "input"
	requiredBigint        = "bigint:NO"
)

func TestMain(main *testing.M) {
	if err := goose.SetDialect("postgres"); err != nil {
		slog.Error("set migration dialect", "error", err)
		os.Exit(1)
	}

	os.Exit(main.Run())
}

const fields = `id title createdAt updatedAt deletedAt satellite {
 __typename
 ... on Tool { id description1 createdAt updatedAt deletedAt }
 ... on Table { id description2 createdAt updatedAt deletedAt }
 ... on Chair { id description3 type createdAt updatedAt deletedAt }
}`
const mutate = `mutation($input: MainMutationInput!) { main(input:$input) { main { ` + fields + ` } deletedId } }`

const read = `query($id:ID,$limit:Int! = 20,$offset:Int! = 0) {
 main(id:$id,limit:$limit,offset:$offset) { ` + fields + ` } }`

type response struct {
	Data   object `json:"data"`
	Errors []struct {
		Message    string `json:"message"`
		Extensions object `json:"extensions"`
	} `json:"errors"`
}

func setup(t *testing.T) *fixture {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	require.NotEmpty(
		t,
		dsn,
		"integration tests require TEST_DATABASE_URL pointing to a dedicated PostgreSQL test instance",
	)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()

		require.NoError(t, admin.Close(cleanupCtx), "close test admin")
	})

	name := fmt.Sprintf("gql_test_%d_%d", os.Getpid(), time.Now().UnixNano())
	quoted := pgx.Identifier{name}.Sanitize()
	_, err = admin.Exec(ctx, "CREATE DATABASE "+quoted)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()

		_, err := admin.Exec(cleanupCtx, "DROP DATABASE "+quoted)
		require.NoError(t, err, "drop owned test database")
	})

	testDSN := dsn + " dbname=" + name
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, parseErr := url.Parse(dsn)
		require.NoError(t, parseErr)

		parsed.Path = "/" + name
		query := parsed.Query()
		query.Del("database")
		query.Del("dbname")
		parsed.RawQuery = query.Encode()
		testDSN = parsed.String()
	}

	cfg, err := pgxpool.ParseConfig(testDSN)
	require.NoError(t, err)
	require.Equal(t, name, cfg.ConnConfig.Database, "test DSN must target the owned database")

	database, err := sql.Open("pgx", testDSN)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, database.Close(), "close migration connection")
	})
	require.NoError(t, goose.UpContext(ctx, database, migrationsDir))

	cfg.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	applicationDB, err := postgres.New(ctx, config.Database{
		URL:            testDSN,
		ConnectTimeout: 5 * time.Second,
		MaxConns:       8,
	})
	require.NoError(t, err)
	t.Cleanup(applicationDB.Close)

	server := httptest.NewServer(graph.NewHandler(applicationDB, slog.New(slog.DiscardHandler)))
	server.Client().Timeout = 30 * time.Second
	t.Cleanup(server.Close)

	return &fixture{pool: pool, server: server, database: database}
}

type fixture struct {
	pool     *pgxpool.Pool
	server   *httptest.Server
	database *sql.DB
}

func (testFixture *fixture) request(ctx context.Context, query string, variables object) (response, error) {
	body, err := json.Marshal(object{"query": query, "variables": variables})
	if err != nil {
		return response{}, fmt.Errorf("marshal GraphQL request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, testFixture.server.URL, bytes.NewReader(body))
	if err != nil {
		return response{}, fmt.Errorf("create GraphQL request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")

	res, err := testFixture.server.Client().Do(request)
	if err != nil {
		return response{}, fmt.Errorf("send GraphQL request: %w", err)
	}

	var out response

	err = json.NewDecoder(res.Body).Decode(&out)

	return out, errors.Join(err, res.Body.Close())
}
func (testFixture *fixture) gql(t *testing.T, query string, variables object) response {
	t.Helper()
	reply, err := testFixture.request(t.Context(), query, variables)
	require.NoError(t, err)

	return reply
}
func (testFixture *fixture) create(t *testing.T, kind, title string, patch object) object {
	t.Helper()
	reply := testFixture.gql(
		t,
		mutate,
		object{inputArgument: object{createOperation: object{titleField: title, satelliteField: object{kind: patch}}}},
	)

	return updated(t, reply)
}
func (testFixture *fixture) update(t *testing.T, patch object) response {
	t.Helper()

	return testFixture.gql(t, mutate, object{inputArgument: object{updateOperation: patch}})
}
func (testFixture *fixture) remove(t *testing.T, mainID string) response {
	t.Helper()

	return testFixture.gql(t, mutate, object{inputArgument: object{deleteOperation: object{"id": mainID}}})
}
func (testFixture *fixture) list(t *testing.T, vars object) []any {
	t.Helper()
	result := testFixture.gql(t, read, vars)
	require.Empty(t, result.Errors)
	rows, validType := result.Data[mainField].([]any)
	require.True(t, validType, "main result must be a list: %v", result.Data)

	return rows
}
func idOf(t *testing.T, main object) string {
	t.Helper()

	mainID, validType := main["id"].(string)
	require.True(t, validType, "main ID must be a string: %v", main)

	return mainID
}
func satellite(t *testing.T, main object) object {
	t.Helper()

	result, validType := main[satelliteField].(object)
	require.True(t, validType, "satellite must be an object: %v", main)

	return result
}
func updated(t *testing.T, reply response) object {
	t.Helper()
	require.Empty(t, reply.Errors)
	payload, validType := reply.Data[mainField].(object)
	require.True(t, validType, "mutation payload must be an object: %v", reply.Data)
	require.Nil(t, payload[deletedIDField])
	main, validType := payload[mainField].(object)
	require.True(t, validType, "created or updated main must be an object: %v", payload)

	return main
}
func (testFixture *fixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	_, err := testFixture.pool.Exec(t.Context(), sql, args...)
	require.NoError(t, err)
}
func (testFixture *fixture) snapshot(t *testing.T) string {
	t.Helper()

	var value string

	err := testFixture.pool.QueryRow(t.Context(), `SELECT jsonb_build_object(
 'main',(SELECT jsonb_agg(m ORDER BY id) FROM main m),
 'tools',(SELECT jsonb_agg(s ORDER BY id) FROM tools s),
 'tables',(SELECT jsonb_agg(s ORDER BY id) FROM tables s),
 'chairs',(SELECT jsonb_agg(s ORDER BY id) FROM chairs s))::text`).Scan(&value)
	require.NoError(t, err)

	return value
}
func (testFixture *fixture) assertLink(t *testing.T, main object, table string, deleted bool) {
	t.Helper()

	var (
		subID, mainID, satelliteID int64
		kind                       string
		mainDeleted, satDeleted    *time.Time
		mainUpdated, satUpdated    time.Time
		others                     int
	)

	require.Contains(t, []string{toolsTable, tablesTable, chairsTable}, table, "invalid test table")
	sql := `SELECT m.sub_id,m.sub_obj,s.id,s.main_id,m.deleted_at,s.deleted_at,m.update_at,s.update_at,
 (SELECT count(*) FROM tools WHERE main_id=m.id)+
 (SELECT count(*) FROM tables WHERE main_id=m.id)+
 (SELECT count(*) FROM chairs WHERE main_id=m.id)
 FROM main m JOIN ` + table + ` s ON s.main_id=m.id WHERE m.id=$1`
	err := testFixture.pool.QueryRow(t.Context(), sql, idOf(t, main)).
		Scan(&subID, &kind, &satelliteID, &mainID, &mainDeleted, &satDeleted, &mainUpdated, &satUpdated, &others)
	require.NoError(t, err)
	require.Equal(t, table, kind)
	require.Equal(t, satelliteID, subID)
	require.Equal(t, idOf(t, main), strconv.FormatInt(mainID, 10))
	require.Equal(t, idOf(t, satellite(t, main)), strconv.FormatInt(satelliteID, 10))
	require.Equal(t, 1, others)

	if deleted {
		require.NotNil(t, mainDeleted)
		require.NotNil(t, satDeleted)
		require.WithinDuration(t, *mainDeleted, *satDeleted, 0)
		require.WithinDuration(t, *mainDeleted, mainUpdated, 0)
		require.WithinDuration(t, *satDeleted, satUpdated, 0)
	} else {
		require.Nil(t, mainDeleted)
		require.Nil(t, satDeleted)
	}
}

func TestCreateSatelliteKinds(t *testing.T) {
	cases := []struct {
		name, kind, table, typename, description string
		input                                    object
	}{
		{"tool_without_description", toolBranch, toolsTable, "Tool", toolDescriptionField, object{}},
		{
			"table_with_null_description",
			tableBranch,
			tablesTable,
			"Table",
			tableDescriptionField,
			object{tableDescriptionField: nil},
		},
		{
			"chair_without_description",
			chairBranch,
			chairsTable,
			"Chair",
			chairDescriptionField,
			object{typeField: chairABC},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			testFixture := setup(t)
			main := testFixture.create(t, testCase.kind, "", testCase.input)
			require.Empty(t, main[titleField])
			require.Nil(t, main[deletedAtField])
			require.Equal(t, testCase.typename, satellite(t, main)["__typename"])
			require.Nil(t, satellite(t, main)[testCase.description])

			for _, obj := range []object{main, satellite(t, main)} {
				for _, key := range []string{createdAtField, updatedAtField} {
					value, validType := obj[key].(string)
					require.True(t, validType, "%s must be a timestamp string", key)

					parsed, err := time.Parse(time.RFC3339Nano, value)
					require.NoError(t, err)
					require.Equal(t, time.UTC, parsed.Location())
					require.True(t, strings.HasSuffix(value, "Z"), "%s must use UTC: %s", key, value)
				}
			}

			testFixture.assertLink(t, main, testCase.table, false)
			mainID := idOf(t, main)

			var description *string
			require.NoError(
				t,
				testFixture.pool.QueryRow(t.Context(),
					"SELECT "+testCase.description+" FROM "+testCase.table+" WHERE main_id=$1",
					mainID).
					Scan(&description),
			)
			require.Nil(t, description, "%s must contain SQL NULL", testCase.description)
		})
	}
}

func TestReadAndPagination(t *testing.T) {
	testFixture := setup(t)
	created := []object{
		testFixture.create(t, toolBranch, toolBranch, object{}),
		testFixture.create(t, tableBranch, tableBranch, object{}),
		testFixture.create(t, chairBranch, chairBranch, object{typeField: chairABC}),
	}
	ids := []string{idOf(t, created[0]), idOf(t, created[1]), idOf(t, created[2])}

	cases := []struct {
		name string
		vars object
		want []string
	}{
		{"default_arguments", nil, ids},
		{"null_id", object{"id": nil}, ids},
		{"filter_id", object{"id": ids[1]}, ids[1:2]},
		{"unknown_id", object{"id": "9223372036854775807"}, []string{}},
		{"offset_after_filter", object{"id": ids[1], offsetArgument: 1}, []string{}},
		{"minimum_limit_and_offset", object{limitArgument: 1, offsetArgument: 1}, ids[1:2]},
		{"maximum_limit", object{limitArgument: 100}, ids},
		{"offset_after_list", object{limitArgument: 100, offsetArgument: 100}, []string{}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rows := testFixture.list(t, testCase.vars)

			got := make([]string, 0, len(rows))
			for _, row := range rows {
				main, validType := row.(object)
				require.True(t, validType, "list item must be an object: %v", row)

				got = append(got, idOf(t, main))
				for _, original := range created {
					if idOf(t, original) == idOf(t, main) {
						require.Equal(t, original, main)
					}
				}
			}

			require.Equal(t, testCase.want, got)
		})
	}
}

func TestInvalidReadArguments(t *testing.T) {
	testFixture := setup(t)

	cases := []struct {
		name string
		vars object
	}{
		{"zero_limit", object{limitArgument: 0}},
		{"limit_above_maximum", object{limitArgument: 101}},
		{"negative_offset", object{offsetArgument: -1}},
		{"zero_id", object{"id": "0"}},
		{"negative_id", object{"id": "-1"}},
		{"signed_id", object{"id": "+1"}},
		{"fractional_id", object{"id": "1.5"}},
		{"padded_id", object{"id": " 1"}},
		{"overflow_id", object{"id": "9223372036854775808"}},
		{"empty_id", object{"id": ""}},
		{"alphabetic_id", object{"id": chairABC}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := testFixture.gql(t, read, testCase.vars)
			require.NotEmpty(t, result.Errors)
			require.Equal(t, "BAD_USER_INPUT", result.Errors[0].Extensions["code"])
		})
	}
}

func TestPartialUpdates(t *testing.T) {
	cases := []struct {
		kind, table, desc string
		initial           object
	}{
		{toolBranch, toolsTable, toolDescriptionField, object{toolDescriptionField: originalDescription}},
		{tableBranch, tablesTable, tableDescriptionField, object{tableDescriptionField: originalDescription}},
		{
			chairBranch,
			chairsTable,
			chairDescriptionField,
			object{chairDescriptionField: originalDescription, typeField: chairABC},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.kind, func(t *testing.T) {
			testFixture := setup(t)
			main := testFixture.create(t, testCase.kind, "before", testCase.initial)
			mainID := idOf(t, main)
			actualMain := updated(t, testFixture.update(t, object{"id": mainID, titleField: "after"}))
			require.Equal(t, "after", actualMain[titleField])
			require.Equal(t, originalDescription, satellite(t, actualMain)[testCase.desc])
			require.Equal(t, main[createdAtField], actualMain[createdAtField])
			require.Equal(t, satellite(t, main)[updatedAtField], satellite(t, actualMain)[updatedAtField])

			for _, step := range []struct {
				name  string
				value any
			}{
				{"replace_description", "replacement"},
				{"empty_description", ""},
				{"clear_description", nil},
			} {
				before := actualMain
				actualMain = updated(
					t,
					testFixture.update(
						t,
						object{"id": mainID, satelliteField: object{testCase.kind: object{testCase.desc: step.value}}},
					),
				)
				require.Equal(t, "after", actualMain[titleField], step.name)
				require.Equal(t, step.value, satellite(t, actualMain)[testCase.desc], step.name)
				require.Equal(t, main[createdAtField], actualMain[createdAtField], step.name)
				require.Equal(
					t,
					satellite(t, main)[createdAtField],
					satellite(t, actualMain)[createdAtField],
					step.name,
				)
				require.NotEqual(t, before[updatedAtField], actualMain[updatedAtField], step.name)

				var actual *string
				require.NoError(
					t,
					testFixture.pool.QueryRow(t.Context(),
						"SELECT "+testCase.desc+" FROM "+testCase.table+" WHERE main_id=$1",
						mainID).
						Scan(&actual),
				)

				if step.value == nil {
					require.Nil(t, actual, step.name)
				} else {
					require.NotNil(t, actual, step.name)
					require.Equal(t, step.value, *actual, step.name)
				}
			}

			testFixture.assertLink(t, actualMain, testCase.table, false)
		})
	}
}

func TestChairTypeOnlyUpdates(t *testing.T) {
	testFixture := setup(t)

	main := testFixture.create(t, chairBranch, chairBranch, object{chairDescriptionField: "keep", typeField: chairABC})
	for _, typ := range []string{chairCDE, chairABC} {
		actualMain := updated(
			t,
			testFixture.update(
				t,
				object{"id": idOf(t, main), satelliteField: object{chairBranch: object{typeField: typ}}},
			),
		)
		require.Equal(t, typ, satellite(t, actualMain)[typeField])
		require.Equal(t, "keep", satellite(t, actualMain)[chairDescriptionField])
		testFixture.assertLink(t, actualMain, chairsTable, false)
	}
}

func TestInvalidUpdatesNeverWrite(t *testing.T) {
	for _, kind := range []string{toolBranch, tableBranch, chairBranch} {
		t.Run(kind, func(t *testing.T) {
			testFixture := setup(t)

			initial := object{}
			if kind == chairBranch {
				initial[typeField] = chairABC
			}

			main := testFixture.create(t, kind, "unchanged", initial)
			mainID := idOf(t, main)

			cases := []struct {
				name  string
				patch object
			}{
				{"empty_update", object{"id": mainID}},
				{"null_title", object{"id": mainID, titleField: nil}},
				{"null_satellite", object{"id": mainID, satelliteField: nil}},
				{"empty_satellite_patch", object{"id": mainID, satelliteField: object{kind: object{}}}},
				{
					"title_with_empty_satellite_patch",
					object{"id": mainID, titleField: "must rollback", satelliteField: object{kind: object{}}},
				},
			}
			for _, testCase := range cases {
				t.Run(testCase.name, func(t *testing.T) {
					before := testFixture.snapshot(t)
					result := testFixture.update(t, testCase.patch)
					require.NotEmpty(t, result.Errors)
					require.Equal(t, "BAD_USER_INPUT", result.Errors[0].Extensions["code"])
					require.JSONEq(t, before, testFixture.snapshot(t))
				})
			}
		})
	}
}

func TestSatelliteTypeCannotChange(t *testing.T) {
	cases := []struct {
		kind    string
		initial object
		patch   object
	}{
		{toolBranch, object{}, object{chairBranch: object{typeField: chairABC}}},
		{tableBranch, object{}, object{chairBranch: object{typeField: chairABC}}},
		{chairBranch, object{typeField: chairABC}, object{toolBranch: object{toolDescriptionField: "wrong"}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.kind, func(t *testing.T) {
			testFixture := setup(t)
			main := testFixture.create(t, testCase.kind, "unchanged", testCase.initial)
			before := testFixture.snapshot(t)
			result := testFixture.update(
				t,
				object{"id": idOf(t, main), titleField: "must rollback", satelliteField: testCase.patch},
			)
			require.NotEmpty(t, result.Errors)
			require.Equal(t, "SATELLITE_TYPE_MISMATCH", result.Errors[0].Extensions["code"])
			require.JSONEq(t, before, testFixture.snapshot(t))
		})
	}
}

func TestNullChairTypeNeverWrites(t *testing.T) {
	testFixture := setup(t)
	chair := testFixture.create(t, chairBranch, chairBranch, object{typeField: chairABC})
	mainID := idOf(t, chair)
	before := testFixture.snapshot(t)
	result := testFixture.update(t,
		object{"id": mainID,
			satelliteField: object{chairBranch: object{typeField: nil}}})
	require.NotEmpty(t, result.Errors)
	require.Equal(t, "BAD_USER_INPUT", result.Errors[0].Extensions["code"])
	require.JSONEq(t, before, testFixture.snapshot(t))
}

func TestUnknownMainMutations(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		input object
	}{
		{updateOperation, object{updateOperation: object{"id": "9223372036854775807", titleField: "unknown"}}},
		{deleteOperation, object{deleteOperation: object{"id": "9223372036854775807"}}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			testFixture := setup(t)
			before := testFixture.snapshot(t)
			result := testFixture.gql(t, mutate, object{inputArgument: testCase.input})
			require.NotEmpty(t, result.Errors)
			require.Equal(t, "NOT_FOUND", result.Errors[0].Extensions["code"])
			require.JSONEq(t, before, testFixture.snapshot(t))
		})
	}
}

func TestSoftDelete(t *testing.T) {
	for _, testCase := range []struct {
		kind, table string
		input       object
	}{
		{toolBranch, toolsTable, object{}},
		{tableBranch, tablesTable, object{}},
		{chairBranch, chairsTable, object{typeField: chairABC}},
	} {
		t.Run(testCase.kind, func(t *testing.T) {
			testFixture := setup(t)
			main := testFixture.create(t, testCase.kind, deleteOperation, testCase.input)
			mainID := idOf(t, main)
			result := testFixture.remove(t, mainID)
			require.Empty(t, result.Errors)
			require.Equal(t, object{mainField: nil, deletedIDField: mainID}, result.Data[mainField])
			testFixture.assertLink(t, main, testCase.table, true)
			require.Empty(t, testFixture.list(t, object{"id": mainID}))
			before := testFixture.snapshot(t)
			result = testFixture.remove(t, mainID)
			require.NotEmpty(t, result.Errors)
			require.Equal(t, "ALREADY_DELETED", result.Errors[0].Extensions["code"])
			require.JSONEq(t, before, testFixture.snapshot(t))
			result = testFixture.update(t, object{"id": mainID, titleField: "restore"})
			require.NotEmpty(t, result.Errors)
			require.Equal(t, "NOT_FOUND", result.Errors[0].Extensions["code"])
			require.JSONEq(t, before, testFixture.snapshot(t))
			require.Empty(t, testFixture.list(t, nil))
		})
	}
}

func TestInvalidGraphQLNeverWrites(t *testing.T) {
	testFixture := setup(t)
	main := testFixture.create(t, toolBranch, "unchanged", object{})
	mainID := idOf(t, main)

	type requestCase struct {
		name, query string
		variables   object
	}

	var cases []requestCase
	for _, testCase := range []struct {
		name    string
		input   object
		literal string
	}{
		{"root_empty", object{}, "{}"},
		{"root_two_branches",
			object{deleteOperation: object{"id": mainID},
				createOperation: object{titleField: "x",
					satelliteField: object{toolBranch: object{}}}},
			"{delete:{id:\"" + mainID + "\"},create:{title:\"x\",satellite:{tool:{}}}}"},

		{"root_null", object{deleteOperation: nil}, "{delete:null}"},
		{"root_extra_null",
			object{deleteOperation: object{"id": mainID},
				createOperation: nil},
			"{delete:{id:\"" + mainID + "\"},create:null}"},

		{"create_satellite_empty",
			object{createOperation: object{titleField: "x",
				satelliteField: object{}}},
			"{create:{title:\"x\",satellite:{}}}"},

		{"create_satellite_two_branches",
			object{createOperation: object{titleField: "x",
				satelliteField: object{toolBranch: object{},
					tableBranch: object{}}}},
			"{create:{title:\"x\",satellite:{tool:{},table:{}}}}"},

		{"create_satellite_null",
			object{createOperation: object{titleField: "x",
				satelliteField: object{toolBranch: nil}}},
			"{create:{title:\"x\",satellite:{tool:null}}}"},

		{"create_satellite_extra_null",
			object{createOperation: object{titleField: "x",
				satelliteField: object{toolBranch: object{},
					tableBranch: nil}}},
			"{create:{title:\"x\",satellite:{tool:{},table:null}}}"},

		{"update_satellite_empty",
			object{updateOperation: object{"id": mainID,
				titleField:     invalidValue,
				satelliteField: object{}}},
			"{update:{id:\"" + mainID + "\",satellite:{}}}"},

		{"update_satellite_two_branches",
			object{updateOperation: object{"id": mainID,
				titleField: invalidValue,
				satelliteField: object{toolBranch: object{toolDescriptionField: "x"},
					chairBranch: object{typeField: chairABC}}}},
			"{update:{id:\"" + mainID + "\",satellite:{tool:{description1:\"x\"},chair:{type:abc}}}}"},

		{"update_satellite_null",
			object{updateOperation: object{"id": mainID,
				titleField:     invalidValue,
				satelliteField: object{toolBranch: nil}}},
			"{update:{id:\"" + mainID + "\",satellite:{tool:null}}}"},

		{"update_satellite_extra_null",
			object{updateOperation: object{"id": mainID,
				titleField: invalidValue,
				satelliteField: object{toolBranch: object{toolDescriptionField: "x"},
					tableBranch: nil}}},
			"{update:{id:\"" + mainID + "\",satellite:{tool:{description1:\"x\"},table:null}}}"},
	} {
		cases = append(
			cases,
			requestCase{
				name:      testCase.name + "/variables",
				query:     mutate,
				variables: object{inputArgument: testCase.input},
			},
			requestCase{
				name:  testCase.name + "/literal",
				query: "mutation {main(input:" + testCase.literal + "){deletedId}}",
			},
		)
	}

	for _, testCase := range []struct {
		name  string
		input object
	}{
		{"missing_title", object{createOperation: object{satelliteField: object{toolBranch: object{}}}}},
		{"null_title", object{createOperation: object{titleField: nil, satelliteField: object{toolBranch: object{}}}}},
		{"missing_chair_type",
			object{createOperation: object{titleField: "x",
				satelliteField: object{chairBranch: object{}}}}},

		{"null_chair_type",
			object{createOperation: object{titleField: "x",
				satelliteField: object{chairBranch: object{typeField: nil}}}}},

		{"invalid_create_chair_type",
			object{createOperation: object{titleField: "x",
				satelliteField: object{chairBranch: object{typeField: invalidValue}}}}},

		{"supplied_main_id",
			object{createOperation: object{"id": "5",
				titleField:     "x",
				satelliteField: object{toolBranch: object{}}}}},

		{"supplied_sub_id",
			object{createOperation: object{titleField: "x",
				"sub_id":       "1",
				satelliteField: object{toolBranch: object{}}}}},

		{"supplied_sub_obj",
			object{createOperation: object{titleField: "x",
				"sub_obj":      toolsTable,
				satelliteField: object{toolBranch: object{}}}}},

		{"supplied_created_at",
			object{createOperation: object{titleField: "x",
				createdAtField: "2026-01-01T00:00:00Z",
				satelliteField: object{toolBranch: object{}}}}},

		{"supplied_backlink",
			object{createOperation: object{titleField: "x",
				satelliteField: object{toolBranch: object{mainIDColumn: mainID}}}}},

		{"supplied_deleted_at", object{updateOperation: object{"id": mainID, deletedAtField: nil}}},
		{"supplied_satellite_id",
			object{updateOperation: object{"id": mainID,
				satelliteField: object{toolBranch: object{"id": "999"}}}}},

		{"invalid_update_chair_type",
			object{updateOperation: object{"id": mainID,
				satelliteField: object{chairBranch: object{typeField: invalidValue}}}}},
	} {
		cases = append(
			cases,
			requestCase{name: testCase.name, query: mutate, variables: object{inputArgument: testCase.input}},
		)
	}

	for _, branch := range []struct{ name, typ, selection string }{
		{"root_branch", "MainDeleteInput", "{delete:$branch}"},
		{"create_branch", "ToolCreateInput", "{create:{title:\"x\",satellite:{tool:$branch}}}"},
		{"update_branch", "ToolUpdateInput", "{update:{id:\"" + mainID + "\",satellite:{tool:$branch}}}"},
	} {
		for _, declaration := range []struct{ name, suffix string }{{"nullable", ""}, {"required", "!"}} {
			for _, value := range []struct {
				name      string
				variables object
			}{{"missing", nil}, {"null", object{"branch": nil}}} {
				cases = append(cases, requestCase{
					name: branch.name + "/" + declaration.name + "/" + value.name,
					query: fmt.Sprintf(
						"mutation($branch:%s%s){main(input:%s){deletedId}}",
						branch.typ,
						declaration.suffix,
						branch.selection,
					),
					variables: value.variables,
				})
			}
		}
	}

	cases = append(cases, requestCase{
		name: "invalid_second_alias_prevents_first_write",
		query: `mutation($bad:MainMutationInput!){
 first:main(input:{create:{title:"no write",satellite:{tool:{}}}}){deletedId}
 second:main(input:$bad){deletedId}
}`,

		variables: object{"bad": object{deleteOperation: object{"id": mainID}, createOperation: nil}},
	})
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			before := testFixture.snapshot(t)
			result := testFixture.gql(t, testCase.query, testCase.variables)
			require.NotEmpty(t, result.Errors)
			require.JSONEq(t, before, testFixture.snapshot(t))
		})
	}
}

func TestGraphQLSchema(t *testing.T) {
	testFixture := setup(t)
	result := testFixture.gql(t, `{
 __schema { queryType { fields { name } } mutationType { fields { name } } subscriptionType { name } }
 root: __type(name:"MainMutationInput") { isOneOf inputFields { name defaultValue type { kind } } }
 create: __type(name:"SatelliteCreateInput") { isOneOf }
 update: __type(name:"SatelliteUpdateInput") { isOneOf }
}`, nil)
	require.Empty(t, result.Errors)
	schema, validType := result.Data["__schema"].(object)
	require.True(t, validType)

	for _, typ := range []string{"queryType", "mutationType"} {
		require.Equal(t, object{"fields": []any{object{"name": mainField}}}, schema[typ], typ)
	}

	require.Nil(t, schema["subscriptionType"])

	for _, typ := range []string{"root", createOperation, updateOperation} {
		inputType, validType := result.Data[typ].(object)
		require.True(t, validType)
		require.Equal(t, true, inputType["isOneOf"], typ)
	}

	root, validType := result.Data["root"].(object)
	require.True(t, validType)
	inputFields, validType := root["inputFields"].([]any)
	require.True(t, validType)

	for _, field := range inputFields {
		inputField, validType := field.(object)
		require.True(t, validType)
		require.Nil(t, inputField["defaultValue"])
		fieldType, validType := inputField[typeField].(object)
		require.True(t, validType)
		require.NotEqual(t, "NON_NULL", fieldType["kind"])
	}

	for _, testCase := range []struct{ name, query string }{
		{"query_tool", "{tool(id:\"1\"){id}}"},
		{"query_chair", "{chair(id:\"1\"){id}}"},
		{"create_tool", "mutation{createTool{id}}"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := testFixture.gql(t, testCase.query, nil)
			require.NotEmpty(t, result.Errors)
		})
	}
}

func TestBrokenRelationshipsFailRead(t *testing.T) {
	for _, testCase := range []struct{ name, sql string }{
		{"incorrect_sub_id", "UPDATE main SET sub_id=sub_id+1000 WHERE id=$1"},
		{"deleted_satellite", "UPDATE tools SET deleted_at=clock_timestamp() WHERE main_id=$1"},
		{"extra_satellite", "INSERT INTO tables(main_id,description2) VALUES($1,'foreign satellite')"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			testFixture := setup(t)
			main := testFixture.create(t, toolBranch, "broken", object{})
			testFixture.exec(t, testCase.sql, idOf(t, main))
			before := testFixture.snapshot(t)
			result := testFixture.gql(t, read, object{"id": idOf(t, main)})
			require.NotEmpty(t, result.Errors)
			require.Equal(t, "INTERNAL_SERVER_ERROR", result.Errors[0].Extensions["code"])
			require.JSONEq(t, before, testFixture.snapshot(t))
		})
	}
}

func TestSatelliteWriteFailureRollsBack(t *testing.T) {
	for _, testCase := range []struct {
		kind, table, desc string
		input             object
	}{
		{toolBranch, toolsTable, toolDescriptionField, object{toolDescriptionField: initialDescription}},
		{tableBranch, tablesTable, tableDescriptionField, object{tableDescriptionField: initialDescription}},
		{chairBranch,
			chairsTable,
			chairDescriptionField,
			object{chairDescriptionField: initialDescription,
				typeField: chairABC}},
	} {
		t.Run(testCase.kind, func(t *testing.T) {
			testFixture := setup(t)
			main := testFixture.create(t, testCase.kind, initialDescription, testCase.input)
			testFixture.exec(
				t,
				`CREATE FUNCTION test_fail_write() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'test private SQL failure, must not leak'; END $$`,
			)
			testFixture.exec(
				t,
				"CREATE TRIGGER test_fail BEFORE INSERT OR UPDATE ON "+testCase.table+
					" FOR EACH ROW EXECUTE FUNCTION test_fail_write()",
			)

			cases := []struct {
				name  string
				input object
			}{
				{
					createOperation,
					object{
						createOperation: object{
							titleField:     rollbackValue,
							satelliteField: object{testCase.kind: testCase.input},
						},
					},
				},
				{
					updateOperation,
					object{
						updateOperation: object{
							"id":           idOf(t, main),
							titleField:     rollbackValue,
							satelliteField: object{testCase.kind: object{testCase.desc: rollbackValue}},
						},
					},
				},
				{deleteOperation, object{deleteOperation: object{"id": idOf(t, main)}}},
			}
			for _, operation := range cases {
				t.Run(operation.name, func(t *testing.T) {
					before := testFixture.snapshot(t)
					reply := testFixture.gql(t, mutate, object{inputArgument: operation.input})
					result := reply
					require.NotEmpty(t, result.Errors)
					require.Equal(t, "INTERNAL_SERVER_ERROR", result.Errors[0].Extensions["code"])
					require.NotContains(t, reply.Errors[0].Message, "private SQL")
					require.Nil(t, reply.Data[mainField])
					require.JSONEq(t, before, testFixture.snapshot(t))
				})
			}

			testFixture.exec(t, "DROP TRIGGER test_fail ON "+testCase.table)
			testFixture.exec(t, "DROP FUNCTION test_fail_write()")
			testFixture.assertLink(t, main, testCase.table, false)
		})
	}
}

func TestCommitFailureRollsBack(t *testing.T) {
	testFixture := setup(t)
	main := testFixture.create(t, toolBranch, "commit failure", object{})
	testFixture.exec(
		t,
		`CREATE FUNCTION test_fail_commit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'deferred failure at commit'; END $$`,
	)
	testFixture.exec(
		t,
		`CREATE CONSTRAINT TRIGGER test_fail_commit AFTER INSERT OR UPDATE ON tools
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION test_fail_commit()`,
	)

	cases := []struct {
		name  string
		input object
	}{
		{
			createOperation,
			object{
				createOperation: object{titleField: "commit rollback", satelliteField: object{toolBranch: object{}}},
			},
		},
		{
			updateOperation,
			object{
				updateOperation: object{
					"id":           idOf(t, main),
					satelliteField: object{toolBranch: object{toolDescriptionField: rollbackValue}},
				},
			},
		},
		{deleteOperation, object{deleteOperation: object{"id": idOf(t, main)}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			before := testFixture.snapshot(t)
			reply := testFixture.gql(t, mutate, object{inputArgument: testCase.input})
			result := reply
			require.NotEmpty(t, result.Errors)
			require.Equal(t, "INTERNAL_SERVER_ERROR", result.Errors[0].Extensions["code"])
			require.Nil(t, reply.Data[mainField])
			require.JSONEq(t, before, testFixture.snapshot(t))
		})
	}
}

func TestConcurrentWrites(t *testing.T) {
	t.Run("double_delete", func(t *testing.T) {
		testFixture := setup(t)
		main := testFixture.create(t, toolBranch, "race", object{})
		mainID := idOf(t, main)

		var workers sync.WaitGroup

		results := make(chan response, 2)
		errs := make(chan error, 2)
		start := make(chan struct{})

		for range 2 {
			workers.Go(func() {
				<-start

				reply, err := testFixture.request(
					t.Context(),
					mutate,
					object{inputArgument: object{deleteOperation: object{"id": mainID}}},
				)
				results <- reply

				errs <- err
			})
		}

		close(start)
		workers.Wait()
		close(results)
		close(errs)

		for err := range errs {
			require.NoError(t, err)
		}

		success, failed := 0, 0

		for reply := range results {
			if len(reply.Errors) == 0 {
				success++
			} else {
				result := reply
				require.NotEmpty(t, result.Errors)
				require.Equal(t, "ALREADY_DELETED", result.Errors[0].Extensions["code"])

				failed++
			}
		}

		require.Equal(t, 1, success)
		require.Equal(t, 1, failed)
		testFixture.assertLink(t, main, toolsTable, true)
	})

	for i := range 10 {
		t.Run(fmt.Sprintf("update_delete_%d", i), func(t *testing.T) {
			testFixture := setup(t)
			main := testFixture.create(
				t,
				chairBranch,
				previousValue,
				object{chairDescriptionField: previousValue, typeField: chairABC},
			)
			mainID := idOf(t, main)
			start := make(chan struct{})

			var (
				workers                    sync.WaitGroup
				updateResult, deleteResult response
				updateErr, deleteErr       error
			)

			workers.Go(func() {
				<-start

				updateResult, updateErr = testFixture.request(
					t.Context(),
					mutate,
					object{
						inputArgument: object{
							updateOperation: object{
								"id":       mainID,
								titleField: updatedValue,
								satelliteField: object{
									chairBranch: object{chairDescriptionField: updatedValue, typeField: chairCDE},
								},
							},
						},
					},
				)
			})
			workers.Go(func() {
				<-start

				deleteResult, deleteErr = testFixture.request(
					t.Context(),
					mutate,
					object{inputArgument: object{deleteOperation: object{"id": mainID}}},
				)
			})
			close(start)
			workers.Wait()
			require.NoError(t, updateErr)
			require.NoError(t, deleteErr)
			require.Empty(t, deleteResult.Errors)

			var title, desc, typ string
			require.NoError(
				t,
				testFixture.pool.QueryRow(t.Context(), `SELECT m.title,c.description3,c.type::text
FROM main m JOIN chairs c ON c.main_id=m.id WHERE m.id=$1`, mainID).
					Scan(&title, &desc, &typ),
			)

			if len(updateResult.Errors) == 0 {
				require.Equal(t, []string{updatedValue, updatedValue, chairCDE}, []string{title, desc, typ})
			} else {
				result := updateResult
				require.NotEmpty(t, result.Errors)
				require.Equal(t, "NOT_FOUND", result.Errors[0].Extensions["code"])
				require.Equal(t, []string{previousValue, previousValue, chairABC}, []string{title, desc, typ})
			}

			testFixture.assertLink(t, main, chairsTable, true)
			require.Empty(t, testFixture.list(t, object{"id": mainID}))
		})
	}
}

func TestMigrationsRoundTrip(t *testing.T) {
	testFixture := setup(t)
	ctx := t.Context()
	require.NoError(t, goose.UpContext(ctx, testFixture.database, migrationsDir), "repeat up")
	require.NoError(t, goose.DownContext(ctx, testFixture.database, migrationsDir))

	var table *string
	require.NoError(t, testFixture.pool.QueryRow(ctx, `SELECT to_regclass('main')::text`).Scan(&table))
	require.Nil(t, table, "down left main table")
	require.NoError(t, goose.UpContext(ctx, testFixture.database, migrationsDir))
	rows, err := testFixture.pool.Query(
		ctx,
		`SELECT table_name FROM information_schema.tables WHERE table_schema='public' ORDER BY table_name`,
	)
	require.NoError(t, err)

	defer rows.Close()

	var names []string

	for rows.Next() {
		var n string
		require.NoError(t, rows.Scan(&n))
		names = append(names, n)
	}

	rows.Close()
	require.NoError(t, rows.Err())
	require.Equal(t, []string{chairsTable, "goose_db_version", mainField, tablesTable, toolsTable}, names)
	testFixture.create(t, toolBranch, "after down and up", object{})
}

func TestDatabaseConstraints(t *testing.T) {
	testFixture := setup(t)

	main := testFixture.create(t, toolBranch, "constraints", object{})
	for _, testCase := range []struct {
		name, sql, code string
		args            []any
	}{
		{"foreign_key", "INSERT INTO tools(main_id) VALUES(9223372036854775807)", "23503", nil},
		{"unique_main_id", "INSERT INTO tools(main_id) VALUES($1)", "23505", []any{idOf(t, main)}},
		{"satellite_kind", "UPDATE main SET sub_obj='invalid' WHERE id=$1", "23514", []any{idOf(t, main)}},
		{"chair_enum", "INSERT INTO chairs(main_id,type) VALUES($1,'invalid')", "22P02", []any{idOf(t, main)}},
		{"required_title", "UPDATE main SET title=NULL WHERE id=$1", "23502", []any{idOf(t, main)}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			before := testFixture.snapshot(t)
			_, err := testFixture.pool.Exec(t.Context(), testCase.sql, testCase.args...)

			var databaseError *pgconn.PgError
			require.ErrorAs(t, err, &databaseError)
			require.Equal(t, testCase.code, databaseError.Code)
			require.JSONEq(t, before, testFixture.snapshot(t))
		})
	}
}

func TestGraphQLGET(t *testing.T) {
	testFixture := setup(t)
	request, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		testFixture.server.URL+"?query=%7Bmain%7Bid%7D%7D",
		nil,
	)
	require.NoError(t, err)
	res, err := testFixture.server.Client().Do(request)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	require.Equal(t, http.StatusOK, res.StatusCode)
}
