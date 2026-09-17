package docmigrate

// Compiling the released JSON schemas into ValidateFuncs, once at package
// init.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/santhosh-tekuri/jsonschema/v6"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

// releasedValidators compiles one ValidateFunc per released schema version,
// exactly once, at package init -- never lazily and never per call. A
// compilation failure is a hard startup failure: a server that cannot
// validate a released document shape must not start.
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
		// schema_version is an int32 column; a released version outside that
		// range could never be stored, so it is a build-time impossibility
		// rather than a runtime condition to tolerate.
		if version < 1 || version > math.MaxInt32 {
			panic(fmt.Sprintf("docmigrate: released schema version %d is out of range for schema_version", version))
		}
		out[int32(version)] = validate
	}
	return out
}

// newSchemaValidator compiles raw into a ValidateFunc with format assertion
// enabled (matching ajv's configuration in packages/schema) and an empty
// scheme map for the URL loader. Resolving any external $ref -- network or
// filesystem -- can
// never succeed. The schema is registered under its own $id, so its internal
// $refs resolve exactly as they do in packages/schema.
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
