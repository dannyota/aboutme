package docmigrate_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
)

func suiteBHeadline(v int32) string { return fmt.Sprintf("v%d", v) }

func suiteBDecodeDoc(doc json.RawMessage) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(doc, &m); err != nil {
		return nil, fmt.Errorf("synthetic: decode document: %w", err)
	}
	return m, nil
}

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

func suiteBPair(lower int32) docmigrate.AdjacentConverters {
	return docmigrate.AdjacentConverters{Up: suiteBUpTo(lower + 1), Down: suiteBDownTo(lower)}
}

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

func suiteBValidators(highest int32, calls *atomic.Int64) map[int32]docmigrate.ValidateFunc {
	out := make(map[int32]docmigrate.ValidateFunc, highest)
	for v := int32(1); v <= highest; v++ {
		out[v] = suiteBValidatorFor(v, calls)
	}
	return out
}

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

func TestSuiteB_NewProjector_CopiesConfiguration(t *testing.T) {
	pairs := map[int32]docmigrate.AdjacentConverters{1: suiteBPair(1)}
	validators := suiteBValidators(2, nil)
	accepted := []int32{1, 2}
	emitted := []int32{1, 2}

	p := suiteBNewProjector(t, pairs, validators, accepted, emitted, 2)

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
			steps := tc.to - tc.from
			if steps < 0 {
				steps = -steps
			}
			if want := int64(steps) + 1; calls.Load() < want {
				t.Errorf("Convert(%d -> %d) ran %d validations, want at least %d (source plus each step's target)",
					tc.from, tc.to, calls.Load(), want)
			}
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

func TestSuiteB_Convert_IdentityValidatesAndPreservesBytes(t *testing.T) {
	var calls atomic.Int64
	p := suiteBNewProjector(t,
		map[int32]docmigrate.AdjacentConverters{1: suiteBPair(1)},
		suiteBValidators(2, &calls),
		[]int32{1, 2}, []int32{1, 2}, 2)

	valid := suiteBDocAt(t, 2)
	got, err := p.Convert(valid, 2, 2)
	if err != nil {
		t.Fatalf("Convert identity returned an error: %v", err)
	}
	if !bytes.Equal(got, valid) {
		t.Errorf("Convert identity returned %s, want the exact input bytes %s", got, valid)
	}
	if calls.Load() != 1 {
		t.Errorf("Convert identity ran %d validations, want 1", calls.Load())
	}

	junk := json.RawMessage(`{"schemaVersion":2,"not":"a document"}`)
	if _, err := p.Convert(junk, 2, 2); !errors.Is(err, docmigrate.ErrInvalidDocument) {
		t.Errorf("Convert identity of invalid source returned %v, want ErrInvalidDocument", err)
	}
}

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

func TestSuiteB_AcceptWire_And_EmitWire_FailClosed(t *testing.T) {
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

func TestSuiteB_WireDeclarationsAreIndependent(t *testing.T) {
	p := suiteBNewProjector(t,
		map[int32]docmigrate.AdjacentConverters{1: suiteBPair(1)},
		suiteBValidators(2, nil),
		[]int32{1, 2}, []int32{2}, 2)

	v1 := suiteBDocAt(t, 1)
	current, version, err := p.AcceptWire(v1, 1)
	if err != nil {
		t.Fatalf("AcceptWire rejected v1 from the accepted-only set: %v", err)
	}
	if version != 2 {
		t.Fatalf("AcceptWire returned version %d, want 2", version)
	}
	if _, err := p.EmitWire(current, 1); !errors.Is(err, docmigrate.ErrUnsupportedVersion) {
		t.Errorf("EmitWire accepted v1 outside the emitted set: got %v, want ErrUnsupportedVersion", err)
	}
	if _, err := p.EmitWire(current, 2); err != nil {
		t.Errorf("EmitWire rejected current v2 from the emitted set: %v", err)
	}
}

func TestSuiteB_EmitWire_LossyDownConversionFailsClosed(t *testing.T) {
	const sentinelKey = "legacyOptionalSentinel"
	old := suiteBDocAt(t, 1)
	oldMap, err := suiteBDecodeDoc(old)
	if err != nil {
		t.Fatalf("decode old-version fixture: %v", err)
	}
	oldPD, err := suiteBPersonalDetailsOf(oldMap)
	if err != nil {
		t.Fatalf("old-version personalDetails: %v", err)
	}
	oldPD[sentinelKey] = "must survive"
	old, err = json.Marshal(oldMap)
	if err != nil {
		t.Fatalf("marshal old-version fixture with sentinel: %v", err)
	}
	preparing := suiteBNewProjector(t,
		map[int32]docmigrate.AdjacentConverters{1: suiteBPair(1)},
		suiteBValidators(2, nil), []int32{1, 2}, []int32{1, 2}, 2)
	current, version, err := preparing.AcceptWire(old, 1)
	if err != nil {
		t.Fatalf("AcceptWire(old v1 with sentinel): %v", err)
	}
	if version != 2 {
		t.Fatalf("AcceptWire returned version %d, want 2", version)
	}
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, current, "", "  "); err != nil {
		t.Fatalf("format current fixture: %v", err)
	}
	current = formatted.Bytes()

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
		delete(pd, sentinelKey)
		return json.Marshal(m)
	}

	t.Run("preserving converter keeps every old-version field", func(t *testing.T) {
		p := suiteBNewProjector(t,
			map[int32]docmigrate.AdjacentConverters{1: suiteBPair(1)},
			suiteBValidators(2, nil), []int32{1, 2}, []int32{1, 2}, 2)
		emitted, err := p.EmitWire(current, 1)
		if err != nil {
			t.Fatalf("EmitWire with preserving Down converter: %v", err)
		}
		emittedMap, err := suiteBDecodeDoc(emitted)
		if err != nil {
			t.Fatalf("decode emitted v1: %v", err)
		}
		emittedPD, err := suiteBPersonalDetailsOf(emittedMap)
		if err != nil {
			t.Fatalf("emitted personalDetails: %v", err)
		}
		if got := emittedPD[sentinelKey]; got != "must survive" {
			t.Errorf("emitted optional sentinel = %v, want %q", got, "must survive")
		}
		if suiteBNormalize(t, emitted) != suiteBNormalize(t, old) {
			t.Errorf("preserving emission changed the old-version document:\n got %s\nwant %s",
				suiteBNormalize(t, emitted), suiteBNormalize(t, old))
		}
		restored, _, err := p.AcceptWire(emitted, 1)
		if err != nil {
			t.Fatalf("AcceptWire of preserving emission: %v", err)
		}
		if suiteBNormalize(t, restored) != suiteBNormalize(t, current) {
			t.Errorf("preserving wire round trip changed the current document:\n got %s\nwant %s",
				suiteBNormalize(t, restored), suiteBNormalize(t, current))
		}
	})

	t.Run("schema-valid sentinel loss fails closed", func(t *testing.T) {
		p := suiteBNewProjector(t,
			map[int32]docmigrate.AdjacentConverters{1: {Up: suiteBUpTo(2), Down: lossyDown}},
			suiteBValidators(2, nil), []int32{1, 2}, []int32{1, 2}, 2)

		emitted, err := p.EmitWire(current, 1)
		if !errors.Is(err, docmigrate.ErrLossyConversion) {
			t.Fatalf("EmitWire error = %v, want ErrLossyConversion; emitted %s", err, emitted)
		}
	})
}

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
			want := []int32{1, 2, 3, 4, 5, 6}
			if !slices.Equal(first, want) {
				t.Errorf("%s() = %v, want %v", tc.name, first, want)
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

func TestSuiteB_IdentityProjector_ProductionDeclarations(t *testing.T) {
	p := docmigrate.NewIdentityProjector()
	if got := p.CurrentVersion(); got != docmigrate.CurrentVersion {
		t.Errorf("NewIdentityProjector().CurrentVersion() = %d, want %d", got, docmigrate.CurrentVersion)
	}

	doc := fullCurrentDoc(t)
	pd, c, cu := splitDoc(t, doc)
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

func TestSuiteB_Project_IsPureAndDeterministic(t *testing.T) {
	var calls atomic.Int64
	p := suiteBNewProjector(t,
		map[int32]docmigrate.AdjacentConverters{1: suiteBPair(1)},
		suiteBValidators(2, &calls),
		[]int32{1, 2}, []int32{1, 2}, 1)

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
			continue
		}
		if _, present := m["schemaVersion"]; present {
			t.Errorf("Project leaked schemaVersion into the %s part: %s", part.name, part.raw)
		}
	}
}

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
