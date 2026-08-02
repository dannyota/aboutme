// Package resume is the P2A document codec and store-layer validation
// pipeline: the single write-path choke point every resume write passes
// through (docs/plans/phase-2a-resume-store.md, decisions D1/D4/D16/D19).
//
// A stored resume is four Postgres columns: schema_version (int) plus three
// jsonb columns (personal_details, content, customization). The wire/domain
// shape (schema.Resume) is one JSON object carrying schemaVersion alongside
// those same three keys. This package is the only place that assembles the
// four columns into the one document shape, or decomposes the one shape
// back into the four columns (D4) -- callers never do this by hand.
package resume

import (
	"bytes"
	"encoding/json"
	"fmt"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

// MaxDocumentBytes is the P2A store-layer size bound on the canonical
// assembled document (docs/plans/budgets.md: "Resume document total ...
// 512 KB", P2A store). Measured on AssembleCanonical's output (D10): the
// marshaled full document, including the injected schemaVersion, independent
// of jsonb's internal on-disk representation.
const MaxDocumentBytes = 512 * 1024

// AssembleCanonical marshals doc -- including its SchemaVersion field, which
// the three stored jsonb columns never carry themselves (D4) -- into the
// canonical full-document JSON used for JSON-Schema validation and the
// MaxDocumentBytes bound (D10). doc.SchemaVersion is whatever the caller set
// it to: DecodeParts injects it from the row's own schema_version column: a
// caller assembling a brand-new document sets it directly (D19: always
// CurrentVersion for anything that reaches ValidateForStore).
func AssembleCanonical(doc schema.Resume) ([]byte, error) {
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("resume: assembling canonical document: %w", err)
	}
	return out, nil
}

// DecodeParts strict-decodes the three stored jsonb parts -- personalDetails,
// content, customization, exactly as they'd be read back from their three
// Postgres columns -- plus the row's separate schema_version column, into one
// schema.Resume (D4: this is the only assembly point). "Strict" means an
// unknown field anywhere in personalDetails or customization is a decode
// error, not a silently-dropped field; within content, each section's
// entries are strict-decoded the same way by schema.Section's own
// UnmarshalJSON (gen/go/section.go) -- a field foreign to an entry's
// sectionType is rejected there.
func DecodeParts(personalDetails, content, customization json.RawMessage, schemaVersion int32) (schema.Resume, error) {
	var doc schema.Resume
	doc.SchemaVersion = int64(schemaVersion)

	if err := strictUnmarshal(personalDetails, &doc.PersonalDetails); err != nil {
		return schema.Resume{}, fmt.Errorf("resume: decoding personalDetails: %w", err)
	}

	var contentMap map[string]schema.Section
	// content is a map, not a struct: DisallowUnknownFields has no effect on
	// map decoding (it only rejects unrecognized STRUCT fields; a map's keys
	// are inherently open-ended section keys). Per-entry strictness comes
	// from schema.Section's own UnmarshalJSON instead -- see that file's
	// header and decodeEntries.
	dec := json.NewDecoder(bytes.NewReader(content))
	if err := dec.Decode(&contentMap); err != nil {
		return schema.Resume{}, fmt.Errorf("resume: decoding content: %w", err)
	}
	doc.Content = contentMap

	if err := strictUnmarshal(customization, &doc.Customization); err != nil {
		return schema.Resume{}, fmt.Errorf("resume: decoding customization: %w", err)
	}

	return doc, nil
}

// EncodeParts is DecodeParts' inverse: it decomposes doc into the three
// jsonb parts a caller persists into personal_details/content/customization
// (D4). schemaVersion is deliberately dropped from all three -- it lives in
// doc.SchemaVersion and belongs to the row's own schema_version column,
// never inside a jsonb part.
func EncodeParts(doc schema.Resume) (personalDetails, content, customization json.RawMessage, err error) {
	personalDetails, err = json.Marshal(doc.PersonalDetails)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("resume: encoding personalDetails: %w", err)
	}
	content, err = json.Marshal(doc.Content)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("resume: encoding content: %w", err)
	}
	customization, err = json.Marshal(doc.Customization)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("resume: encoding customization: %w", err)
	}
	return personalDetails, content, customization, nil
}

// strictUnmarshal decodes data into target, rejecting any field target's
// type doesn't declare, and rejecting trailing data after the single JSON
// value (encoding/json.Decoder alone doesn't enforce that second part --
// only json.Unmarshal does, and it lacks DisallowUnknownFields).
func strictUnmarshal(data json.RawMessage, target any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("unexpected trailing data after JSON value")
	}
	return nil
}
