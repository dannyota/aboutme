package project

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

// Decode parses a closed owner request: a project document plus an optional
// slug. encoding/json matches field names case-insensitively, so every
// property name is first checked exactly against the request shape, with no
// duplicate keys and no trailing JSON. Null is accepted only for optional date
// ranges and date-range ends.
func Decode(raw []byte, target any) error {
	if !utf8.Valid(raw) {
		return ErrInvalid
	}
	tokens := json.NewDecoder(bytes.NewReader(raw))
	tokens.UseNumber()
	if err := consume(tokens, 0, requestShape()); err != nil {
		return ErrInvalid
	}
	if _, err := tokens.Token(); !errors.Is(err, io.EOF) {
		return ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return ErrInvalid
	}
	return nil
}

// shape is the closed JSON form of one value in a project request.
type shape struct {
	// fields holds the exact property names of a closed object.
	fields map[string]*shape
	// required lists the properties a closed object must carry.
	required []string
	// values is the value form of an object with free keys.
	values *shape
	// items is the element form of an array.
	items *shape
	// nullable admits JSON null.
	nullable bool
}

func requestShape() *shape {
	scalar := &shape{}
	yearMonth := func(nullable bool) *shape {
		return &shape{fields: map[string]*shape{"y": scalar, "m": scalar}, nullable: nullable}
	}
	text := &shape{fields: map[string]*shape{"name": scalar, "role": scalar, "summary": scalar, "outcome": scalar, "description": scalar}}
	return &shape{fields: map[string]*shape{
		"slug": scalar, "repositoryUrl": scalar, "demoUrl": scalar, "defaultLanguage": scalar,
		"dates": {
			fields:   map[string]*shape{"start": yearMonth(false), "end": yearMonth(true), "present": scalar},
			required: []string{"start", "end", "present"}, nullable: true,
		},
		"technologies": {items: scalar},
		"text":         {values: text},
	}}
}

func consume(d *json.Decoder, depth int, s *shape) error {
	if depth > 100 {
		return ErrInvalid
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	if token == nil {
		if s.nullable {
			return nil
		}
		return ErrInvalid
	}
	delim, ok := token.(json.Delim)
	if !ok {
		if s.fields != nil || s.values != nil || s.items != nil {
			return ErrInvalid
		}
		return nil
	}
	switch {
	case delim == '{' && (s.fields != nil || s.values != nil):
		seen := make(map[string]bool)
		for d.More() {
			token, err = d.Token()
			if err != nil {
				return err
			}
			name, ok := token.(string)
			if !ok || seen[name] {
				return ErrInvalid
			}
			seen[name] = true
			child := s.values
			if s.fields != nil {
				child = s.fields[name]
			}
			if child == nil {
				return ErrInvalid
			}
			if err = consume(d, depth+1, child); err != nil {
				return err
			}
		}
		for _, name := range s.required {
			if !seen[name] {
				return ErrInvalid
			}
		}
	case delim == '[' && s.items != nil:
		for d.More() {
			if err = consume(d, depth+1, s.items); err != nil {
				return err
			}
		}
	default:
		return ErrInvalid
	}
	_, err = d.Token()
	return err
}
