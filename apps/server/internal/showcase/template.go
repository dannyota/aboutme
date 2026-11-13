package showcase

import (
	"encoding/json"
	"fmt"
	"sync"

	schema "danny.vn/aboutme/packages/schema/gen/go"
)

var (
	presetIndexOnce sync.Once
	presetIndex     map[string]string
)

func TemplateID(customization schema.Customization) (*string, error) {
	raw, err := json.Marshal(customization)
	if err != nil {
		return nil, fmt.Errorf("showcase: encode customization: %w", err)
	}
	form, err := resumeMatchForm(raw)
	if err != nil {
		return nil, err
	}
	presetIndexOnce.Do(buildPresetIndex)
	id, found := presetIndex[form]
	if !found {
		return nil, nil
	}
	return &id, nil
}

func buildPresetIndex() {
	presetIndex = make(map[string]string, len(Presets))
	for _, preset := range Presets {
		form, err := canonicalForm([]byte(preset.Match))
		if err != nil {
			continue
		}
		if _, taken := presetIndex[form]; !taken {
			presetIndex[form] = preset.ID
		}
	}
}

func resumeMatchForm(raw []byte) (string, error) {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return "", fmt.Errorf("showcase: decode customization")
	}
	delete(value, "pageFormat")
	delete(value, "dateFormat")
	delete(value, "colorScheme")
	if font, ok := value["font"].(map[string]any); ok {
		delete(font, "textAlign")
	}
	if header, ok := value["header"].(map[string]any); ok {
		delete(header, "photoPosition")
	}
	if layout, ok := value["layout"].(map[string]any); ok {
		delete(layout, "sections")
	}
	return encodeCanonical(value)
}

func canonicalForm(raw []byte) (string, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("showcase: decode preset: %w", err)
	}
	return encodeCanonical(value)
}

func encodeCanonical(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("showcase: encode canonical form: %w", err)
	}
	return string(encoded), nil
}
