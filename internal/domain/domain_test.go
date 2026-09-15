package domain

import (
	"errors"
	"math"
	"testing"
)

func ptr[T any](value T) *T { return &value }

func TestParseID(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  int64
	}{
		{"1", 1}, {"42", 42}, {"00042", 42}, {"9223372036854775807", math.MaxInt64},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseID(tc.input)
			if err != nil || got != tc.want {
				t.Fatalf("ParseID(%q) = %d, %v; want %d", tc.input, got, err, tc.want)
			}
		})
	}
	for _, input := range []string{"", "0", "000", "-1", "+1", " 1", "1 ", "1.0", "1e2", "0x2a", "9223372036854775808", "99999999999999999999999999999999999999", "٤٢", "４２", "1\x00"} {
		t.Run("reject_"+input, func(t *testing.T) {
			_, err := ParseID(input)
			assertBadInput(t, err)
		})
	}
}

func TestValidateList(t *testing.T) {
	for _, input := range []ListInput{
		{Limit: 1}, {Limit: 100, Offset: 100}, {ID: ptr(int64(1)), Limit: 20},
		{ID: ptr(int64(math.MaxInt64)), Limit: 20},
	} {
		if err := ValidateList(input); err != nil {
			t.Errorf("valid input %+v: %v", input, err)
		}
	}
	for _, input := range []ListInput{
		{Limit: 0}, {Limit: -1}, {Limit: 101}, {Limit: 20, Offset: -1},
		{ID: ptr(int64(0)), Limit: 20}, {ID: ptr(int64(-1)), Limit: 20},
	} {
		assertBadInput(t, ValidateList(input))
	}
}

func TestValidateCreate(t *testing.T) {
	for _, input := range []CreateInput{
		{Kind: Tools}, {Kind: Tables}, {Kind: Tools, Title: "", Description: ptr("")},
		{Kind: Chairs, ChairType: ABC}, {Kind: Chairs, ChairType: CDE, Description: ptr("chair")},
	} {
		if err := ValidateCreate(input); err != nil {
			t.Errorf("valid input %+v: %v", input, err)
		}
	}
	for _, input := range []CreateInput{
		{}, {Kind: Kind("tool")}, {Kind: Chairs}, {Kind: Chairs, ChairType: ChairType("ABC")},
		{Kind: Tools, ChairType: ABC}, {Kind: Tables, ChairType: CDE},
	} {
		assertBadInput(t, ValidateCreate(input))
	}
}

func TestValidateUpdate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input UpdateInput
		valid bool
	}{
		{"title only", UpdateInput{ID: 1, Title: Patch[string]{Set: true, Value: ptr("new")}}, true},
		{"empty title is a string", UpdateInput{ID: 1, Title: Patch[string]{Set: true, Value: ptr("")}}, true},
		{"no changes", UpdateInput{ID: 1}, false},
		{"invalid id", UpdateInput{Title: Patch[string]{Set: true, Value: ptr("new")}}, false},
		{"null title", UpdateInput{ID: 1, Title: Patch[string]{Set: true}}, false},
		{"null satellite", UpdateInput{ID: 1, Satellite: Patch[SatellitePatch]{Set: true}}, false},
		{"empty tool patch", updateSatellite(SatellitePatch{Kind: Tools}), false},
		{"empty table patch", updateSatellite(SatellitePatch{Kind: Tables}), false},
		{"empty chair patch", updateSatellite(SatellitePatch{Kind: Chairs}), false},
		{"invalid kind", updateSatellite(SatellitePatch{Kind: Kind("unknown"), Description: Patch[string]{Set: true}}), false},
		{"null tool description", updateSatellite(SatellitePatch{Kind: Tools, Description: Patch[string]{Set: true}}), true},
		{"empty table description", updateSatellite(SatellitePatch{Kind: Tables, Description: Patch[string]{Set: true, Value: ptr("")}}), true},
		{"chair description", updateSatellite(SatellitePatch{Kind: Chairs, Description: Patch[string]{Set: true, Value: ptr("new")}}), true},
		{"type only abc", updateSatellite(SatellitePatch{Kind: Chairs, Type: Patch[ChairType]{Set: true, Value: ptr(ABC)}}), true},
		{"type only cde", updateSatellite(SatellitePatch{Kind: Chairs, Type: Patch[ChairType]{Set: true, Value: ptr(CDE)}}), true},
		{"null type", updateSatellite(SatellitePatch{Kind: Chairs, Type: Patch[ChairType]{Set: true}}), false},
		{"unknown type", updateSatellite(SatellitePatch{Kind: Chairs, Type: Patch[ChairType]{Set: true, Value: ptr(ChairType("bad"))}}), false},
		{"type on tool", updateSatellite(SatellitePatch{Kind: Tools, Type: Patch[ChairType]{Set: true, Value: ptr(ABC)}}), false},
		{"type on table", updateSatellite(SatellitePatch{Kind: Tables, Type: Patch[ChairType]{Set: true, Value: ptr(CDE)}}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateUpdate(tc.input)
			if tc.valid {
				if err != nil {
					t.Fatalf("valid update rejected: %v", err)
				}
			} else {
				assertBadInput(t, err)
			}
		})
	}
}

func TestPatchPresenceIsIndependentOfValue(t *testing.T) {
	input := UpdateInput{ID: 1, Title: Patch[string]{Value: ptr("ignored")}}
	assertBadInput(t, ValidateUpdate(input))
	input.Satellite = Patch[SatellitePatch]{Set: true, Value: &SatellitePatch{
		Kind: Tools, Description: Patch[string]{Set: true},
	}}
	if err := ValidateUpdate(input); err != nil {
		t.Fatal(err)
	}
}

func TestErrorPreservesCauseWithoutExposingIt(t *testing.T) {
	cause := errors.New("private database details")
	err := &Error{Code: InternalServerError, Message: "internal server error", Cause: cause}
	if err.Error() != "internal server error" || !errors.Is(err, cause) || ErrorCode(err) != InternalServerError {
		t.Fatalf("unexpected error behavior: %v", err)
	}
	if ErrorCode(errors.New("unknown failure")) != InternalServerError {
		t.Fatal("untyped errors must be internal")
	}
}

func updateSatellite(patch SatellitePatch) UpdateInput {
	return UpdateInput{ID: 1, Satellite: Patch[SatellitePatch]{Set: true, Value: &patch}}
}

func assertBadInput(t *testing.T, err error) {
	t.Helper()
	if err == nil || ErrorCode(err) != BadUserInput {
		t.Fatalf("want BAD_USER_INPUT, got %v", err)
	}
}
