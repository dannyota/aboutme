// Package resume owns the document codec and store-layer validation boundary.
//
// A stored resume document uses schema_version plus three jsonb columns:
// personal_details, content, and customization. The row also has scalar
// metadata. The wire/domain shape (schema.Resume) carries schemaVersion beside
// those same three keys. This package owns the current-version codec;
// docmigrate owns assembly and splitting during version conversion. Other
// callers do neither by hand. IdempotencyStore composes writes inside one
// transaction and forbids external side effects in its callback. See
// docs/design/data.md and
// docs/adr/0016-transactional-idempotency.md.
package resume

import (
	"bytes"
	"encoding/json"
	"fmt"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

// MaxDocumentBytes is the store-layer size bound on the canonical assembled
// document; see docs/plans/budgets.md. It measures AssembleCanonical's output,
// including schemaVersion, independent of jsonb's on-disk representation.
const MaxDocumentBytes = 512 * 1024

// AssembleCanonical marshals doc -- including its SchemaVersion field, which
// the three stored jsonb columns never carry themselves -- into the
// canonical full-document JSON used for JSON-Schema validation and the
// MaxDocumentBytes bound. doc.SchemaVersion is whatever the caller set
// it to: DecodeParts injects it from the row's own schema_version column: a
// caller assembling a brand-new document sets it directly.
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
// schema.Resume. "Strict" means an unknown field anywhere in personalDetails
// or customization is a decode
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
	// header and decodeEntries. The trailing-data check below still applies
	// (matching strictUnmarshal's own second check for personalDetails/
	// customization) -- nothing about decoding into a map exempts it.
	dec := json.NewDecoder(bytes.NewReader(content))
	if err := dec.Decode(&contentMap); err != nil {
		return schema.Resume{}, fmt.Errorf("resume: decoding content: %w", err)
	}
	if dec.More() {
		return schema.Resume{}, fmt.Errorf("resume: decoding content: unexpected trailing data after JSON value")
	}
	doc.Content = contentMap

	if err := strictUnmarshal(customization, &doc.Customization); err != nil {
		return schema.Resume{}, fmt.Errorf("resume: decoding customization: %w", err)
	}

	return doc, nil
}

// encodeParts is DecodeParts' inverse: it decomposes doc into the three
// jsonb parts a caller persists into personal_details/content/customization
// columns. schemaVersion is deliberately dropped from all three -- it lives in
// doc.SchemaVersion and belongs to the row's own schema_version column,
// never inside a jsonb part.
//
// Package-private so no package outside internal/resume can produce the three
// jsonb values a document write persists. export_test.go exposes a test seam.
func encodeParts(doc schema.Resume) (personalDetails, content, customization json.RawMessage, err error) {
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
