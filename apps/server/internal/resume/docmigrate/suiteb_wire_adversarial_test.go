// This pure suite covers `TestWireConverters_BothDirectionsFailClosed`:
// synthetic v1<->v2(<->v3) conversion in both directions, old-client
// preparation, down-emission, source/target validation, and every
// missing-path arm. The live-database rows live in the sibling package's
// docmigrate_adversarial_suiteb_test.go.
//
// Synthetic version family used throughout (the tests own it; production has
// exactly one released version, so a second one has to be fabricated):
//
//	vN  ==  {"schemaVersion": N, "personalDetails": {...}, "content": {...},
//	         "customization": {...}}
//	v1  has NO personalDetails.headline key.
//	vN (N>=2) has personalDetails.headline == "vN".
//
// so Up(N->N+1) stamps the headline and Down(N+1->N) removes or rewrites it --
// an adjacent pair that actually changes the document, not a relabelling.
package docmigrate_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
)

// suiteBHeadline is the marker synthetic version v (>= 2) carries at
// personalDetails.headline.
func suiteBHeadline(v int32) string { return fmt.Sprintf("v%d", v) }

// suiteBDecodeDoc decodes a whole synthetic document into a generic map.
func suiteBDecodeDoc(doc json.RawMessage) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(doc, &m); err != nil {
		return nil, fmt.Errorf("synthetic: decode document: %w", err)
	}
	return m, nil
}

// suiteBPersonalDetailsOf returns the document's personalDetails object.
func suiteBPersonalDetailsOf(m map[string]any) (map[string]any, error) {
	raw, ok := m["personalDetails"]
	if !ok {
		return nil, errors.New("synthetic: document has no personalDetails")
	}
	pd, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("synthetic: personalDetails is not an object")
	}
	return pd, nil
}

// suiteBValidatorFor builds a ValidateFunc for one synthetic version. calls,
// when non-nil, counts every invocation so a test can prove a validator was
// not run for the identity passthrough.
func suiteBValidatorFor(version int32, calls *atomic.Int64) docmigrate.ValidateFunc {
	return func(doc json.RawMessage) error {
		if calls != nil {
			calls.Add(1)
		}
		m, err := suiteBDecodeDoc(doc)
		if err != nil {
			return err
		}
		sv, ok := m["schemaVersion"].(float64)
		if !ok || int32(sv) != version {
			return fmt.Errorf("synthetic v%d: schemaVersion is %v, want %d", version, m["schemaVersion"], version)
		}
		for _, key := range []string{"personalDetails", "content", "customization"} {
			if _, present := m[key]; !present {
				return fmt.Errorf("synthetic v%d: missing %s", version, key)
			}
		}
		pd, err := suiteBPersonalDetailsOf(m)
		if err != nil {
			return err
		}
		headline, present := pd["headline"]
		switch {
		case version == 1 && present:
			return fmt.Errorf("synthetic v1: personalDetails.headline must be absent, got %v", headline)
		case version >= 2 && !present:
			return fmt.Errorf("synthetic v%d: personalDetails.headline is required", version)
		case version >= 2:
			got, isString := headline.(string)
			if !isString || got != suiteBHeadline(version) {
				return fmt.Errorf("synthetic v%d: personalDetails.headline is %v, want %q", version, headline, suiteBHeadline(version))
			}
		}
		return nil
	}
}

// suiteBUpTo returns the N-1 -> N converter for the synthetic family.
func suiteBUpTo(target int32) docmigrate.ConvertFunc {
	return func(doc json.RawMessage) (json.RawMessage, error) {
		m, err := suiteBDecodeDoc(doc)
		if err != nil {
			return nil, err
		}
		pd, err := suiteBPersonalDetailsOf(m)
		if err != nil {
			return nil, err
		}
		m["schemaVersion"] = float64(target)
		pd["headline"] = suiteBHeadline(target)
		return json.Marshal(m)
	}
}

// suiteBDownTo returns the N+1 -> N converter for the synthetic family.
func suiteBDownTo(target int32) docmigrate.ConvertFunc {
	return func(doc json.RawMessage) (json.RawMessage, error) {
		m, err := suiteBDecodeDoc(doc)
		if err != nil {
			return nil, err
		}
		pd, err := suiteBPersonalDetailsOf(m)
		if err != nil {
			return nil, err
		}
		m["schemaVersion"] = float64(target)
		if target == 1 {
			delete(pd, "headline")
		} else {
			pd["headline"] = suiteBHeadline(target)
		}
		return json.Marshal(m)
	}
}

// suiteBPair is the adjacent pair keyed by its lower version.
func suiteBPair(lower int32) docmigrate.AdjacentConverters {
	return docmigrate.AdjacentConverters{Up: suiteBUpTo(lower + 1), Down: suiteBDownTo(lower)}
}

// suiteBDocAt builds a valid synthetic document at version v.
func suiteBDocAt(t *testing.T, v int32) json.RawMessage {
	t.Helper()
	pd := map[string]any{"fullName": "Ada Lovelace", "details": []any{}}
	if v >= 2 {
		pd["headline"] = suiteBHeadline(v)
	}
	doc := map[string]any{
		"schemaVersion":   v,
		"personalDetails": pd,
		"content":         map[string]any{},
		"customization":   map[string]any{"pageFormat": "a4"},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal synthetic v%d document: %v", v, err)
	}
	return raw
}

// suiteBSplit decomposes a whole document into the three stored jsonb parts,
// dropping schemaVersion as the stored columns require.
func suiteBSplit(t *testing.T, doc json.RawMessage) (pd, c, cu json.RawMessage) {
	t.Helper()
	m, err := suiteBDecodeDoc(doc)
	if err != nil {
		t.Fatalf("split synthetic document: %v", err)
	}
	part := func(key string) json.RawMessage {
		raw, marshalErr := json.Marshal(m[key])
		if marshalErr != nil {
			t.Fatalf("marshal part %s: %v", key, marshalErr)
		}
		return raw
	}
	return part("personalDetails"), part("content"), part("customization")
}

// suiteBNormalize re-marshals JSON through a generic map so two documents that
// differ only in key order or whitespace compare equal.
func suiteBNormalize(t *testing.T, doc json.RawMessage) string {
	t.Helper()
	m, err := suiteBDecodeDoc(doc)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("normalize: re-marshal: %v", err)
	}
	return string(raw)
}

// suiteBValidators builds validators for versions 1..highest.
func suiteBValidators(highest int32, calls *atomic.Int64) map[int32]docmigrate.ValidateFunc {
	out := make(map[int32]docmigrate.ValidateFunc, highest)
	for v := int32(1); v <= highest; v++ {
		out[v] = suiteBValidatorFor(v, calls)
	}
	return out
}

// suiteBNewProjector builds a synthetic projector or fails the test.
func suiteBNewProjector(t *testing.T, pairs map[int32]docmigrate.AdjacentConverters,
	validators map[int32]docmigrate.ValidateFunc, accepted, emitted []int32, current int32,
) *docmigrate.Projector {
	t.Helper()
	p, err := docmigrate.NewProjector(pairs, validators, accepted, emitted, current)
	if err != nil {
		t.Fatalf("NewProjector(current=%d): unexpected error: %v", current, err)
	}
	return p
}

// TestSuiteB_NewProjector_FailsClosedOnIncoherentConfig covers every
// constructor arm: a pair missing Up or Down, a pair or
// declared version with no validator, an empty or duplicated declared set, a
// version below 1, or a current version that is not itself both accepted and
// emitted.
func TestSuiteB_NewProjector_FailsClosedOnIncoherentConfig(t *testing.T) {
	full := func() map[int32]docmigrate.AdjacentConverters {
		return map[int32]docmigrate.AdjacentConverters{1: suiteBPair(1)}
	}

	cases := []struct {
		name       string
		pairs      map[int32]docmigrate.AdjacentConverters
		validators map[int32]docmigrate.ValidateFunc
		accepted   []int32
		emitted    []int32
		current    int32
	}{
		{
			name:       "pair missing Up",
			pairs:      map[int32]docmigrate.AdjacentConverters{1: {Down: suiteBDownTo(1)}},
			validators: suiteBValidators(2, nil),
			accepted:   []int32{1, 2}, emitted: []int32{1, 2}, current: 2,
		},
		{
			name:       "pair missing Down",
			pairs:      map[int32]docmigrate.AdjacentConverters{1: {Up: suiteBUpTo(2)}},
			validators: suiteBValidators(2, nil),
			accepted:   []int32{1, 2}, emitted: []int32{1, 2}, current: 2,
		},
		{
			name:       "pair missing both",
			pairs:      map[int32]docmigrate.AdjacentConverters{1: {}},
			validators: suiteBValidators(2, nil),
			accepted:   []int32{1, 2}, emitted: []int32{1, 2}, current: 2,
		},
		{
			name:       "pair upper version has no validator",
			pairs:      full(),
			validators: suiteBValidators(1, nil),
			accepted:   []int32{1}, emitted: []int32{1}, current: 1,
		},
		{
			name:       "accepted version has no validator",
			pairs:      full(),
			validators: suiteBValidators(2, nil),
			accepted:   []int32{1, 2, 3}, emitted: []int32{1, 2}, current: 2,
		},
		{
			name:       "emitted version has no validator",
			pairs:      full(),
			validators: suiteBValidators(2, nil),
			accepted:   []int32{1, 2}, emitted: []int32{1, 2, 3}, current: 2,
		},
		{
			name:       "empty accepted set",
			pairs:      full(),
			validators: suiteBValidators(2, nil),
			accepted:   nil, emitted: []int32{1, 2}, current: 2,
		},
		{
			name:       "empty emitted set",
			pairs:      full(),
			validators: suiteBValidators(2, nil),
			accepted:   []int32{1, 2}, emitted: []int32{}, current: 2,
		},
		{
			name:       "duplicated accepted version",
			pairs:      full(),
			validators: suiteBValidators(2, nil),
			accepted:   []int32{1, 2, 2}, emitted: []int32{1, 2}, current: 2,
		},
		{
			name:       "duplicated emitted version",
			pairs:      full(),
			validators: suiteBValidators(2, nil),
			accepted:   []int32{1, 2}, emitted: []int32{2, 2}, current: 2,
		},
		{
			name:       "accepted version below 1",
			pairs:      full(),
			validators: suiteBValidators(2, nil),
			accepted:   []int32{0, 1, 2}, emitted: []int32{1, 2}, current: 2,
		},
		{
			name:       "emitted version below 1",
			pairs:      full(),
			validators: suiteBValidators(2, nil),
			accepted:   []int32{1, 2}, emitted: []int32{-1, 1, 2}, current: 2,
		},
		{
			name:       "pair keyed below 1",
			pairs:      map[int32]docmigrate.AdjacentConverters{0: suiteBPair(0)},
			validators: suiteBValidators(2, nil),
			accepted:   []int32{1, 2}, emitted: []int32{1, 2}, current: 2,
		},
		{
			name:       "current below 1",
			pairs:      full(),
			validators: suiteBValidators(2, nil),
			accepted:   []int32{1, 2}, emitted: []int32{1, 2}, current: 0,
		},
		{
			name:       "current not accepted",
			pairs:      full(),
			validators: suiteBValidators(2, nil),
			accepted:   []int32{1}, emitted: []int32{1, 2}, current: 2,
		},
		{
			name:       "current not emitted",
			pairs:      full(),
			validators: suiteBValidators(2, nil),
			accepted:   []int32{1, 2}, emitted: []int32{1}, current: 2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := docmigrate.NewProjector(tc.pairs, tc.validators, tc.accepted, tc.emitted, tc.current)
			if err == nil {
				t.Fatalf("NewProjector accepted an incoherent configuration (%s); want an error", tc.name)
			}
			if p != nil {
				t.Errorf("NewProjector returned a non-nil projector alongside its error: %+v", p)
			}
		})
	}
}

// TestSuiteB_NewProjector_CopiesConfiguration proves the documented "All
// fields are copied at construction, so a projector cannot be reconfigured
// after startup by mutating what was handed to NewProjector". A shared
// production projector that aliased its caller's maps would be a live
// reconfiguration hole.
func TestSuiteB_NewProjector_CopiesConfiguration(t *testing.T) {
	pairs := map[int32]docmigrate.AdjacentConverters{1: suiteBPair(1)}
	validators := suiteBValidators(2, nil)
	accepted := []int32{1, 2}
	emitted := []int32{1, 2}

	p := suiteBNewProjector(t, pairs, validators, accepted, emitted, 2)

	// Rip the configuration out from under the constructed projector.
	delete(pairs, 1)
	delete(validators, 1)
	delete(validators, 2)
	accepted[0] = 99
	emitted[0] = 99

	got, version, err := p.AcceptWire(suiteBDocAt(t, 1), 1)
	if err != nil {
		t.Fatalf("AcceptWire after mutating the constructor's inputs: %v", err)
	}
	if version != 2 {
		t.Errorf("AcceptWire returned version %d, want 2", version)
	}
	if err := suiteBValidatorFor(2, nil)(got); err != nil {
		t.Errorf("AcceptWire output is not a valid synthetic v2 document: %v", err)
	}
}

// TestSuiteB_Convert_WalksBothDirections proves each
// adjacent pair is exercised Up and Down, single-step and multi-step, and
// every step validates its source and its output.
func TestSuiteB_Convert_WalksBothDirections(t *testing.T) {
	var calls atomic.Int64
	p := suiteBNewProjector(t,
		map[int32]docmigrate.AdjacentConverters{1: suiteBPair(1), 2: suiteBPair(2)},
		suiteBValidators(3, &calls),
		[]int32{1, 2, 3}, []int32{1, 2, 3}, 2)

	cases := []struct {
		name     string
		from, to int32
	}{
		{"1 to 2", 1, 2},
		{"2 to 1", 2, 1},
		{"2 to 3", 2, 3},
		{"3 to 2", 3, 2},
		{"1 to 2 to 3", 1, 3},
		{"3 to 2 to 1", 3, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls.Store(0)
			source := suiteBDocAt(t, tc.from)
			got, err := p.Convert(source, tc.from, tc.to)
			if err != nil {
				t.Fatalf("Convert(%d -> %d): %v", tc.from, tc.to, err)
			}
			if invalid := suiteBValidatorFor(tc.to, nil)(got); invalid != nil {
				t.Fatalf("Convert(%d -> %d) output is not valid at v%d: %v", tc.from, tc.to, tc.to, invalid)
			}
			// Source plus one validation per step proves every step validates
			// both its source and its output. A walk that validated only
			// the endpoints would let a broken middle converter through.
			steps := tc.to - tc.from
			if steps < 0 {
				steps = -steps
			}
			if want := int64(steps) + 1; calls.Load() < want {
				t.Errorf("Convert(%d -> %d) ran %d validations, want at least %d (source plus each step's target)",
					tc.from, tc.to, calls.Load(), want)
			}
			// Round-tripping back must restore the original document.
			back, err := p.Convert(got, tc.to, tc.from)
			if err != nil {
				t.Fatalf("Convert back (%d -> %d): %v", tc.to, tc.from, err)
			}
			if suiteBNormalize(t, back) != suiteBNormalize(t, source) {
				t.Errorf("round trip %d -> %d -> %d lost data:\n got %s\nwant %s",
					tc.from, tc.to, tc.from, suiteBNormalize(t, back), suiteBNormalize(t, source))
			}
		})
	}
}

// TestSuiteB_Convert_IdentityIsBytewisePassthroughWithNoValidator pins the
// documented asymmetry: from == to returns the exact input bytes and runs NO
// validator, which keeps a projected read pure and cheap.
func TestSuiteB_Convert_IdentityIsBytewisePassthroughWithNoValidator(t *testing.T) {
	var calls atomic.Int64
	p := suiteBNewProjector(t,
		map[int32]docmigrate.AdjacentConverters{1: suiteBPair(1)},
		suiteBValidators(2, &calls),
		[]int32{1, 2}, []int32{1, 2}, 2)

	// Deliberately invalid at v2: if the identity path validated, this fails.
	junk := json.RawMessage(`{"schemaVersion":2,"not":"a document"}`)
	got, err := p.Convert(junk, 2, 2)
	if err != nil {
		t.Fatalf("Convert identity returned an error: %v", err)
	}
	if !bytes.Equal(got, junk) {
		t.Errorf("Convert identity returned %s, want the exact input bytes %s", got, junk)
	}
	if calls.Load() != 0 {
		t.Errorf("Convert identity ran %d validations, want 0", calls.Load())
	}
}

// TestSuiteB_Convert_FailsClosed covers every missing-path arm: unknown
// version, missing adjacent converter, invalid source, converter error,
// converter output that is not JSON, and converter output invalid for its
// target schema.
func TestSuiteB_Convert_FailsClosed(t *testing.T) {
	t.Run("unknown source version", func(t *testing.T) {
		p := suiteBNewProjector(t,
			map[int32]docmigrate.AdjacentConverters{1: suiteBPair(1)},
			suiteBValidators(2, nil), []int32{1, 2}, []int32{1, 2}, 2)
		if _, err := p.Convert(suiteBDocAt(t, 1), 9, 2); !errors.Is(err, docmigrate.ErrUnknownVersion) {
			t.Errorf("Convert from an unregistered version: got %v, want ErrUnknownVersion", err)
		}
	})

	t.Run("unknown target version", func(t *testing.T) {
		p := suiteBNewProjector(t,
			map[int32]docmigrate.AdjacentConverters{1: suiteBPair(1)},
			suiteBValidators(2, nil), []int32{1, 2}, []int32{1, 2}, 2)
		if _, err := p.Convert(suiteBDocAt(t, 1), 1, 9); !errors.Is(err, docmigrate.ErrUnknownVersion) {
			t.Errorf("Convert to an unregistered version: got %v, want ErrUnknownVersion", err)
		}
	})

	t.Run("no adjacent converter for a registered version", func(t *testing.T) {
		// Versions 1..3 all have schemas, but only the 1<->2 pair exists, so
		// the 2 -> 3 step has nothing to walk.
		p := suiteBNewProjector(t,
			map[int32]docmigrate.AdjacentConverters{1: suiteBPair(1)},
			suiteBValidators(3, nil), []int32{1, 2, 3}, []int32{1, 2, 3}, 2)
		if _, err := p.Convert(suiteBDocAt(t, 1), 1, 3); !errors.Is(err, docmigrate.ErrNoConverter) {
			t.Errorf("Convert across a converter gap upward: got %v, want ErrNoConverter", err)
		}
		if _, err := p.Convert(suiteBDocAt(t, 3), 3, 1); !errors.Is(err, docmigrate.ErrNoConverter) {
			t.Errorf("Convert across a converter gap downward: got %v, want ErrNoConverter", err)
		}
	})

	t.Run("invalid source document", func(t *testing.T) {
		p := suiteBNewProjector(t,
			map[int32]docmigrate.AdjacentConverters{1: suiteBPair(1)},
			suiteBValidators(2, nil), []int32{1, 2}, []int32{1, 2}, 2)
		// A v2-shaped document announced as v1: invalid at the source schema.
		if _, err := p.Convert(suiteBDocAt(t, 2), 1, 2); !errors.Is(err, docmigrate.ErrInvalidDocument) {
			t.Errorf("Convert with an invalid source: got %v, want ErrInvalidDocument", err)
		}
	})

	t.Run("source is not JSON at all", func(t *testing.T) {
		p := suiteBNewProjector(t,
			map[int32]docmigrate.AdjacentConverters{1: suiteBPair(1)},
			suiteBValidators(2, nil), []int32{1, 2}, []int32{1, 2}, 2)
		if _, err := p.Convert(json.RawMessage(`{"schemaVersion":1,`), 1, 2); err == nil {
			t.Error("Convert accepted a truncated JSON source; want an error")
		}
	})

	t.Run("converter returns an error", func(t *testing.T) {
		sentinel := errors.New("synthetic converter refused")
		pairs := map[int32]docmigrate.AdjacentConverters{1: {
			Up:   func(json.RawMessage) (json.RawMessage, error) { return nil, sentinel },
			Down: suiteBDownTo(1),
		}}
		p := suiteBNewProjector(t, pairs, suiteBValidators(2, nil), []int32{1, 2}, []int32{1, 2}, 2)
		_, err := p.Convert(suiteBDocAt(t, 1), 1, 2)
		if err == nil {
			t.Fatal("Convert swallowed a converter error; want it surfaced")
		}
		if !errors.Is(err, sentinel) && !strings.Contains(err.Error(), sentinel.Error()) {
			t.Errorf("Convert lost the converter's cause: got %v, want it to carry %v", err, sentinel)
		}
	})

	t.Run("converter emits invalid JSON", func(t *testing.T) {
		pairs := map[int32]docmigrate.AdjacentConverters{1: {
			Up:   func(json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`{oops`), nil },
			Down: suiteBDownTo(1),
		}}
		p := suiteBNewProjector(t, pairs, suiteBValidators(2, nil), []int32{1, 2}, []int32{1, 2}, 2)
		if _, err := p.Convert(suiteBDocAt(t, 1), 1, 2); err == nil {
			t.Error("Convert accepted converter output that is not valid JSON; want an error")
		}
	})

	t.Run("converter output invalid for its target schema", func(t *testing.T) {
		pairs := map[int32]docmigrate.AdjacentConverters{1: {
			// Bumps the version but forgets the v2 headline the target schema requires.
			Up: func(doc json.RawMessage) (json.RawMessage, error) {
				m, err := suiteBDecodeDoc(doc)
				if err != nil {
					return nil, err
				}
				m["schemaVersion"] = float64(2)
				return json.Marshal(m)
			},
			Down: suiteBDownTo(1),
		}}
		p := suiteBNewProjector(t, pairs, suiteBValidators(2, nil), []int32{1, 2}, []int32{1, 2}, 2)
		if _, err := p.Convert(suiteBDocAt(t, 1), 1, 2); !errors.Is(err, docmigrate.ErrInvalidDocument) {
			t.Errorf("Convert with a lossy converter: got %v, want ErrInvalidDocument", err)
		}
	})
}

// TestSuiteB_AcceptWire_And_EmitWire_FailClosed is the transport-agnostic
// boundary an HTTP layer consumes: old-client preparation, down-emission, and
// the declaration gate. "What the server accepts is a declaration, not a
// capability" -- an undeclared version must fail even when the chain could
// convert it.
func TestSuiteB_AcceptWire_And_EmitWire_FailClosed(t *testing.T) {
	// Versions 1..3 are all convertible; only 1 and 2 are declared.
	newProjector := func(t *testing.T, calls *atomic.Int64) *docmigrate.Projector {
		t.Helper()
		return suiteBNewProjector(t,
			map[int32]docmigrate.AdjacentConverters{1: suiteBPair(1), 2: suiteBPair(2)},
			suiteBValidators(3, calls),
			[]int32{1, 2}, []int32{1, 2}, 2)
	}

	t.Run("accepts a declared old version and returns the current one", func(t *testing.T) {
		p := newProjector(t, nil)
		got, version, err := p.AcceptWire(suiteBDocAt(t, 1), 1)
		if err != nil {
			t.Fatalf("AcceptWire(v1): %v", err)
		}
		if version != 2 {
			t.Errorf("AcceptWire returned version %d, want the current version 2", version)
		}
		if err := suiteBValidatorFor(2, nil)(got); err != nil {
			t.Errorf("AcceptWire output is not target-validated v2: %v", err)
		}
	})

	t.Run("undeclared accepted version fails even though it is convertible", func(t *testing.T) {
		p := newProjector(t, nil)
		_, _, err := p.AcceptWire(suiteBDocAt(t, 3), 3)
		if !errors.Is(err, docmigrate.ErrUnsupportedVersion) {
			t.Errorf("AcceptWire(v3, undeclared but convertible): got %v, want ErrUnsupportedVersion", err)
		}
	})

	t.Run("unknown accepted version fails closed", func(t *testing.T) {
		p := newProjector(t, nil)
		_, _, err := p.AcceptWire(suiteBDocAt(t, 1), 9)
		if err == nil {
			t.Fatal("AcceptWire accepted a version with no schema at all; want an error")
		}
		if !errors.Is(err, docmigrate.ErrUnsupportedVersion) && !errors.Is(err, docmigrate.ErrUnknownVersion) {
			t.Errorf("AcceptWire(v9): got %v, want ErrUnsupportedVersion or ErrUnknownVersion", err)
		}
	})

	t.Run("invalid wire document fails closed", func(t *testing.T) {
		p := newProjector(t, nil)
		_, _, err := p.AcceptWire(suiteBDocAt(t, 2), 1)
		if !errors.Is(err, docmigrate.ErrInvalidDocument) {
			t.Errorf("AcceptWire with a document invalid at its declared version: got %v, want ErrInvalidDocument", err)
		}
	})

	t.Run("validates unconditionally at the current version", func(t *testing.T) {
		// Unlike Convert's identity passthrough, the wire boundary validates
		// even when no conversion is needed: those bytes came from a client.
		var calls atomic.Int64
		p := newProjector(t, &calls)
		junk := json.RawMessage(`{"schemaVersion":2,"not":"a document"}`)
		if _, _, err := p.AcceptWire(junk, 2); err == nil {
			t.Error("AcceptWire passed an invalid current-version document straight through; want an error")
		}
		if calls.Load() == 0 {
			t.Error("AcceptWire ran no validator at the current version; the wire boundary must validate unconditionally")
		}
		calls.Store(0)
		if _, err := p.EmitWire(junk, 2); err == nil {
			t.Error("EmitWire passed an invalid current-version document straight through; want an error")
		}
		if calls.Load() == 0 {
			t.Error("EmitWire ran no validator at the current version; the wire boundary must validate unconditionally")
		}
	})

	t.Run("emits a declared old version", func(t *testing.T) {
		p := newProjector(t, nil)
		got, err := p.EmitWire(suiteBDocAt(t, 2), 1)
		if err != nil {
			t.Fatalf("EmitWire(v1): %v", err)
		}
		if err := suiteBValidatorFor(1, nil)(got); err != nil {
			t.Errorf("EmitWire output is not validated against immutable v1: %v", err)
		}
	})

	t.Run("undeclared emitted version fails even though it is convertible", func(t *testing.T) {
		p := newProjector(t, nil)
		if _, err := p.EmitWire(suiteBDocAt(t, 2), 3); !errors.Is(err, docmigrate.ErrUnsupportedVersion) {
			t.Errorf("EmitWire(v3, undeclared but convertible): got %v, want ErrUnsupportedVersion", err)
		}
	})

	t.Run("emitting a document that is not at the current version fails closed", func(t *testing.T) {
		p := newProjector(t, nil)
		if _, err := p.EmitWire(suiteBDocAt(t, 1), 1); !errors.Is(err, docmigrate.ErrInvalidDocument) {
			t.Errorf("EmitWire with a non-current source: got %v, want ErrInvalidDocument", err)
		}
	})
}

// TestSuiteB_EmitWire_LossyDownConversionFailsClosed pins the contract's own
// example: a Down converter that drops data the target version requires
// surfaces as an error rather than as a quietly truncated document handed to
// an old client.
func TestSuiteB_EmitWire_LossyDownConversionFailsClosed(t *testing.T) {
	requireFullName := func(version int32) docmigrate.ValidateFunc {
		base := suiteBValidatorFor(version, nil)
		return func(doc json.RawMessage) error {
			if err := base(doc); err != nil {
				return err
			}
			m, err := suiteBDecodeDoc(doc)
			if err != nil {
				return err
			}
			pd, err := suiteBPersonalDetailsOf(m)
			if err != nil {
				return err
			}
			if _, ok := pd["fullName"]; !ok {
				return fmt.Errorf("synthetic v%d: personalDetails.fullName is required", version)
			}
			return nil
		}
	}
	lossyDown := func(doc json.RawMessage) (json.RawMessage, error) {
		lifted, err := suiteBDownTo(1)(doc)
		if err != nil {
			return nil, err
		}
		m, err := suiteBDecodeDoc(lifted)
		if err != nil {
			return nil, err
		}
		pd, err := suiteBPersonalDetailsOf(m)
		if err != nil {
			return nil, err
		}
		delete(pd, "fullName")
		return json.Marshal(m)
	}

	p := suiteBNewProjector(t,
		map[int32]docmigrate.AdjacentConverters{1: {Up: suiteBUpTo(2), Down: lossyDown}},
		map[int32]docmigrate.ValidateFunc{1: requireFullName(1), 2: suiteBValidatorFor(2, nil)},
		[]int32{1, 2}, []int32{1, 2}, 2)

	if _, err := p.EmitWire(suiteBDocAt(t, 2), 1); !errors.Is(err, docmigrate.ErrInvalidDocument) {
		t.Errorf("EmitWire with a lossy Down converter: got %v, want ErrInvalidDocument", err)
	}
}

// TestSuiteB_WireRoundTripPreservesV1Fields proves round-trip preservation of
// all v1 fields: an old client's document, accepted at v1 and
// emitted back at v1, comes back unchanged.
func TestSuiteB_WireRoundTripPreservesV1Fields(t *testing.T) {
	p := suiteBNewProjector(t,
		map[int32]docmigrate.AdjacentConverters{1: suiteBPair(1)},
		suiteBValidators(2, nil), []int32{1, 2}, []int32{1, 2}, 2)

	original := suiteBDocAt(t, 1)
	current, version, err := p.AcceptWire(original, 1)
	if err != nil {
		t.Fatalf("AcceptWire: %v", err)
	}
	if version != 2 {
		t.Fatalf("AcceptWire returned version %d, want 2", version)
	}
	back, err := p.EmitWire(current, 1)
	if err != nil {
		t.Fatalf("EmitWire: %v", err)
	}
	if suiteBNormalize(t, back) != suiteBNormalize(t, original) {
		t.Errorf("v1 -> current -> v1 lost or changed data:\n got %s\nwant %s",
			suiteBNormalize(t, back), suiteBNormalize(t, original))
	}
}

// TestSuiteB_DeclaredVersionSlicesAreCopies pins "Callers receive copies so
// they cannot mutate the production declaration" for the two package-level
// declarations. With exactly one released version, both sets are
// {CurrentVersion}.
func TestSuiteB_DeclaredVersionSlicesAreCopies(t *testing.T) {
	for _, tc := range []struct {
		name string
		get  func() []int32
	}{
		{"AcceptedVersions", docmigrate.AcceptedVersions},
		{"EmittedVersions", docmigrate.EmittedVersions},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := tc.get()
			if len(first) == 0 {
				t.Fatalf("%s() is empty; the server must declare at least the current version", tc.name)
			}
			want := []int32{docmigrate.CurrentVersion}
			if len(first) != len(want) || first[0] != want[0] {
				t.Errorf("%s() = %v, want %v (one released version, D19)", tc.name, first, want)
			}
			ascending := true
			for i := 1; i < len(first); i++ {
				if first[i] <= first[i-1] {
					ascending = false
				}
			}
			if !ascending {
				t.Errorf("%s() = %v, want strictly ascending", tc.name, first)
			}
			for i := range first {
				first[i] = 9999
			}
			second := tc.get()
			for i := range second {
				if second[i] == 9999 {
					t.Fatalf("%s() handed out an alias of the production declaration: %v", tc.name, second)
				}
			}
		})
	}
}

// TestSuiteB_IdentityProjector_ProductionDeclarations pins the production
// configuration: no adjacent pairs, every stored row already current, projection a
// pure passthrough, and any other stored version fails closed.
func TestSuiteB_IdentityProjector_ProductionDeclarations(t *testing.T) {
	p := docmigrate.NewIdentityProjector()
	if got := p.CurrentVersion(); got != docmigrate.CurrentVersion {
		t.Errorf("NewIdentityProjector().CurrentVersion() = %d, want %d", got, docmigrate.CurrentVersion)
	}

	doc := suiteBDocAt(t, docmigrate.CurrentVersion)
	pd, c, cu := suiteBSplit(t, doc)
	gotPD, gotC, gotCU, err := p.Project(pd, c, cu, docmigrate.CurrentVersion)
	if err != nil {
		t.Fatalf("Project at the current version: %v", err)
	}
	for _, part := range []struct {
		name     string
		got, was json.RawMessage
	}{
		{"personalDetails", gotPD, pd},
		{"content", gotC, c},
		{"customization", gotCU, cu},
	} {
		if !bytes.Equal(part.got, part.was) {
			t.Errorf("Project passthrough changed %s:\n got %s\nwant %s", part.name, part.got, part.was)
		}
	}

	if _, _, _, err := p.Project(pd, c, cu, docmigrate.CurrentVersion+1); err == nil {
		t.Error("Project accepted a stored version this build has no schema for; want a closed failure")
	}
	if _, _, err := p.AcceptWire(doc, docmigrate.CurrentVersion+1); err == nil {
		t.Error("AcceptWire accepted an undeclared version; want ErrUnsupportedVersion")
	}
	if _, err := p.EmitWire(doc, docmigrate.CurrentVersion+1); err == nil {
		t.Error("EmitWire accepted an undeclared version; want ErrUnsupportedVersion")
	}
}

// TestSuiteB_Project_IsPureAndDeterministic pins purity at the
// function level: same input, same output, every time, with no mutation of the
// caller's byte slices and no schemaVersion key leaking into the parts.
func TestSuiteB_Project_IsPureAndDeterministic(t *testing.T) {
	var calls atomic.Int64
	p := suiteBNewProjector(t,
		map[int32]docmigrate.AdjacentConverters{1: suiteBPair(1)},
		suiteBValidators(2, &calls),
		[]int32{1, 2}, []int32{1, 2}, 1)

	// A row stored at synthetic v2 under a current-v1 projector: the
	// schema_version >= 1 CHECK constraint forces synthetic old versions to
	// sit above the current one, so "old" here means "numerically higher".
	stored := suiteBDocAt(t, 2)
	pd, c, cu := suiteBSplit(t, stored)
	pdCopy := append(json.RawMessage(nil), pd...)
	cCopy := append(json.RawMessage(nil), c...)
	cuCopy := append(json.RawMessage(nil), cu...)

	firstPD, firstC, firstCU, err := p.Project(pd, c, cu, 2)
	if err != nil {
		t.Fatalf("Project(storedVersion=2, current=1): %v", err)
	}
	secondPD, secondC, secondCU, err := p.Project(pd, c, cu, 2)
	if err != nil {
		t.Fatalf("Project (second call): %v", err)
	}
	if !bytes.Equal(firstPD, secondPD) || !bytes.Equal(firstC, secondC) || !bytes.Equal(firstCU, secondCU) {
		t.Error("Project is not deterministic: two calls on identical input produced different parts")
	}
	if !bytes.Equal(pd, pdCopy) || !bytes.Equal(c, cCopy) || !bytes.Equal(cu, cuCopy) {
		t.Error("Project mutated its caller's input slices")
	}

	// The projection actually happened: v2's headline marker is gone.
	var projected map[string]any
	if err := json.Unmarshal(firstPD, &projected); err != nil {
		t.Fatalf("decode projected personalDetails: %v", err)
	}
	if _, present := projected["headline"]; present {
		t.Errorf("Project did not run the Down converter: personalDetails still carries the v2 headline: %s", firstPD)
	}
	for _, part := range []struct {
		name string
		raw  json.RawMessage
	}{{"personalDetails", firstPD}, {"content", firstC}, {"customization", firstCU}} {
		var m map[string]any
		if err := json.Unmarshal(part.raw, &m); err != nil {
			continue // content/customization need not be objects for this check
		}
		if _, present := m["schemaVersion"]; present {
			t.Errorf("Project leaked schemaVersion into the %s part (D4 forbids it): %s", part.name, part.raw)
		}
	}
}

// TestSuiteB_Project_FailsClosedOnUnknownStoredVersion is the projection half
// of the fail-closed rule: a stored version with no converter path must error,
// never yield a silently un-projected document.
func TestSuiteB_Project_FailsClosedOnUnknownStoredVersion(t *testing.T) {
	p := suiteBNewProjector(t,
		map[int32]docmigrate.AdjacentConverters{1: suiteBPair(1)},
		suiteBValidators(2, nil), []int32{1, 2}, []int32{1, 2}, 1)

	stored := suiteBDocAt(t, 2)
	pd, c, cu := suiteBSplit(t, stored)

	t.Run("no schema for the stored version", func(t *testing.T) {
		gotPD, gotC, gotCU, err := p.Project(pd, c, cu, 9)
		if err == nil {
			t.Fatalf("Project(storedVersion=9) returned parts with no error: %s / %s / %s", gotPD, gotC, gotCU)
		}
		if !errors.Is(err, docmigrate.ErrUnknownVersion) && !errors.Is(err, docmigrate.ErrNoConverter) {
			t.Errorf("Project(storedVersion=9): got %v, want ErrUnknownVersion or ErrNoConverter", err)
		}
		if gotPD != nil || gotC != nil || gotCU != nil {
			t.Errorf("Project returned parts alongside its error: %s / %s / %s", gotPD, gotC, gotCU)
		}
	})

	t.Run("schema exists but no converter path", func(t *testing.T) {
		// Valid at its own stored version, so the walk gets past source
		// validation and fails on the missing 2 <-> 3 pair, not on the doc.
		pd3, c3, cu3 := suiteBSplit(t, suiteBDocAt(t, 3))
		gapped := suiteBNewProjector(t,
			map[int32]docmigrate.AdjacentConverters{1: suiteBPair(1)},
			suiteBValidators(3, nil), []int32{1, 2, 3}, []int32{1, 2, 3}, 1)
		if _, _, _, err := gapped.Project(pd3, c3, cu3, 3); !errors.Is(err, docmigrate.ErrNoConverter) {
			t.Errorf("Project across a converter gap: got %v, want ErrNoConverter", err)
		}
	})

	t.Run("stored version below 1", func(t *testing.T) {
		if _, _, _, err := p.Project(pd, c, cu, 0); err == nil {
			t.Error("Project accepted storedVersion 0, which the schema_version CHECK forbids; want an error")
		}
	})
}
