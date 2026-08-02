// Package docmigrate is the doc-shape migration machinery for stored
// resumes: it projects a row's three jsonb parts, plus its own
// schema_version, forward to the current document version on read (D18),
// and (Task 8) drives the CAS backfill that persists that projection so
// storage itself eventually catches up. Task 6 wires only the identity
// case this package needs on day one -- every row is already at
// CurrentVersion, so Project is a pure passthrough, never a real
// conversion -- leaving AcceptedVersions/EmittedVersion, the ConvertFunc
// chain, and BackfillOne's persistence path for Task 8 to add once a
// second document version actually exists.
//
// Project returns the three jsonb PARTS, not a typed schema.Resume (owner
// ruling, fix round 1): this matches D13, which defines a converter as
// func(json.RawMessage) (json.RawMessage, error) over the FULL assembled
// document precisely because typed structs only exist for the CURRENT
// version -- a converter lifting a v1 document cannot decode it into the
// current Go type at all, since the type it would decode into doesn't
// describe v1's shape. Task 8's real Project assembles the parts into one
// full document, runs the ConvertFunc chain over that json.RawMessage,
// then re-splits the result back into three parts (D4's own
// decomposition) -- typed decode never happens inside this package. This
// also means docmigrate has no dependency on internal/resume's codec: the
// one strict, DisallowUnknownFields decode (resume.DecodeParts) happens
// exactly once, at the boundary, in resume.Store's own projectRow, after
// Project has already lifted the parts to CurrentVersion.
//
// D12(ii) binding for P2B: every write path must persist the FULL document
// through internal/resume's codec (AssembleCanonical/the package-private
// encodeParts) on every save -- never a granular jsonb_set-style PATCH. A
// granular patch would let old-shape content re-enter storage through a
// column the backfill CAS (WHERE schema_version=$old AND
// revision=$observed) never re-checks, silently undoing a completed
// backfill. This sentence is P2B's binding-in-writing condition from D12;
// it is recorded here, verbatim in spirit, because this package is what a
// violation would corrupt.
package docmigrate

import (
	"encoding/json"
	"fmt"
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
// stored row is already at CurrentVersion, so Project is a pure
// passthrough, never a conversion. Task 8's NewProjector(convs, current)
// supersedes this once a second version exists; production wiring
// switches to it then, not before.
func NewIdentityProjector() *Projector {
	return &Projector{}
}

// Project lifts personalDetails/content/customization -- exactly the
// three jsonb parts a caller reads back from the resumes table's own
// columns -- forward to CurrentVersion, returning the three CurrentVersion
// parts (still undecoded json.RawMessage; the caller's own strict decode
// happens afterward). It is PURE (D18): it never touches the database,
// and calling it twice on the same input always produces the same output.
//
// Task 6's identity projector only ever accepts storedVersion ==
// CurrentVersion, returning the input parts unchanged; a stored version
// below CurrentVersion has no converter registered yet, so this fails
// closed with an error rather than silently passing through content it
// cannot actually migrate (D19's "fail closed, never a silent passthrough"
// reasoning, applied here ahead of there being a second version to fail
// on). Task 8's converter chain replaces this check with real
// per-version conversion over the full assembled document.
func (p *Projector) Project(personalDetails, content, customization json.RawMessage, storedVersion int32) (pd, c, cu json.RawMessage, err error) {
	if storedVersion != CurrentVersion {
		return nil, nil, nil, fmt.Errorf(
			"docmigrate: no converter registered for stored schema_version %d (identity projector only handles %d)",
			storedVersion, CurrentVersion)
	}
	return personalDetails, content, customization, nil
}
