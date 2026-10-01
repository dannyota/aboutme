package docmigrate

import (
	"encoding/json"
	"errors"
)

// Document v5 adds the optional customization.colorScheme leaf
// (docs/design/public-page-theme.md, docs/adr/0004-resume-document-contract.md).
// Lifting v4 changes only the version; lowering drops the leaf, which is the
// declared v4 emission loss.

func convertV4ToV5(doc json.RawMessage) (json.RawMessage, error) {
	value, err := decodeDocumentValue(doc)
	if err != nil {
		return nil, err
	}
	value["schemaVersion"] = json.Number("5")
	return encodeConverted(value)
}

func convertV5ToV4(doc json.RawMessage) (json.RawMessage, error) {
	value, err := decodeDocumentValue(doc)
	if err != nil {
		return nil, err
	}
	if err := removeV5Fields(value); err != nil {
		return nil, err
	}
	value["schemaVersion"] = json.Number("4")
	return encodeConverted(value)
}

// removeV5Fields deletes every field v4 cannot represent, in place.
func removeV5Fields(value map[string]any) error {
	customization, ok := value["customization"].(map[string]any)
	if !ok {
		return errors.New("customization is not an object")
	}
	delete(customization, "colorScheme")
	return nil
}
