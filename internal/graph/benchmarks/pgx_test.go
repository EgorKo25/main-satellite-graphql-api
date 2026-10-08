package benchmarks_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

type binaryRow struct {
	types  *pgtype.Map
	fields []pgconn.FieldDescription
	values [][]byte
}

func (row *binaryRow) FieldDescriptions() []pgconn.FieldDescription { return row.fields }
func (row *binaryRow) RawValues() [][]byte                          { return row.values }
func (row *binaryRow) Values() ([]any, error) {
	return nil, errors.New("Values is not used by the named collector")
}

func (row *binaryRow) Scan(destinations ...any) error {
	for index, field := range row.fields {
		if err := row.types.Scan(field.DataTypeOID, field.Format, row.values[index], destinations[index]); err != nil {
			return fmt.Errorf("scan %s: %w", field.Name, err)
		}
	}

	return nil
}

func binaryDecoder(tb testing.TB, inputs []sample) decoder {
	tb.Helper()

	rows := make(map[domain.Kind]*binaryRow, len(inputs))
	for _, input := range inputs {
		var (
			common      domain.Satellite
			description *string
			field       string
			chairType   domain.ChairType
		)

		switch value := input.want.(type) {
		case *domain.Tool:
			common, description, field = value.Satellite, value.Description1, "description1"
		case *domain.Table:
			common, description, field = value.Satellite, value.Description2, "description2"
		case *domain.Chair:
			common, description, field, chairType = value.Satellite, value.Description3, "description3", value.Type
		}

		row := &binaryRow{types: pgtype.NewMap(), fields: []pgconn.FieldDescription{
			{Name: "id", DataTypeOID: pgtype.Int8OID, Format: pgtype.BinaryFormatCode},
			{Name: "main_id", DataTypeOID: pgtype.Int8OID, Format: pgtype.BinaryFormatCode},
			{Name: "created_at", DataTypeOID: pgtype.TimestamptzOID, Format: pgtype.BinaryFormatCode},
			{Name: "updated_at", DataTypeOID: pgtype.TimestamptzOID, Format: pgtype.BinaryFormatCode},
			{Name: "deleted_at", DataTypeOID: pgtype.TimestamptzOID, Format: pgtype.BinaryFormatCode},
			{Name: field, DataTypeOID: pgtype.TextOID, Format: pgtype.TextFormatCode},
		}}
		row.types.RegisterType(&pgtype.Type{Name: "timestamptz", OID: pgtype.TimestamptzOID,
			Codec: &pgtype.TimestamptzCodec{ScanLocation: time.UTC}})

		values := []any{common.ID, common.MainID, common.CreatedAt, common.UpdatedAt, common.DeletedAt, description}

		if input.kind == domain.Chairs {
			row.fields = append(row.fields, pgconn.FieldDescription{
				Name: "type", DataTypeOID: pgtype.TextOID, Format: pgtype.TextFormatCode,
			})
			values = append(values, string(chairType))
		}

		for index, value := range values {
			column := row.fields[index]
			encoded, err := row.types.Encode(column.DataTypeOID, column.Format, value, nil)
			require.NoError(tb, err)

			row.values = append(row.values, encoded)
		}

		rows[input.kind] = row
	}

	return decoder{name: "pgx_binary", decode: func(kind domain.Kind, _ []byte) (domain.SubObject, error) {
		switch kind {
		case domain.Tools:
			return pgx.RowToAddrOfStructByName[domain.Tool](rows[kind])
		case domain.Tables:
			return pgx.RowToAddrOfStructByName[domain.Table](rows[kind])
		case domain.Chairs:
			return pgx.RowToAddrOfStructByName[domain.Chair](rows[kind])
		default:
			return nil, fmt.Errorf("unknown kind %q", kind)
		}
	}}
}

func TestBinaryDecoder(t *testing.T) {
	t.Parallel()

	inputs := samples(t, nil)
	implementation := binaryDecoder(t, inputs)

	for _, input := range inputs {
		value, err := implementation.decode(input.kind, input.data)
		require.NoError(t, err)
		require.Empty(t, cmp.Diff(input.want, value))
	}
}
