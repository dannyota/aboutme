package docmigrate

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Document v4 adds optional customization.header.photoPosition and an
// optional subtitle on project entries
// (docs/adr/0044-header-photo-position-and-project-subtitle.md). Lifting v3
// changes only the version; lowering drops both fields, which is the declared
// v3 emission loss.

func convertV3ToV4(doc json.RawMessage) (json.RawMessage, error) {
	value, err := decodeDocumentValue(doc)
	if err != nil {
		return nil, err
	}
	value["schemaVersion"] = json.Number("4")
	return encodeConverted(value)
}

func convertV4ToV3(doc json.RawMessage) (json.RawMessage, error) {
	value, err := decodeDocumentValue(doc)
	if err != nil {
		return nil, err
	}
	if err := removeV4Fields(value); err != nil {
		return nil, err
	}
	value["schemaVersion"] = json.Number("3")
	return encodeConverted(value)
}

// removeV4Fields deletes every field v3 cannot represent, in place. The
// header is optional, so a document without one has no position to drop.
func removeV4Fields(value map[string]any) error {
	customization, ok := value["customization"].(map[string]any)
	if !ok {
		return errors.New("customization is not an object")
	}
	if rawHeader, present := customization["header"]; present {
		header, isObject := rawHeader.(map[string]any)
		if !isObject {
			return errors.New("customization.header is not an object")
		}
		delete(header, "photoPosition")
	}
	return removeProjectSubtitles(value)
}

func removeProjectSubtitles(value map[string]any) error {
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
			delete(entry, "subtitle")
		}
	}
	return nil
}
