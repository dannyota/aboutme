package docmigrate

import (
	"bytes"
	"testing"
)

// v3Document returns the current minimal fixture with every v3-only field set:
// a detail per display mode and justified body text.
func v3Document(t *testing.T) map[string]any {
	t.Helper()
	doc := decodeFontTestMap(t, readFontV2Fixture(t, "packages", "schema", "fixtures", "minimal.json"))
	if doc["schemaVersion"] != float64(3) {
		t.Fatalf("current minimal fixture version = %v, want 3", doc["schemaVersion"])
	}
	personalDetails := fontTestObject(t, doc, "personalDetails")
	personalDetails["details"] = []any{
		map[string]any{"id": "018f0000-0000-7000-8000-0000000000a1", "type": "github",
			"value": "https://github.com/ada", "isHidden": false, "display": "label"},
		map[string]any{"id": "018f0000-0000-7000-8000-0000000000a2", "type": "custom", "label": "Scholar",
			"value": "https://scholar.example.com/ada", "isHidden": false, "display": "full"},
		map[string]any{"id": "018f0000-0000-7000-8000-0000000000a3", "type": "email",
			"value": "ada@example.com", "isHidden": false, "display": "short"},
		map[string]any{"id": "018f0000-0000-7000-8000-0000000000a4", "type": "website",
			"value": "https://ada.example.com", "isHidden": false},
	}
	font := fontTestObject(t, fontTestObject(t, doc, "customization"), "font")
	font["textAlign"] = "justify"
	return doc
}

// withoutV3Fields strips exactly what v2 cannot represent.
func withoutV3Fields(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	out := decodeFontTestMap(t, mustJSON(t, doc))
	delete(fontTestObject(t, fontTestObject(t, out, "customization"), "font"), "textAlign")
	if details, ok := fontTestObject(t, out, "personalDetails")["details"].([]any); ok {
		for _, detail := range details {
			if object, isObject := detail.(map[string]any); isObject {
				delete(object, "display")
			}
		}
	}
	return out
}

func TestV2V3RoundTripIsExact(t *testing.T) {
	for _, name := range []string{"minimal.json", "full.json"} {
		t.Run(name, func(t *testing.T) {
			v2 := readFontV2Fixture(t, "packages", "schema", "fixtures", "v2", name)
			if err := releasedValidators[2](v2); err != nil {
				t.Fatalf("v2 fixture invalid at v2: %v", err)
			}
			v3, err := convertV2ToV3(v2)
			if err != nil {
				t.Fatalf("convert v2 to v3: %v", err)
			}
			if validateErr := releasedValidators[3](v3); validateErr != nil {
				t.Fatalf("converted document invalid at v3: %v", validateErr)
			}
			back, err := convertV3ToV2(v3)
			if err != nil {
				t.Fatalf("convert v3 to v2: %v", err)
			}
			if !bytes.Equal(normalizeJSONForFontTest(t, back), normalizeJSONForFontTest(t, v2)) {
				t.Fatal("v2 -> v3 -> v2 changed the document")
			}
			want := decodeFontTestMap(t, v2)
			want["schemaVersion"] = float64(3)
			if !bytes.Equal(normalizeJSONForFontTest(t, v3), mustJSON(t, want)) {
				t.Fatal("v2 -> v3 changed anything but schemaVersion")
			}
		})
	}
}

func TestV3ToV2DropsOnlyDisplayAndTextAlign(t *testing.T) {
	doc := v3Document(t)
	v2, err := convertV3ToV2(mustJSON(t, doc))
	if err != nil {
		t.Fatalf("convert v3 to v2: %v", err)
	}
	if err := releasedValidators[2](v2); err != nil {
		t.Fatalf("down-converted document invalid at v2: %v", err)
	}
	want := withoutV3Fields(t, doc)
	want["schemaVersion"] = float64(2)
	if !bytes.Equal(normalizeJSONForFontTest(t, v2), mustJSON(t, want)) {
		t.Fatalf("v3 -> v2 = %s, want %s", normalizeJSONForFontTest(t, v2), mustJSON(t, want))
	}
}

func TestV3ToV2HandlesAbsentDetails(t *testing.T) {
	doc := v3Document(t)
	delete(fontTestObject(t, doc, "personalDetails"), "details")
	v2, err := convertV3ToV2(mustJSON(t, doc))
	if err != nil {
		t.Fatalf("convert v3 without details: %v", err)
	}
	if err := releasedValidators[2](v2); err != nil {
		t.Fatalf("down-converted document invalid at v2: %v", err)
	}
}

func TestV2V3ConvertersRejectMalformedShapes(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"details not an array": func(doc map[string]any) {
			fontTestObject(t, doc, "personalDetails")["details"] = "nope"
		},
		"detail not an object": func(doc map[string]any) {
			fontTestObject(t, doc, "personalDetails")["details"] = []any{"nope"}
		},
		"personalDetails not an object": func(doc map[string]any) { doc["personalDetails"] = []any{} },
		"customization missing":         func(doc map[string]any) { delete(doc, "customization") },
	} {
		t.Run(name, func(t *testing.T) {
			doc := v3Document(t)
			mutate(doc)
			if _, err := convertV3ToV2(mustJSON(t, doc)); err == nil {
				t.Fatal("convertV3ToV2 accepted a malformed document")
			}
		})
	}
}

func TestProductionEmitsV2WithDeclaredV3Loss(t *testing.T) {
	doc := v3Document(t)
	emitted, err := NewIdentityProjector().EmitWire(mustJSON(t, doc), 2)
	if err != nil {
		t.Fatalf("emit v2: %v", err)
	}
	want := withoutV3Fields(t, doc)
	want["schemaVersion"] = float64(2)
	if !bytes.Equal(normalizeJSONForFontTest(t, emitted), mustJSON(t, want)) {
		t.Fatalf("emitted v2 = %s", normalizeJSONForFontTest(t, emitted))
	}
}

func TestProductionEmitsV1WithV3LossAndFontFallback(t *testing.T) {
	doc := v3Document(t)
	fontTestObject(t, fontTestObject(t, doc, "customization"), "font")["family"] = "noto-serif"
	emitted, err := NewIdentityProjector().EmitWire(mustJSON(t, doc), 1)
	if err != nil {
		t.Fatalf("emit v1: %v", err)
	}
	want := withoutV3Fields(t, doc)
	want["schemaVersion"] = float64(1)
	fontTestObject(t, fontTestObject(t, want, "customization"), "font")["family"] = "Alegreya"
	if !bytes.Equal(normalizeJSONForFontTest(t, emitted), mustJSON(t, want)) {
		t.Fatalf("emitted v1 = %s", normalizeJSONForFontTest(t, emitted))
	}
}

func TestProductionAcceptsV2AsV3WithoutNewFields(t *testing.T) {
	v2 := readFontV2Fixture(t, "packages", "schema", "fixtures", "v2", "full.json")
	accepted, version, err := NewIdentityProjector().AcceptWire(v2, 2)
	if err != nil {
		t.Fatalf("accept v2: %v", err)
	}
	if version != 3 {
		t.Fatalf("accepted version = %d, want 3", version)
	}
	want := decodeFontTestMap(t, v2)
	want["schemaVersion"] = float64(3)
	if !bytes.Equal(normalizeJSONForFontTest(t, accepted), mustJSON(t, want)) {
		t.Fatal("accepting v2 changed anything but schemaVersion")
	}
}

func TestProductionEmissionLossPolicyAllowsOnlyDeclaredV3Loss(t *testing.T) {
	doc := v3Document(t)
	current := mustJSON(t, doc)
	emittedMap := withoutV3Fields(t, doc)
	emittedMap["schemaVersion"] = float64(2)
	restored := mustJSON(t, withoutV3Fields(t, doc))

	if err := productionEmissionLossPolicy(current, mustJSON(t, emittedMap), restored, 2); err != nil {
		t.Fatalf("declared v3 loss rejected: %v", err)
	}

	fontTestObject(t, emittedMap, "personalDetails")["fullName"] = "Changed"
	if err := productionEmissionLossPolicy(current, mustJSON(t, emittedMap), restored, 2); err == nil {
		t.Fatal("emitted non-v3 change passed the production emission policy")
	}

	fontFallback := withoutV3Fields(t, doc)
	fontFallback["schemaVersion"] = float64(2)
	fontTestObject(t, fontTestObject(t, fontFallback, "customization"), "font")["family"] = "alegreya"
	if err := productionEmissionLossPolicy(current, mustJSON(t, fontFallback), restored, 2); err == nil {
		t.Fatal("a v2 emission passed with a changed font family")
	}
	if err := productionEmissionLossPolicy(current, mustJSON(t, emittedMap), restored, 3); err == nil {
		t.Fatal("a loss passed for the current version")
	}
}
