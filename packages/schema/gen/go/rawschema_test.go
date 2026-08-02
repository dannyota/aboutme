// rawschema_test.go is hand-written (not touched by generate.mjs, unlike the
// rawschema.go it tests). It reads packages/schema/resume.schema.json at
// test time and asserts schema.RawSchema byte-equals it exactly — the Go
// side of decision D2's copy-drift loop (the TypeScript side is
// test/gen.test.ts's existing regenerate-and-byte-compare check, which
// already exercises scripts/generate.mjs's whole output, rawschema.go
// included).
package schema

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRawSchema_ByteEqualsResumeSchemaJSON(t *testing.T) {
	path := filepath.Join("..", "..", "resume.schema.json")
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if !bytes.Equal(RawSchema, want) {
		t.Fatalf("RawSchema (%d bytes) does not byte-equal %s (%d bytes)", len(RawSchema), path, len(want))
	}
}
