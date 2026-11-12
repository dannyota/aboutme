package api_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/api"
)

// field and quotedField wrap one value under the property "v".
type field[T any] struct {
	V T `json:"v"`
}

type quotedField[T any] struct {
	V T `json:"v,string"` //nolint:staticcheck // The test puts ",string" on every kind to match encoding/json.
}

type letter struct {
	A int `json:"a"`
}

type namedChild struct {
	Name string `json:"name"`
}

// TestJSONShapeOf_StringOptionQuotesOnlyScalars mirrors encoding/json, which
// honors ",string" only for strings, numbers, and booleans: a quoted struct
// keeps its exact names inside.
func TestJSONShapeOf_StringOptionQuotesOnlyScalars(t *testing.T) {
	t.Parallel()

	type quoted struct {
		Child namedChild `json:"child,string"` //nolint:staticcheck // encoding/json ignores ",string" here; the test pins that.
		Count int        `json:"count,string"`
		Ptr   *int       `json:"ptr,string"`
		List  []string   `json:"list,string"` //nolint:staticcheck // encoding/json ignores ",string" here; the test pins that.
	}
	for _, test := range []struct {
		body, path, problem string
	}{
		{`{"child":{"Name":"x"}}`, "child.Name", `is not declared (the declared name is "name")`},
		{`{"child":"{}"}`, "child", "must be an object"},
		{`{"count":1}`, "count", "must be a string"},
		{`{"ptr":2}`, "ptr", "must be a string"},
		{`{"list":"[]"}`, "list", "must be an array"},
	} {
		err := api.CheckJSONBinding([]byte(test.body), &quoted{})
		var pathErr *api.JSONPathError
		if !errors.As(err, &pathErr) || pathErr.Path != test.path || pathErr.Problem != test.problem {
			t.Errorf("CheckJSONBinding(%s) = %v, want %s %s", test.body, err, test.path, test.problem)
		}
	}
	exact := `{"child":{"name":"x"},"count":"1","ptr":"2","list":["a"]}`
	if err := api.CheckJSONBinding([]byte(exact), &quoted{}); err != nil {
		t.Fatalf("CheckJSONBinding(%s): %v", exact, err)
	}
	if err := json.Unmarshal([]byte(exact), &quoted{}); err != nil {
		t.Fatalf("encoding/json disagrees with %s: %v", exact, err)
	}
}

// TestCheckJSONBinding_AgreesWithEncodingJSON decodes the same values with
// encoding/json and with the checker, for every kind the checker models.
// The checker never rejects a value encoding/json accepts. It may accept a
// value whose content only the type's own decoder judges, such as base64
// text or a number out of a small type's range; those inputs are listed per
// kind, and the decode that follows the check still rejects them.
func TestCheckJSONBinding_AgreesWithEncodingJSON(t *testing.T) {
	t.Parallel()

	inputs := []string{
		`"x"`, `"1"`, `"aGk="`, `"-0"`, `"01"`, `"1e3"`, `"00000000-0000-0000-0000-000000000001"`,
		`1`, `-1`, `1.5`, `1e3`, `300`, `true`, `false`, `null`,
		`[]`, `[1,2]`, `[null]`, `[-1]`, `["a"]`, `[[1]]`, `{}`, `{"a":1}`, `{"a":"1"}`,
	}
	stringInputs := map[string]bool{}
	for _, in := range inputs {
		if in[0] == '"' {
			stringInputs[in] = true
		}
	}
	cases := []struct {
		name   string
		target func() any
		// lenient lists inputs whose content only the type's decoder checks.
		lenient map[string]bool
	}{
		{"string", func() any { return &field[string]{} }, nil},
		{"bool", func() any { return &field[bool]{} }, nil},
		{"int", func() any { return &field[int]{} }, nil},
		{"int64 pointer", func() any { return &field[*int64]{} }, nil},
		{"uint8", func() any { return &field[uint8]{} }, map[string]bool{`-1`: true, `1.5`: true, `1e3`: true, `300`: true}},
		{"float64", func() any { return &field[float64]{} }, nil},
		{"json.Number", func() any { return &field[json.Number]{} }, nil},
		{"[]byte", func() any { return &field[[]byte]{} }, union(stringInputs, map[string]bool{`[-1]`: true})},
		{"[]int", func() any { return &field[[]int]{} }, nil},
		{"[2]int", func() any { return &field[[2]int]{} }, nil},
		{"map[string]int", func() any { return &field[map[string]int]{} }, nil},
		{"struct", func() any { return &field[letter]{} }, nil},
		{"any", func() any { return &field[any]{} }, nil},
		{"json.RawMessage", func() any { return &field[json.RawMessage]{} }, nil},
		{"text unmarshaler", func() any { return &field[uuid.UUID]{} }, stringInputs},
		{"quoted int", func() any { return &quotedField[int]{} }, stringInputs},
		{"quoted string", func() any { return &quotedField[string]{} }, stringInputs},
		{"quoted struct", func() any { return &quotedField[letter]{} }, nil},
		{"json unmarshaler", func() any { return &field[time.Time]{} }, allOf(inputs)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for _, in := range inputs {
				body := []byte(`{"v":` + in + `}`)
				goErr := json.Unmarshal(body, test.target())
				checkErr := api.CheckJSONBinding(body, test.target())
				switch {
				case goErr == nil && checkErr != nil:
					t.Errorf("%s: checker rejects %s, which encoding/json accepts: %v", test.name, in, checkErr)
				case goErr != nil && checkErr == nil && !test.lenient[in]:
					t.Errorf("%s: checker accepts %s, which encoding/json rejects: %v", test.name, in, goErr)
				}
			}
		})
	}
}

func union(sets ...map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, set := range sets {
		for key := range set {
			out[key] = true
		}
	}
	return out
}

func allOf(inputs []string) map[string]bool {
	out := map[string]bool{}
	for _, in := range inputs {
		out[in] = true
	}
	return out
}
