package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// splitResumeDoc pulls the three jsonb columns out of the embedded current
// document and confirms the schema version.
func splitResumeDoc(raw []byte) (personalDetails, content, customization []byte, err error) {
	var doc struct {
		SchemaVersion   int             `json:"schemaVersion"`
		PersonalDetails json.RawMessage `json:"personalDetails"`
		Content         json.RawMessage `json:"content"`
		Customization   json.RawMessage `json:"customization"`
	}
	if unmarshalErr := json.Unmarshal(raw, &doc); unmarshalErr != nil {
		return nil, nil, nil, fmt.Errorf("decode full.json: %w", unmarshalErr)
	}
	if doc.SchemaVersion != 5 {
		return nil, nil, nil, fmt.Errorf("full.json schemaVersion = %d, want 5", doc.SchemaVersion)
	}
	personalDetails, err = withoutPhoto(doc.PersonalDetails)
	if err != nil {
		return nil, nil, nil, err
	}
	return personalDetails, doc.Content, doc.Customization, nil
}

// withoutPhoto drops the fixture's photo reference: the seed has no media
// backend, and a key without bytes fails the owner photo read. It removes the
// top-level member directly from the original JSON so all remaining bytes and
// key order stay unchanged.
func withoutPhoto(raw json.RawMessage) (json.RawMessage, error) {
	if !json.Valid(raw) {
		return nil, errors.New("decode personalDetails: invalid JSON")
	}
	entries, err := scanObjectEntries(raw)
	if err != nil {
		return nil, fmt.Errorf("decode personalDetails: %w", err)
	}
	for index, entry := range entries {
		if entry.key != "photo" {
			continue
		}
		removeStart := entry.leadingStart
		removeEnd := entry.valueEnd
		if entry.commaStart >= 0 {
			removeEnd = entry.commaStart + 1
		} else if index > 0 {
			removeStart = entries[index-1].commaStart
		}
		out := make([]byte, 0, len(raw)-(removeEnd-removeStart))
		out = append(out, raw[:removeStart]...)
		out = append(out, raw[removeEnd:]...)
		return out, nil
	}
	return append(json.RawMessage(nil), raw...), nil
}

type jsonObjectEntry struct {
	key          string
	leadingStart int
	valueEnd     int
	commaStart   int
}

func scanObjectEntries(raw []byte) ([]jsonObjectEntry, error) {
	index := skipJSONSpace(raw, 0)
	if index >= len(raw) || raw[index] != '{' {
		return nil, errors.New("personalDetails must be a JSON object")
	}
	index++
	entries := make([]jsonObjectEntry, 0)
	for {
		leadingStart := index
		index = skipJSONSpace(raw, index)
		if index >= len(raw) {
			return nil, errors.New("personalDetails object is truncated")
		}
		if raw[index] == '}' {
			if skipJSONSpace(raw, index+1) != len(raw) {
				return nil, errors.New("personalDetails has trailing data")
			}
			return entries, nil
		}
		keyEnd, err := scanJSONString(raw, index)
		if err != nil {
			return nil, err
		}
		var key string
		if keyErr := json.Unmarshal(raw[index:keyEnd], &key); keyErr != nil {
			return nil, fmt.Errorf("decode personalDetails key: %w", keyErr)
		}
		index = skipJSONSpace(raw, keyEnd)
		if index >= len(raw) || raw[index] != ':' {
			return nil, errors.New("personalDetails object key is missing a colon")
		}
		index = skipJSONSpace(raw, index+1)
		valueEnd, err := scanJSONValue(raw, index)
		if err != nil {
			return nil, err
		}
		index = skipJSONSpace(raw, valueEnd)
		commaStart := -1
		if index < len(raw) && raw[index] == ',' {
			commaStart = index
			index++
			nextIndex := skipJSONSpace(raw, index)
			if nextIndex >= len(raw) || raw[nextIndex] == '}' {
				return nil, errors.New("personalDetails object has a trailing comma")
			}
		} else if index >= len(raw) || raw[index] != '}' {
			return nil, errors.New("personalDetails object member is missing a comma")
		}
		entries = append(entries, jsonObjectEntry{
			key:          key,
			leadingStart: leadingStart,
			valueEnd:     valueEnd,
			commaStart:   commaStart,
		})
		if commaStart < 0 {
			if skipJSONSpace(raw, index+1) != len(raw) {
				return nil, errors.New("personalDetails has trailing data")
			}
			return entries, nil
		}
	}
}

func skipJSONSpace(raw []byte, index int) int {
	for index < len(raw) {
		switch raw[index] {
		case ' ', '\t', '\r', '\n':
			index++
		default:
			return index
		}
	}
	return index
}

func scanJSONString(raw []byte, index int) (int, error) {
	if index >= len(raw) || raw[index] != '"' {
		return 0, errors.New("personalDetails object key is not a JSON string")
	}
	for index = index + 1; index < len(raw); index++ {
		switch raw[index] {
		case '\\':
			index++
		case '"':
			return index + 1, nil
		default:
			if raw[index] < 0x20 {
				return 0, errors.New("personalDetails contains an invalid JSON string")
			}
		}
	}
	return 0, errors.New("personalDetails contains an unterminated JSON string")
}

func scanJSONValue(raw []byte, index int) (int, error) {
	if index >= len(raw) {
		return 0, errors.New("personalDetails object member has no value")
	}
	switch raw[index] {
	case '"':
		return scanJSONString(raw, index)
	case '{':
		return scanJSONContainer(raw, index, '}')
	case '[':
		return scanJSONContainer(raw, index, ']')
	case 't':
		return scanJSONLiteral(raw, index, "true")
	case 'f':
		return scanJSONLiteral(raw, index, "false")
	case 'n':
		return scanJSONLiteral(raw, index, "null")
	default:
		end := index
		for end < len(raw) && !strings.ContainsRune(" \t\r\n,]}", rune(raw[end])) {
			end++
		}
		if end == index || !json.Valid(raw[index:end]) {
			return 0, errors.New("personalDetails contains an invalid JSON value")
		}
		return end, nil
	}
}

func scanJSONLiteral(raw []byte, index int, literal string) (int, error) {
	end := index + len(literal)
	if end > len(raw) || string(raw[index:end]) != literal {
		return 0, errors.New("personalDetails contains an invalid JSON literal")
	}
	return end, nil
}

func scanJSONContainer(raw []byte, index int, closing byte) (int, error) {
	index++
	for {
		index = skipJSONSpace(raw, index)
		if index >= len(raw) {
			return 0, errors.New("personalDetails contains an unterminated JSON value")
		}
		if raw[index] == closing {
			return index + 1, nil
		}
		var err error
		if closing == '}' {
			keyEnd, keyErr := scanJSONString(raw, index)
			if keyErr != nil {
				return 0, keyErr
			}
			index = skipJSONSpace(raw, keyEnd)
			if index >= len(raw) || raw[index] != ':' {
				return 0, errors.New("personalDetails object key is missing a colon")
			}
			index = skipJSONSpace(raw, index+1)
		}
		index, err = scanJSONValue(raw, index)
		if err != nil {
			return 0, err
		}
		index = skipJSONSpace(raw, index)
		if index < len(raw) && raw[index] == ',' {
			index++
			continue
		}
		if index < len(raw) && raw[index] == closing {
			return index + 1, nil
		}
		return 0, errors.New("personalDetails JSON value is missing a comma")
	}
}
