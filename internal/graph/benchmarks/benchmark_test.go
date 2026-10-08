package benchmarks_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/domain"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
)

type sample struct {
	kind domain.Kind
	data []byte
	want domain.SubObject
}

func samples(tb testing.TB, description *string) []sample {
	tb.Helper()

	created, err := time.Parse(time.RFC3339Nano, "2026-10-08T12:00:00.123456+03:00")
	require.NoError(tb, err)
	updated, err := time.Parse(time.RFC3339Nano, "2026-10-09T00:00:00.000001-04:00")
	require.NoError(tb, err)

	common := domain.Satellite{ID: 9223372036854775807, MainID: 42, CreatedAt: created, UpdatedAt: updated}
	objects := []domain.SubObject{
		&domain.Chair{Satellite: common, Description3: description, Type: domain.CDE},
		&domain.Tool{Satellite: common, Description1: description},
		&domain.Table{Satellite: common, Description2: description},
	}
	result := make([]sample, 0, len(objects))

	for _, object := range objects {
		data, marshalErr := json.Marshal(object)
		require.NoError(tb, marshalErr)

		result = append(result, sample{kind: object.Kind(), data: data, want: object})
	}

	return result
}

func TestDecoders(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		description *string
	}{
		{name: "SQL NULL"},
		{name: "empty", description: new("")},
		{name: "escaped unicode", description: new("стул\n\"chair\"\\🪑")},
		{name: "large", description: new(strings.Repeat("x", 4096))},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			for _, implementation := range decoders() {
				for _, input := range samples(t, test.description) {
					value, err := implementation.decode(input.kind, input.data)
					require.NoError(t, err, implementation.name)
					require.Equal(t, input.want, value, implementation.name)
					_, err = implementation.decode(domain.Chairs,
						[]byte(`{"id":2,"type":"abc","description3":"overwrite"}`))
					require.NoError(t, err)
					require.Equal(t, input.want, value, "result must own its data after parser reuse")
				}
			}
		})
	}
}

func TestDecoderErrors(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		data string
	}{
		{name: "missing"},
		{name: "null", data: "null"},
		{name: "array", data: "[]"},
		{name: "truncated", data: `{"id":`},
		{name: "overflow", data: `{"id":9223372036854775808}`},
		{name: "ID string", data: `{"id":"1"}`},
		{name: "ID fraction", data: `{"id":1.5}`},
		{name: "description number", data: `{"description3":42}`},
		{name: "type number", data: `{"type":42}`},
		{name: "timestamp", data: `{"created_at":"invalid"}`},
		{name: "timestamp number", data: `{"update_at":42}`},
		{name: "deleted timestamp", data: `{"deleted_at":"invalid"}`},
		{name: "trailing object", data: `{} {}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			for _, implementation := range decoders() {
				value, err := implementation.decode(domain.Chairs, []byte(test.data))
				require.Error(t, err, implementation.name)
				require.Nil(t, value, implementation.name)
			}
		})
	}
}

func TestFactories(t *testing.T) {
	t.Parallel()

	for _, factory := range []func(domain.Kind) domain.SubObject{factoryMap, factorySwitch, factoryLO} {
		for _, kind := range []domain.Kind{domain.Tools, domain.Tables, domain.Chairs} {
			first, second := factory(kind), factory(kind)
			require.Equal(t, kind, first.Kind())
			require.NotSame(t, first, second)
		}

		require.Nil(t, factory("unknown"))
	}
}

var (
	objectSink domain.SubObject
	pageSink   []domain.SubObject
)

func BenchmarkFactory(b *testing.B) {
	kinds := []domain.Kind{domain.Tools, domain.Tables, domain.Chairs}

	for _, test := range []struct {
		name    string
		factory func(domain.Kind) domain.SubObject
	}{
		{name: "map", factory: factoryMap},
		{name: "switch", factory: factorySwitch},
		{name: "lo_switch", factory: factoryLO},
	} {
		b.Run(test.name, func(b *testing.B) {
			index := 0
			for b.Loop() {
				objectSink = test.factory(kinds[index%len(kinds)])
				index++
			}
		})
	}
}

func BenchmarkDecode(b *testing.B) {
	for _, size := range []int{32, 4096} {
		inputs := samples(b, new(strings.Repeat("x", size)))
		implementations := append(decoders(), binaryDecoder(b, inputs))

		for _, count := range []int{1, 20, 100} {
			for _, implementation := range implementations {
				name := fmt.Sprintf("description=%d/rows=%d/decoder=%s", size, count, implementation.name)
				b.Run(name, func(b *testing.B) {
					var err error

					for _, input := range inputs {
						_, err = implementation.decode(input.kind, input.data)
						require.NoError(b, err)
					}

					for b.Loop() {
						pageSink = make([]domain.SubObject, count)
						for index := range count {
							input := inputs[index%len(inputs)]

							pageSink[index], err = implementation.decode(input.kind, input.data)
							if err != nil {
								break
							}
						}

						if err != nil {
							break
						}
					}

					require.NoError(b, err)
					require.Empty(b, cmp.Diff(inputs[0].want, pageSink[0]))
				})
			}
		}
	}
}

func BenchmarkDecodeError(b *testing.B) {
	for _, test := range []struct {
		name string
		data string
	}{
		{name: "overflow", data: `{"id":9223372036854775808}`},
		{name: "timestamp", data: `{"created_at":"invalid"}`},
	} {
		for _, implementation := range decoders() {
			b.Run(test.name+"/decoder="+implementation.name, func(b *testing.B) {
				data := []byte(test.data)

				var err error

				for b.Loop() {
					objectSink, err = implementation.decode(domain.Chairs, data)
				}

				require.Error(b, err)
				require.Nil(b, objectSink)
			})
		}
	}
}
