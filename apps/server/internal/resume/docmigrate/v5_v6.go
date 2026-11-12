package docmigrate

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Document v6 adds an optional projectId on project entries
// (docs/design/projects.md, docs/adr/0004-resume-document-contract.md).
// Lifting v5 changes only the version; lowering drops every projectId, which
// is the declared v5 emission loss.

func convertV5ToV6(doc json.RawMessage) (json.RawMessage, error) {
	value, err := decodeDocumentValue(doc)
	if err != nil {
		return nil, err
	}
	value["schemaVersion"] = json.Number("6")
	return encodeConverted(value)
}

func convertV6ToV5(doc json.RawMessage) (json.RawMessage, error) {
	value, err := decodeDocumentValue(doc)
	if err != nil {
		return nil, err
	}
	if err := removeV6Fields(value); err != nil {
		return nil, err
	}
	value["schemaVersion"] = json.Number("5")
	return encodeConverted(value)
}

// removeV6Fields deletes every field v5 cannot represent, in place.
func removeV6Fields(value map[string]any) error {
	content, ok := value["content"].(map[string]any)
	if !ok {
		return errors.New("content is not an object")
	}
	for key, rawSection := range content {
		section, isObject := rawSection.(map[string]any)
		if !isObject {
			return fmt.Errorf("content[%q] is not an object", key)
		}
		if section["sectionType"] != "project" {
			continue
		}
		entries, isArray := section["entries"].([]any)
		if !isArray {
			return fmt.Errorf("content[%q].entries is not an array", key)
		}
		for index, rawEntry := range entries {
			entry, isEntry := rawEntry.(map[string]any)
			if !isEntry {
				return fmt.Errorf("content[%q].entries[%d] is not an object", key, index)
			}
			delete(entry, "projectId")
		}
	}
	return nil
}
