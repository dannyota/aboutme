// Package docmigrate converts stored and wire resume documents between
// released schema versions. See docs/design/data.md and
// docs/adr/0017-resume-document-versioning.md.
//
//   - Project lifts a row's three jsonb parts, plus the row's own
//     schema_version, to the current document version without a database
//     write.
//   - Convert walks the registered adjacent-version converters in either
//     direction over a whole document, validating the source and every
//     target against that version's immutable schema.
//   - AcceptWire and EmitWire are the transport-agnostic wire boundary: the
//     server declares, as two distinct sets, which document versions it
//     accepts from clients and which it emits back.
//
// A converter is func(json.RawMessage) (json.RawMessage, error) over the
// full assembled document, never a typed decode. Typed structs only
// describe the current version, so a converter lifting a v1 document cannot
// decode it into the current Go type at all -- the type it would decode into
// does not describe v1's shape. Project therefore assembles the three parts
// into one document, runs the chain over those bytes, and re-splits the
// result back into three parts. Typed decode never happens inside this
// package: the one strict, DisallowUnknownFields decode
// (resume.DecodeParts) happens exactly once, at the boundary, in
// resume.Store's projectRow, after Project has already lifted the parts.
//
// Convert validates every source, including identity conversions. Project
// owns the deliberate read-path exception: a row already at the current
// version takes Project's byte-for-byte short circuit without a schema
// validation pass. internal/resume validates documents on writes. AcceptWire
// and EmitWire also validate unconditionally because there the bytes come
// from, or go to, a client.
//
// Every write must persist the full document through internal/resume's codec.
// A granular jsonb_set-style write could restore an old-shape part after a
// backfill and violate the row's declared schema version.
package docmigrate

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

// CurrentVersion is generated from the reviewed schema release manifest.
const CurrentVersion int32 = int32(schema.CurrentVersion)

// acceptedVersions and emittedVersions consume the independently authored
// declarations generated from released-versions.json.
var (
	acceptedVersions = int32Versions(schema.AcceptedVersions())
	emittedVersions  = int32Versions(schema.EmittedVersions())
)

func int32Versions(versions []int) []int32 {
	out := make([]int32, len(versions))
	for i, version := range versions {
		if version < 1 || version > math.MaxInt32 {
			panic(fmt.Sprintf("docmigrate: generated schema version %d is out of int32 range", version))
		}
		out[i] = int32(version)
	}
	return out
}

// AcceptedVersions returns the document versions this server accepts from
// clients, ascending. The returned slice is a copy, so a caller cannot
// rewrite the production declaration for everyone else.
func AcceptedVersions() []int32 { return slices.Clone(acceptedVersions) }

// EmittedVersions returns the document versions this server will emit,
// ascending. The returned slice is a copy.
func EmittedVersions() []int32 { return slices.Clone(emittedVersions) }

// Fail-closed failure modes are exported so an HTTP boundary can distinguish
// a client wire error from a missing converter or unknown stored version.
var (
	// ErrUnsupportedVersion means the wire version is not in this
	// projector's declared accepted (AcceptWire) or emitted (EmitWire) set.
	// The machinery may well be able to convert it; the declaration says it
	// must not.
	ErrUnsupportedVersion = errors.New("docmigrate: undeclared document version")

	// ErrUnknownVersion means no schema is registered for a version, so
	// nothing can be validated, converted, or emitted at it. Guessing a
	// nearby version would persist or serve a document under a contract
	// nothing describes.
	ErrUnknownVersion = errors.New("docmigrate: unknown document version")

	// ErrNoConverter means a step of the requested walk has no registered
	// adjacent converter in the required direction.
	ErrNoConverter = errors.New("docmigrate: no adjacent converter")

	// ErrInvalidDocument means a document failed the schema of the version
	// it claims to be -- as a conversion source, as a conversion output, or
	// at the wire boundary.
	ErrInvalidDocument = errors.New("docmigrate: document invalid for its schema version")

	// ErrLossyConversion means wire emission produced a schema-valid older
	// document that could not be converted back to the exact same current
	// document. Emission fails unless the production projector's immutable
	// policy permits that exact declared loss.
	ErrLossyConversion = errors.New("docmigrate: lossy wire conversion")
)

// ConvertFunc converts one FULL canonical document by exactly one version.
// It receives and returns whole-document bytes and is responsible for
// setting the document's own schemaVersion to its target -- the target
// schema's `const` catches a converter that forgets.
type ConvertFunc func(doc json.RawMessage) (json.RawMessage, error)

// AdjacentConverters is keyed by its LOWER version N and supplies N -> N+1
// (Up) and N+1 -> N (Down). Both functions are mandatory for every
// registered pair: a version that can be read but not written back, or
// vice versa, is a one-way door.
type AdjacentConverters struct {
	Up   ConvertFunc
	Down ConvertFunc
}

// ValidateFunc validates one released-version document against that
// version's immutable schema. Production validators are compiled once, at
// package init, from the released registry in packages/schema.
type ValidateFunc func(doc json.RawMessage) error

// Projector holds one immutable conversion configuration: the adjacent
// converter pairs, the per-version validators, the declared accepted and
// emitted sets, and the version everything is projected to. All fields are
// copied at construction, so a projector cannot be reconfigured after
// startup by mutating what was handed to NewProjector.
type Projector struct {
	pairs      map[int32]AdjacentConverters
	validators map[int32]ValidateFunc
	accepted   []int32
	emitted    []int32
	current    int32
	lossPolicy EmissionLossPolicy
}

// EmissionLossPolicy validates one declared lossy wire emission. Generic
// projectors have no policy and therefore require exact round trips.
type EmissionLossPolicy func(current, emitted, restored json.RawMessage, target int32) error

// NewProjector builds a Projector and fails closed on any incoherent
// configuration: a pair missing Up or Down, a pair or declared version with
// no validator, an empty or duplicated declared set, a version below 1, or a
// current version that is not itself both accepted and emitted.
func NewProjector(pairs map[int32]AdjacentConverters, validators map[int32]ValidateFunc,
	accepted, emitted []int32, current int32,
) (*Projector, error) {
	return newProjector(pairs, validators, accepted, emitted, current, nil)
}

func newProjector(pairs map[int32]AdjacentConverters, validators map[int32]ValidateFunc,
	accepted, emitted []int32, current int32, lossPolicy EmissionLossPolicy,
) (*Projector, error) {
	if len(validators) == 0 {
		return nil, errors.New("docmigrate: no schema validator registered")
	}
	for version, validate := range validators {
		if version < 1 {
			return nil, fmt.Errorf("docmigrate: validator registered for version %d: versions start at 1", version)
		}
		if validate == nil {
			return nil, fmt.Errorf("docmigrate: validator for version %d is nil", version)
		}
	}

	for lower, pair := range pairs {
		if lower < 1 {
			return nil, fmt.Errorf("docmigrate: adjacent pair registered for version %d: versions start at 1", lower)
		}
		if pair.Up == nil {
			return nil, fmt.Errorf("docmigrate: adjacent pair %d<->%d has no Up converter", lower, lower+1)
		}
		if pair.Down == nil {
			return nil, fmt.Errorf("docmigrate: adjacent pair %d<->%d has no Down converter", lower, lower+1)
		}
		for _, version := range []int32{lower, lower + 1} {
			if _, ok := validators[version]; !ok {
				return nil, fmt.Errorf("%w: adjacent pair %d<->%d needs a validator for version %d",
					ErrUnknownVersion, lower, lower+1, version)
			}
		}
	}

	acceptedCopy, err := checkDeclared("accepted", accepted, validators)
	if err != nil {
		return nil, err
	}
	emittedCopy, err := checkDeclared("emitted", emitted, validators)
	if err != nil {
		return nil, err
	}

	if _, ok := validators[current]; !ok {
		return nil, fmt.Errorf("%w: current version %d has no validator", ErrUnknownVersion, current)
	}
	if !slices.Contains(acceptedCopy, current) {
		return nil, fmt.Errorf("docmigrate: current version %d is not in the accepted set %v", current, acceptedCopy)
	}
	if !slices.Contains(emittedCopy, current) {
		return nil, fmt.Errorf("docmigrate: current version %d is not in the emitted set %v", current, emittedCopy)
	}

	return &Projector{
		pairs:      cloneMap(pairs),
		validators: cloneMap(validators),
		accepted:   acceptedCopy,
		emitted:    emittedCopy,
		current:    current,
		lossPolicy: lossPolicy,
	}, nil
}

// checkDeclared validates one declared set and returns a sorted copy.
func checkDeclared(name string, versions []int32, validators map[int32]ValidateFunc) ([]int32, error) {
	if len(versions) == 0 {
		return nil, fmt.Errorf("docmigrate: the %s version set is empty", name)
	}
	out := slices.Clone(versions)
	slices.Sort(out)
	for i, version := range out {
		if version < 1 {
			return nil, fmt.Errorf("docmigrate: %s version %d: versions start at 1", name, version)
		}
		if i > 0 && out[i-1] == version {
			return nil, fmt.Errorf("docmigrate: duplicate %s version %d", name, version)
		}
		if _, ok := validators[version]; !ok {
			return nil, fmt.Errorf("%w: %s version %d has no validator", ErrUnknownVersion, name, version)
		}
	}
	return out, nil
}

// cloneMap returns a shallow copy of m, so the constructor's caller cannot
// change a projector's behavior afterwards by mutating what it passed.
func cloneMap[V any](m map[int32]V) map[int32]V {
	out := make(map[int32]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// NewIdentityProjector returns the immutable production projector. The name is
// retained for API compatibility; released adjacent converters now lift v1 to
// current v2 and emit v2 through the declared v1 font fallback policy.
// The returned projector is immutable and shared.
func NewIdentityProjector() *Projector { return identityProjector }

var identityProjector = mustIdentityProjector()

func mustIdentityProjector() *Projector {
	p, err := newProjector(
		map[int32]AdjacentConverters{1: {Up: convertV1ToV2, Down: convertV2ToV1}},
		releasedValidators,
		acceptedVersions,
		emittedVersions,
		CurrentVersion,
		productionEmissionLossPolicy,
	)
	if err != nil {
		panic("docmigrate: building the production projector: " + err.Error())
	}
	return p
}

// CurrentVersion reports the version this projector projects stored
// documents to and treats as canonical. resume.Store decodes projected parts
// at exactly this version rather than at the package constant: the decode
// version must be the version the parts were projected TO, or the two could
// silently disagree.
func (p *Projector) CurrentVersion() int32 { return p.current }
