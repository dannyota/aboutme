// Package schema holds the closed JSON Schema of the deployment document
// (docs/design/deployment-transparency/document.md) and validates bytes
// against it.
package schema

import (
	"bytes"
	_ "embed"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// V1 is deployment.v1.json, byte for byte.
//
//go:embed deployment.v1.json
var V1 []byte

const v1URL = "https://aboutme.vn/.well-known/deployment.v1.json"

var v1 = mustCompile()

func mustCompile() *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(V1))
	if err != nil {
		panic(fmt.Sprintf("deployment.v1.json: %v", err))
	}
	c := jsonschema.NewCompiler()
	if err = c.AddResource(v1URL, doc); err != nil {
		panic(fmt.Sprintf("deployment.v1.json: %v", err))
	}
	s, err := c.Compile(v1URL)
	if err != nil {
		panic(fmt.Sprintf("deployment.v1.json: %v", err))
	}
	return s
}

// Validate reports whether b is one JSON value that satisfies the version 1
// schema.
func Validate(b []byte) error {
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if err := v1.Validate(inst); err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	return nil
}
