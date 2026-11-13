package resume

import (
	"bytes"
	"encoding/json"
	"fmt"

	schema "danny.vn/aboutme/packages/schema/gen/go"
)

const MaxDocumentBytes = 512 * 1024

func AssembleCanonical(doc schema.Resume) ([]byte, error) {
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("resume: assembling canonical document: %w", err)
	}
	return out, nil
}

func DecodeParts(personalDetails, content, customization json.RawMessage, schemaVersion int32) (schema.Resume, error) {
	var doc schema.Resume
	doc.SchemaVersion = int64(schemaVersion)

	if err := strictUnmarshal(personalDetails, &doc.PersonalDetails); err != nil {
		return schema.Resume{}, fmt.Errorf("resume: decoding personalDetails: %w", err)
	}

	var contentMap map[string]schema.Section
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

// json.Decoder does not reject trailing data, and json.Unmarshal has no DisallowUnknownFields.
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
