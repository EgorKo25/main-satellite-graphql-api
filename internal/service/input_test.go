package service_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/service"
)

func TestListInputValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   service.ListInput
		wantErr error
	}{
		{name: "minimum", input: service.ListInput{Limit: 1}},
		{name: "maximum", input: service.ListInput{Limit: 100, Offset: 100}},
		{name: "id_filter", input: service.ListInput{ID: new(int64(1)), Limit: 20}},
		{name: "maximum_id", input: service.ListInput{ID: new(int64(math.MaxInt64)), Limit: 20}},
		{name: "zero_limit", input: service.ListInput{}, wantErr: service.ErrInvalidInput},
		{name: "negative_limit", input: service.ListInput{Limit: -1}, wantErr: service.ErrInvalidInput},
		{name: "large_limit", input: service.ListInput{Limit: 101}, wantErr: service.ErrInvalidInput},
		{name: "negative_offset", input: service.ListInput{Limit: 20, Offset: -1}, wantErr: service.ErrInvalidInput},
		{name: "zero_id", input: service.ListInput{ID: new(int64(0)), Limit: 20}, wantErr: service.ErrInvalidInput},
		{
			name:    "negative_id",
			input:   service.ListInput{ID: new(int64(-1)), Limit: 20},
			wantErr: service.ErrInvalidInput,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.ErrorIs(t, test.input.Validate(), test.wantErr)
		})
	}
}

func TestCreateInputValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		satellite service.SatelliteCreateInput
		wantErr   error
	}{
		{name: "tool_without_description", satellite: service.SatelliteCreateInput{Tool: &service.ToolCreateInput{}}},
		{
			name:      "table_without_description",
			satellite: service.SatelliteCreateInput{Table: &service.TableCreateInput{}},
		},
		{
			name:      "empty_description",
			satellite: service.SatelliteCreateInput{Tool: &service.ToolCreateInput{Description1: new("")}},
		},
		{
			name:      "chair_abc",
			satellite: service.SatelliteCreateInput{Chair: &service.ChairCreateInput{Type: domain.ABC}},
		},
		{
			name:      "chair_cde",
			satellite: service.SatelliteCreateInput{Chair: &service.ChairCreateInput{Type: domain.CDE}},
		},
		{name: "missing_satellite", wantErr: service.ErrInvalidInput},
		{
			name:      "missing_chair_type",
			satellite: service.SatelliteCreateInput{Chair: &service.ChairCreateInput{}},
			wantErr:   service.ErrInvalidInput,
		},
		{
			name:      "invalid_chair_type",
			satellite: service.SatelliteCreateInput{Chair: &service.ChairCreateInput{Type: "ABC"}},
			wantErr:   service.ErrInvalidInput,
		},
		{
			name: "tool_and_table",
			satellite: service.SatelliteCreateInput{
				Tool:  &service.ToolCreateInput{},
				Table: &service.TableCreateInput{},
			},
			wantErr: service.ErrInvalidInput,
		},
		{
			name: "tool_and_chair",
			satellite: service.SatelliteCreateInput{
				Tool:  &service.ToolCreateInput{},
				Chair: &service.ChairCreateInput{Type: domain.ABC},
			},
			wantErr: service.ErrInvalidInput,
		},
		{
			name: "table_and_chair",
			satellite: service.SatelliteCreateInput{
				Table: &service.TableCreateInput{},
				Chair: &service.ChairCreateInput{Type: domain.CDE},
			},
			wantErr: service.ErrInvalidInput,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			input := service.CreateInput{Title: "", Satellite: test.satellite}

			require.ErrorIs(t, input.Validate(), test.wantErr)
		})
	}
}

func TestUpdateInputValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   service.UpdateInput
		wantErr error
	}{
		{name: "title_only", input: service.UpdateInput{ID: 1, TitleSet: true, Title: new("new")}},
		{name: "empty_title", input: service.UpdateInput{ID: 1, TitleSet: true, Title: new("")}},
		{name: "no_changes", input: service.UpdateInput{ID: 1}, wantErr: service.ErrInvalidInput},
		{
			name:    "invalid_id",
			input:   service.UpdateInput{TitleSet: true, Title: new("new")},
			wantErr: service.ErrInvalidInput,
		},
		{name: "null_title", input: service.UpdateInput{ID: 1, TitleSet: true}, wantErr: service.ErrInvalidInput},
		{
			name:    "null_satellite",
			input:   service.UpdateInput{ID: 1, SatelliteSet: true},
			wantErr: service.ErrInvalidInput,
		},
		{
			name:    "unmarked_title",
			input:   service.UpdateInput{ID: 1, Title: new("ignored")},
			wantErr: service.ErrInvalidInput,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.ErrorIs(t, test.input.Validate(), test.wantErr)
		})
	}
}

func TestSatelliteUpdateValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		satellite service.SatelliteUpdateInput
		wantErr   error
	}{
		{name: "missing_kind", wantErr: service.ErrInvalidInput},
		{
			name:      "empty_tool",
			satellite: service.SatelliteUpdateInput{Tool: &service.ToolUpdateInput{}},
			wantErr:   service.ErrInvalidInput,
		},
		{
			name:      "empty_table",
			satellite: service.SatelliteUpdateInput{Table: &service.TableUpdateInput{}},
			wantErr:   service.ErrInvalidInput,
		},
		{
			name:      "empty_chair",
			satellite: service.SatelliteUpdateInput{Chair: &service.ChairUpdateInput{}},
			wantErr:   service.ErrInvalidInput,
		},
		{
			name:      "null_tool_description",
			satellite: service.SatelliteUpdateInput{Tool: &service.ToolUpdateInput{Description1Set: true}},
		},
		{
			name:      "null_table_description",
			satellite: service.SatelliteUpdateInput{Table: &service.TableUpdateInput{Description2Set: true}},
		},
		{
			name:      "null_chair_description",
			satellite: service.SatelliteUpdateInput{Chair: &service.ChairUpdateInput{Description3Set: true}},
		},
		{
			name: "empty_tool_description",
			satellite: service.SatelliteUpdateInput{
				Tool: &service.ToolUpdateInput{Description1Set: true, Description1: new("")},
			},
		},
		{
			name: "empty_table_description",
			satellite: service.SatelliteUpdateInput{
				Table: &service.TableUpdateInput{Description2Set: true, Description2: new("")},
			},
		},
		{
			name: "empty_chair_description",
			satellite: service.SatelliteUpdateInput{
				Chair: &service.ChairUpdateInput{Description3Set: true, Description3: new("")},
			},
		},
		{
			name: "chair_type_abc",
			satellite: service.SatelliteUpdateInput{
				Chair: &service.ChairUpdateInput{TypeSet: true, Type: new(domain.ABC)},
			},
		},
		{
			name: "chair_type_cde",
			satellite: service.SatelliteUpdateInput{
				Chair: &service.ChairUpdateInput{TypeSet: true, Type: new(domain.CDE)},
			},
		},
		{
			name:      "null_chair_type",
			satellite: service.SatelliteUpdateInput{Chair: &service.ChairUpdateInput{TypeSet: true}},
			wantErr:   service.ErrInvalidInput,
		},
		{
			name: "invalid_chair_type",
			satellite: service.SatelliteUpdateInput{
				Chair: &service.ChairUpdateInput{TypeSet: true, Type: new(domain.ChairType("bad"))},
			},
			wantErr: service.ErrInvalidInput,
		},
		{
			name: "two_kinds",
			satellite: service.SatelliteUpdateInput{
				Tool:  &service.ToolUpdateInput{Description1Set: true},
				Table: &service.TableUpdateInput{Description2Set: true},
			},
			wantErr: service.ErrInvalidInput,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			input := service.UpdateInput{ID: 1, SatelliteSet: true, Satellite: &test.satellite}

			require.ErrorIs(t, input.Validate(), test.wantErr)
		})
	}
}

func BenchmarkUpdateInputValidate(b *testing.B) {
	tests := []struct {
		name  string
		input service.UpdateInput
	}{
		{name: "title", input: service.UpdateInput{ID: 1, TitleSet: true, Title: new("new")}},
		{
			name: "chair",
			input: service.UpdateInput{
				ID:           1,
				SatelliteSet: true,
				Satellite: &service.SatelliteUpdateInput{
					Chair: &service.ChairUpdateInput{TypeSet: true, Type: new(domain.CDE)},
				},
			},
		},
		{name: "invalid", input: service.UpdateInput{ID: 1, SatelliteSet: true}},
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
