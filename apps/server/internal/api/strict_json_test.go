package api_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/api"
)

type strictInner struct {
	Key   *string `json:"key"`
	Count int     `json:"count"`
}

type strictEmbedded struct {
	Shared string `json:"shared"`
}

type strictRequest struct {
	Title   *string                `json:"title"`
	Items   []strictInner          `json:"items"`
	ByName  map[string]strictInner `json:"byName"`
	Raw     json.RawMessage        `json:"raw"`
	Any     any                    `json:"any"`
	ID      uuid.UUID              `json:"id"`
	Flag    bool                   `json:"flag"`
	Ratio   float64                `json:"ratio"`
	Skipped string                 `json:"-"`
	Plain   string
	Nested  *strictInner               `json:"nested"`
	Free    map[string]json.RawMessage `json:"free"`
	strictEmbedded
}

func checkStrict(t *testing.T, body string) error {
	t.Helper()
	return api.CheckJSONBinding([]byte(body), &strictRequest{})
}

func TestCheckJSONBinding_RejectsInexactNamesWithTheirPath(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name, body, path, problem string
	}{
		{"case-variant top level", `{"Title":"x"}`, "Title", `is not declared (the declared name is "title")`},
		{"case-variant duplicate", `{"title":"x","Title":"y"}`, "Title", `is not declared (the declared name is "title")`},
		{"exact duplicate", `{"title":"x","title":"y"}`, "title", "is repeated"},
		{"unknown", `{"other":1}`, "other", "is not declared"},
		{"case-variant in an array element", `{"items":[{"key":"a"},{"Key":"b"}]}`, "items[1].Key", `is not declared (the declared name is "key")`},
		{"case-variant in a map value", `{"byName":{"en":{"KEY":"a"}}}`, "byName.en.KEY", `is not declared (the declared name is "key")`},
		{"case-variant in a pointer struct", `{"nested":{"Count":1}}`, "nested.Count", `is not declared (the declared name is "count")`},
		{"case-variant promoted field", `{"Shared":"x"}`, "Shared", `is not declared (the declared name is "shared")`},
		{"field name instead of tag", `{"Skipped":"x"}`, "Skipped", "is not declared"},
		{"tag of the hidden field", `{"-":"x"}`, "-", "is not declared"},
		{"case-variant untagged field", `{"plain":"x"}`, "plain", `is not declared (the declared name is "Plain")`},
		{"string type", `{"title":1}`, "title", "must be a string"},
		{"integer type", `{"items":[{"count":1.5}]}`, "items[0].count", "must be an integer"},
		{"number type", `{"ratio":"1"}`, "ratio", "must be a number"},
		{"boolean type", `{"flag":"true"}`, "flag", "must be true or false"},
		{"text unmarshaler type", `{"id":7}`, "id", "must be a string"},
		{"object type", `{"nested":[]}`, "nested", "must be an object"},
		{"array type", `{"items":{}}`, "items", "must be an array"},
		{"root type", `[]`, "", "must be an object"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := checkStrict(t, test.body)
			var pathErr *api.JSONPathError
			if !errors.As(err, &pathErr) {
				t.Fatalf("CheckJSONBinding(%s) = %v, want a path error", test.body, err)
			}
			if pathErr.Path != test.path || pathErr.Problem != test.problem {
				t.Fatalf("CheckJSONBinding(%s) = %q %q, want %q %q", test.body, pathErr.Path, pathErr.Problem, test.path, test.problem)
			}
		})
	}
}

func TestCheckJSONBinding_AcceptsExactNamesAndOpaqueValues(t *testing.T) {
	t.Parallel()

	body := `{"title":null,"items":[{"key":"a","count":2},null],"byName":{"en":{"key":"b"},"EN":{"count":0}},` +
		`"raw":{"Anything":[1,{"Goes":true}]},"any":{"Free":1},"id":"00000000-0000-0000-0000-000000000001",` +
		`"flag":false,"ratio":1e3,"Plain":"x","nested":null,"free":{"X":{"Y":1}},"shared":"s"}`
	if err := checkStrict(t, body); err != nil {
		t.Fatalf("CheckJSONBinding: %v", err)
	}
	var decoded strictRequest
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("encoding/json disagrees with the accepted body: %v", err)
	}
}

func TestCheckJSONBinding_RejectsMalformedAndTrailingJSON(t *testing.T) {
	t.Parallel()

	for _, body := range []string{``, `{`, `{"title":"x"}{}`, `{"title":"x"} 1`, `{"title":"x"}}`} {
		if err := checkStrict(t, body); err == nil {
			t.Errorf("CheckJSONBinding(%q) accepted malformed JSON", body)
		}
	}
}

func TestCheckJSONShape_HonorsNullAndRequiredRules(t *testing.T) {
	t.Parallel()

	text := &api.JSONShape{Kind: api.JSONString}
	shape := &api.JSONShape{Fields: map[string]*api.JSONShape{
		"name": text,
		"span": {
			Fields:   map[string]*api.JSONShape{"start": {Kind: api.JSONInteger}, "end": {Kind: api.JSONInteger, Nullable: true}},
			Required: []string{"start", "end"}, Nullable: true,
		},
	}}
	for _, test := range []struct {
		body, path, problem string
	}{
		{`{"name":null}`, "name", "must not be null"},
		{`{"span":{"start":1}}`, "span.end", "is required"},
		{`{"span":{"start":null,"end":null}}`, "span.start", "must not be null"},
	} {
		err := api.CheckJSONShape([]byte(test.body), shape)
		var pathErr *api.JSONPathError
		if !errors.As(err, &pathErr) || pathErr.Path != test.path || pathErr.Problem != test.problem {
			t.Errorf("CheckJSONShape(%s) = %v, want %s %s", test.body, err, test.path, test.problem)
		}
	}
	for _, body := range []string{`{"name":"x","span":null}`, `{"span":{"start":1,"end":null}}`} {
		if err := api.CheckJSONShape([]byte(body), shape); err != nil {
			t.Errorf("CheckJSONShape(%s): %v", body, err)
		}
	}
}

func TestCheckJSONShape_BoundsDepth(t *testing.T) {
	t.Parallel()

	type node struct {
		Next *node `json:"next"`
	}
	deep := strings.Repeat(`{"next":`, 102) + "null" + strings.Repeat("}", 102)
	err := api.CheckJSONShape([]byte(deep), api.JSONShapeOf(reflect.TypeFor[node]()))
	var pathErr *api.JSONPathError
	if !errors.As(err, &pathErr) || pathErr.Problem != "is nested too deeply" {
		t.Fatalf("CheckJSONShape(deep) = %v, want a depth error", err)
	}
}

func TestJSONPathError_MessageNamesTheBoundedPath(t *testing.T) {
	t.Parallel()

	cases := map[string]*api.JSONPathError{
		`request body field "items[1].Key" is not declared`:    {Path: "items[1].Key", Problem: "is not declared"},
		`request body must be an object`:                       {Problem: "must be an object"},
		`request body field "commands[2].Op" is not declared`:  (&api.JSONPathError{Path: "Op", Problem: "is not declared"}).Under("commands[2]"),
		`request body field "commands[2][0]" must be a string`: (&api.JSONPathError{Path: "[0]", Problem: "must be a string"}).Under("commands[2]"),
		`request body field "commands[2]" must be an object`:   (&api.JSONPathError{Problem: "must be an object"}).Under("commands[2]"),
	}
	for want, err := range cases {
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	}
	long := strings.Repeat("k", 200)
	err := checkStrict(t, `{"`+long+`":1}`)
	var pathErr *api.JSONPathError
	if !errors.As(err, &pathErr) || len(pathErr.Path) > 70 || !strings.HasPrefix(pathErr.Path, "kkkk") {
		t.Fatalf("long key path = %v, want a bounded prefix", err)
	}
}
