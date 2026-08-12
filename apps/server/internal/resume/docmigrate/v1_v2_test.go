package docmigrate

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestV1V2OriginalFamiliesRoundTripExactly(t *testing.T) {
	for v1Family, v2ID := range v1FamilyToV2ID {
		t.Run(v2ID, func(t *testing.T) {
			var doc map[string]any
			if err := json.Unmarshal(readFontV2Fixture(t, "packages", "schema", "fixtures", "v1", "minimal.json"), &doc); err != nil {
				t.Fatalf("decode v1 fixture: %v", err)
			}
			customization := fontTestObject(t, doc, "customization")
			fontTestObject(t, customization, "font")["family"] = v1Family
			v1 := mustJSON(t, doc)

			v2, err := convertV1ToV2(v1)
			if err != nil {
				t.Fatalf("convert v1 to v2: %v", err)
			}
			if got := fontFamilyOf(t, v2); got != v2ID {
				t.Fatalf("v2 family = %q, want %q", got, v2ID)
			}
			roundTrip, err := convertV2ToV1(v2)
			if err != nil {
				t.Fatalf("convert v2 to v1: %v", err)
			}
			if !bytes.Equal(normalizeJSONForFontTest(t, roundTrip), normalizeJSONForFontTest(t, v1)) {
				t.Fatal("original v1 family did not round-trip at the JSON-value level")
			}
		})
	}
}

func TestV2ToV1PreservesEveryNonFontValue(t *testing.T) {
	v2 := fontV2Document(t, "cormorant-garamond")
	v1, err := convertV2ToV1(v2)
	if err != nil {
		t.Fatalf("convert v2 to v1: %v", err)
	}
	if got := fontFamilyOf(t, v1); got != "Alegreya" {
		t.Fatalf("v1 fallback = %q, want Alegreya", got)
	}
	if !bytes.Equal(withoutVersionAndFont(t, v2), withoutVersionAndFont(t, v1)) {
		t.Fatal("converter changed a non-font JSON value")
	}
}

func TestProductionEmissionPolicyRejectsEmittedNonFontChange(t *testing.T) {
	current := fontV2Document(t, "noto-sans")
	emitted := decodeFontTestMap(t, current)
	emitted["schemaVersion"] = float64(1)
	emittedCustomization := fontTestObject(t, emitted, "customization")
	fontTestObject(t, emittedCustomization, "font")["family"] = "Inter"
	fontTestObject(t, emitted, "personalDetails")["fullName"] = "Changed"
	restored := decodeFontTestMap(t, current)
	restoredCustomization := fontTestObject(t, restored, "customization")
	fontTestObject(t, restoredCustomization, "font")["family"] = "inter"

	if err := productionEmissionLossPolicy(current, mustJSON(t, emitted), mustJSON(t, restored), 1); err == nil {
		t.Fatal("emitted non-font change passed the production emission policy")
	}
}
