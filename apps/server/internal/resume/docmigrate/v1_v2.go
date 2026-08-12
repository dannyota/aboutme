package docmigrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

var v1FamilyToV2ID = map[string]string{
	"Be Vietnam Pro": "be-vietnam-pro",
	"Inter":          "inter",
	"Source Sans 3":  "source-sans-3",
	"Alegreya":       "alegreya",
	"Roboto Serif":   "roboto-serif",
}

var v2IDToV1Family = map[string]string{
	"be-vietnam-pro":             "Be Vietnam Pro",
	"inter":                      "Inter",
	"noto-sans":                  "Inter",
	"noto-serif":                 "Alegreya",
	"roboto":                     "Inter",
	"open-sans":                  "Inter",
	"plus-jakarta-sans":          "Inter",
	"work-sans":                  "Inter",
	"nunito-sans":                "Inter",
	"montserrat":                 "Inter",
	"fira-sans":                  "Inter",
	"barlow":                     "Inter",
	"alegreya":                   "Alegreya",
	"spectral":                   "Alegreya",
	"literata":                   "Alegreya",
	"newsreader":                 "Alegreya",
	"space-mono":                 "Source Sans 3",
	"crimson-pro":                "Alegreya",
	"eb-garamond":                "Alegreya",
	"aleo":                       "Alegreya",
	"cormorant-garamond":         "Alegreya",
	"roboto-serif":               "Roboto Serif",
	"roboto-mono":                "Source Sans 3",
	"dm-sans":                    "Inter",
	"atkinson-hyperlegible-next": "Inter",
	"source-sans-3":              "Source Sans 3",
}

func convertV1ToV2(doc json.RawMessage) (json.RawMessage, error) {
	return convertFontFamily(doc, 2, v1FamilyToV2ID)
}

func convertV2ToV1(doc json.RawMessage) (json.RawMessage, error) {
	return convertFontFamily(doc, 1, v2IDToV1Family)
}

func convertFontFamily(doc json.RawMessage, target int32, mapping map[string]string) (json.RawMessage, error) {
	value, err := decodeDocumentValue(doc)
	if err != nil {
		return nil, err
	}
	font, err := fontObject(value)
	if err != nil {
		return nil, err
	}
	family, ok := font["family"].(string)
	if !ok {
		return nil, errors.New("font.family is not a string")
	}
	mapped, ok := mapping[family]
	if !ok {
		return nil, fmt.Errorf("font.family %q has no mapping to version %d", family, target)
	}
	font["family"] = mapped
	value["schemaVersion"] = json.Number(fmt.Sprintf("%d", target))
	out, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encoding converted document: %w", err)
	}
	return out, nil
}

func productionEmissionLossPolicy(current, emitted, restored json.RawMessage, target int32) error {
	if target != 1 {
		return fmt.Errorf("no declared loss for target version %d", target)
	}
	currentValue, err := decodeDocumentValue(current)
	if err != nil {
		return fmt.Errorf("decoding current document: %w", err)
	}
	emittedValue, err := decodeDocumentValue(emitted)
	if err != nil {
		return fmt.Errorf("decoding emitted document: %w", err)
	}
	restoredValue, err := decodeDocumentValue(restored)
	if err != nil {
		return fmt.Errorf("decoding restored document: %w", err)
	}

	currentFamily, err := removeVersionAndFontFamily(currentValue)
	if err != nil {
		return fmt.Errorf("current document: %w", err)
	}
	emittedFamily, err := removeVersionAndFontFamily(emittedValue)
	if err != nil {
		return fmt.Errorf("emitted document: %w", err)
	}
	restoredFamily, err := removeVersionAndFontFamily(restoredValue)
	if err != nil {
		return fmt.Errorf("restored document: %w", err)
	}
	wantV1, ok := v2IDToV1Family[currentFamily]
	if !ok || emittedFamily != wantV1 {
		return fmt.Errorf("font fallback %q -> %q is not declared", currentFamily, emittedFamily)
	}
	wantRestored, ok := v1FamilyToV2ID[wantV1]
	if !ok || restoredFamily != wantRestored {
		return fmt.Errorf("restored fallback family %q is not the canonical v2 ID for %q", restoredFamily, wantV1)
	}
	for name, candidate := range map[string]map[string]any{
		"emitted":  emittedValue,
		"restored": restoredValue,
	} {
		equal, compareErr := jsonValuesEqual(currentValue, candidate)
		if compareErr != nil {
			return fmt.Errorf("comparing %s non-font values: %w", name, compareErr)
		}
		if !equal {
			return fmt.Errorf("%s document changed a non-font JSON value", name)
		}
	}
	return nil
}

func decodeDocumentValue(raw json.RawMessage) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value map[string]any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func fontObject(value map[string]any) (map[string]any, error) {
	customization, ok := value["customization"].(map[string]any)
	if !ok {
		return nil, errors.New("customization is not an object")
	}
	font, ok := customization["font"].(map[string]any)
	if !ok {
		return nil, errors.New("customization.font is not an object")
	}
	return font, nil
}

func removeVersionAndFontFamily(value map[string]any) (string, error) {
	delete(value, "schemaVersion")
	font, err := fontObject(value)
	if err != nil {
		return "", err
	}
	family, ok := font["family"].(string)
	if !ok {
		return "", errors.New("customization.font.family is not a string")
	}
	delete(font, "family")
	return family, nil
}
