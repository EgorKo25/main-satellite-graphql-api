package postgres_test

import (
	"math"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
	"github.com/stretchr/testify/require"
)

func TestListInputValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   postgres.ListInput
		wantErr error
	}{
		{name: "minimum", input: postgres.ListInput{Limit: 1}},
		{name: "maximum", input: postgres.ListInput{Limit: 100, Offset: 100}},
		{name: "id filter", input: postgres.ListInput{ID: new(int64(1)), Limit: 20}},
		{name: "maximum id", input: postgres.ListInput{ID: new(int64(math.MaxInt64)), Limit: 20}},
		{name: "zero limit", input: postgres.ListInput{}, wantErr: postgres.ErrInvalidInput},
		{name: "negative limit", input: postgres.ListInput{Limit: -1}, wantErr: postgres.ErrInvalidInput},
		{name: "large limit", input: postgres.ListInput{Limit: 101}, wantErr: postgres.ErrInvalidInput},
		{name: "negative offset", input: postgres.ListInput{Limit: 20, Offset: -1}, wantErr: postgres.ErrInvalidInput},
		{name: "zero id", input: postgres.ListInput{ID: new(int64(0)), Limit: 20}, wantErr: postgres.ErrInvalidInput},
		{
			name:    "negative id",
			input:   postgres.ListInput{ID: new(int64(-1)), Limit: 20},
			wantErr: postgres.ErrInvalidInput,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.ErrorIs(t, test.input.Validate(), test.wantErr)
		})
	}
}

func TestChairCreateValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   postgres.ChairCreate
		wantErr error
	}{
		{name: "abc without description", input: postgres.ChairCreate{Type: domain.ABC}},
		{name: "cde without description", input: postgres.ChairCreate{Type: domain.CDE}},
		{name: "empty description", input: postgres.ChairCreate{Type: domain.ABC, Description3: new("")}},
		{name: "missing type", wantErr: postgres.ErrInvalidInput},
		{name: "uppercase type", input: postgres.ChairCreate{Type: "ABC"}, wantErr: postgres.ErrInvalidInput},
		{name: "unknown type", input: postgres.ChairCreate{Type: "xyz"}, wantErr: postgres.ErrInvalidInput},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.ErrorIs(t, test.input.Validate(), test.wantErr)
		})
	}
}

func TestDescriptionUpdateValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		description graphql.Omittable[*string]
		wantErr     error
	}{
		{name: "missing", wantErr: postgres.ErrInvalidInput},
		{name: "null", description: graphql.OmittableOf[*string](nil)},
		{name: "empty", description: graphql.OmittableOf(new(""))},
		{name: "text", description: graphql.OmittableOf(new("description"))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var (
				tool  = postgres.ToolUpdate{Description1: test.description}
				table = postgres.TableUpdate{Description2: test.description}
				chair = postgres.ChairUpdate{Description3: test.description}
			)
			require.ErrorIs(t, tool.Validate(), test.wantErr)
			require.ErrorIs(t, table.Validate(), test.wantErr)
			require.ErrorIs(t, chair.Validate(), test.wantErr)
		})
	}
}

func TestChairTypeUpdateValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		chairType *domain.ChairType
		wantErr   error
	}{
		{name: "abc", chairType: new(domain.ABC)},
		{name: "cde", chairType: new(domain.CDE)},
		{name: "null", wantErr: postgres.ErrInvalidInput},
		{name: "empty", chairType: new(domain.ChairType("")), wantErr: postgres.ErrInvalidInput},
		{name: "uppercase", chairType: new(domain.ChairType("ABC")), wantErr: postgres.ErrInvalidInput},
		{name: "unknown", chairType: new(domain.ChairType("xyz")), wantErr: postgres.ErrInvalidInput},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			input := postgres.ChairUpdate{Type: graphql.OmittableOf(test.chairType)}
			require.ErrorIs(t, input.Validate(), test.wantErr)
		})
	}
}

func BenchmarkChairUpdateValidate(b *testing.B) {
	tests := []struct {
		name  string
		input postgres.ChairUpdate
	}{
		{name: "description", input: postgres.ChairUpdate{Description3: graphql.OmittableOf(new("new"))}},
		{name: "type", input: postgres.ChairUpdate{Type: graphql.OmittableOf(new(domain.CDE))}},
		{name: "invalid"},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()

			for b.Loop() {
				_ = test.input.Validate()
			}
		})
	}
}
