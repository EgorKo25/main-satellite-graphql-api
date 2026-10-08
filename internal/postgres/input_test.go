package postgres_test

import (
	"testing"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/require"
)

func TestListInputValidate(t *testing.T) {
	t.Parallel()

	validate := validator.New(validator.WithRequiredStructEnabled())

	tests := []struct {
		name    string
		input   postgres.ListInput
		wantErr bool
	}{
		{name: "minimum", input: postgres.ListInput{Limit: 1}},
		{name: "maximum", input: postgres.ListInput{Limit: 100, Offset: 100}},
		{name: "zero limit", input: postgres.ListInput{}, wantErr: true},
		{name: "negative limit", input: postgres.ListInput{Limit: -1}, wantErr: true},
		{name: "large limit", input: postgres.ListInput{Limit: 101}, wantErr: true},
		{name: "negative offset", input: postgres.ListInput{Limit: 20, Offset: -1}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := validate.Struct(test.input)
			if test.wantErr {
				var validationErrors validator.ValidationErrors

				require.ErrorAs(t, err, &validationErrors)

				return
			}

			require.NoError(t, err)
		})
	}
}
