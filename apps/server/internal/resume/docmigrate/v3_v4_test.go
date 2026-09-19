package docmigrate

import (
	"bytes"
	"testing"
)

// v4Document returns the current minimal fixture with every v3-only field
// set, as v3Document does, plus a left header photo and a project section
// with one subtitled entry and one without.
func v4Document(t *testing.T) map[string]any {
	t.Helper()
	current := decodeFontTestMap(t, readFontV2Fixture(t, "packages", "schema", "fixtures", "minimal.json"))
	if current["schemaVersion"] != float64(4) {
		t.Fatalf("current minimal fixture version = %v, want 4", current["schemaVersion"])
	}
	doc := v3Document(t)
	doc["schemaVersion"] = float64(4)
	fontTestObject(t, doc, "customization")["header"] = map[string]any{
		"align": "left", "detailsLayout": "inline", "iconStyle": "outline", "photoPosition": "left",
	}
	fontTestObject(t, doc, "content")["projects"] = map[string]any{
		"sectionType": "project",
		"entries": []any{
			map[string]any{"id": "018f0000-0000-7000-8000-0000000000c1", "title": "Engine", "subtitle": "Go, PostgreSQL"},
			map[string]any{"id": "018f0000-0000-7000-8000-0000000000c2", "title": "Notes"},
		},
	}
	fontTestObject(t, doc, "content")["custom"] = map[string]any{
		"sectionType": "custom",
		"entries":     []any{map[string]any{"id": "018f0000-0000-7000-8000-0000000000c3", "subtitle": "kept"}},
	}
	sections := fontTestObject(t, fontTestObject(t, fontTestObject(t, doc, "customization"), "layout"), "sections")
	sections["main"] = []any{"projects", "custom"}
	return doc
}

// withoutV4Fields strips exactly what v3 cannot represent.
func withoutV4Fields(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	out := decodeFontTestMap(t, mustJSON(t, doc))
	if header, ok := fontTestObject(t, out, "customization")["header"].(map[string]any); ok {
		delete(header, "photoPosition")
	}
	for _, rawSection := range fontTestObject(t, out, "content") {
		section, ok := rawSection.(map[string]any)
		if !ok || section["sectionType"] != "project" {
			continue
		}
		for _, entry := range fontTestArray(t, section, "entries") {
			delete(fontTestEntry(t, entry), "subtitle")
		}
	}
	return out
}

func TestV4DocumentIsValidAtV4Only(t *testing.T) {
	doc := mustJSON(t, v4Document(t))
	if err := releasedValidators[4](doc); err != nil {
		t.Fatalf("v4 document invalid at v4: %v", err)
	}
	asV3 := v4Document(t)
	asV3["schemaVersion"] = float64(3)
	if err := releasedValidators[3](mustJSON(t, asV3)); err == nil {
		t.Fatal("v3 accepted customization.header.photoPosition")
	}
}

func TestV3V4RoundTripIsExact(t *testing.T) {
	for _, name := range []string{"minimal.json", "full.json"} {
		t.Run(name, func(t *testing.T) {
			v3 := readFontV2Fixture(t, "packages", "schema", "fixtures", "v3", name)
			if err := releasedValidators[3](v3); err != nil {
				t.Fatalf("v3 fixture invalid at v3: %v", err)
			}
			v4, err := convertV3ToV4(v3)
			if err != nil {
				t.Fatalf("convert v3 to v4: %v", err)
			}
			if validateErr := releasedValidators[4](v4); validateErr != nil {
				t.Fatalf("converted document invalid at v4: %v", validateErr)
			}
			back, err := convertV4ToV3(v4)
			if err != nil {
				t.Fatalf("convert v4 to v3: %v", err)
			}
			if !bytes.Equal(normalizeJSONForFontTest(t, back), normalizeJSONForFontTest(t, v3)) {
				t.Fatal("v3 -> v4 -> v3 changed the document")
			}
			want := decodeFontTestMap(t, v3)
			want["schemaVersion"] = float64(4)
			if !bytes.Equal(normalizeJSONForFontTest(t, v4), mustJSON(t, want)) {
				t.Fatal("v3 -> v4 changed anything but schemaVersion")
			}
		})
	}
}

func TestV4ToV3DropsOnlyPhotoPositionAndProjectSubtitles(t *testing.T) {
	for _, position := range []string{"top", "left", "right"} {
		t.Run(position, func(t *testing.T) {
			doc := v4Document(t)
			header := fontTestObject(t, fontTestObject(t, doc, "customization"), "header")
			header["photoPosition"] = position
			v3, err := convertV4ToV3(mustJSON(t, doc))
			if err != nil {
				t.Fatalf("convert v4 to v3: %v", err)
			}
			if err := releasedValidators[3](v3); err != nil {
				t.Fatalf("down-converted document invalid at v3: %v", err)
			}
			want := withoutV4Fields(t, doc)
			want["schemaVersion"] = float64(3)
			if !bytes.Equal(normalizeJSONForFontTest(t, v3), mustJSON(t, want)) {
				t.Fatalf("v4 -> v3 = %s, want %s", normalizeJSONForFontTest(t, v3), mustJSON(t, want))
			}
		})
	}
}

func TestV4ToV3HandlesAbsentHeader(t *testing.T) {
	doc := v4Document(t)
	delete(fontTestObject(t, doc, "customization"), "header")
	v3, err := convertV4ToV3(mustJSON(t, doc))
	if err != nil {
		t.Fatalf("convert v4 without header: %v", err)
	}
	if err := releasedValidators[3](v3); err != nil {
		t.Fatalf("down-converted document invalid at v3: %v", err)
	}
}

func TestV3V4ConvertersRejectMalformedShapes(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"header not an object":        func(doc map[string]any) { fontTestObject(t, doc, "customization")["header"] = "nope" },
		"header null":                 func(doc map[string]any) { fontTestObject(t, doc, "customization")["header"] = nil },
		"customization not an object": func(doc map[string]any) { doc["customization"] = []any{} },
		"customization missing":       func(doc map[string]any) { delete(doc, "customization") },
		"content missing":             func(doc map[string]any) { delete(doc, "content") },
		"section not an object":       func(doc map[string]any) { fontTestObject(t, doc, "content")["projects"] = "nope" },
		"project entries not an array": func(doc map[string]any) {
			fontTestObject(t, fontTestObject(t, doc, "content"), "projects")["entries"] = "nope"
		},
		"project entry not an object": func(doc map[string]any) {
			fontTestObject(t, fontTestObject(t, doc, "content"), "projects")["entries"] = []any{"nope"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			doc := v4Document(t)
			mutate(doc)
			if _, err := convertV4ToV3(mustJSON(t, doc)); err == nil {
				t.Fatal("convertV4ToV3 accepted a malformed document")
			}
		})
	}
}

func TestProductionEmitsV3WithDeclaredV4Loss(t *testing.T) {
	doc := v4Document(t)
	emitted, err := NewIdentityProjector().EmitWire(mustJSON(t, doc), 3)
	if err != nil {
		t.Fatalf("emit v3: %v", err)
	}
	want := withoutV4Fields(t, doc)
	want["schemaVersion"] = float64(3)
	if !bytes.Equal(normalizeJSONForFontTest(t, emitted), mustJSON(t, want)) {
		t.Fatalf("emitted v3 = %s", normalizeJSONForFontTest(t, emitted))
	}
}

func TestProductionAcceptsV3AsV4WithoutNewFields(t *testing.T) {
	v3 := readFontV2Fixture(t, "packages", "schema", "fixtures", "v3", "full.json")
	accepted, version, err := NewIdentityProjector().AcceptWire(v3, 3)
	if err != nil {
		t.Fatalf("accept v3: %v", err)
	}
	if version != 4 {
		t.Fatalf("accepted version = %d, want 4", version)
	}
	want := decodeFontTestMap(t, v3)
	want["schemaVersion"] = float64(4)
	if !bytes.Equal(normalizeJSONForFontTest(t, accepted), mustJSON(t, want)) {
		t.Fatal("accepting v3 changed anything but schemaVersion")
	}
}

func TestProductionEmissionLossPolicyAllowsOnlyDeclaredV4Loss(t *testing.T) {
	doc := v4Document(t)
	current := mustJSON(t, doc)
	emittedMap := withoutV4Fields(t, doc)
	emittedMap["schemaVersion"] = float64(3)
	restored := mustJSON(t, withoutV4Fields(t, doc))

	if err := productionEmissionLossPolicy(current, mustJSON(t, emittedMap), restored, 3); err != nil {
		t.Fatalf("declared v4 loss rejected: %v", err)
	}

	// A v3 emission must keep the v3 fields; only photoPosition may go.
	lostV3 := withoutV3Fields(t, withoutV4Fields(t, doc))
	lostV3["schemaVersion"] = float64(3)
	if err := productionEmissionLossPolicy(current, mustJSON(t, lostV3), restored, 3); err == nil {
		t.Fatal("a v3 emission passed without its v3 fields")
	}

	changed := withoutV4Fields(t, doc)
	changed["schemaVersion"] = float64(3)
	fontTestObject(t, fontTestObject(t, changed, "customization"), "header")["align"] = "center"
	if err := productionEmissionLossPolicy(current, mustJSON(t, changed), restored, 3); err == nil {
		t.Fatal("a v3 emission passed with a changed header align")
	}
	lostCustom := withoutV4Fields(t, doc)
	lostCustom["schemaVersion"] = float64(3)
	customEntries := fontTestArray(t, fontTestObject(t, fontTestObject(t, lostCustom, "content"), "custom"), "entries")
	delete(fontTestEntry(t, customEntries[0]), "subtitle")
	if err := productionEmissionLossPolicy(current, mustJSON(t, lostCustom), restored, 3); err == nil {
		t.Fatal("a v3 emission passed without a custom entry subtitle")
	}
	if err := productionEmissionLossPolicy(current, current, current, 0); err == nil {
		t.Fatal("a loss passed for version 0")
	}
}

func fontTestArray(t *testing.T, parent map[string]any, key string) []any {
	t.Helper()
	array, ok := parent[key].([]any)
	if !ok {
		t.Fatalf("%s = %T, want array", key, parent[key])
	}
	return array
}

func fontTestEntry(t *testing.T, value any) map[string]any {
	t.Helper()
	entry, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("entry = %T, want object", value)
	}
	return entry
}
