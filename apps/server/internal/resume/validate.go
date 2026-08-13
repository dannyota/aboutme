package resume

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

// resumeSchemaID is derived from the current embedded schema, so releasing a
// new current version cannot leave the compiler registered under a stale key.
var resumeSchemaID = currentResumeSchemaID()

func currentResumeSchemaID() string {
	var raw struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(schema.RawSchema, &raw); err != nil {
		panic("resume: reading embedded schema $id: " + err.Error())
	}
	if raw.ID == "" {
		panic("resume: embedded schema has no $id")
	}
	return raw.ID
}

// compileCount counts how many times mustCompileSchema has run. It exists
// only so export_test.go can prove compiledSchema below was compiled
// exactly once, at package init -- never lazily, never
// per call.
var compileCount int

// newSchemaCompiler builds the *jsonschema.Compiler this package always
// uses, with two fail-closed settings:
//
//   - AssertFormat(): format assertion enabled, matching packages/schema's
//     ajv configuration (addFormats(new Ajv2020({allErrors:true,
//     strict:true})), which ASSERTS format -- draft 2020-12 defaults
//     jsonschema/v6 to annotation-only, so this must be explicit or a
//     malformed uuid/uri would silently validate.
//   - UseLoader(jsonschema.SchemeURLLoader{}): an EMPTY scheme map rejects
//     every URL scheme, including "file" (the library's own default
//     loader, absent this call, resolves file: URLs from the local
//     filesystem). Resolving any external $ref -- network or filesystem --
//     can therefore never succeed.
func newSchemaCompiler() *jsonschema.Compiler {
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	c.UseLoader(jsonschema.SchemeURLLoader{})
	return c
}

// compiledSchema is compiled exactly ONCE, at package init, from the
// embedded schema.RawSchema: never lazily, never
// per-call. A compilation failure here is a hard startup failure (panic),
// since a server that cannot validate resume documents must not start.
var compiledSchema = mustCompileSchema()

func mustCompileSchema() *jsonschema.Schema {
	compileCount++

	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema.RawSchema))
	if err != nil {
		panic("resume: parsing embedded schema.RawSchema: " + err.Error())
	}

	c := newSchemaCompiler()
	if addErr := c.AddResource(resumeSchemaID, doc); addErr != nil {
		panic("resume: registering embedded resume schema: " + addErr.Error())
	}
	sch, err := c.Compile(resumeSchemaID)
	if err != nil {
		panic("resume: compiling embedded resume schema: " + err.Error())
	}
	return sch
}

// ValidationError is ValidateForStore's failure mode: every issue found
// across the whole pipeline, stable-sorted (path-first) so repeated runs
// over the same document always report issues in the same order.
type ValidationError struct {
	Issues     []string
	Structured []ValidationIssue
}

// ValidationIssue is one client-safe store validation issue. Path uses the
// document's dotted/bracketed notation and Code is a stable schema keyword or
// aggregate rule name.
type ValidationIssue struct {
	Path    string
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("resume: %d validation issue(s): %s", len(e.Issues), strings.Join(e.Issues, "; "))
}

// ValidateForStore is the single write-path choke point: canonical
// marshal -> JSON-Schema validation (embedded schema.RawSchema) ->
// MaxDocumentBytes -> schema.ValidateDocument (the store-layer aggregate
// rules, including entry-id uniqueness). Every layer runs
// regardless of whether an earlier one already failed -- schema.
// ValidateDocument operates on the typed Go value directly and never
// depends on the JSON-Schema layer having passed -- so a single call
// reports every issue found (mirrors ajv's allErrors:true and schema.
// ValidateDocument's own "every violation, not just the first" contract).
// Returns nil only if doc is valid at every layer.
func ValidateForStore(doc schema.Resume) error {
	canonical, err := AssembleCanonical(doc)
	if err != nil {
		message := fmt.Sprintf("(document): could not assemble canonical document: %v", err)
		return &ValidationError{
			Issues:     []string{message},
			Structured: []ValidationIssue{{Path: "(document)", Code: "invalid", Message: message}},
		}
	}

	var entries []issueEntry

	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(canonical))
	if err != nil {
		entries = append(entries, issueEntry{path: "(document)", code: "invalid", text: fmt.Sprintf("(document): could not parse canonical document: %v", err)})
	} else if err := compiledSchema.Validate(instance); err != nil {
		entries = append(entries, schemaIssueEntries(err)...)
	}

	if len(canonical) > MaxDocumentBytes {
		entries = append(entries, issueEntry{path: "(document)", code: "max_bytes", text: fmt.Sprintf(
			"(document): canonical document is %d bytes, exceeds the %d-byte limit",
			len(canonical), MaxDocumentBytes,
		)})
	}

	for _, issue := range schema.ValidateDocument(doc) {
		entries = append(entries, issueEntry{path: issue.Path, code: issue.Rule, text: issue.String()})
	}

	if len(entries) == 0 {
		return nil
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].path != entries[j].path {
			return entries[i].path < entries[j].path
		}
		return entries[i].text < entries[j].text
	})
	issues := make([]string, len(entries))
	structured := make([]ValidationIssue, len(entries))
	for i, e := range entries {
		issues[i] = e.text
		structured[i] = ValidationIssue{Path: nonEmptyValidationPath(e.path), Code: nonEmptyValidationCode(e.code), Message: e.text}
	}
	return &ValidationError{Issues: issues, Structured: structured}
}

// DescribeValidationError extracts client-safe issue details from store or
// released-schema validation errors. It always returns at least one issue.
func DescribeValidationError(err error) []ValidationIssue {
	var validation *ValidationError
	if errors.As(err, &validation) && len(validation.Structured) > 0 {
		return append([]ValidationIssue(nil), validation.Structured...)
	}
	entries := schemaIssueEntries(err)
	if len(entries) == 0 {
		return []ValidationIssue{{Path: "(document)", Code: "invalid", Message: "document does not match its declared schema"}}
	}
	issues := make([]ValidationIssue, len(entries))
	for index, entry := range entries {
		issues[index] = ValidationIssue{
			Path: nonEmptyValidationPath(entry.path), Code: nonEmptyValidationCode(entry.code), Message: entry.text,
		}
	}
	return issues
}

func nonEmptyValidationPath(path string) string {
	if path == "" {
		return "(document)"
	}
	return path
}

func nonEmptyValidationCode(code string) string {
	if code == "" {
		return "invalid"
	}
	return code
}

// issueEntry pairs a rendered issue message (text) with the dotted/
// bracketed path it applies to (path), so ValidateForStore can sort by path
// FIRST and text only as a tiebreaker -- genuinely "path-first", not merely
// an accident of which layer's message starts with an earlier letter. Path
// uses the store's dotted/bracketed convention
// ("content.work.entries[0].jobTitle"), including for schema issues, so the
// two layers' paths compare meaningfully against each other, not just
// within their own layer.
type issueEntry struct {
	path string
	code string
	text string
}

// schemaIssueEntries flattens a jsonschema/v6 validation error's cause tree
// into one issueEntry per LEAF violation (mirrors ajv's allErrors:true:
// every bottom-level constraint violation reported, not just the first).
// The top-level error jsonschema/v6 returns from Schema.Validate always
// wraps its causes under a synthetic kind.Schema node (see that library's
// (*Schema).validate), so this never emits that wrapper's own generic
// "jsonschema validation failed with ..." message -- only real per-field
// violations, each already formatted as "at '/instance/location': message"
// by the leaf ValidationError's own Error() method.
func schemaIssueEntries(err error) []issueEntry {
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return []issueEntry{{path: "(document)", code: "invalid", text: "document does not match its declared schema"}}
	}
	var out []issueEntry
	collectSchemaIssueEntries(ve, &out)
	return out
}

func collectSchemaIssueEntries(ve *jsonschema.ValidationError, out *[]issueEntry) {
	if len(ve.Causes) == 0 {
		code := "invalid"
		if keywordPath := ve.ErrorKind.KeywordPath(); len(keywordPath) > 0 {
			code = keywordPath[len(keywordPath)-1]
		}
		*out = append(*out, issueEntry{path: instanceLocationPath(ve.InstanceLocation), code: code, text: ve.Error()})
		return
	}
	for _, cause := range ve.Causes {
		collectSchemaIssueEntries(cause, out)
	}
}

// instanceLocationPath renders a jsonschema/v6 InstanceLocation (one path
// segment per element, array indices as plain numeric strings) using
// schema.ValidationIssue.Path's OWN dotted/bracketed convention
// ("content.work.entries[0].jobTitle") instead of jsonschema/v6's native
// JSON-pointer-with-leading-slash rendering ("/content/work/entries/0/
// jobTitle") -- '/' sorts before every letter, so leaving it in place would
// make every schema issue sort before every store issue regardless of path,
// the same layer-segregated ordering this function prevents.
func instanceLocationPath(segments []string) string {
	var b strings.Builder
	for _, s := range segments {
		if isDigits(s) {
			b.WriteByte('[')
			b.WriteString(s)
			b.WriteByte(']')
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(s)
	}
	return b.String()
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
