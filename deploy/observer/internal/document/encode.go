package document

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/dannyota/aboutme/deploy/observer/schema"
)

// MaxBytes is the largest document the observer writes.
const MaxBytes = 64 << 10

// Encode serializes doc and validates the bytes against the closed schema,
// every string against its field pattern. Any failure drops the whole
// write; nothing is truncated or passed through.
func Encode(doc *Document) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(true)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	b := buf.Bytes()
	if len(b) > MaxBytes {
		return nil, fmt.Errorf("document is %d bytes, over %d", len(b), MaxBytes)
	}
	if err := schema.Validate(b); err != nil {
		return nil, err
	}
	return b, nil
}
