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
// Deliberate asymmetry between the read path and the wire boundary: an
// identity conversion (from == to) is a byte-for-byte passthrough that runs
// no validator, so projecting a row already at the current version never
// turns a read into a validation pass. internal/resume validates documents on
// writes. AcceptWire and EmitWire validate unconditionally, including at the
// current version, because there the bytes come from, or go to, a client.
//
// Every write must persist the full document through internal/resume's codec.
// A granular jsonb_set-style write could restore an old-shape part after a
// backfill and violate the row's declared schema version.
package docmigrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"

	"github.com/santhosh-tekuri/jsonschema/v6"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

// CurrentVersion is the schema version every stored resume is projected to
// on read and used for writes. It moves only when another document version
// is released in packages/schema. Releases are append-only and require an
// adjacent Up/Down pair plus a deliberate change to the declared sets below.
const CurrentVersion int32 = 1

// acceptedVersions and emittedVersions are the production declaration: the
// document versions this server accepts from clients, and the versions it
// will emit back. They are distinct sets on purpose -- a release can start
// accepting a new version before it emits it, or keep accepting an old one
// after it stops emitting it -- and they are written out explicitly rather
// than derived from the released registry, because "released" is a fact
// about packages/schema and "accepted"/"emitted" are decisions about this
// server. With one released version both are {1}.
var (
	acceptedVersions = []int32{CurrentVersion}
	emittedVersions  = []int32{CurrentVersion}
)

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
}

// NewProjector builds a Projector and fails closed on any incoherent
// configuration: a pair missing Up or Down, a pair or declared version with
// no validator, an empty or duplicated declared set, a version below 1, or a
// current version that is not itself both accepted and emitted.
func NewProjector(pairs map[int32]AdjacentConverters, validators map[int32]ValidateFunc,
	accepted, emitted []int32, current int32,
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

// NewIdentityProjector returns the production projector: the released
// validators, the accepted/emitted declaration, and no
// adjacent pairs, because exactly one version is released. Every
// stored row is already at CurrentVersion, so Project is a pure passthrough.
// The returned projector is immutable and shared.
func NewIdentityProjector() *Projector { return identityProjector }

var identityProjector = mustIdentityProjector()

func mustIdentityProjector() *Projector {
	p, err := NewProjector(nil, releasedValidators, acceptedVersions, emittedVersions, CurrentVersion)
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

// Convert walks the registered adjacent pairs from `from` to `to` in either
// direction. It validates the source against `from`'s schema and each
// converter's output against that step's target schema, and fails closed on
// an unknown version, a missing direction, a converter error, output that is
// not valid JSON, or output invalid for its target schema.
//
// from == to is the identity: the exact input bytes come back, with no
// validator run. That keeps a projected read pure and cheap;
// the wire boundary validates separately and unconditionally.
//
// Convert is NOT gated on the accepted/emitted declarations -- those gate
// the wire boundary, not internal conversion.
func (p *Projector) Convert(doc json.RawMessage, from, to int32) (json.RawMessage, error) {
	return p.convert(doc, from, to, true)
}

// convert is Convert's core. validateSource is false only for callers that
// have already validated the source themselves (AcceptWire/EmitWire), so the
// same document is never validated twice.
func (p *Projector) convert(doc json.RawMessage, from, to int32, validateSource bool) (json.RawMessage, error) {
	if _, err := p.validatorFor(from); err != nil {
		return nil, err
	}
	if _, err := p.validatorFor(to); err != nil {
		return nil, err
	}
	if from == to {
		return doc, nil
	}
	if validateSource {
		if err := p.validate(from, doc); err != nil {
			return nil, fmt.Errorf("docmigrate: source document at version %d: %w", from, err)
		}
	}

	step := int32(1)
	if to < from {
		step = -1
	}
	current := doc
	for version := from; version != to; version += step {
		next := version + step
		convert, err := p.converterFor(version, next)
		if err != nil {
			return nil, err
		}
		out, err := convert(current)
		if err != nil {
			return nil, fmt.Errorf("docmigrate: converting %d->%d: %w", version, next, err)
		}
		if !json.Valid(out) {
			return nil, fmt.Errorf("docmigrate: converting %d->%d: converter produced invalid JSON", version, next)
		}
		if err := p.validate(next, out); err != nil {
			return nil, fmt.Errorf("docmigrate: converted document at version %d: %w", next, err)
		}
		current = out
	}
	return current, nil
}

// converterFor returns the single-step converter from -> to, which must be
// one adjacent step apart.
func (p *Projector) converterFor(from, to int32) (ConvertFunc, error) {
	switch to {
	case from + 1:
		if pair, ok := p.pairs[from]; ok {
			return pair.Up, nil
		}
	case from - 1:
		if pair, ok := p.pairs[to]; ok {
			return pair.Down, nil
		}
	default:
		return nil, fmt.Errorf("docmigrate: %d->%d is not an adjacent step", from, to)
	}
	return nil, fmt.Errorf("%w: %d->%d", ErrNoConverter, from, to)
}

func (p *Projector) validatorFor(version int32) (ValidateFunc, error) {
	validate, ok := p.validators[version]
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnknownVersion, version)
	}
	return validate, nil
}

func (p *Projector) validate(version int32, doc json.RawMessage) error {
	validate, err := p.validatorFor(version)
	if err != nil {
		return err
	}
	if err := validate(doc); err != nil {
		return fmt.Errorf("%w: version %d: %w", ErrInvalidDocument, version, err)
	}
	return nil
}

// AcceptWire prepares a document arriving in a declared accepted version for
// the current canonical shape: it validates the input against that version's
// immutable schema, converts it up or down to CurrentVersion, and validates
// every intermediate and the final result. It returns the canonical document
// and the version it is now in.
//
// An undeclared version fails closed with ErrUnsupportedVersion even when
// the converter chain could handle it: what the server accepts is a
// declaration, not a capability.
func (p *Projector) AcceptWire(doc json.RawMessage, version int32) (json.RawMessage, int32, error) {
	if !slices.Contains(p.accepted, version) {
		return nil, 0, fmt.Errorf("%w: %d is not accepted (accepted: %v)", ErrUnsupportedVersion, version, p.accepted)
	}
	if err := p.validate(version, doc); err != nil {
		return nil, 0, fmt.Errorf("docmigrate: accepting a version %d document: %w", version, err)
	}
	out, err := p.convert(doc, version, p.current, false)
	if err != nil {
		return nil, 0, err
	}
	return out, p.current, nil
}

// EmitWire converts a current-version document to a declared emitted
// version, validating the source at CurrentVersion and the result against
// the target version's immutable schema. A Down converter that drops data
// the target version requires therefore surfaces as an error rather than as
// a quietly truncated document handed to an old client.
func (p *Projector) EmitWire(doc json.RawMessage, version int32) (json.RawMessage, error) {
	if !slices.Contains(p.emitted, version) {
		return nil, fmt.Errorf("%w: %d is not emitted (emitted: %v)", ErrUnsupportedVersion, version, p.emitted)
	}
	if err := p.validate(p.current, doc); err != nil {
		return nil, fmt.Errorf("docmigrate: emitting a version %d document: %w", version, err)
	}
	return p.convert(doc, p.current, version, false)
}

// Project lifts personalDetails/content/customization -- exactly the three
// jsonb parts read back from the resumes table's own columns -- plus the
// row's own schema_version, forward to this projector's current version,
// returning the three current-version parts (still undecoded
// json.RawMessage; the caller's strict decode happens afterwards).
//
// It is pure: it never touches the database, and calling it twice on
// the same input always produces the same output. A row already at the
// current version short-circuits to a byte-for-byte passthrough. A row at a
// version this build has no schema for fails closed -- never a silent
// passthrough of content that cannot be migrated.
func (p *Projector) Project(personalDetails, content, customization json.RawMessage, storedVersion int32) (pd, c, cu json.RawMessage, err error) {
	if _, lookupErr := p.validatorFor(storedVersion); lookupErr != nil {
		return nil, nil, nil, lookupErr
	}
	if storedVersion == p.current {
		return personalDetails, content, customization, nil
	}

	doc, err := assembleDocument(personalDetails, content, customization, storedVersion)
	if err != nil {
		return nil, nil, nil, err
	}
	converted, err := p.convert(doc, storedVersion, p.current, true)
	if err != nil {
		return nil, nil, nil, err
	}
	return splitDocument(converted, p.current)
}

// documentKeys are the only top-level keys a canonical document has: the
// three stored jsonb parts plus the schema version. The set is fixed
// across versions because it is the storage decomposition itself, not a
// property of any one document shape.
var documentKeys = []string{"schemaVersion", "personalDetails", "content", "customization"}

// assembleDocument builds the full canonical document from the three stored
// parts plus the row's schema_version, without decoding any part: the parts
// are spliced in as raw JSON, so no value is ever reformatted on the way in.
func assembleDocument(personalDetails, content, customization json.RawMessage, version int32) (json.RawMessage, error) {
	parts := map[string]json.RawMessage{
		"personalDetails": personalDetails,
		"content":         content,
		"customization":   customization,
	}
	for name, raw := range parts {
		if len(raw) == 0 {
			return nil, fmt.Errorf("docmigrate: stored %s is empty", name)
		}
		if !json.Valid(raw) {
			return nil, fmt.Errorf("docmigrate: stored %s is not valid JSON", name)
		}
	}
	parts["schemaVersion"] = json.RawMessage(strconv.FormatInt(int64(version), 10))

	out, err := json.Marshal(parts)
	if err != nil {
		return nil, fmt.Errorf("docmigrate: assembling stored document: %w", err)
	}
	return out, nil
}

// splitDocument decomposes a converted document back into the three stored
// parts. It fails closed unless the document has exactly the four
// canonical top-level keys and its own schemaVersion equals wantVersion: a
// converter that invents a fifth top-level key has produced something this
// storage layout cannot hold, and one that leaves schemaVersion behind has
// produced a document that lies about itself.
func splitDocument(doc json.RawMessage, wantVersion int32) (pd, c, cu json.RawMessage, err error) {
	var fields map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(doc))
	if err := dec.Decode(&fields); err != nil {
		return nil, nil, nil, fmt.Errorf("docmigrate: splitting converted document: %w", err)
	}
	if dec.More() {
		return nil, nil, nil, errors.New("docmigrate: splitting converted document: unexpected trailing data after JSON value")
	}

	for _, key := range documentKeys {
		if _, ok := fields[key]; !ok {
			return nil, nil, nil, fmt.Errorf("docmigrate: converted document has no %q", key)
		}
	}
	if len(fields) != len(documentKeys) {
		extra := make([]string, 0, len(fields))
		for key := range fields {
			if !slices.Contains(documentKeys, key) {
				extra = append(extra, key)
			}
		}
		slices.Sort(extra)
		return nil, nil, nil, fmt.Errorf("docmigrate: converted document has unexpected top-level key(s) %v", extra)
	}

	var version int32
	if err := json.Unmarshal(fields["schemaVersion"], &version); err != nil {
		return nil, nil, nil, fmt.Errorf("docmigrate: converted document has an unreadable schemaVersion: %w", err)
	}
	if version != wantVersion {
		return nil, nil, nil, fmt.Errorf("docmigrate: converted document claims schemaVersion %d, want %d", version, wantVersion)
	}

	return fields["personalDetails"], fields["content"], fields["customization"], nil
}

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
