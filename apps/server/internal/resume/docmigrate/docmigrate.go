package docmigrate

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"

	schema "danny.vn/aboutme/packages/schema/gen/go"
)

const CurrentVersion int32 = int32(schema.CurrentVersion)

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

func AcceptedVersions() []int32 { return slices.Clone(acceptedVersions) }

func EmittedVersions() []int32 { return slices.Clone(emittedVersions) }

var (
	ErrUnsupportedVersion = errors.New("docmigrate: undeclared document version")

	ErrUnknownVersion = errors.New("docmigrate: unknown document version")

	ErrNoConverter = errors.New("docmigrate: no adjacent converter")

	ErrInvalidDocument = errors.New("docmigrate: document invalid for its schema version")

	ErrLossyConversion = errors.New("docmigrate: lossy wire conversion")
)

type ConvertFunc func(doc json.RawMessage) (json.RawMessage, error)

type AdjacentConverters struct {
	Up   ConvertFunc
	Down ConvertFunc
}

type ValidateFunc func(doc json.RawMessage) error

type Projector struct {
	pairs      map[int32]AdjacentConverters
	validators map[int32]ValidateFunc
	accepted   []int32
	emitted    []int32
	current    int32
	lossPolicy EmissionLossPolicy
}

type EmissionLossPolicy func(current, emitted, restored json.RawMessage, target int32) error

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

func cloneMap[V any](m map[int32]V) map[int32]V {
	out := make(map[int32]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func NewIdentityProjector() *Projector { return identityProjector }

var identityProjector = mustIdentityProjector()

func mustIdentityProjector() *Projector {
	p, err := newProjector(
		map[int32]AdjacentConverters{
			1: {Up: convertV1ToV2, Down: convertV2ToV1},
			2: {Up: convertV2ToV3, Down: convertV3ToV2},
			3: {Up: convertV3ToV4, Down: convertV4ToV3},
			4: {Up: convertV4ToV5, Down: convertV5ToV4},
			5: {Up: convertV5ToV6, Down: convertV6ToV5},
		},
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

func (p *Projector) CurrentVersion() int32 { return p.current }
