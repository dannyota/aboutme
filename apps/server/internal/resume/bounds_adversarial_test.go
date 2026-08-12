package resume

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

const (
	adversarialBoundsMaxRichTextBytes = 16 * 1024
	adversarialBoundsMaxDocumentBytes = 512 * 1024
)

type adversarialBoundsCase struct {
	name          string
	schemaPath    string
	schemaKeyword string
	limit         int
	mutate        func(map[string]any, int)
	issueContains []string
}

func TestBoundsAdversarialSchemaInventory(t *testing.T) {
	t.Parallel()

	schemaPath := adversarialBoundsFrozenSchemaPath(t)
	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read frozen resume schema %s: %v", schemaPath, err)
	}
	var rawSchema any
	if err := json.Unmarshal(schemaBytes, &rawSchema); err != nil {
		t.Fatalf("decode frozen resume schema %s: %v", schemaPath, err)
	}

	actual := make(map[string]int)
	adversarialBoundsWalkSchema(t, rawSchema, "", actual)

	expected := make(map[string]int)
	for _, testCase := range adversarialBoundsSchemaCases() {
		key := adversarialBoundsInventoryKey(testCase.schemaPath, testCase.schemaKeyword)
		if previous, exists := expected[key]; exists {
			t.Fatalf("duplicate exercised schema bound %q (limits %d and %d)", key, previous, testCase.limit)
		}
		expected[key] = testCase.limit
	}

	var differences []string
	for key, want := range expected {
		got, exists := actual[key]
		switch {
		case !exists:
			differences = append(differences, fmt.Sprintf("missing exercised bound: %s = %d", key, want))
		case got != want:
			differences = append(differences, fmt.Sprintf("wrong exercised limit: %s = %d, want %d", key, got, want))
		}
	}
	for key, got := range actual {
		if _, exists := expected[key]; !exists {
			differences = append(differences, fmt.Sprintf("unexercised schema bound: %s = %d", key, got))
		}
	}
	if len(differences) > 0 {
		sort.Strings(differences)
		t.Fatalf("schema bound inventory mismatch:\n%s", strings.Join(differences, "\n"))
	}
}

func TestBoundsAdversarialSchemaLimitPairs(t *testing.T) {
	for _, testCase := range adversarialBoundsSchemaCases() {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Run("at_limit", func(t *testing.T) {
				document := adversarialBoundsBaseDocument()
				testCase.mutate(document, testCase.limit)
				if err := adversarialBoundsValidate(t, document); err != nil {
					t.Fatalf("%s %s=%d must be accepted, got %T: %v", testCase.schemaPath, testCase.schemaKeyword, testCase.limit, err, err)
				}
			})

			t.Run("limit_plus_one", func(t *testing.T) {
				document := adversarialBoundsBaseDocument()
				testCase.mutate(document, testCase.limit+1)
				validationErr := adversarialBoundsRequireValidationError(
					t,
					adversarialBoundsValidate(t, document),
					fmt.Sprintf("%s %s=%d", testCase.schemaPath, testCase.schemaKeyword, testCase.limit+1),
				)
				for _, substring := range testCase.issueContains {
					if !adversarialBoundsIssuesContain(validationErr.Issues, substring) {
						t.Fatalf("%s rejection issues must contain %q, got: %v", testCase.schemaPath, substring, validationErr.Issues)
					}
				}
			})
		})
	}
}

func TestBoundsAdversarialRichTextUTF8ByteLimitPair(t *testing.T) {
	t.Run("at_limit", func(t *testing.T) {
		document := adversarialBoundsBaseDocument()
		adversarialBoundsSetEntryField(
			document,
			"profile",
			"text",
			adversarialBoundsStringWithUTF8Bytes(adversarialBoundsMaxRichTextBytes),
		)
		if err := adversarialBoundsValidate(t, document); err != nil {
			t.Fatalf("rich text at %d UTF-8 bytes must be accepted, got %T: %v", adversarialBoundsMaxRichTextBytes, err, err)
		}
	})

	t.Run("limit_plus_one", func(t *testing.T) {
		document := adversarialBoundsBaseDocument()
		adversarialBoundsSetEntryField(
			document,
			"profile",
			"text",
			adversarialBoundsStringWithUTF8Bytes(adversarialBoundsMaxRichTextBytes+1),
		)
		validationErr := adversarialBoundsRequireValidationError(
			t,
			adversarialBoundsValidate(t, document),
			fmt.Sprintf("rich text at %d UTF-8 bytes", adversarialBoundsMaxRichTextBytes+1),
		)
		if validationErr == nil {
			t.Fatal("rich-text validation error must not be nil")
		}
	})
}

func TestBoundsAdversarialCanonicalDocumentByteLimitPair(t *testing.T) {
	if MaxDocumentBytes != adversarialBoundsMaxDocumentBytes {
		t.Fatalf("MaxDocumentBytes = %d, want contract value %d", MaxDocumentBytes, adversarialBoundsMaxDocumentBytes)
	}

	t.Run("at_limit", func(t *testing.T) {
		document := adversarialBoundsCanonicalSizedDocument(t, adversarialBoundsMaxDocumentBytes)
		if got := adversarialBoundsCanonicalSize(t, document); got != adversarialBoundsMaxDocumentBytes {
			t.Fatalf("canonical document size = %d, want %d", got, adversarialBoundsMaxDocumentBytes)
		}
		if err := adversarialBoundsValidate(t, document); err != nil {
			t.Fatalf("canonical document at %d bytes must be accepted, got %T: %v", adversarialBoundsMaxDocumentBytes, err, err)
		}
	})

	t.Run("limit_plus_one", func(t *testing.T) {
		document := adversarialBoundsCanonicalSizedDocument(t, adversarialBoundsMaxDocumentBytes+1)
		if got := adversarialBoundsCanonicalSize(t, document); got != adversarialBoundsMaxDocumentBytes+1 {
			t.Fatalf("canonical document size = %d, want %d", got, adversarialBoundsMaxDocumentBytes+1)
		}
		validationErr := adversarialBoundsRequireValidationError(
			t,
			adversarialBoundsValidate(t, document),
			fmt.Sprintf("canonical document at %d bytes", adversarialBoundsMaxDocumentBytes+1),
		)
		if validationErr == nil {
			t.Fatal("canonical-document validation error must not be nil")
		}
	})
}

func adversarialBoundsSchemaCases() []adversarialBoundsCase {
	ascii := func(length int) string { return strings.Repeat("a", length) }
	link := func(length int) string {
		const prefix = "https://x/"
		if length < len(prefix) {
			panic("link bound shorter than required valid prefix")
		}
		return prefix + strings.Repeat("a", length-len(prefix))
	}

	testCases := []adversarialBoundsCase{
		adversarialBoundsEntryFieldCase(
			"maxLength_richText",
			"/$defs/richText",
			"profile",
			"text",
			16384,
			ascii,
		),
		adversarialBoundsEntryFieldCase(
			"maxLength_link",
			"/$defs/link",
			"work",
			"employerLink",
			2048,
			link,
		),
		{
			name:          "maxLength_sectionKey",
			schemaPath:    "/$defs/sectionKey",
			schemaKeyword: "maxLength",
			limit:         36,
			mutate: func(document map[string]any, limit int) {
				adversarialBoundsSetSection(document, strings.Repeat("a", limit), "profile", nil)
			},
		},
		{
			name:          "maxLength_iconKey",
			schemaPath:    "/$defs/iconKey",
			schemaKeyword: "maxLength",
			limit:         64,
			mutate: func(document map[string]any, limit int) {
				section := adversarialBoundsSetSection(document, "section", "profile", nil)
				section["iconKey"] = strings.Repeat("a", limit)
			},
		},
		adversarialBoundsEntryFieldCase(
			"maxLength_work_jobTitle",
			"/$defs/workEntry/allOf/1/properties/jobTitle",
			"work",
			"jobTitle",
			160,
			ascii,
		),
		adversarialBoundsEntryFieldCase(
			"maxLength_work_employer",
			"/$defs/workEntry/allOf/1/properties/employer",
			"work",
			"employer",
			160,
			ascii,
		),
		adversarialBoundsEntryFieldCase(
			"maxLength_work_city",
			"/$defs/workEntry/allOf/1/properties/city",
			"work",
			"city",
			120,
			ascii,
		),
		adversarialBoundsEntryFieldCase(
			"maxLength_work_country",
			"/$defs/workEntry/allOf/1/properties/country",
			"work",
			"country",
			120,
			ascii,
		),
		adversarialBoundsEntryFieldCase(
			"maxLength_education_degree",
			"/$defs/educationEntry/allOf/1/properties/degree",
			"education",
			"degree",
			160,
			ascii,
		),
		adversarialBoundsEntryFieldCase(
			"maxLength_education_school",
			"/$defs/educationEntry/allOf/1/properties/school",
			"education",
			"school",
			160,
			ascii,
		),
		adversarialBoundsEntryFieldCase(
			"maxLength_education_city",
			"/$defs/educationEntry/allOf/1/properties/city",
			"education",
			"city",
			120,
			ascii,
		),
		adversarialBoundsEntryFieldCase(
			"maxLength_education_country",
			"/$defs/educationEntry/allOf/1/properties/country",
			"education",
			"country",
			120,
			ascii,
		),
		adversarialBoundsEntryFieldCase(
			"maxLength_skill_name",
			"/$defs/skillEntry/allOf/1/properties/name",
			"skill",
			"name",
			120,
			ascii,
		),
		adversarialBoundsEntryFieldCase(
			"maxLength_language_name",
			"/$defs/languageEntry/allOf/1/properties/name",
			"language",
			"name",
			120,
			ascii,
		),
		adversarialBoundsEntryFieldCase(
			"maxLength_certificate_title",
			"/$defs/certificateEntry/allOf/1/properties/title",
			"certificate",
			"title",
			160,
			ascii,
		),
		adversarialBoundsEntryFieldCase(
			"maxLength_certificate_issuer",
			"/$defs/certificateEntry/allOf/1/properties/issuer",
			"certificate",
			"issuer",
			160,
			ascii,
		),
		adversarialBoundsEntryFieldCase(
			"maxLength_project_title",
			"/$defs/projectEntry/allOf/1/properties/title",
			"project",
			"title",
			160,
			ascii,
		),
		adversarialBoundsEntryFieldCase(
			"maxLength_custom_title",
			"/$defs/customEntry/allOf/1/properties/title",
			"custom",
			"title",
			160,
			ascii,
		),
		adversarialBoundsEntryFieldCase(
			"maxLength_custom_subtitle",
			"/$defs/customEntry/allOf/1/properties/subtitle",
			"custom",
			"subtitle",
			160,
			ascii,
		),
		adversarialBoundsEntryFieldCase(
			"maxLength_custom_city",
			"/$defs/customEntry/allOf/1/properties/city",
			"custom",
			"city",
			120,
			ascii,
		),
	}

	sectionTypes := []string{
		"profile",
		"work",
		"education",
		"skill",
		"language",
		"certificate",
		"project",
		"custom",
	}
	for index, sectionType := range sectionTypes {
		testCases = append(testCases, adversarialBoundsSectionEntriesCase(index, sectionType))
	}

	testCases = append(testCases,
		adversarialBoundsCase{
			name:          "maxLength_displayName",
			schemaPath:    "/$defs/displayName",
			schemaKeyword: "maxLength",
			limit:         80,
			mutate: func(document map[string]any, limit int) {
				section := adversarialBoundsSetSection(document, "section", "profile", nil)
				section["displayName"] = strings.Repeat("a", limit)
			},
		},
		adversarialBoundsCase{
			name:          "maxProperties_content",
			schemaPath:    "/$defs/content",
			schemaKeyword: "maxProperties",
			limit:         24,
			mutate: func(document map[string]any, limit int) {
				adversarialBoundsSetContentSections(document, limit)
			},
		},
		adversarialBoundsCase{
			name:          "maxLength_photo_key",
			schemaPath:    "/$defs/photo/properties/key",
			schemaKeyword: "maxLength",
			limit:         512,
			mutate: func(document map[string]any, limit int) {
				personalDetails, ok := document["personalDetails"].(map[string]any)
				if !ok {
					panic(fmt.Sprintf("personalDetails = %T, want map[string]any", document["personalDetails"]))
				}
				personalDetails["photo"] = map[string]any{"key": strings.Repeat("a", limit)}
			},
		},
		adversarialBoundsPersonalDetailFieldCase(
			"maxLength_personalDetail_label",
			"/$defs/personalDetail/properties/label",
			"label",
			40,
		),
		adversarialBoundsPersonalDetailFieldCase(
			"maxLength_personalDetail_value",
			"/$defs/personalDetail/properties/value",
			"value",
			256,
		),
		adversarialBoundsPersonalDetailsFieldCase(
			"maxLength_personalDetails_fullName",
			"/$defs/personalDetails/properties/fullName",
			"fullName",
			160,
		),
		adversarialBoundsPersonalDetailsFieldCase(
			"maxLength_personalDetails_headline",
			"/$defs/personalDetails/properties/headline",
			"headline",
			160,
		),
		adversarialBoundsCase{
			name:          "maxItems_personalDetails_details",
			schemaPath:    "/$defs/personalDetails/properties/details",
			schemaKeyword: "maxItems",
			limit:         16,
			mutate: func(document map[string]any, limit int) {
				personalDetails, ok := document["personalDetails"].(map[string]any)
				if !ok {
					panic(fmt.Sprintf("personalDetails = %T, want map[string]any", document["personalDetails"]))
				}
				personalDetails["details"] = adversarialBoundsPersonalDetails(limit)
			},
		},
		adversarialBoundsLayoutItemsCase("main"),
		adversarialBoundsLayoutItemsCase("sidebar"),
	)

	return testCases
}

func adversarialBoundsEntryFieldCase(
	name string,
	schemaPath string,
	sectionType string,
	field string,
	limit int,
	value func(int) string,
) adversarialBoundsCase {
	return adversarialBoundsCase{
		name:          name,
		schemaPath:    schemaPath,
		schemaKeyword: "maxLength",
		limit:         limit,
		mutate: func(document map[string]any, requested int) {
			adversarialBoundsSetEntryField(document, sectionType, field, value(requested))
		},
	}
}

func adversarialBoundsSectionEntriesCase(index int, sectionType string) adversarialBoundsCase {
	return adversarialBoundsCase{
		name:          "maxItems_" + sectionType + "_entries",
		schemaPath:    fmt.Sprintf("/$defs/section/oneOf/%d/properties/entries", index),
		schemaKeyword: "maxItems",
		limit:         64,
		mutate: func(document map[string]any, requested int) {
			entries := make([]any, requested)
			for entryIndex := range entries {
				entries[entryIndex] = adversarialBoundsEntry(entryIndex)
			}
			adversarialBoundsSetSection(document, "section", sectionType, entries)
		},
	}
}

func adversarialBoundsPersonalDetailFieldCase(
	name string,
	schemaPath string,
	field string,
	limit int,
) adversarialBoundsCase {
	return adversarialBoundsCase{
		name:          name,
		schemaPath:    schemaPath,
		schemaKeyword: "maxLength",
		limit:         limit,
		mutate: func(document map[string]any, requested int) {
			detail := adversarialBoundsPersonalDetail(0)
			detail[field] = strings.Repeat("a", requested)
			personalDetails := adversarialBoundsRequireObject(document["personalDetails"], "personalDetails")
			personalDetails["details"] = []any{detail}
		},
	}
}

func adversarialBoundsPersonalDetailsFieldCase(
	name string,
	schemaPath string,
	field string,
	limit int,
) adversarialBoundsCase {
	return adversarialBoundsCase{
		name:          name,
		schemaPath:    schemaPath,
		schemaKeyword: "maxLength",
		limit:         limit,
		mutate: func(document map[string]any, requested int) {
			personalDetails := adversarialBoundsRequireObject(document["personalDetails"], "personalDetails")
			personalDetails[field] = strings.Repeat("a", requested)
		},
	}
}

func adversarialBoundsLayoutItemsCase(column string) adversarialBoundsCase {
	return adversarialBoundsCase{
		name:          "maxItems_layout_" + column,
		schemaPath:    "/$defs/customization/properties/layout/properties/sections/properties/" + column,
		schemaKeyword: "maxItems",
		limit:         24,
		issueContains: []string{"maxItems", "/customization/layout/sections/" + column},
		mutate: func(document map[string]any, requested int) {
			contentCount := requested
			if contentCount > 24 {
				contentCount = 24
			}
			adversarialBoundsSetContentSections(document, contentCount)

			placement := make([]any, requested)
			for index := range placement {
				placement[index] = adversarialBoundsSectionKey(index)
			}
			sections := adversarialBoundsLayoutSections(document)
			sections["main"] = []any{}
			sections["sidebar"] = []any{}
			sections[column] = placement
		},
	}
}

func adversarialBoundsBaseDocument() map[string]any {
	return map[string]any{
		"schemaVersion":   2,
		"personalDetails": map[string]any{},
		"content":         map[string]any{},
		"customization": map[string]any{
			"font": map[string]any{
				"family":     "inter",
				"baseSizePx": 16,
			},
			"colors": map[string]any{
				"primary":    "#112233",
				"text":       "#223344",
				"background": "#ffffff",
			},
			"spacing": map[string]any{
				"sectionGap": 16,
				"entryGap":   8,
				"lineHeight": 1.5,
			},
			"heading": map[string]any{
				"style":    "normal",
				"showRule": true,
			},
			"layout": map[string]any{
				"columns": 1,
				"sections": map[string]any{
					"main":    []any{},
					"sidebar": []any{},
				},
			},
			"sectionDisplay": map[string]any{
				"skill":    map[string]any{"style": "text"},
				"language": map[string]any{"style": "text"},
			},
			"pageFormat": "a4",
			"dateFormat": "YYYY",
		},
	}
}

func adversarialBoundsSetEntryField(document map[string]any, sectionType string, field string, value string) {
	entry := adversarialBoundsEntry(0)
	entry[field] = value
	adversarialBoundsSetSection(document, "section", sectionType, []any{entry})
}

func adversarialBoundsSetSection(
	document map[string]any,
	key string,
	sectionType string,
	entries []any,
) map[string]any {
	if entries == nil {
		entries = []any{}
	}
	section := map[string]any{
		"sectionType": sectionType,
		"entries":     entries,
	}
	document["content"] = map[string]any{key: section}
	sections := adversarialBoundsLayoutSections(document)
	sections["main"] = []any{key}
	sections["sidebar"] = []any{}
	return section
}

func adversarialBoundsSetContentSections(document map[string]any, count int) {
	content := make(map[string]any, count)
	keys := make([]any, count)
	for index := 0; index < count; index++ {
		key := adversarialBoundsSectionKey(index)
		keys[index] = key
		content[key] = map[string]any{
			"sectionType": "profile",
			"entries":     []any{},
		}
	}
	document["content"] = content

	mainCount := count
	if mainCount > 24 {
		mainCount = 24
	}
	sections := adversarialBoundsLayoutSections(document)
	sections["main"] = append([]any{}, keys[:mainCount]...)
	sections["sidebar"] = append([]any{}, keys[mainCount:]...)
}

func adversarialBoundsLayoutSections(document map[string]any) map[string]any {
	customization := adversarialBoundsRequireObject(document["customization"], "customization")
	layout := adversarialBoundsRequireObject(customization["layout"], "customization.layout")
	return adversarialBoundsRequireObject(layout["sections"], "customization.layout.sections")
}

func adversarialBoundsRequireObject(value any, name string) map[string]any {
	object, ok := value.(map[string]any)
	if !ok {
		panic(fmt.Sprintf("%s = %T, want map[string]any", name, value))
	}
	return object
}

func adversarialBoundsEntry(index int) map[string]any {
	return map[string]any{"id": adversarialBoundsUUID(index)}
}

func adversarialBoundsPersonalDetail(index int) map[string]any {
	return map[string]any{
		"id":       adversarialBoundsUUID(index),
		"type":     "custom",
		"value":    "",
		"isHidden": false,
	}
}

func adversarialBoundsPersonalDetails(count int) []any {
	details := make([]any, count)
	for index := range details {
		details[index] = adversarialBoundsPersonalDetail(index)
	}
	return details
}

func adversarialBoundsUUID(index int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012x", index+1)
}

func adversarialBoundsSectionKey(index int) string {
	index++
	var reversed []byte
	for index > 0 {
		index--
		reversed = append(reversed, byte('a'+index%26))
		index /= 26
	}
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	return "s" + string(reversed)
}

func adversarialBoundsStringWithUTF8Bytes(byteLength int) string {
	return strings.Repeat("é", byteLength/2) + strings.Repeat("a", byteLength%2)
}

func adversarialBoundsCanonicalSizedDocument(t *testing.T, target int) map[string]any {
	t.Helper()

	document := adversarialBoundsBaseDocument()
	entries := make([]any, 32)
	for index := range entries {
		entry := adversarialBoundsEntry(index)
		entry["text"] = ""
		entries[index] = entry
	}
	adversarialBoundsSetSection(document, "profile", "profile", entries)

	remaining := target - adversarialBoundsCanonicalSize(t, document)
	if remaining < 0 {
		t.Fatalf("minimal padding document exceeds requested canonical size %d", target)
	}
	for _, rawEntry := range entries {
		if remaining == 0 {
			break
		}
		padding := remaining
		if padding > adversarialBoundsMaxRichTextBytes {
			padding = adversarialBoundsMaxRichTextBytes
		}
		entry := adversarialBoundsRequireObject(rawEntry, "profile entry")
		entry["text"] = strings.Repeat("x", padding)
		remaining -= padding
	}
	if remaining != 0 {
		t.Fatalf("could not construct %d-byte canonical document: %d padding bytes remain", target, remaining)
	}
	if got := adversarialBoundsCanonicalSize(t, document); got != target {
		t.Fatalf("constructed canonical document size = %d, want %d", got, target)
	}
	return document
}

func adversarialBoundsCanonicalSize(t *testing.T, document map[string]any) int {
	t.Helper()

	decoded := adversarialBoundsDecode(t, document)
	canonical, err := AssembleCanonical(decoded)
	if err != nil {
		t.Fatalf("assemble canonical document: %v", err)
	}
	return len(canonical)
}

func adversarialBoundsValidate(t *testing.T, document map[string]any) error {
	t.Helper()
	return ValidateForStore(adversarialBoundsDecode(t, document))
}

func adversarialBoundsDecode(t *testing.T, document map[string]any) schema.Resume {
	t.Helper()

	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal generated document: %v", err)
	}

	var decoded schema.Resume
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode generated document: %v", err)
	}
	return decoded
}

func adversarialBoundsRequireValidationError(t *testing.T, err error, context string) *ValidationError {
	t.Helper()

	if err == nil {
		t.Fatalf("%s must be rejected with *ValidationError, got nil", context)
	}
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("%s must return *ValidationError, got %T: %v", context, err, err)
	}
	if reflect.TypeOf(err) != reflect.TypeOf(validationErr) {
		t.Fatalf("%s must return an unwrapped *ValidationError, got %T: %v", context, err, err)
	}
	return validationErr
}

func adversarialBoundsIssuesContain(issues []string, substring string) bool {
	for _, issue := range issues {
		if strings.Contains(issue, substring) {
			return true
		}
	}
	return false
}

func adversarialBoundsFrozenSchemaPath(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate bounds adversarial test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", "..", "packages", "schema", "resume.schema.json"))
}

func adversarialBoundsWalkSchema(t *testing.T, value any, path string, inventory map[string]int) {
	t.Helper()

	switch node := value.(type) {
	case map[string]any:
		for _, keyword := range []string{"maxLength", "maxItems", "maxProperties"} {
			rawLimit, exists := node[keyword]
			if !exists {
				continue
			}
			floatLimit, ok := rawLimit.(float64)
			if !ok || floatLimit < 0 || floatLimit != float64(int(floatLimit)) {
				t.Fatalf("schema %s %s is not a non-negative integer: %#v", path, keyword, rawLimit)
			}
			key := adversarialBoundsInventoryKey(path, keyword)
			if _, duplicate := inventory[key]; duplicate {
				t.Fatalf("duplicate schema inventory key %q", key)
			}
			inventory[key] = int(floatLimit)
		}
		for key, child := range node {
			adversarialBoundsWalkSchema(t, child, path+"/"+adversarialBoundsEscapeJSONPointer(key), inventory)
		}
	case []any:
		for index, child := range node {
			adversarialBoundsWalkSchema(t, child, fmt.Sprintf("%s/%d", path, index), inventory)
		}
	}
}

func adversarialBoundsInventoryKey(path string, keyword string) string {
	return path + " " + keyword
}

func adversarialBoundsEscapeJSONPointer(token string) string {
	token = strings.ReplaceAll(token, "~", "~0")
	return strings.ReplaceAll(token, "/", "~1")
}
