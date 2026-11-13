package resume

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	schema "danny.vn/aboutme/packages/schema/gen/go"
)

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

var compileCount int

// jsonschema/v6 only annotates format under draft 2020-12, and its default loader reads file: URLs.
func newSchemaCompiler() *jsonschema.Compiler {
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	c.UseLoader(jsonschema.SchemeURLLoader{})
	return c
}

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

type ValidationError struct {
	Issues     []string
	Structured []ValidationIssue
}

type ValidationIssue struct {
	Path    string
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("resume: %d validation issue(s): %s", len(e.Issues), strings.Join(e.Issues, "; "))
}

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

type issueEntry struct {
	path string
	code string
	text string
}

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
