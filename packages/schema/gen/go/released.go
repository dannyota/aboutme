// Code generated from released-versions.json. DO NOT EDIT.

package schema

import (
	"bytes"
	"errors"
	"fmt"

	schemav1 "github.com/dannyota/aboutme/packages/schema/gen/go/v1"
)

// CurrentVersion is the document-shape version resume.schema.json currently
// describes, and the version every stored resume is projected to on read and
// persisted at on write. It matches apps/server's docmigrate.CurrentVersion
// by construction: both move only when a new version is released here.
const CurrentVersion = 1

// ReleasedSchema is one released document-shape version: its immutable schema
// file and the retained generated types derived from that file. Released
// entries are append-only (design spec §3, "Wire-version compatibility"), so
// a value of this type describes a contract that can never change, only be
// superseded.
type ReleasedSchema struct {
	// Version is the released schema_version this entry describes.
	Version int
	// Schema is the immutable schema file's path, relative to packages/schema.
	Schema string
	// GoPackage is the retained Go types' path, relative to packages/schema.
	GoPackage string
	// TSTypes is the retained TypeScript types' path, relative to
	// packages/schema.
	TSTypes string
	// RawSchema is the schema file's exact byte content.
	RawSchema []byte
}

// ErrUnknownSchemaVersion is returned for a version this build has no
// released schema for. Callers must treat it as a hard failure: a document
// claiming an unreleased version cannot be validated, converted, or emitted,
// and guessing a nearby version would persist a document under a contract
// nothing describes.
var ErrUnknownSchemaVersion = errors.New("schema: unknown released schema version")

// releasedSchemas is generated from released-versions.json, ascending by
// version.
var releasedSchemas = []ReleasedSchema{
	{
		Version:   1,
		Schema:    "resume.v1.schema.json",
		GoPackage: "gen/go/v1",
		TSTypes:   "gen/ts/v1/resume.ts",
		RawSchema: schemav1.RawSchema,
	},
}

// ReleasedVersions returns every released document-shape version in
// ascending order. The returned slice is freshly allocated, so a caller
// cannot reorder or truncate the registry for everyone else.
func ReleasedVersions() []int {
	versions := make([]int, len(releasedSchemas))
	for i, released := range releasedSchemas {
		versions[i] = released.Version
	}
	return versions
}

// ReleasedSchemaFor returns the released schema for version. It fails closed:
// an unreleased version yields an error wrapping ErrUnknownSchemaVersion and
// a zero ReleasedSchema, never a fallback to the current or nearest version.
// The returned RawSchema is a copy, so a caller cannot mutate the immutable
// bytes the retained package holds.
func ReleasedSchemaFor(version int) (ReleasedSchema, error) {
	for _, released := range releasedSchemas {
		if released.Version == version {
			released.RawSchema = bytes.Clone(released.RawSchema)
			return released, nil
		}
	}
	return ReleasedSchema{}, fmt.Errorf("%w: %d", ErrUnknownSchemaVersion, version)
}
