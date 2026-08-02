// Package docmigrate is the doc-shape migration machinery for stored
// resumes: it projects a row's three jsonb parts, plus its own
// schema_version, forward to the current document version on read (D18),
// and (Task 8) drives the CAS backfill that persists that projection so
// storage itself eventually catches up. Task 6 wires only the identity
// case this package needs on day one -- every row is already at
// CurrentVersion, so Project is a pure decode, never a real conversion --
// leaving AcceptedVersions/EmittedVersion, the ConvertFunc chain, and
// BackfillOne's persistence path for Task 8 to add once a second document
// version actually exists.
//
// D12(ii) binding for P2B: every write path must persist the FULL document
// through internal/resume's codec (AssembleCanonical/EncodeParts) on every
// save -- never a granular jsonb_set-style PATCH. A granular patch would
// let old-shape content re-enter storage through a column the backfill
// CAS (WHERE schema_version=$old AND revision=$observed) never re-checks,
// silently undoing a completed backfill. This sentence is P2B's
// binding-in-writing condition from D12; it is recorded here, verbatim in
// spirit, because this package is what a violation would corrupt.
package docmigrate

import (
	"encoding/json"
	"fmt"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

// CurrentVersion is the schema version every stored resume is projected to
// on read (D18) and persisted at on write (D19). It moves only when a
// second document version is introduced -- there is no version 2 to serve
// yet (D19), so Task 6 pins it at 1.
const CurrentVersion int32 = 1

// Projector projects a stored resume's three jsonb parts, plus the row's
// own schema_version, forward to CurrentVersion. Task 6 wires only the
// identity case: every stored row is already at CurrentVersion, so
// Project never actually converts anything. Task 8 replaces the empty
// struct with a real convs map + current version pair and adds
// NewProjector to construct it.
type Projector struct{}

// NewIdentityProjector returns the Projector aboutme runs today: every
// stored row is already at CurrentVersion, so Project is a pure decode,
// never a conversion. Task 8's NewProjector(convs, current) supersedes
// this once a second version exists; production wiring switches to it
// then, not before.
func NewIdentityProjector() *Projector {
	return &Projector{}
}

// Project decodes personalDetails/content/customization -- exactly the
// three jsonb parts a caller reads back from the resumes table's own
// columns -- into one schema.Resume, with SchemaVersion set to
// storedVersion. It is PURE (D18): it never touches the database, and
// calling it twice on the same input always produces the same output.
//
// Task 6's identity projector only ever accepts storedVersion ==
// CurrentVersion; a stored version below CurrentVersion has no converter
// registered yet, so this fails closed with an error rather than silently
// passing through content it cannot actually migrate (D19's "fail closed,
// never a silent passthrough" reasoning, applied here ahead of there
// being a second version to fail on). Task 8's converter chain replaces
// this check with real per-version conversion.
func (p *Projector) Project(personalDetails, content, customization json.RawMessage, storedVersion int32) (schema.Resume, error) {
	if storedVersion != CurrentVersion {
		return schema.Resume{}, fmt.Errorf(
			"docmigrate: no converter registered for stored schema_version %d (identity projector only handles %d)",
			storedVersion, CurrentVersion)
	}

	var doc schema.Resume
	doc.SchemaVersion = int64(storedVersion)

	if err := json.Unmarshal(personalDetails, &doc.PersonalDetails); err != nil {
		return schema.Resume{}, fmt.Errorf("docmigrate: decoding personalDetails: %w", err)
	}

	var contentMap map[string]schema.Section
	if err := json.Unmarshal(content, &contentMap); err != nil {
		return schema.Resume{}, fmt.Errorf("docmigrate: decoding content: %w", err)
	}
	doc.Content = contentMap

	if err := json.Unmarshal(customization, &doc.Customization); err != nil {
		return schema.Resume{}, fmt.Errorf("docmigrate: decoding customization: %w", err)
	}

	return doc, nil
}
