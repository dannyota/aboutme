package docmigrate

import (
	"encoding/json"
	"errors"
)

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

func removeV5Fields(value map[string]any) error {
	customization, ok := value["customization"].(map[string]any)
	if !ok {
		return errors.New("customization is not an object")
	}
	delete(customization, "colorScheme")
	return nil
}
