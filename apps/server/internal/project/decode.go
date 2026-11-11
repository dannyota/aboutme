package project

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

// Decode parses a closed document without duplicate keys or trailing JSON.
// Null is accepted only for optional date ranges and date-range ends.
func Decode(raw []byte, target any) error {
	if !utf8.Valid(raw) {
		return ErrInvalid
	}
	tokens := json.NewDecoder(bytes.NewReader(raw))
	tokens.UseNumber()
	if err := consume(tokens, 0, ""); err != nil {
		return ErrInvalid
	}
	if _, err := tokens.Token(); !errors.Is(err, io.EOF) {
		return ErrInvalid
	}
	if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return ErrInvalid
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ErrInvalid
	}
	if dates, ok := fields["dates"]; ok && !bytes.Equal(bytes.TrimSpace(dates), []byte("null")) {
		var values map[string]json.RawMessage
		if err := json.Unmarshal(dates, &values); err != nil {
			return ErrInvalid
		}
		for _, required := range []string{"start", "end", "present"} {
			if _, ok := values[required]; !ok {
				return ErrInvalid
			}
		}
	}
	return nil
}

func consume(d *json.Decoder, depth int, key string) error {
	if depth > 100 {
		return ErrInvalid
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	if token == nil && key != "dates" && key != "dates.end" {
		return ErrInvalid
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
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
			if err = consume(d, depth+1, strings.TrimPrefix(key+"."+name, ".")); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err = consume(d, depth+1, ""); err != nil {
				return err
			}
		}
	default:
		return ErrInvalid
	}
	_, err = d.Token()
	return err
}
