// Command conformance is hand-written, NOT generated, and not part of the
// schema package's public API. It exists only so
// packages/schema/test/conformance.test.ts can prove "the Go dispatch
// decodes it" by actually running Go code against gen/go/section.go's
// hand-written Section.UnmarshalJSON, instead of re-implementing Go's
// decode logic in JS (which would just test the JS reimplementation, not
// the real thing).
//
// Reads one JSON Section object (resume.schema.json's `content[key]`
// shape: sectionType/displayName/iconKey/entries) from stdin, decodes it
// via schema.Section, and exits 0 (printing "OK") on success or 1 (with the
// error on stderr) on failure — most notably "unknown sectionType", which
// is exactly what happens when the schema declares a sectionType that
// section.go's hand-maintained switch statements haven't been updated for
// yet (design spec §3, "Codegen fidelity").
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

func main() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	var section schema.Section
	if err := json.Unmarshal(data, &section); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := section.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println("OK")
}
