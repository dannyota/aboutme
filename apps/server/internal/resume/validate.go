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

	var issues []string

	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(canonical))
	if err != nil {
		issues = append(issues, fmt.Sprintf("(document): could not parse canonical document: %v", err))
	} else if err := compiledSchema.Validate(instance); err != nil {
		issues = append(issues, schemaIssues(err)...)
	}

	if len(canonical) > MaxDocumentBytes {
		issues = append(issues, fmt.Sprintf(
			"(document): canonical document is %d bytes, exceeds the %d-byte limit",
			len(canonical), MaxDocumentBytes,
		))
	}

	for _, issue := range schema.ValidateDocument(doc) {
		issues = append(issues, issue.String())
	}

	if len(issues) == 0 {
		return nil
	}
	sort.Strings(issues)
	return &ValidationError{Issues: issues}
}

// schemaIssues flattens a jsonschema/v6 validation error's cause tree into
// one string per LEAF violation (mirrors ajv's allErrors:true: every
// bottom-level constraint violation reported, not just the first). The
// top-level error jsonschema/v6 returns from Schema.Validate always wraps
// its causes under a synthetic kind.Schema node (see that library's
// (*Schema).validate), so this never emits that wrapper's own generic
// "jsonschema validation failed with ..." message -- only real per-field
// violations, each already formatted as "at '/instance/location': message"
// by the leaf ValidationError's own Error() method.
func schemaIssues(err error) []string {
	ve, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return []string{err.Error()}
	}
	var out []string
	collectSchemaIssues(ve, &out)
	return out
}

func collectSchemaIssues(ve *jsonschema.ValidationError, out *[]string) {
	if len(ve.Causes) == 0 {
		*out = append(*out, ve.Error())
		return
	}
	for _, cause := range ve.Causes {
		collectSchemaIssues(cause, out)
	}
}
