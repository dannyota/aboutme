package resume

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

// resumeSchemaID is resume.schema.json's own $id -- the resource key the
// embedded schema is registered and compiled under (D1).
const resumeSchemaID = "https://aboutme.vn/schema/resume/v1"

// compileCount counts how many times mustCompileSchema has run. It exists
// only so export_test.go can prove compiledSchema below was compiled
// exactly once, at package init (D1 condition (c)) -- never lazily, never
// per call.
var compileCount int

// newSchemaCompiler builds the *jsonschema.Compiler this package always
// uses, with both D1 conditions (a) and (b) applied:
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
// embedded schema.RawSchema (D1 condition (c)): never lazily, never
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
	if err := c.AddResource(resumeSchemaID, doc); err != nil {
		panic("resume: registering embedded resume schema: " + err.Error())
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
	Issues []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("resume: %d validation issue(s): %s", len(e.Issues), strings.Join(e.Issues, "; "))
}

// ValidateForStore is the single write-path choke point (D16/D1): canonical
// marshal -> JSON-Schema validation (embedded schema.RawSchema) ->
// MaxDocumentBytes -> schema.ValidateDocument (the store-layer aggregate
// rules, including Task 2's entry-id uniqueness). Every layer runs
// regardless of whether an earlier one already failed -- schema.
// ValidateDocument operates on the typed Go value directly and never
// depends on the JSON-Schema layer having passed -- so a single call
// reports every issue found (mirrors ajv's allErrors:true and schema.
// ValidateDocument's own "every violation, not just the first" contract).
// Returns nil only if doc is valid at every layer.
func ValidateForStore(doc schema.Resume) error {
	canonical, err := AssembleCanonical(doc)
	if err != nil {
		return &ValidationError{Issues: []string{
			fmt.Sprintf("(document): could not assemble canonical document: %v", err),
		}}
	}

	var entries []issueEntry

	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(canonical))
	if err != nil {
		entries = append(entries, issueEntry{text: fmt.Sprintf("(document): could not parse canonical document: %v", err)})
	} else if err := compiledSchema.Validate(instance); err != nil {
		entries = append(entries, schemaIssueEntries(err)...)
	}

	if len(canonical) > MaxDocumentBytes {
		entries = append(entries, issueEntry{text: fmt.Sprintf(
			"(document): canonical document is %d bytes, exceeds the %d-byte limit",
			len(canonical), MaxDocumentBytes,
		)})
	}

	for _, issue := range schema.ValidateDocument(doc) {
		entries = append(entries, issueEntry{path: issue.Path, text: issue.String()})
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
	for i, e := range entries {
		issues[i] = e.text
	}
	return &ValidationError{Issues: issues}
}

// issueEntry pairs a rendered issue message (text) with the dotted/
// bracketed path it applies to (path), so ValidateForStore can sort by path
// FIRST and text only as a tiebreaker -- genuinely "path-first", not merely
// an accident of which layer's message happens to start with an earlier
// letter (round-2 review minor finding: plain sort.Strings on the rendered
// text alone sorted schema issues -- which all render as "at '/...': ..." --
// strictly before every store issue, which render as "rule (path): ...",
// regardless of the paths actually involved, since 'a' < every rule name's
// first letter). path uses store's own dotted/bracketed convention
// ("content.work.entries[0].jobTitle"), including for schema issues, so the
// two layers' paths compare meaningfully against each other, not just
// within their own layer.
type issueEntry struct {
	path string
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
	ve, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return []issueEntry{{text: err.Error()}}
	}
	var out []issueEntry
	collectSchemaIssueEntries(ve, &out)
	return out
}

func collectSchemaIssueEntries(ve *jsonschema.ValidationError, out *[]issueEntry) {
	if len(ve.Causes) == 0 {
		*out = append(*out, issueEntry{path: instanceLocationPath(ve.InstanceLocation), text: ve.Error()})
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
// the same layer-segregation bug this function exists to avoid.
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
