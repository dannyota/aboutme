package showcase

import (
	"encoding/json"
	"fmt"
	"sync"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

var (
	presetIndexOnce sync.Once
	presetIndex     map[string]string
)

// TemplateID derives the template of a resume from its customization: the ID
// of the preset whose customization equals it, compared as canonical JSON
// without the five leaves a template apply keeps for the owner or resets and
// without placement, or nil when no preset matches (docs/design/showcase.md
// "What a listing shows"). A match that fails to encode is an error.
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
			// A generated table that does not parse is a build defect, caught
			// by the preset table test; leave the preset unmatched here.
			continue
		}
		if _, taken := presetIndex[form]; !taken {
			presetIndex[form] = preset.ID
		}
	}
}

// resumeMatchForm is the canonical JSON of a stored customization without the
// leaves the showcase ignores: pageFormat, dateFormat, colorScheme,
// font.textAlign, header.photoPosition, and layout.sections.
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

// canonicalForm re-encodes JSON with sorted keys and Go number formatting, so a
// generated table and a marshaled customization compare equal when they hold
// equal values.
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
