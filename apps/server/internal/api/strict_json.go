package api

// Exact-name checking for JSON request bodies. encoding/json binds an object
// key to a struct field case-insensitively, so {"Title":"x"} fills `title`
// and a case-variant pair passes a duplicate-key check. Request decoders walk
// the body against a closed shape first, with exact names, and report the
// first failing value by its path.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"
)

// maxJSONShapeDepth bounds object and array nesting, matching the request
// body depth limit.
const maxJSONShapeDepth = 100

// maxPathKeyBytes bounds each client-supplied key copied into a path.
const maxPathKeyBytes = 64

// JSONKind is the scalar form a JSONShape leaf accepts.
type JSONKind uint8

const (
	// JSONAny accepts any JSON value without inspecting it.
	JSONAny JSONKind = iota
	// JSONString accepts a JSON string.
	JSONString
	// JSONNumber accepts a JSON number.
	JSONNumber
	// JSONInteger accepts a JSON number that is a signed 64-bit integer.
	JSONInteger
	// JSONBoolean accepts true or false.
	JSONBoolean
)

// JSONShape is the closed form of one JSON value in a request body. A shape
// with Fields is an object with exactly those property names, one with Values
// is an object with free keys, one with Items is an array, and any other
// shape is a leaf of Kind.
type JSONShape struct {
	// Fields maps each exact property name of a closed object to its form.
	Fields map[string]*JSONShape
	// Required lists the properties a closed object must carry.
	Required []string
	// Values is the form of every value of an object with free keys.
	Values *JSONShape
	// Items is the form of every array element.
	Items *JSONShape
	// Kind is the scalar form of a leaf.
	Kind JSONKind
	// Nullable admits JSON null in place of the value.
	Nullable bool
}

// JSONPathError reports the first request body value that does not fit its
// shape. Its message names the value and never includes submitted values.
type JSONPathError struct {
	// Path is the dotted path to the value, with [i] for an array element;
	// it is empty for the whole body. Client keys are cut to 64 bytes.
	Path string
	// Problem is a fixed description of what is wrong with the value.
	Problem string
}

// Error returns a client-safe message naming the path.
func (e *JSONPathError) Error() string {
	if e.Path == "" {
		return "request body " + e.Problem
	}
	return "request body field " + strconv.Quote(e.Path) + " " + e.Problem
}

// Under returns the error with its path placed below parent, for a value a
// decoder checks separately from the body that holds it.
func (e *JSONPathError) Under(parent string) *JSONPathError {
	path := parent
	switch {
	case e.Path == "":
	case strings.HasPrefix(e.Path, "["):
		path += e.Path
	default:
		path += "." + e.Path
	}
	return &JSONPathError{Path: path, Problem: e.Problem}
}

// CheckJSONBinding checks that raw is one JSON value whose property names
// match target's JSON names exactly at every object level encoding/json would
// bind into a struct. target is the value a caller then decodes into.
func CheckJSONBinding(raw []byte, target any) error {
	return CheckJSONShape(raw, JSONShapeOf(reflect.TypeOf(target)))
}

// CheckJSONShape checks that raw is exactly one JSON value of shape: valid
// UTF-8, no repeated or undeclared property, no disallowed null, no missing
// required property, scalars of the declared kind, and no trailing data. A
// mismatch is a *JSONPathError; malformed JSON is any other error.
func CheckJSONShape(raw []byte, shape *JSONShape) error {
	if !utf8.Valid(raw) {
		return errors.New("api: request body is not valid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := walkJSON(dec, shape, "", 0); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("api: request body has trailing data")
	}
	return nil
}

func walkJSON(dec *json.Decoder, s *JSONShape, path string, depth int) error {
	if depth > maxJSONShapeDepth {
		return &JSONPathError{Path: path, Problem: "is nested too deeply"}
	}
	if s.Fields == nil && s.Values == nil && s.Items == nil && s.Kind == JSONAny {
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return err
		}
		if !s.Nullable && string(value) == "null" {
			return &JSONPathError{Path: path, Problem: "must not be null"}
		}
		return nil
	}
	token, err := dec.Token()
	if err != nil {
		return err
	}
	if token == nil {
		if s.Nullable {
			return nil
		}
		return &JSONPathError{Path: path, Problem: "must not be null"}
	}
	switch {
	case s.Fields != nil || s.Values != nil:
		if token != json.Delim('{') {
			return &JSONPathError{Path: path, Problem: "must be an object"}
		}
		return walkObject(dec, s, path, depth)
	case s.Items != nil:
		if token != json.Delim('[') {
			return &JSONPathError{Path: path, Problem: "must be an array"}
		}
		for index := 0; dec.More(); index++ {
			if err = walkJSON(dec, s.Items, path+"["+strconv.Itoa(index)+"]", depth+1); err != nil {
				return err
			}
		}
		_, err = dec.Token()
		return err
	default:
		return checkScalar(token, s.Kind, path)
	}
}

func walkObject(dec *json.Decoder, s *JSONShape, path string, depth int) error {
	seen := make(map[string]bool)
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return err
		}
		name, ok := token.(string)
		if !ok {
			return errors.New("api: object key is not a string")
		}
		child := s.Values
		if s.Fields != nil {
			child = s.Fields[name]
		}
		switch {
		case seen[name]:
			return &JSONPathError{Path: fieldPath(path, name), Problem: "is repeated"}
		case child == nil:
			return &JSONPathError{Path: fieldPath(path, name), Problem: undeclared(s.Fields, name)}
		}
		seen[name] = true
		if err = walkJSON(dec, child, fieldPath(path, name), depth+1); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	for _, name := range s.Required {
		if !seen[name] {
			return &JSONPathError{Path: fieldPath(path, name), Problem: "is required"}
		}
	}
	return nil
}

// undeclared names the declared property a case variant was meant to be.
func undeclared(fields map[string]*JSONShape, name string) string {
	match := ""
	for declared := range fields {
		if strings.EqualFold(declared, name) && (match == "" || declared < match) {
			match = declared
		}
	}
	if match == "" {
		return "is not declared"
	}
	return "is not declared (the declared name is " + strconv.Quote(match) + ")"
}

func checkScalar(token json.Token, kind JSONKind, path string) error {
	var ok bool
	var problem string
	switch kind {
	case JSONString:
		_, ok = token.(string)
		problem = "must be a string"
	case JSONBoolean:
		_, ok = token.(bool)
		problem = "must be true or false"
	case JSONNumber:
		_, ok = token.(json.Number)
		problem = "must be a number"
	case JSONInteger:
		number, isNumber := token.(json.Number)
		_, parseErr := strconv.ParseInt(string(number), 10, 64)
		ok = isNumber && parseErr == nil
		problem = "must be an integer"
	default:
		_, delim := token.(json.Delim)
		ok = !delim
		problem = "must be a single value"
	}
	if !ok {
		return &JSONPathError{Path: path, Problem: problem}
	}
	return nil
}

func fieldPath(parent, name string) string {
	if len(name) > maxPathKeyBytes {
		cut := maxPathKeyBytes
		for cut > 0 && !utf8.RuneStart(name[cut]) {
			cut--
		}
		name = name[:cut] + "..."
	}
	if parent == "" {
		return name
	}
	return parent + "." + name
}
