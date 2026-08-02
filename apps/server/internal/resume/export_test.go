package resume

// export_test.go exposes package-private internals under test-only names,
// for tests that must prove something about the REAL configuration this
// package uses (not a reimplementation of it) without widening resume's
// actual public API. Compiled only into test binaries (the _test.go
// suffix), never shipped.

import (
	"encoding/json"

	"github.com/santhosh-tekuri/jsonschema/v6"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

// CompileCountForTest reports how many times mustCompileSchema has run.
// Package-level var initializers run exactly once, at package init, before
// any test executes -- so a value of 1 here (checked before any of this
// package's exported functions are even called) already proves the schema
// was compiled once at init, not lazily on first use. D1 condition (c).
func CompileCountForTest() int {
	return compileCount
}

// CompiledSchemaPointerForTest exposes compiledSchema's pointer identity, so
// a test can prove ValidateForStore reuses the same *jsonschema.Schema
// across repeated calls instead of recompiling per call.
func CompiledSchemaPointerForTest() *jsonschema.Schema {
	return compiledSchema
}

// NewSchemaCompilerForTest exposes the exact compiler construction
// (AssertFormat + no URL loader) ValidateForStore's package-init compile
// uses, so a test can prove the no-URL-loader condition (D1 condition (b))
// against the real configuration rather than a similar-looking stand-in.
func NewSchemaCompilerForTest() *jsonschema.Compiler {
	return newSchemaCompiler()
}

// IsResumeCapExceededForTest exposes isResumeCapExceeded (store.go) to
// package resume_test, so a test can prove the D7 cap-violation mapping
// requires an EXACT match on both the SQLSTATE and the message -- not the
// SQLSTATE alone, which other CHECK constraints on resumes also raise --
// without going through a live database.
func IsResumeCapExceededForTest(err error) bool {
	return isResumeCapExceeded(err)
}

// EncodePartsForTest exposes encodeParts (codec.go), now package-private
// (fix round 1, owner ruling: it is the function that actually produces
// the three jsonb values a write persists, so it is the half of the D16
// choke point the compiler can enforce), so tests can still exercise the
// exact function ValidateForStore's own callers use, rather than a
// reimplementation of it.
func EncodePartsForTest(doc schema.Resume) (personalDetails, content, customization json.RawMessage, err error) {
	return encodeParts(doc)
}
