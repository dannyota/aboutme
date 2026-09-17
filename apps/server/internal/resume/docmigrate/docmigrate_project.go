package docmigrate

// Project and its document assembly/split helpers: lifting the three stored
// jsonb parts to the projector's current version and back.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
)

// Project lifts personalDetails/content/customization -- exactly the three
// jsonb parts read back from the resumes table's own columns -- plus the
// row's own schema_version, forward to this projector's current version,
// returning the three current-version parts (still undecoded
// json.RawMessage; the caller's strict decode happens afterwards).
//
// It is pure: it never touches the database, and calling it twice on
// the same input always produces the same output. A row already at the
// current version short-circuits to a byte-for-byte passthrough. A row at a
// version this build has no schema for fails closed -- never a silent
// passthrough of content that cannot be migrated.
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

// documentKeys are the only top-level keys a canonical document has: the
// three stored jsonb parts plus the schema version. The set is fixed
// across versions because it is the storage decomposition itself, not a
// property of any one document shape.
var documentKeys = []string{"schemaVersion", "personalDetails", "content", "customization"}

// assembleDocument builds the full canonical document from the three stored
// parts plus the row's schema_version, without decoding any part: the parts
// are spliced in as raw JSON, so no value is ever reformatted on the way in.
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

// splitDocument decomposes a converted document back into the three stored
// parts. It fails closed unless the document has exactly the four
// canonical top-level keys and its own schemaVersion equals wantVersion: a
// converter that invents a fifth top-level key has produced something this
// storage layout cannot hold, and one that leaves schemaVersion behind has
// produced a document that lies about itself.
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
