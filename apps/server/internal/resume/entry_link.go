package resume

import (
	"bytes"

	"github.com/santhosh-tekuri/jsonschema/v6"

	schema "danny.vn/aboutme/packages/schema/gen/go"
)

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

func ValidEntryLink(s string) bool {
	return entryLinkSchema.Validate(s) == nil
}
