package docmigrate

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Document v3 adds optional personalDetails.details[].display and
// customization.font.textAlign (docs/adr/0041-contact-link-display-and-body-justify.md).
// Lifting v2 changes only the version; lowering drops both fields, which is
// the declared v2 emission loss.

func convertV2ToV3(doc json.RawMessage) (json.RawMessage, error) {
	value, err := decodeDocumentValue(doc)
	if err != nil {
		return nil, err
	}
	value["schemaVersion"] = json.Number("3")
	return encodeConverted(value)
}

func convertV3ToV2(doc json.RawMessage) (json.RawMessage, error) {
	value, err := decodeDocumentValue(doc)
	if err != nil {
		return nil, err
	}
	if err := removeV3Fields(value); err != nil {
		return nil, err
	}
	value["schemaVersion"] = json.Number("2")
	return encodeConverted(value)
}

func encodeConverted(value map[string]any) (json.RawMessage, error) {
	out, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encoding converted document: %w", err)
	}
	return out, nil
}

// removeV3Fields deletes every field v2 cannot represent, in place.
func removeV3Fields(value map[string]any) error {
	font, err := fontObject(value)
	if err != nil {
		return err
	}
	delete(font, "textAlign")
	personalDetails, ok := value["personalDetails"].(map[string]any)
	if !ok {
		return errors.New("personalDetails is not an object")
	}
	rawDetails, present := personalDetails["details"]
	if !present {
		return nil
	}
	details, ok := rawDetails.([]any)
	if !ok {
		return errors.New("personalDetails.details is not an array")
	}
	for index, rawDetail := range details {
		detail, ok := rawDetail.(map[string]any)
		if !ok {
			return fmt.Errorf("personalDetails.details[%d] is not an object", index)
		}
		delete(detail, "display")
	}
	return nil
}

// productionEmissionLossPolicy permits exactly the declared losses of an
// older emission: v4's photoPosition and project subtitles for any older
// target, v3's display and textAlign for v2 and v1, plus the v1 font fallback
// for v1. Everything else must survive unchanged.
func productionEmissionLossPolicy(current, emitted, restored json.RawMessage, target int32) error {
	if target < 1 || target > 3 {
		return fmt.Errorf("no declared loss for target version %d", target)
	}
	currentValue, err := decodeDocumentValue(current)
	if err != nil {
		return fmt.Errorf("decoding current document: %w", err)
	}
	if removeErr := removeV4Fields(currentValue); removeErr != nil {
		return fmt.Errorf("current document: %w", removeErr)
	}
	if target < 3 {
		if removeErr := removeV3Fields(currentValue); removeErr != nil {
			return fmt.Errorf("current document: %w", removeErr)
		}
	}
	withoutNewer, err := json.Marshal(currentValue)
	if err != nil {
		return fmt.Errorf("encoding current document: %w", err)
	}
	if target == 1 {
		return v1FontFallbackPolicy(withoutNewer, emitted, restored)
	}
	for name, candidate := range map[string]json.RawMessage{"emitted": emitted, "restored": restored} {
		value, err := decodeDocumentValue(candidate)
		if err != nil {
			return fmt.Errorf("decoding %s document: %w", name, err)
		}
		want := cloneWithoutVersion(currentValue)
		delete(value, "schemaVersion")
		equal, err := jsonValuesEqual(want, value)
		if err != nil {
			return fmt.Errorf("comparing %s document: %w", name, err)
		}
		if !equal {
			return fmt.Errorf("%s document changed a value v%d can represent", name, target)
		}
	}
	return nil
}

func cloneWithoutVersion(value map[string]any) map[string]any {
	out := make(map[string]any, len(value))
	for key, field := range value {
		if key != "schemaVersion" {
			out[key] = field
		}
	}
	return out
}
