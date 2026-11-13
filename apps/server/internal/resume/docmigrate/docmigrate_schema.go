package docmigrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/santhosh-tekuri/jsonschema/v6"

	schema "danny.vn/aboutme/packages/schema/gen/go"
)

var releasedValidators = mustReleasedValidators()

func mustReleasedValidators() map[int32]ValidateFunc {
	out := make(map[int32]ValidateFunc)
	for _, version := range schema.ReleasedVersions() {
		released, err := schema.ReleasedSchemaFor(version)
		if err != nil {
			panic("docmigrate: reading released schema: " + err.Error())
		}
		validate, err := newSchemaValidator(released.RawSchema)
		if err != nil {
			panic(fmt.Sprintf("docmigrate: compiling released schema v%d: %v", version, err))
		}
		if version < 1 || version > math.MaxInt32 {
			panic(fmt.Sprintf("docmigrate: released schema version %d is out of range for schema_version", version))
		}
		out[int32(version)] = validate
	}
	return out
}

func newSchemaValidator(raw []byte) (ValidateFunc, error) {
	var head struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, fmt.Errorf("parsing schema: %w", err)
	}
	if head.ID == "" {
		return nil, errors.New("schema has no $id")
	}

	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("parsing schema: %w", err)
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	c.UseLoader(jsonschema.SchemeURLLoader{})
	if addErr := c.AddResource(head.ID, doc); addErr != nil {
		return nil, fmt.Errorf("registering schema %s: %w", head.ID, addErr)
	}
	compiled, err := c.Compile(head.ID)
	if err != nil {
		return nil, fmt.Errorf("compiling schema %s: %w", head.ID, err)
	}

	return func(doc json.RawMessage) error {
		instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
		if err != nil {
			return fmt.Errorf("parsing document: %w", err)
		}
		return compiled.Validate(instance)
	}, nil
}
