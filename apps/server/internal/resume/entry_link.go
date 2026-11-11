package resume

import (
	"bytes"

	"github.com/santhosh-tekuri/jsonschema/v6"

	schema "danny.vn/aboutme/packages/schema/gen/go"
)

// entryLinkSchema is the current schema's $defs/link, compiled once at
// package init like compiledSchema.
var entryLinkSchema = mustCompileEntryLink()

func mustCompileEntryLink() *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema.RawSchema))
	if err != nil {
		panic("resume: parsing embedded schema for the entry link: " + err.Error())
	}
	c := newSchemaCompiler()
	if addErr := c.AddResource(resumeSchemaID, doc); addErr != nil {
		panic("resume: registering embedded schema for the entry link: " + addErr.Error())
	}
	compiled, err := c.Compile(resumeSchemaID + "#/$defs/link")
	if err != nil {
		panic("resume: compiling the entry link schema: " + err.Error())
	}
	return compiled
}

// ValidEntryLink reports whether s satisfies the entry link contract every
// document write validates, so a copied value cannot fail the write.
func ValidEntryLink(s string) bool {
	return entryLinkSchema.Validate(s) == nil
}
