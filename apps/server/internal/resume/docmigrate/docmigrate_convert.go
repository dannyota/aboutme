package docmigrate

// Projector's conversion engine: walking the registered adjacent converters
// between two document versions, and the wire boundary (AcceptWire,
// EmitWire) built on it, including EmitWire's semantic round-trip check.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"slices"
)

// Convert walks the registered adjacent pairs from `from` to `to` in either
// direction. It validates the source against `from`'s schema and each
// converter's output against that step's target schema, and fails closed on
// an unknown version, a missing direction, a converter error, output that is
// not valid JSON, or output invalid for its target schema.
//
// from == to validates the source and returns its exact bytes. Project keeps
// current-version reads byte-stable through its own short circuit rather than
// weakening this public conversion interface.
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
	if validateSource {
		if err := p.validate(from, doc); err != nil {
			return nil, fmt.Errorf("docmigrate: source document at version %d: %w", from, err)
		}
	}
	if from == to {
		return doc, nil
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
// the target version's immutable schema. It then converts that result back
// to CurrentVersion and compares JSON values semantically. This catches a
// schema-valid Down converter that drops optional data, without requiring
// converters to preserve whitespace or object-key order.
func (p *Projector) EmitWire(doc json.RawMessage, version int32) (json.RawMessage, error) {
	if !slices.Contains(p.emitted, version) {
		return nil, fmt.Errorf("%w: %d is not emitted (emitted: %v)", ErrUnsupportedVersion, version, p.emitted)
	}
	if err := p.validate(p.current, doc); err != nil {
		return nil, fmt.Errorf("docmigrate: emitting a version %d document: %w", version, err)
	}
	emitted, err := p.convert(doc, p.current, version, false)
	if err != nil {
		return nil, err
	}
	if version == p.current {
		return emitted, nil
	}
	restored, err := p.convert(emitted, version, p.current, false)
	if err != nil {
		return nil, fmt.Errorf("%w: restoring emitted version %d: %w", ErrLossyConversion, version, err)
	}
	equal, err := jsonSemanticallyEqual(doc, restored)
	if err != nil {
		return nil, fmt.Errorf("%w: comparing restored version %d: %w", ErrLossyConversion, version, err)
	}
	if !equal {
		if p.lossPolicy == nil {
			return nil, fmt.Errorf("%w: current -> %d -> current changed the document", ErrLossyConversion, version)
		}
		if err := p.lossPolicy(doc, emitted, restored, version); err != nil {
			return nil, fmt.Errorf("%w: current -> %d -> current: %w", ErrLossyConversion, version, err)
		}
	}
	return emitted, nil
}

// jsonSemanticallyEqual compares JSON values without treating whitespace,
// object-key order, or string escaping as changes. Arrays remain ordered.
// JSON numbers retain their exact decimal value through UseNumber and are
// compared as arbitrary-precision rationals, so equivalent spellings match
// without rounding distinct values through float64.
func jsonSemanticallyEqual(left, right json.RawMessage) (bool, error) {
	decode := func(raw json.RawMessage) (any, error) {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var value any
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		var trailing any
		if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
			if err == nil {
				return nil, errors.New("multiple JSON values")
			}
			return nil, err
		}
		return value, nil
	}

	leftValue, err := decode(left)
	if err != nil {
		return false, fmt.Errorf("decoding current document: %w", err)
	}
	rightValue, err := decode(right)
	if err != nil {
		return false, fmt.Errorf("decoding restored document: %w", err)
	}
	return jsonValuesEqual(leftValue, rightValue)
}

func jsonValuesEqual(left, right any) (bool, error) {
	switch left := left.(type) {
	case nil:
		return right == nil, nil
	case bool:
		right, ok := right.(bool)
		return ok && left == right, nil
	case string:
		right, ok := right.(string)
		return ok && left == right, nil
	case json.Number:
		right, ok := right.(json.Number)
		if !ok {
			return false, nil
		}
		leftRat, ok := new(big.Rat).SetString(left.String())
		if !ok {
			return false, errors.New("cannot compare left JSON number exactly")
		}
		rightRat, ok := new(big.Rat).SetString(right.String())
		if !ok {
			return false, errors.New("cannot compare right JSON number exactly")
		}
		return leftRat.Cmp(rightRat) == 0, nil
	case []any:
		right, ok := right.([]any)
		if !ok || len(left) != len(right) {
			return false, nil
		}
		for i := range left {
			equal, err := jsonValuesEqual(left[i], right[i])
			if err != nil || !equal {
				return equal, err
			}
		}
		return true, nil
	case map[string]any:
		right, ok := right.(map[string]any)
		if !ok || len(left) != len(right) {
			return false, nil
		}
		for key, leftValue := range left {
			rightValue, ok := right[key]
			if !ok {
				return false, nil
			}
			equal, err := jsonValuesEqual(leftValue, rightValue)
			if err != nil || !equal {
				return equal, err
			}
		}
		return true, nil
	default:
		return false, fmt.Errorf("unsupported decoded JSON value %T", left)
	}
}
