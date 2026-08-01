// store_validate_test.go is hand-written, alongside store_validate.go — not
// generated, not touched by generate.mjs. Loads the SAME
// packages/schema/fixtures/{,store/}*.json corpus that
// test/store-validation.test.ts uses, so the Go and TypeScript halves of the
// store layer are conformance-tested against one shared set of documents,
// not two independently hand-maintained ones.
package schema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// fixturesDir resolves packages/schema/fixtures relative to this file
// (gen/go/), independent of the test runner's working directory.
func fixturesDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "fixtures")
}

func loadResumeFixture(t *testing.T, parts ...string) Resume {
	t.Helper()
	path := filepath.Join(append([]string{fixturesDir(t)}, parts...)...)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}
	var resume Resume
	if err := json.Unmarshal(data, &resume); err != nil {
		t.Fatalf("decoding fixture %s: %v", path, err)
	}
	return resume
}

func ruleSet(issues []ValidationIssue) map[string]bool {
	set := make(map[string]bool, len(issues))
	for _, i := range issues {
		set[i.Rule] = true
	}
	return set
}

func TestValidateDocument_CleanFixturesProduceNoIssues(t *testing.T) {
	// Every one of these is schema-valid AND store-valid — draft
	// permissiveness (design spec §3, revised 2026-08-01) must never trip
	// these rules on a half-typed document.
	for _, name := range []string{"minimal.json", "full.json", "draft-partial.json", "draft-cleared-name-empty-section.json"} {
		t.Run(name, func(t *testing.T) {
			resume := loadResumeFixture(t, name)
			if issues := ValidateDocument(resume); len(issues) != 0 {
				t.Fatalf("expected zero issues, got %d: %v", len(issues), issues)
			}
		})
	}
}

func TestValidateDocument_RichTextByteLength(t *testing.T) {
	resume := loadResumeFixture(t, "store", "invalid-oversize-richtext-bytes.json")

	text := resume.Content["profile"].ProfileEntries[0].Text
	if text == nil {
		t.Fatalf("expected profile entry 0 to have a Text value")
	}
	if got := len([]rune(*text)); got != 9000 {
		t.Fatalf("expected 9000 code points, got %d", got)
	}
	if got := RichTextByteLength(*text); got != 18000 {
		t.Fatalf("expected 18000 UTF-8 bytes, got %d", got)
	}

	issues := ValidateDocument(resume)
	if !ruleSet(issues)["rich-text-byte-length"] {
		t.Fatalf("expected a rich-text-byte-length issue, got: %v", issues)
	}
}

func TestValidateRichTextByteLength_BoundaryAtLimit(t *testing.T) {
	limit := MaxRichTextBytes
	if issues := ValidateRichTextByteLength(stringOfLen(limit), "p", limit); len(issues) != 0 {
		t.Fatalf("expected no issues exactly at the limit, got: %v", issues)
	}
	if issues := ValidateRichTextByteLength(stringOfLen(limit+1), "p", limit); len(issues) != 1 {
		t.Fatalf("expected exactly one issue one byte over the limit, got: %v", issues)
	}
}

func stringOfLen(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}

func TestValidateDocument_LayoutAggregateInvariant(t *testing.T) {
	cases := []struct {
		fixture string
		rule    string
	}{
		{"invalid-layout-duplicate-across-arrays.json", "layout-exactly-once"},
		{"invalid-layout-missing-content-key.json", "layout-missing-content-key"},
		{"invalid-layout-orphan-content-key.json", "layout-orphan-content-key"},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			resume := loadResumeFixture(t, "store", tc.fixture)
			issues := ValidateDocument(resume)
			if !ruleSet(issues)[tc.rule] {
				t.Fatalf("expected a %q issue, got: %v", tc.rule, issues)
			}
		})
	}
}

// Phase-gate re-review finding M1 (integration-owner ruling): TS used to
// iterate a Map in placement order while this file iterated
// sortedKeys/sortedIntKeys, so the two halves emitted the same 4 issues in
// a different order for this exact document (the re-review's own
// reproduction case — see test/store-validation.test.ts's "emits layout
// issues in a canonical (path, rule, message) order" test for the TS side
// of this same assertion). ValidateDocument's return-boundary sort
// canonicalizes this.
func TestValidateDocument_LayoutIssueOrderMatchesTS(t *testing.T) {
	resume := Resume{
		Content: map[string]Section{
			"zebra":  NewWorkSection("", "", nil),
			"alpha":  NewWorkSection("", "", nil),
			"orphan": NewWorkSection("", "", nil),
		},
		Customization: Customization{
			Layout: Layout{
				Sections: Sections{
					Main:    []string{"zebra", "zebra", "missingOne"},
					Sidebar: []string{"missingTwo", "alpha"},
				},
			},
		},
	}
	issues := ValidateDocument(resume)
	wantRules := []string{
		"layout-exactly-once",
		"layout-missing-content-key",
		"layout-missing-content-key",
		"layout-orphan-content-key",
	}
	if len(issues) != len(wantRules) {
		t.Fatalf("expected %d issues, got %d: %v", len(wantRules), len(issues), issues)
	}
	for i, want := range wantRules {
		if issues[i].Rule != want {
			t.Fatalf("issue %d: expected rule %q, got %q (full: %v)", i, want, issues[i].Rule, issues)
		}
	}
	if !contains(issues[1].Message, "missingOne") {
		t.Fatalf("expected issue 1 to mention missingOne, got: %s", issues[1].Message)
	}
	if !contains(issues[2].Message, "missingTwo") {
		t.Fatalf("expected issue 2 to mention missingTwo, got: %s", issues[2].Message)
	}
}

func TestValidateLayoutSections_AcceptsExactlyOncePlacement(t *testing.T) {
	resume := loadResumeFixture(t, "full.json")
	if issues := ValidateLayoutSections(resume.Content, resume.Customization.Layout); len(issues) != 0 {
		t.Fatalf("expected no layout issues, got: %v", issues)
	}
}

func TestValidateDocument_ReversedDateRange(t *testing.T) {
	resume := loadResumeFixture(t, "store", "invalid-reversed-date-range.json")
	issues := ValidateDocument(resume)
	if !ruleSet(issues)["date-range-order"] {
		t.Fatalf("expected a date-range-order issue, got: %v", issues)
	}
}

func TestValidateDateRange_AcceptsEqualAndAscendingAndOpenRanges(t *testing.T) {
	if issues := ValidateDateRange(&DateRange{Start: YearMonth{Y: 2020}, End: &YearMonth{Y: 2020}, Present: false}, "p"); len(issues) != 0 {
		t.Fatalf("expected no issues for start==end, got: %v", issues)
	}
	if issues := ValidateDateRange(&DateRange{Start: YearMonth{Y: 2020}, End: &YearMonth{Y: 2022}, Present: false}, "p"); len(issues) != 0 {
		t.Fatalf("expected no issues for start<end, got: %v", issues)
	}
	if issues := ValidateDateRange(&DateRange{Start: YearMonth{Y: 2020}, End: nil, Present: true}, "p"); len(issues) != 0 {
		t.Fatalf("expected no issues for an open (present, end=nil) range, got: %v", issues)
	}
}

func TestValidateDateRange_ComparesByMonthWithinSameYear(t *testing.T) {
	start := int64(6)
	end := int64(3)
	issues := ValidateDateRange(&DateRange{Start: YearMonth{Y: 2020, M: &start}, End: &YearMonth{Y: 2020, M: &end}, Present: false}, "p")
	if len(issues) != 1 {
		t.Fatalf("expected exactly one issue, got: %v", issues)
	}
	if !contains(issues[0].Message, "2020-06") || !contains(issues[0].Message, "2020-03") {
		t.Fatalf("expected message to name both months, got: %s", issues[0].Message)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (func() bool {
		for i := 0; i+len(substr) <= len(s); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}

// fixtures/store/invalid-duplicate-entry-id.json (AC-DOC-002: entry ids
// unique across the whole resume) is a pre-existing fixture for a separate,
// already-tracked P2A store-layer rule (see test/schema.test.ts's comment on
// why fixtures/store/ is excluded from the schema fixture scan) — not one of
// the rules this validator implements, so it is deliberately not exercised
// here, mirroring test/store-validation.test.ts's own note.

// Phase-gate review finding I1: validation/store.ts used to throw uncaught
// TypeErrors on inputs this Go half handles cleanly (a hostile sectionType
// crashed a JS object-literal lookup; a dates object missing start
// null-dereffed). These fixtures are deliberately schema-INVALID (ajv would
// reject them) so the divergence is exercised via the shared corpus, not
// just asserted about in isolation. On the Go side there is no divergence to
// fix — Section.UnmarshalJSON already rejects an unknown sectionType with a
// clean error at the decode boundary (see section.go), and DateRange.Start
// being a value type already tolerates a missing start as a zero YearMonth
// — but these tests pin that behavior down explicitly, the same way the
// removed review probe (zz_probe_test.go) did, so a future refactor can't
// silently reintroduce a panic on the Go side either.
func TestValidateDocument_HostileSectionTypeDecodesCleanlyNotPanics(t *testing.T) {
	cases := []string{
		"invalid-hostile-sectiontype-constructor.json",
		"invalid-hostile-sectiontype-proto.json",
		"invalid-hostile-sectiontype-hasownproperty.json",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(fixturesDir(t), "store", name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading fixture %s: %v", path, err)
			}

			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("decoding %s panicked: %v", name, r)
				}
			}()

			var resume Resume
			decodeErr := json.Unmarshal(data, &resume)
			if decodeErr == nil {
				t.Fatalf("expected a decode error for a hostile sectionType in %s, got a clean decode", name)
			}
			if !contains(decodeErr.Error(), "unknown sectionType") {
				t.Fatalf("expected an \"unknown sectionType\" decode error for %s, got: %v", name, decodeErr)
			}
		})
	}
}

func TestValidateDocument_MissingDatesStartDoesNotPanic(t *testing.T) {
	resume := loadResumeFixture(t, "store", "invalid-missing-dates-start.json")
	issues := ValidateDocument(resume)
	if ruleSet(issues)["date-range-order"] {
		t.Fatalf("expected no date-range-order issue for a zero-value Start, got: %v", issues)
	}
}

// Blanket regression guard (item 2's "never exceptions" requirement): every
// fixture in the shared corpus, valid or hostile, must be handled without
// panicking — a clean decode error is an acceptable outcome for a malformed
// fixture, a panic is not.
func TestFixtureCorpus_NeverPanics(t *testing.T) {
	storeDir := filepath.Join(fixturesDir(t), "store")
	entries, err := os.ReadDir(storeDir)
	if err != nil {
		t.Fatalf("reading %s: %v", storeDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(storeDir, name))
			if err != nil {
				t.Fatalf("reading fixture %s: %v", name, err)
			}

			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("fixture %s panicked: %v", name, r)
				}
			}()

			var resume Resume
			if err := json.Unmarshal(data, &resume); err != nil {
				// A clean decode error is the expected, non-panicking
				// outcome for a malformed fixture — nothing further to
				// validate.
				return
			}
			_ = ValidateDocument(resume)
		})
	}
}

// Phase-gate review finding C1 follow-up: resume.schema.json's
// personalDetail $def now restricts `value` to https:// (or "") when `type`
// is one of website/linkedin/github/twitter — but that check is ajv-only
// (TypeScript). This package has no JSON-Schema pattern validator at all
// (PersonalDetail.Value above is a bare, unconstrained string), so without
// ValidatePersonalDetailURLSchemes a document that reaches this package's
// write path without first passing through ajv would let a hostile scheme
// straight into content.details. See ValidatePersonalDetailURLSchemes's own
// comment in store_validate.go for why this is the one rule here that
// duplicates something JSON Schema could already express.
func TestValidateDocument_PersonalDetailUrlScheme(t *testing.T) {
	resume := loadResumeFixture(t, "store", "invalid-personal-detail-url-scheme.json")
	issues := ValidateDocument(resume)
	if len(issues) != 1 {
		t.Fatalf("expected exactly one issue (the hostile detail, not the clean linkedin one), got %d: %v", len(issues), issues)
	}
	if issues[0].Rule != "personal-detail-url-scheme" {
		t.Fatalf("expected rule personal-detail-url-scheme, got: %v", issues[0])
	}
	if issues[0].Path != "personalDetails.details[0].value" {
		t.Fatalf("expected path personalDetails.details[0].value, got: %v", issues[0].Path)
	}
}

func TestValidatePersonalDetailURLSchemes(t *testing.T) {
	httpsVal := "https://ada.example.com"
	emptyVal := ""
	jsVal := "javascript:alert(document.cookie)"
	dataVal := "data:text/html,<script>alert(1)</script>"
	protoRelVal := "//evil.example.com"
	vbVal := "vbscript:msgbox(1)"
	mixedCaseVal := "JavaScript:alert(1)"
	mailtoVal := "mailto:ada@example.com"

	cases := []struct {
		name     string
		typ      Type
		value    *string
		expectOK bool
	}{
		{"website javascript", Website, &jsVal, false},
		{"website data", Website, &dataVal, false},
		{"website protocol-relative", Website, &protoRelVal, false},
		{"linkedin javascript", Linkedin, &jsVal, false},
		{"github vbscript", Github, &vbVal, false},
		{"twitter mixed-case javascript", Twitter, &mixedCaseVal, false},
		{"website mailto (type-appropriate subset: https-only)", Website, &mailtoVal, false},
		{"website https accepted", Website, &httpsVal, true},
		{"linkedin empty accepted (draft-permissive)", Linkedin, &emptyVal, true},
		{"email type unaffected (out of scope)", Email, &jsVal, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			details := []PersonalDetail{{ID: "018f0000-0000-7000-8000-000000000601", Type: tc.typ, Value: *tc.value, IsHidden: false}}
			issues := ValidatePersonalDetailURLSchemes(details)
			ok := len(issues) == 0
			if ok != tc.expectOK {
				t.Fatalf("type=%s value=%q: expected ok=%v, got issues: %v", tc.typ, *tc.value, tc.expectOK, issues)
			}
		})
	}
}

// Phase-gate re-review finding NEW-M1: an earlier version of this rule
// allowlisted exactly the four URL Type values
// (detailTypesRequiringHTTPS[d.Type]), so any out-of-enum Type — reachable
// because nothing in this package validates Type against
// resume.schema.json's enum (Type has no UnmarshalJSON) — fell through to
// "not a URL type, skip" and let "javascript:" straight past the store
// layer. These pin down the fail-closed fix: only the four KNOWN non-URL
// types stay exempt.
func TestValidatePersonalDetailURLSchemes_OutOfEnumTypeNoLongerBypasses(t *testing.T) {
	jsVal := "javascript:alert(1)"
	for _, typ := range []Type{"WEBSITE", "url", "", "not-a-real-type"} {
		t.Run(string(typ), func(t *testing.T) {
			details := []PersonalDetail{{ID: "018f0000-0000-7000-8000-000000000602", Type: typ, Value: jsVal, IsHidden: false}}
			issues := ValidatePersonalDetailURLSchemes(details)
			if len(issues) == 0 {
				t.Fatalf("type=%q: expected the fail-closed default to catch a javascript: value, got zero issues", typ)
			}
		})
	}
}

func TestValidatePersonalDetailURLSchemes_KnownNonURLTypesStayUnconstrained(t *testing.T) {
	jsVal := "javascript:alert(1)"
	for _, typ := range []Type{Email, Phone, Location, TypeCustom} {
		t.Run(string(typ), func(t *testing.T) {
			details := []PersonalDetail{{ID: "018f0000-0000-7000-8000-000000000603", Type: typ, Value: jsVal, IsHidden: false}}
			issues := ValidatePersonalDetailURLSchemes(details)
			if len(issues) != 0 {
				t.Fatalf("type=%q: expected to stay unconstrained (no design-spec-defined value format), got: %v", typ, issues)
			}
		})
	}
}

// Phase-gate re-review finding NEW-I1: the TS half's validatePersonalDetailUrlSchemes
// (and three older functions in that same file) threw on a JSON document
// where an array-shaped field carried a non-array value instead of
// null/undefined — e.g. {"personalDetails":{"details":{"a":1}}} — because
// `?? []` only substitutes for null/undefined. The re-review's concrete
// claim about THIS side of the store layer was "Go, same JSON body, does
// not panic — json.Unmarshal returns [a clean type-mismatch error]"; these
// tests pin that claim down as an executable assertion (not just a manual
// probe) so a future change to Resume's field types can't silently drop it.
// Every case below is the JSON-text equivalent of one of the TS half's new
// "structurally malformed documents" tests in
// test/store-validation.test.ts.
func TestJSONUnmarshal_StructurallyMalformedArrayFieldsDecodeCleanlyNotPanic(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			"section entries carrying a non-array object (TS: store.ts:130/283)",
			`{"content":{"w":{"sectionType":"work","entries":{"a":1}}}}`,
		},
		{
			"layout.sections.main carrying a non-array object (TS: store.ts:171-173)",
			`{"customization":{"layout":{"sections":{"main":{"a":1},"sidebar":[]}}}}`,
		},
		{
			"personalDetails.details carrying a non-array object (TS: store.ts:341-342)",
			`{"personalDetails":{"details":{"a":1}}}`,
		},
		{
			"personalDetails.details carrying a bare number",
			`{"personalDetails":{"details":5}}`,
		},
		{
			"personalDetails.details carrying a bare string",
			`{"personalDetails":{"details":"x"}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("decoding %q panicked: %v", tc.body, r)
				}
			}()
			var resume Resume
			err := json.Unmarshal([]byte(tc.body), &resume)
			if err == nil {
				t.Fatalf("expected a decode error for a structurally malformed array field, got a clean decode: %+v", resume)
			}
			// Not asserting the exact encoding/json error string (it's
			// stdlib-version-dependent, e.g. "json: cannot unmarshal object
			// into Go struct field ... of type []schema.PersonalDetail") —
			// only that decode fails cleanly (an error return) rather than
			// panicking, which the deferred recover() above already proves.
		})
	}
}

// The TS half's null-document guard (validateDocument(null/undefined) → [])
// has no direct Go equivalent — ValidateDocument takes Resume by value, so a
// nil document is not constructible — but a zero-value Resume{} (the
// closest Go analogue to "nothing was ever decoded") must still round-trip
// through every rule without panicking, mirroring the TS guard's intent.
func TestValidateDocument_ZeroValueResumeProducesNoIssues(t *testing.T) {
	issues := ValidateDocument(Resume{})
	if len(issues) != 0 {
		t.Fatalf("expected zero issues for a zero-value Resume, got: %v", issues)
	}
}

// Phase-gate re-review finding NEW-M3: resume.schema.json's photo.key
// pattern used to reject a ".." substring via a negative lookahead, which
// does not compile under Go's RE2 engine. The lookahead was removed from
// the schema pattern; this store-layer rule is the new home for the ".."
// rejection.
func TestValidatePhotoKeyTraversal(t *testing.T) {
	cases := []struct {
		name        string
		key         string
		expectIssue bool
	}{
		{"traversal at start", "../../other-user/secret.jpg", true},
		{"traversal mid-string", "a/../b.jpg", true},
		{"literal double dot in a filename", "a..b.jpg", true},
		{"normal key", "resumes/ada/photo-original.jpg", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues := ValidatePhotoKeyTraversal(&Photo{Key: tc.key})
			hasIssue := len(issues) != 0
			if hasIssue != tc.expectIssue {
				t.Fatalf("key=%q: expected issue=%v, got: %v", tc.key, tc.expectIssue, issues)
			}
			if hasIssue && issues[0].Rule != "photo-key-path-traversal" {
				t.Fatalf("expected rule photo-key-path-traversal, got: %v", issues[0])
			}
		})
	}

	t.Run("nil photo does not panic", func(t *testing.T) {
		if issues := ValidatePhotoKeyTraversal(nil); len(issues) != 0 {
			t.Fatalf("expected zero issues for a nil photo, got: %v", issues)
		}
	})
}

func TestValidateDocument_PhotoKeyTraversalIsWired(t *testing.T) {
	resume := Resume{
		PersonalDetails: PersonalDetails{
			Photo: &Photo{Key: "../../other-user/secret.jpg"},
		},
	}
	issues := ValidateDocument(resume)
	if !ruleSet(issues)["photo-key-path-traversal"] {
		t.Fatalf("expected a photo-key-path-traversal issue, got: %v", issues)
	}
}

// Phase-gate re-review finding NEW-M3 (verification method #22 in the
// re-review): every "pattern" keyword anywhere in resume.schema.json must
// compile under Go's regexp package (RE2) — nothing in this repo compiles a
// schema pattern into a Go regexp today, but design spec §3 commits a
// future generated Go validator to reading this same file, so a
// non-RE2-portable pattern here would silently break at generation time.
// This is the regression guard: it would have caught photo.key's original
// negative-lookahead pattern before this fix, and catches any future
// pattern regressing the same way.
func TestResumeSchemaPatterns_CompileUnderGoRE2(t *testing.T) {
	schemaPath := filepath.Join("..", "..", "resume.schema.json")
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("reading %s: %v", schemaPath, err)
	}
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decoding resume.schema.json: %v", err)
	}

	var patterns []string
	var walk func(node any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			for key, value := range v {
				if key == "pattern" {
					if s, ok := value.(string); ok {
						patterns = append(patterns, s)
					}
					continue
				}
				walk(value)
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	walk(doc)

	if len(patterns) == 0 {
		t.Fatalf("expected to find at least one \"pattern\" keyword in resume.schema.json — the walk logic may be broken")
	}
	for _, p := range patterns {
		if _, err := regexp.Compile(p); err != nil {
			t.Errorf("pattern %q does not compile under Go RE2: %v", p, err)
		}
	}
}
