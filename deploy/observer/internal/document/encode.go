package document

import (
	"bytes"
	"encoding/json"
	"fmt"

	"danny.vn/aboutme/deploy/observer/schema"
)

const MaxBytes = 64 << 10

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
