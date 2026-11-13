package publicapi

import (
	"encoding/json"
	"testing"

	schema "danny.vn/aboutme/packages/schema/gen/go"
)

// envelopeMap is request as the renderer receives it.
func envelopeMap(t *testing.T, request any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope
}

// TestProjectDecoderRules_TypesAreExact proves the Go copy of the decoder
// refuses a value of the wrong type wherever the renderer's decoder does:
// null, a number or an array where a string is required is never read as
// an empty string.
func TestProjectDecoderRules_TypesAreExact(t *testing.T) {
	completed := &schema.DateRange{Start: schema.YearMonth{Y: 2020, M: monthOf(1)}, End: &schema.YearMonth{Y: 2021}}
	input := projectTestInput(t, projectTestSnapshot("en", "Ada", nil, schema.MmYyyy, false), 2,
		limitProject("first", 7, completed), limitProject("second", 2, nil))
	list, err := buildProjectListRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	single := input
	single.pages.Projects = input.pages.Projects[:1]
	page, err := buildProjectRequest(single)
	if err != nil {
		t.Fatal(err)
	}
	for name, request := range map[string]any{"list": list, "page": page} {
		if broken := decodeProjectEnvelopeRules(t, envelopeMap(t, request)); len(broken) != 0 {
			t.Fatalf("%s baseline breaks the decoder: %q", name, broken)
		}
	}
	type mutation func(envelope map[string]any)
	field := func(path []string, value any) mutation {
		return func(envelope map[string]any) {
			object := envelope
			for _, key := range path[:len(path)-1] {
				switch next := object[key].(type) {
				case map[string]any:
					object = next
				case []any:
					if first, ok := next[0].(map[string]any); ok {
						object = first
					}
				}
			}
			object[path[len(path)-1]] = value
		}
	}
	pageCases := map[string]mutation{
		"faviconHref null":           field([]string{"faviconHref"}, nil),
		"faviconHref number":         field([]string{"faviconHref"}, 1.0),
		"pageTitle array":            field([]string{"pageTitle"}, []any{"x"}),
		"canonicalOrigin null":       field([]string{"canonicalOrigin"}, nil),
		"discoveryEnabled string":    field([]string{"discoveryEnabled"}, "false"),
		"preview locale null":        field([]string{"preview", "locale"}, nil),
		"preview locale number":      field([]string{"preview", "locale"}, 1.0),
		"preview description null":   field([]string{"preview", "description"}, nil),
		"preview title array":        field([]string{"preview", "title"}, []any{"x"}),
		"preview imageAlt null":      field([]string{"preview", "imageAlt"}, nil),
		"preview imageUrl null":      field([]string{"preview", "imageUrl"}, nil),
		"resume slug null":           field([]string{"resume", "slug"}, nil),
		"resume ownerName number":    field([]string{"resume", "ownerName"}, 1.0),
		"resume lng null":            field([]string{"resume", "lng"}, nil),
		"resume uiLng null":          field([]string{"resume", "uiLng"}, nil),
		"resume colorScheme null":    field([]string{"resume", "colorScheme"}, nil),
		"technologies [null]":        field([]string{"project", "technologies"}, []any{nil}),
		"technologies [number]":      field([]string{"project", "technologies"}, []any{1.0}),
		"technologies string":        field([]string{"project", "technologies"}, "Go"),
		"technologies null":          field([]string{"project", "technologies"}, nil),
		"name null":                  field([]string{"project", "name"}, nil),
		"role null":                  field([]string{"project", "role"}, nil),
		"summary number":             field([]string{"project", "summary"}, 1.0),
		"lang null":                  field([]string{"project", "lang"}, nil),
		"outcome array":              field([]string{"project", "outcome"}, []any{"x"}),
		"description null":           field([]string{"project", "description"}, nil),
		"repositoryUrl null":         field([]string{"project", "repositoryUrl"}, nil),
		"demoUrl number":             field([]string{"project", "demoUrl"}, 1.0),
		"dates null":                 field([]string{"project", "dates"}, nil),
		"dates start null":           field([]string{"project", "dates", "start"}, nil),
		"dates month null":           field([]string{"project", "dates", "start", "m"}, nil),
		"dates year string":          field([]string{"project", "dates", "start", "y"}, "2020"),
		"dates present string":       field([]string{"project", "dates", "present"}, "false"),
		"entryLink null":             field([]string{"entryLink"}, nil),
		"otherProjects null":         field([]string{"otherProjects"}, nil),
		"listUrl null":               field([]string{"listUrl"}, nil),
		"project url null":           field([]string{"project", "url"}, nil),
		"project slug number":        field([]string{"project", "slug"}, 1.0),
		"resume url array":           field([]string{"resume", "url"}, []any{"https://aboutme.example/ada"}),
		"project lang empty":         field([]string{"project", "lang"}, ""),
		"technologies [empty, null]": field([]string{"project", "technologies"}, []any{"", nil}),
	}
	listCases := map[string]mutation{
		"projects null":                 field([]string{"projects"}, nil),
		"row technologies [null]":       field([]string{"projects", "technologies"}, []any{nil}),
		"row moreTechnologies null":     field([]string{"projects", "moreTechnologies"}, nil),
		"row moreTechnologies fraction": field([]string{"projects", "moreTechnologies"}, 1.5),
		"row moreTechnologies string":   field([]string{"projects", "moreTechnologies"}, "1"),
		"row slug null":                 field([]string{"projects", "slug"}, nil),
		"row summary null":              field([]string{"projects", "summary"}, nil),
	}
	for name, mutate := range pageCases {
		envelope := envelopeMap(t, page)
		mutate(envelope)
		if broken := decodeProjectEnvelopeRules(t, envelope); len(broken) == 0 {
			t.Errorf("page %s: decoder copy accepted it, the renderer refuses it", name)
		}
	}
	for name, mutate := range listCases {
		envelope := envelopeMap(t, list)
		mutate(envelope)
		if broken := decodeProjectEnvelopeRules(t, envelope); len(broken) == 0 {
			t.Errorf("list %s: decoder copy accepted it, the renderer refuses it", name)
		}
	}
}

// TestProjectDecoderRules_BlankIsJavaScriptTrim proves the decoder copy reads
// a name as blank as String.prototype.trim does, not as Go's TrimSpace.
func TestProjectDecoderRules_BlankIsJavaScriptTrim(t *testing.T) {
	bom, nel := string(rune(0xfeff)), string(rune(0x85))
	page, err := buildProjectRequest(projectTestInput(t, projectTestSnapshot("en", "Ada", nil, schema.MmYyyy, false), 1, limitProject("names", 1, nil)))
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		key, field, value string
		refused           bool
	}{
		"project name of a byte order mark": {"project", "name", bom, true},
		"owner name of a byte order mark":   {"resume", "ownerName", bom, true},
		"project name of U+0085":            {"project", "name", nel, false},
		"owner name of U+0085":              {"resume", "ownerName", nel, false},
	} {
		envelope := envelopeMap(t, page)
		object, isObject := envelope[test.key].(map[string]any)
		if !isObject {
			t.Fatal(test.key)
		}
		object[test.field] = test.value
		if broken := decodeProjectEnvelopeRules(t, envelope); (len(broken) != 0) != test.refused {
			t.Errorf("%s: broken = %q, want refused %t", name, broken, test.refused)
		}
	}
}
