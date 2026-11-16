package githubevidence

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

func accessTokenCandidate(body []byte) (string, bool) {
	dec := json.NewDecoder(bytes.NewReader(body))
	if open, err := dec.Token(); err != nil || open != json.Delim('{') {
		return "", false
	}
	var found string
	for dec.More() {
		key, err := dec.Token()
		name, isString := key.(string)
		if err != nil || !isString {
			break
		}
		var raw json.RawMessage
		if decodeErr := dec.Decode(&raw); decodeErr != nil {
			break
		}
		if name != "access_token" {
			continue
		}
		value, isValue := jsonString(raw)
		if !isValue || value == "" {
			continue
		}
		if found != "" && found != value {
			return "", true
		}
		found = value
	}
	return found, false
}

func strictObject(body []byte) (map[string]json.RawMessage, error) {
	if !json.Valid(body) {
		return nil, errMalformed
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	if open, err := dec.Token(); err != nil || open != json.Delim('{') {
		return nil, errMalformed
	}
	fields := map[string]json.RawMessage{}
	for dec.More() {
		key, err := dec.Token()
		name, isString := key.(string)
		if err != nil || !isString {
			return nil, errMalformed
		}
		if _, duplicate := fields[name]; duplicate {
			return nil, errMalformed
		}
		var raw json.RawMessage
		if decodeErr := dec.Decode(&raw); decodeErr != nil {
			return nil, errMalformed
		}
		fields[name] = raw
	}
	if end, err := dec.Token(); err != nil || end != json.Delim('}') {
		return nil, errMalformed
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errMalformed
	}
	return fields, nil
}

func jsonString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}
