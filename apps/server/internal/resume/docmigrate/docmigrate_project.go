package docmigrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
)

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

var documentKeys = []string{"schemaVersion", "personalDetails", "content", "customization"}

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
