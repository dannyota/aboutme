package docmigrate

import (
	"bytes"
	"testing"
)

const v6TestProjectID = "018f0000-0000-7000-8000-0000000000d1"

// v6Document returns the v5 test document at version 6 with a project link,
// the one field v5 cannot represent.
func v6Document(t *testing.T) map[string]any {
	t.Helper()
	doc := v5Document(t)
	doc["schemaVersion"] = float64(6)
	projects := fontTestObject(t, fontTestObject(t, doc, "content"), "projects")
	entries, ok := projects["entries"].([]any)
	if !ok || len(entries) == 0 {
		t.Fatal("v5 test document has no project entries")
	}
	entry, ok := entries[0].(map[string]any)
	if !ok {
		t.Fatal("v5 test project entry is not an object")
	}
	entry["projectId"] = v6TestProjectID
	return doc
}

// withoutV6Fields removes the one link v6Document adds, by its literal path,
// independently of the production converter.
func withoutV6Fields(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	out := decodeFontTestMap(t, mustJSON(t, doc))
	projects := fontTestObject(t, fontTestObject(t, out, "content"), "projects")
	entry, ok := projects["entries"].([]any)[0].(map[string]any)
	if !ok {
		t.Fatal("project test entry is not an object")
	}
	delete(entry, "projectId")
	return out
}

// TestV6ToV5KeepsEveryFieldButLinksOfTheFullFixture links the retained v5
// full fixture's project entry and expects exactly that fixture back.
func TestV6ToV5KeepsEveryFieldButLinksOfTheFullFixture(t *testing.T) {
	v5 := readFontV2Fixture(t, "packages", "schema", "fixtures", "v5", "full.json")
	linked := decodeFontTestMap(t, v5)
	linked["schemaVersion"] = float64(6)
	section := fontTestObject(t, fontTestObject(t, linked, "content"), "project")
	entries, ok := section["entries"].([]any)
	if !ok || len(entries) == 0 {
		t.Fatal("v5 full fixture has no project entries")
	}
	for index, raw := range entries {
		entry, isEntry := raw.(map[string]any)
		if !isEntry {
			t.Fatalf("project entry %d is not an object", index)
		}
		entry["projectId"] = v6TestProjectID
	}
	if err := releasedValidators[6](mustJSON(t, linked)); err != nil {
		t.Fatalf("linked full document invalid at v6: %v", err)
	}
	lowered, err := convertV6ToV5(mustJSON(t, linked))
	if err != nil {
		t.Fatalf("convert v6 to v5: %v", err)
	}
	if !bytes.Equal(normalizeJSONForFontTest(t, lowered), normalizeJSONForFontTest(t, v5)) {
		t.Fatalf("v6 -> v5 = %s, want the v5 full fixture", normalizeJSONForFontTest(t, lowered))
	}
}

func TestV6DocumentIsValidAtV6Only(t *testing.T) {
	if err := releasedValidators[6](mustJSON(t, v6Document(t))); err != nil {
		t.Fatalf("v6 document invalid at v6: %v", err)
	}
	asV5 := v6Document(t)
	asV5["schemaVersion"] = float64(5)
	if err := releasedValidators[5](mustJSON(t, asV5)); err == nil {
		t.Fatal("v5 accepted a project entry projectId")
	}
}

func TestV6ValidatorAcceptsProjectIDOnlyOnProjectEntries(t *testing.T) {
	doc := v6Document(t)
	custom := fontTestObject(t, fontTestObject(t, doc, "content"), "custom")
	entry, ok := custom["entries"].([]any)[0].(map[string]any)
	if !ok {
		t.Fatal("custom test entry is not an object")
	}
	entry["projectId"] = v6TestProjectID
	if err := releasedValidators[6](mustJSON(t, doc)); err == nil {
		t.Fatal("v6 accepted projectId on a custom entry")
	}
	for _, value := range []any{"", "project", float64(1), nil, []any{v6TestProjectID}} {
		doc := v6Document(t)
		projects := fontTestObject(t, fontTestObject(t, doc, "content"), "projects")
		projectEntry, isEntry := projects["entries"].([]any)[0].(map[string]any)
		if !isEntry {
			t.Fatal("project test entry is not an object")
		}
		projectEntry["projectId"] = value
		if err := releasedValidators[6](mustJSON(t, doc)); err == nil {
			t.Errorf("v6 accepted projectId %#v", value)
		}
	}
}

func TestV5V6RoundTripIsExact(t *testing.T) {
	for _, name := range []string{"minimal.json", "full.json"} {
		t.Run(name, func(t *testing.T) {
			v5 := readFontV2Fixture(t, "packages", "schema", "fixtures", "v5", name)
			if err := releasedValidators[5](v5); err != nil {
				t.Fatalf("v5 fixture invalid at v5: %v", err)
			}
			v6, err := convertV5ToV6(v5)
			if err != nil {
				t.Fatalf("convert v5 to v6: %v", err)
			}
			if validateErr := releasedValidators[6](v6); validateErr != nil {
				t.Fatalf("converted document invalid at v6: %v", validateErr)
			}
			back, err := convertV6ToV5(v6)
			if err != nil {
				t.Fatalf("convert v6 to v5: %v", err)
			}
			if !bytes.Equal(normalizeJSONForFontTest(t, back), normalizeJSONForFontTest(t, v5)) {
				t.Fatal("v5 -> v6 -> v5 changed the document")
			}
			want := decodeFontTestMap(t, v5)
			want["schemaVersion"] = float64(6)
			if !bytes.Equal(normalizeJSONForFontTest(t, v6), mustJSON(t, want)) {
				t.Fatal("v5 -> v6 changed anything but schemaVersion")
			}
		})
	}
}

func TestV6ToV5DropsOnlyProjectLinks(t *testing.T) {
	doc := v6Document(t)
	v5, err := convertV6ToV5(mustJSON(t, doc))
	if err != nil {
		t.Fatalf("convert v6 to v5: %v", err)
	}
	if err := releasedValidators[5](v5); err != nil {
		t.Fatalf("down-converted document invalid at v5: %v", err)
	}
	want := v5Document(t)
	if !bytes.Equal(normalizeJSONForFontTest(t, v5), mustJSON(t, want)) {
		t.Fatalf("v6 -> v5 = %s, want %s", normalizeJSONForFontTest(t, v5), mustJSON(t, want))
	}
}

func TestV5V6ConvertersRejectMalformedShapes(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"content not an object": func(doc map[string]any) { doc["content"] = []any{} },
		"content missing":       func(doc map[string]any) { delete(doc, "content") },
		"entries not an array": func(doc map[string]any) {
			fontTestObject(t, fontTestObject(t, doc, "content"), "projects")["entries"] = map[string]any{}
		},
	} {
		t.Run(name, func(t *testing.T) {
			doc := v6Document(t)
			mutate(doc)
			if _, err := convertV6ToV5(mustJSON(t, doc)); err == nil {
				t.Fatal("convertV6ToV5 accepted a malformed document")
			}
		})
	}
}

func TestProductionEmitsV5WithDeclaredV6Loss(t *testing.T) {
	doc := v6Document(t)
	emitted, err := NewIdentityProjector().EmitWire(mustJSON(t, doc), 5)
	if err != nil {
		t.Fatalf("emit v5: %v", err)
	}
	want := withoutV6Fields(t, doc)
	want["schemaVersion"] = float64(5)
	if !bytes.Equal(normalizeJSONForFontTest(t, emitted), mustJSON(t, want)) {
		t.Fatalf("emitted v5 = %s", normalizeJSONForFontTest(t, emitted))
	}
}

func TestProductionEmitsCurrentKeepingProjectLinks(t *testing.T) {
	doc := v6Document(t)
	emitted, err := NewIdentityProjector().EmitWire(mustJSON(t, doc), 6)
	if err != nil {
		t.Fatalf("emit v6: %v", err)
	}
	if !bytes.Equal(normalizeJSONForFontTest(t, emitted), mustJSON(t, doc)) {
		t.Fatalf("emitted v6 = %s", normalizeJSONForFontTest(t, emitted))
	}
}

func TestProductionAcceptsV5AsCurrentWithoutProjectLinks(t *testing.T) {
	v5 := readFontV2Fixture(t, "packages", "schema", "fixtures", "v5", "full.json")
	accepted, version, err := NewIdentityProjector().AcceptWire(v5, 5)
	if err != nil {
		t.Fatalf("accept v5: %v", err)
	}
	if version != 6 {
		t.Fatalf("accepted version = %d, want 6", version)
	}
	want := decodeFontTestMap(t, v5)
	want["schemaVersion"] = float64(6)
	if !bytes.Equal(normalizeJSONForFontTest(t, accepted), mustJSON(t, want)) {
		t.Fatal("accepting v5 changed anything but schemaVersion")
	}
}

func TestProductionEmissionLossPolicyAllowsOnlyDeclaredV6Loss(t *testing.T) {
	doc := v6Document(t)
	current := mustJSON(t, doc)
	emittedMap := withoutV6Fields(t, doc)
	emittedMap["schemaVersion"] = float64(5)
	restored := mustJSON(t, withoutV6Fields(t, doc))

	if err := productionEmissionLossPolicy(current, mustJSON(t, emittedMap), restored, 5); err != nil {
		t.Fatalf("declared v6 loss rejected: %v", err)
	}

	// A v5 emission must keep the v5 colorScheme; only project links may go.
	lostV5 := withoutV5Fields(t, withoutV6Fields(t, doc))
	lostV5["schemaVersion"] = float64(5)
	if err := productionEmissionLossPolicy(current, mustJSON(t, lostV5), restored, 5); err == nil {
		t.Fatal("a v5 emission passed without its v5 fields")
	}

	kept := decodeFontTestMap(t, current)
	kept["schemaVersion"] = float64(5)
	if err := productionEmissionLossPolicy(current, mustJSON(t, kept), restored, 5); err == nil {
		t.Fatal("a v5 emission passed while still carrying a project link")
	}

	if err := productionEmissionLossPolicy(current, current, current, 6); err == nil {
		t.Fatal("the loss policy accepted the current version as a lossy target")
	}
}
