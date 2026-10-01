package docmigrate

import (
	"bytes"
	"testing"
)

// v5Document returns the v4 test document at version 5 with a dark color
// scheme, the one field v4 cannot represent.
func v5Document(t *testing.T) map[string]any {
	t.Helper()
	doc := v4Document(t)
	doc["schemaVersion"] = float64(5)
	fontTestObject(t, doc, "customization")["colorScheme"] = "dark"
	return doc
}

// withoutV5Fields strips exactly what v4 cannot represent.
func withoutV5Fields(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	out := decodeFontTestMap(t, mustJSON(t, doc))
	delete(fontTestObject(t, out, "customization"), "colorScheme")
	return out
}

func TestV5DocumentIsValidAtV5Only(t *testing.T) {
	if err := releasedValidators[5](mustJSON(t, v5Document(t))); err != nil {
		t.Fatalf("v5 document invalid at v5: %v", err)
	}
	asV4 := v5Document(t)
	asV4["schemaVersion"] = float64(4)
	if err := releasedValidators[4](mustJSON(t, asV4)); err == nil {
		t.Fatal("v4 accepted customization.colorScheme")
	}
}

func TestV5ValidatorRejectsOtherColorSchemes(t *testing.T) {
	for _, scheme := range []any{"auto", "Dark", "", nil, float64(0), true, []any{"dark"}} {
		doc := v5Document(t)
		fontTestObject(t, doc, "customization")["colorScheme"] = scheme
		if err := releasedValidators[5](mustJSON(t, doc)); err == nil {
			t.Errorf("v5 accepted colorScheme %#v", scheme)
		}
	}
	for _, scheme := range []string{"light", "dark", "system"} {
		doc := v5Document(t)
		fontTestObject(t, doc, "customization")["colorScheme"] = scheme
		if err := releasedValidators[5](mustJSON(t, doc)); err != nil {
			t.Errorf("v5 rejected colorScheme %q: %v", scheme, err)
		}
	}
}

func TestV4V5RoundTripIsExact(t *testing.T) {
	for _, name := range []string{"minimal.json", "full.json"} {
		t.Run(name, func(t *testing.T) {
			v4 := readFontV2Fixture(t, "packages", "schema", "fixtures", "v4", name)
			if err := releasedValidators[4](v4); err != nil {
				t.Fatalf("v4 fixture invalid at v4: %v", err)
			}
			v5, err := convertV4ToV5(v4)
			if err != nil {
				t.Fatalf("convert v4 to v5: %v", err)
			}
			if validateErr := releasedValidators[5](v5); validateErr != nil {
				t.Fatalf("converted document invalid at v5: %v", validateErr)
			}
			back, err := convertV5ToV4(v5)
			if err != nil {
				t.Fatalf("convert v5 to v4: %v", err)
			}
			if !bytes.Equal(normalizeJSONForFontTest(t, back), normalizeJSONForFontTest(t, v4)) {
				t.Fatal("v4 -> v5 -> v4 changed the document")
			}
			want := decodeFontTestMap(t, v4)
			want["schemaVersion"] = float64(5)
			if !bytes.Equal(normalizeJSONForFontTest(t, v5), mustJSON(t, want)) {
				t.Fatal("v4 -> v5 changed anything but schemaVersion")
			}
		})
	}
}

func TestV5ToV4DropsOnlyColorScheme(t *testing.T) {
	for _, scheme := range []string{"light", "dark", "system"} {
		t.Run(scheme, func(t *testing.T) {
			doc := v5Document(t)
			fontTestObject(t, doc, "customization")["colorScheme"] = scheme
			v4, err := convertV5ToV4(mustJSON(t, doc))
			if err != nil {
				t.Fatalf("convert v5 to v4: %v", err)
			}
			if err := releasedValidators[4](v4); err != nil {
				t.Fatalf("down-converted document invalid at v4: %v", err)
			}
			want := withoutV5Fields(t, doc)
			want["schemaVersion"] = float64(4)
			if !bytes.Equal(normalizeJSONForFontTest(t, v4), mustJSON(t, want)) {
				t.Fatalf("v5 -> v4 = %s, want %s", normalizeJSONForFontTest(t, v4), mustJSON(t, want))
			}
		})
	}
}

func TestV5ToV4HandlesAbsentColorScheme(t *testing.T) {
	doc := v5Document(t)
	delete(fontTestObject(t, doc, "customization"), "colorScheme")
	v4, err := convertV5ToV4(mustJSON(t, doc))
	if err != nil {
		t.Fatalf("convert v5 without a color scheme: %v", err)
	}
	if err := releasedValidators[4](v4); err != nil {
		t.Fatalf("down-converted document invalid at v4: %v", err)
	}
}

func TestV4V5ConvertersRejectMalformedShapes(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"customization not an object": func(doc map[string]any) { doc["customization"] = []any{} },
		"customization missing":       func(doc map[string]any) { delete(doc, "customization") },
	} {
		t.Run(name, func(t *testing.T) {
			doc := v5Document(t)
			mutate(doc)
			if _, err := convertV5ToV4(mustJSON(t, doc)); err == nil {
				t.Fatal("convertV5ToV4 accepted a malformed document")
			}
		})
	}
}

func TestProductionEmitsV4WithDeclaredV5Loss(t *testing.T) {
	doc := v5Document(t)
	emitted, err := NewIdentityProjector().EmitWire(mustJSON(t, doc), 4)
	if err != nil {
		t.Fatalf("emit v4: %v", err)
	}
	want := withoutV5Fields(t, doc)
	want["schemaVersion"] = float64(4)
	if !bytes.Equal(normalizeJSONForFontTest(t, emitted), mustJSON(t, want)) {
		t.Fatalf("emitted v4 = %s", normalizeJSONForFontTest(t, emitted))
	}
}

func TestProductionEmitsCurrentKeepingColorScheme(t *testing.T) {
	doc := v5Document(t)
	emitted, err := NewIdentityProjector().EmitWire(mustJSON(t, doc), 5)
	if err != nil {
		t.Fatalf("emit v5: %v", err)
	}
	if !bytes.Equal(normalizeJSONForFontTest(t, emitted), mustJSON(t, doc)) {
		t.Fatalf("emitted v5 = %s", normalizeJSONForFontTest(t, emitted))
	}
}

func TestProductionAcceptsV4AsCurrentWithoutColorScheme(t *testing.T) {
	v4 := readFontV2Fixture(t, "packages", "schema", "fixtures", "v4", "full.json")
	accepted, version, err := NewIdentityProjector().AcceptWire(v4, 4)
	if err != nil {
		t.Fatalf("accept v4: %v", err)
	}
	if version != 5 {
		t.Fatalf("accepted version = %d, want 5", version)
	}
	want := decodeFontTestMap(t, v4)
	want["schemaVersion"] = float64(5)
	if !bytes.Equal(normalizeJSONForFontTest(t, accepted), mustJSON(t, want)) {
		t.Fatal("accepting v4 changed anything but schemaVersion")
	}
}

func TestProductionEmissionLossPolicyAllowsOnlyDeclaredV5Loss(t *testing.T) {
	doc := v5Document(t)
	current := mustJSON(t, doc)
	emittedMap := withoutV5Fields(t, doc)
	emittedMap["schemaVersion"] = float64(4)
	restored := mustJSON(t, withoutV5Fields(t, doc))

	if err := productionEmissionLossPolicy(current, mustJSON(t, emittedMap), restored, 4); err != nil {
		t.Fatalf("declared v5 loss rejected: %v", err)
	}

	// A v4 emission must keep the v4 fields; only colorScheme may go.
	lostV4 := withoutV4Fields(t, withoutV5Fields(t, doc))
	lostV4["schemaVersion"] = float64(4)
	if err := productionEmissionLossPolicy(current, mustJSON(t, lostV4), restored, 4); err == nil {
		t.Fatal("a v4 emission passed without its v4 fields")
	}

	changed := withoutV5Fields(t, doc)
	changed["schemaVersion"] = float64(4)
	fontTestObject(t, fontTestObject(t, changed, "customization"), "header")["align"] = "center"
	if err := productionEmissionLossPolicy(current, mustJSON(t, changed), restored, 4); err == nil {
		t.Fatal("a v4 emission passed with a changed header align")
	}

	kept := decodeFontTestMap(t, current)
	kept["schemaVersion"] = float64(4)
	if err := productionEmissionLossPolicy(current, mustJSON(t, kept), restored, 4); err == nil {
		t.Fatal("a v4 emission passed while still carrying colorScheme")
	}
}
