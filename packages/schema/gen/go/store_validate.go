// store_validate.go is hand-written, alongside section.go (see that file's
// header) — NOT generated, not touched by generate.mjs.
//
// Store-layer aggregate validation for the resume document (design spec §3
// "Relational constraints & store-layer invariants" / §5 sanitizer
// contract). resume.schema.json (JSON Schema) enforces everything
// expressible as a per-value or single-object constraint; these three rules
// are not expressible there — see validation/store.ts's package doc (the TS
// half of this same store layer) for exactly why. This file is the Go half:
// the same three rules, applied against the hand-written Section/Resume
// types from resume.go/section.go, and conformance-tested against the same
// fixtures/store/*.json corpus as store_validate_test.go and
// test/store-validation.test.ts.
//
// Callers (apps/server's Go store layer) run ValidateDocument on every
// write, per design spec §3: "The fully assembled aggregate is validated on
// every write."
package schema

import (
	"fmt"
	"sort"
	"strings"
)

// MaxRichTextBytes is design spec §3's size bound: ≤16 KB rich text per
// entry, byte-exact (resume.schema.json's maxLength only bounds Unicode
// code points, not bytes — see richText's description there).
const MaxRichTextBytes = 16384

// ValidationIssue is one store-layer rule violation.
type ValidationIssue struct {
	// Rule is a stable machine-readable rule id, e.g. "rich-text-byte-length".
	Rule string
	// Path is a dotted/bracketed path into the document, e.g.
	// "content.work.entries[0].description".
	Path string
	// Message is a human-readable explanation, safe to surface in a 422 response.
	Message string
}

func (i ValidationIssue) String() string {
	return fmt.Sprintf("%s (%s): %s", i.Rule, i.Path, i.Message)
}

// RichTextByteLength is the UTF-8 byte length of s. Go strings are already
// UTF-8 byte sequences, so len(s) IS the byte length — this wrapper exists
// only so the intent reads the same as TypeScript's utf8ByteLength.
func RichTextByteLength(s string) int {
	return len(s)
}

// ValidateRichTextByteLength mirrors validation/store.ts's function of the
// same name.
func ValidateRichTextByteLength(value, path string, limit int) []ValidationIssue {
	bytes := RichTextByteLength(value)
	if bytes > limit {
		return []ValidationIssue{{
			Rule: "rich-text-byte-length",
			Path: path,
			Message: fmt.Sprintf(
				"%s: %d UTF-8 bytes exceeds the %d-byte limit "+
					"(resume.schema.json's maxLength only bounds Unicode code points, not bytes)",
				path, bytes, limit,
			),
		}}
	}
	return nil
}

// validateRichTextLengths walks every section's rich-text field(s) —
// mirrors validation/store.ts's RICH_TEXT_FIELDS_BY_SECTION_TYPE table
// (design spec §3's entry-fields table): languageEntry has no rich-text
// field, certificateEntry/projectEntry/customEntry/workEntry/educationEntry
// each have exactly one ("description" except certificate/skill/profile —
// see the per-case field name below), skillEntry has "infoHtml", and
// profileEntry has "text".
func validateRichTextLengths(content map[string]Section) []ValidationIssue {
	var issues []ValidationIssue
	for _, key := range sortedKeys(content) {
		section := content[key]
		switch section.SectionType {
		case Profile:
			for i, e := range section.ProfileEntries {
				if e.Text != nil {
					issues = append(issues, ValidateRichTextByteLength(*e.Text, fmt.Sprintf("content.%s.entries[%d].text", key, i), MaxRichTextBytes)...)
				}
			}
		case Work:
			for i, e := range section.WorkEntries {
				if e.Description != nil {
					issues = append(issues, ValidateRichTextByteLength(*e.Description, fmt.Sprintf("content.%s.entries[%d].description", key, i), MaxRichTextBytes)...)
				}
			}
		case Education:
			for i, e := range section.EducationEntries {
				if e.Description != nil {
					issues = append(issues, ValidateRichTextByteLength(*e.Description, fmt.Sprintf("content.%s.entries[%d].description", key, i), MaxRichTextBytes)...)
				}
			}
		case Skill:
			for i, e := range section.SkillEntries {
				if e.InfoHTML != nil {
					issues = append(issues, ValidateRichTextByteLength(*e.InfoHTML, fmt.Sprintf("content.%s.entries[%d].infoHtml", key, i), MaxRichTextBytes)...)
				}
			}
		case Language:
			// No rich-text field on languageEntry.
		case Certificate:
			for i, e := range section.CertificateEntries {
				if e.Description != nil {
					issues = append(issues, ValidateRichTextByteLength(*e.Description, fmt.Sprintf("content.%s.entries[%d].description", key, i), MaxRichTextBytes)...)
				}
			}
		case Project:
			for i, e := range section.ProjectEntries {
				if e.Description != nil {
					issues = append(issues, ValidateRichTextByteLength(*e.Description, fmt.Sprintf("content.%s.entries[%d].description", key, i), MaxRichTextBytes)...)
				}
			}
		case SectionTypeCustom:
			for i, e := range section.CustomEntries {
				if e.Description != nil {
					issues = append(issues, ValidateRichTextByteLength(*e.Description, fmt.Sprintf("content.%s.entries[%d].description", key, i), MaxRichTextBytes)...)
				}
			}
		}
	}
	return issues
}

// ValidateLayoutSections is the layout aggregate invariant (design spec
// §3): every content[key] must appear exactly once across
// layout.Sections.Main + layout.Sections.Sidebar combined. Mirrors
// validation/store.ts's function of the same name — see its comment for the
// three distinct failure modes reported.
func ValidateLayoutSections(content map[string]Section, layout Layout) []ValidationIssue {
	var issues []ValidationIssue

	contentKeys := make(map[string]bool, len(content))
	for key := range content {
		contentKeys[key] = true
	}

	counts := map[string]int{}
	for _, key := range layout.Sections.Main {
		counts[key]++
	}
	for _, key := range layout.Sections.Sidebar {
		counts[key]++
	}

	for _, key := range sortedIntKeys(counts) {
		count := counts[key]
		if count > 1 {
			issues = append(issues, ValidationIssue{
				Rule: "layout-exactly-once",
				Path: "customization.layout.sections",
				Message: fmt.Sprintf(
					"section key %q appears %d times across layout.sections.main+sidebar combined (must appear exactly once)",
					key, count,
				),
			})
		}
		if !contentKeys[key] {
			issues = append(issues, ValidationIssue{
				Rule:    "layout-missing-content-key",
				Path:    "customization.layout.sections",
				Message: fmt.Sprintf("layout.sections references section key %q, which does not exist in content", key),
			})
		}
	}

	for _, key := range sortedKeys(content) {
		if counts[key] == 0 {
			issues = append(issues, ValidationIssue{
				Rule:    "layout-orphan-content-key",
				Path:    "customization.layout.sections",
				Message: fmt.Sprintf("content section %q is not placed in layout.sections.main or .sidebar", key),
			})
		}
	}

	return issues
}

// yearMonthOrdinal mirrors validation/store.ts's yearMonthOrdinal: a missing
// month is treated as month 1 (start of year) on both sides of the
// comparison — see that file's comment for why.
func yearMonthOrdinal(v YearMonth) int64 {
	m := int64(1)
	if v.M != nil {
		m = *v.M
	}
	return v.Y*12 + m
}

// ValidateDateRange enforces design spec §3: start <= end. Only meaningful
// when End is non-nil — present:true documents already have End nil,
// enforced by resume.schema.json's dateRange allOf/if-then, so there is
// nothing to compare.
func ValidateDateRange(dates *DateRange, path string) []ValidationIssue {
	if dates == nil || dates.End == nil {
		return nil
	}
	if yearMonthOrdinal(dates.Start) > yearMonthOrdinal(*dates.End) {
		return []ValidationIssue{{
			Rule: "date-range-order",
			Path: path,
			Message: fmt.Sprintf(
				"%s: start (%s) is after end (%s)",
				path, formatYearMonth(dates.Start), formatYearMonth(*dates.End),
			),
		}}
	}
	return nil
}

func formatYearMonth(v YearMonth) string {
	if v.M != nil {
		return fmt.Sprintf("%d-%02d", v.Y, *v.M)
	}
	return fmt.Sprintf("%d", v.Y)
}

// validateDateRanges walks every section whose sectionType carries a
// `dates` range (design spec §3's entry-fields table: work, education,
// project, custom). certificate has a single `date` {y,m?}, not a range, so
// there is nothing to order-check there; skill/language/profile have no
// date field at all.
func validateDateRanges(content map[string]Section) []ValidationIssue {
	var issues []ValidationIssue
	for _, key := range sortedKeys(content) {
		section := content[key]
		switch section.SectionType {
		case Work:
			for i, e := range section.WorkEntries {
				issues = append(issues, ValidateDateRange(e.Dates, fmt.Sprintf("content.%s.entries[%d].dates", key, i))...)
			}
		case Education:
			for i, e := range section.EducationEntries {
				issues = append(issues, ValidateDateRange(e.Dates, fmt.Sprintf("content.%s.entries[%d].dates", key, i))...)
			}
		case Project:
			for i, e := range section.ProjectEntries {
				issues = append(issues, ValidateDateRange(e.Dates, fmt.Sprintf("content.%s.entries[%d].dates", key, i))...)
			}
		case SectionTypeCustom:
			for i, e := range section.CustomEntries {
				issues = append(issues, ValidateDateRange(e.Dates, fmt.Sprintf("content.%s.entries[%d].dates", key, i))...)
			}
		}
	}
	return issues
}

// detailTypesWithoutURLConstraint are the personalDetails.details[] chip
// types explicitly exempted from the https-scheme requirement below (design
// spec §3: no design-spec-defined value format for these — see
// resume.schema.json's personalDetail $def description). Every OTHER Type —
// the four URL types (Website/Linkedin/Github/Twitter) AND any out-of-enum
// value, since Type is a bare string-backed type with no UnmarshalJSON enum
// check — is treated as requiring https, fail-closed. Phase-gate re-review
// finding NEW-M1: an earlier version of this rule allowlisted exactly the
// four URL type values, so any out-of-enum Type (reachable because nothing
// in this package validates Type against resume.schema.json's enum) fell
// through to "not a URL type, skip" and let "javascript:" straight past the
// store layer — precisely the "reaches Go's write path without first
// passing through ajv" scenario this rule exists to cover. Default-deny
// closes that: only a Type this file KNOWS has no URL semantics is exempt.
var detailTypesWithoutURLConstraint = map[Type]bool{
	Email:      true,
	Phone:      true,
	Location:   true,
	TypeCustom: true,
}

// ValidatePersonalDetailURLSchemes mirrors validation/store.ts's
// validatePersonalDetailUrlSchemes. resume.schema.json's personalDetail
// $def now restricts `value` to an https:// URL (or "") when Type is one of
// website/linkedin/github/twitter — the same hardening $defs/link already
// applies to employerLink/schoolLink/titleLink/project link. That schema
// check only exists as an ajv (TypeScript) pattern: this package has no
// JSON-Schema validator at all in Go — PersonalDetail.Value above is a bare
// string with no runtime constraint, and Type has no UnmarshalJSON enum
// check either. This is therefore the one rule in this file that duplicates
// something JSON Schema can already express on its own; every other rule
// here exists because JSON Schema can't express it (see this file's header
// comment). Without this, a document that reaches this package's write path
// without first passing through ajv would let a hostile
// "javascript:"/"data:"/"vbscript:" value straight into content.details —
// and since that same document also never saw ajv's `type: enum` check,
// Type cannot be trusted to be one of the eight known values either
// (NEW-M1): this rule is fail-closed on Type, not an allowlist of the four
// URL types.
//
// Phase-gate re-review finding NEW-M2: this checks ONLY the scheme prefix —
// a case-sensitive, anchored "https://" literal — NOT full URI
// well-formedness. resume.schema.json's `then` branch is `pattern` AND
// `format: "uri"`; ajv's `format: "uri"` additionally rejects things this
// function accepts, e.g. an embedded newline/CR/U+2028, embedded whitespace,
// or a non-ASCII IRI host. That gap is intentionally NOT closed here: the
// security-relevant half — rejecting a dangerous scheme — is this
// function's job and is portable (a plain string prefix check, no
// dependency on a `format` implementation), where chasing exact RE2/JS
// URI-parser parity would not meaningfully improve security and risks its
// own divergence bugs. "" (explicitly cleared) stays accepted,
// draft-permissive.
func ValidatePersonalDetailURLSchemes(details []PersonalDetail) []ValidationIssue {
	var issues []ValidationIssue
	for i, d := range details {
		if detailTypesWithoutURLConstraint[d.Type] {
			continue
		}
		if d.Value == "" {
			continue
		}
		if !strings.HasPrefix(d.Value, "https://") {
			issues = append(issues, ValidationIssue{
				Rule: "personal-detail-url-scheme",
				Path: fmt.Sprintf("personalDetails.details[%d].value", i),
				Message: fmt.Sprintf(
					"personalDetails.details[%d].value: type %q requires an https:// URL (or \"\"), got a value that does not start with \"https://\"",
					i, d.Type,
				),
			})
		}
	}
	return issues
}

// ValidatePhotoKeyTraversal mirrors validation/store.ts's
// validatePhotoKeyTraversal (phase-gate re-review finding NEW-M3).
// resume.schema.json's photo.key pattern is
// `^[A-Za-z0-9][A-Za-z0-9!_.*'()/-]*$` — a lookahead-free character class
// that compiles under both ECMA 262 (ajv) and Go's RE2 (design spec §3
// commits the publish policy to being "generated into Go and TS like the
// storage schema — never a hand-written validator", so a future Go-side
// pattern check derived from this file must be able to compile every
// pattern here). The pattern USED to also forbid a ".." substring via a
// negative lookahead (`(?!.*\.\.)`), which RE2 cannot compile at all
// ("invalid or unsupported Perl syntax: `(?!`") — RE2 deliberately excludes
// backtracking constructs for its linear-time matching guarantee. Since
// neither language needs a regex to check "does this string contain two
// consecutive dots" — a plain substring check expresses it directly — the
// ".." rejection lives here instead of in the schema pattern.
func ValidatePhotoKeyTraversal(photo *Photo) []ValidationIssue {
	if photo == nil || !strings.Contains(photo.Key, "..") {
		return nil
	}
	return []ValidationIssue{{
		Rule: "photo-key-path-traversal",
		Path: "personalDetails.photo.key",
		Message: fmt.Sprintf(
			"personalDetails.photo.key: %q contains \"..\" — not a valid S3 object key path segment",
			photo.Key,
		),
	}}
}

// ValidateDocument runs every store-layer aggregate rule against a full
// resume document and returns every violation found (not just the first) —
// mirrors ajv's allErrors: true behavior (test/schema.test.ts) and
// validation/store.ts's validateDocument.
//
// Phase-gate re-review finding M1 (integration-owner ruling): TS iterated a
// Map in placement order while this file iterated sortedKeys/sortedIntKeys,
// so the two halves emitted the same set of issues in a different order for
// the same hostile document. Rather than rewrite each rule function's own
// internal iteration, the FINAL combined list is sorted once here, at the
// return boundary — mirrors validation/store.ts's validateDocument, which
// applies the identical (Path, Rule, Message) sort via Array.prototype.sort.
// Go's string `<` is byte-wise lexicographic, which equals Unicode codepoint
// order for valid UTF-8 (by construction) — the same total order TS's plain
// `<`/`>` string comparison produces for the ASCII/BMP content this codebase
// actually emits into Path/Rule/Message.
func ValidateDocument(r Resume) []ValidationIssue {
	var issues []ValidationIssue
	issues = append(issues, validateRichTextLengths(r.Content)...)
	issues = append(issues, ValidateLayoutSections(r.Content, r.Customization.Layout)...)
	issues = append(issues, validateDateRanges(r.Content)...)
	issues = append(issues, ValidatePersonalDetailURLSchemes(r.PersonalDetails.Details)...)
	issues = append(issues, ValidatePhotoKeyTraversal(r.PersonalDetails.Photo)...)
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Path != issues[j].Path {
			return issues[i].Path < issues[j].Path
		}
		if issues[i].Rule != issues[j].Rule {
			return issues[i].Rule < issues[j].Rule
		}
		return issues[i].Message < issues[j].Message
	})
	return issues
}

func sortedKeys(m map[string]Section) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedIntKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
