package docmigrate

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

type fontCatalogFixture struct {
	Entries []struct {
		ID       string `json:"id"`
		V1Family string `json:"v1Family"`
	} `json:"entries"`
}

func readFontV2Fixture(t *testing.T, path ...string) []byte {
	t.Helper()
	parts := append([]string{"..", "..", "..", "..", ".."}, path...)
	raw, err := os.ReadFile(filepath.Join(parts...))
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Join(path...), err)
	}
	return raw
}

func fontV2Document(t *testing.T, family string) json.RawMessage {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(readFontV2Fixture(t, "packages", "schema", "fixtures", "minimal.json"), &doc); err != nil {
		t.Fatalf("decode minimal fixture: %v", err)
	}
	doc["schemaVersion"] = float64(2)
	customization, ok := doc["customization"].(map[string]any)
	if !ok {
		t.Fatal("minimal fixture customization is not an object")
	}
	font, ok := customization["font"].(map[string]any)
	if !ok {
		t.Fatal("minimal fixture customization.font is not an object")
	}
	font["family"] = family
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode v2 fixture: %v", err)
	}
	return raw
}

func fontFamilyOf(t *testing.T, doc json.RawMessage) string {
	t.Helper()
	var shape struct {
		Customization struct {
			Font struct {
				Family string `json:"family"`
			} `json:"font"`
		} `json:"customization"`
	}
	if err := json.Unmarshal(doc, &shape); err != nil {
		t.Fatalf("decode converted document: %v", err)
	}
	return shape.Customization.Font.Family
}

func TestProductionV1V2FontConvertersCoverCatalog(t *testing.T) {
	if CurrentVersion != 2 || schema.CurrentVersion != 2 {
		t.Fatalf("current versions = docmigrate %d, schema %d; want 2", CurrentVersion, schema.CurrentVersion)
	}

	var catalog fontCatalogFixture
	if err := json.Unmarshal(readFontV2Fixture(t, "apps", "web", "app", "assets", "fonts", "catalog.json"), &catalog); err != nil {
		t.Fatalf("decode font catalog: %v", err)
	}
	if len(catalog.Entries) != 26 {
		t.Fatalf("catalog entries = %d, want 26", len(catalog.Entries))
	}

	projector := NewIdentityProjector()
	for _, entry := range catalog.Entries {
		t.Run(entry.ID, func(t *testing.T) {
			current := fontV2Document(t, entry.ID)
			emitted, err := projector.EmitWire(current, 1)
			if err != nil {
				t.Fatalf("EmitWire(v2->v1): %v", err)
			}
			if got := fontFamilyOf(t, emitted); got != entry.V1Family {
				t.Fatalf("v1 family = %q, want %q", got, entry.V1Family)
			}

			var version struct {
				SchemaVersion int32 `json:"schemaVersion"`
			}
			if decodeErr := json.Unmarshal(emitted, &version); decodeErr != nil {
				t.Fatalf("decode emitted version: %v", decodeErr)
			}
			if version.SchemaVersion != 1 {
				t.Fatalf("emitted schemaVersion = %d, want 1", version.SchemaVersion)
			}
			if !bytes.Equal(withoutVersionAndFont(t, current), withoutVersionAndFont(t, emitted)) {
				t.Fatal("v2->v1 changed a non-font JSON value")
			}

			accepted, acceptedVersion, err := projector.AcceptWire(emitted, 1)
			if err != nil {
				t.Fatalf("AcceptWire(v1->v2): %v", err)
			}
			if acceptedVersion != 2 {
				t.Fatalf("accepted version = %d, want 2", acceptedVersion)
			}
			if entry.ID == "be-vietnam-pro" || entry.ID == "inter" || entry.ID == "source-sans-3" || entry.ID == "alegreya" || entry.ID == "roboto-serif" {
				if !bytes.Equal(normalizeJSONForFontTest(t, current), normalizeJSONForFontTest(t, accepted)) {
					t.Fatal("original v1 family did not round-trip exactly")
				}
			}
		})
	}
}

func withoutVersionAndFont(t *testing.T, raw json.RawMessage) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode document: %v", err)
	}
	delete(doc, "schemaVersion")
	customization := fontTestObject(t, doc, "customization")
	font := fontTestObject(t, customization, "font")
	delete(font, "family")
	return mustJSON(t, doc)
}

func TestV1V2ConvertersRejectUnknownFontMappings(t *testing.T) {
	v1 := readFontV2Fixture(t, "packages", "schema", "fixtures", "v1", "minimal.json")
	var v1Doc map[string]any
	if err := json.Unmarshal(v1, &v1Doc); err != nil {
		t.Fatalf("decode v1 fixture: %v", err)
	}
	customization := fontTestObject(t, v1Doc, "customization")
	fontTestObject(t, customization, "font")["family"] = "Unknown V1 Family"
	v1Unknown := mustJSON(t, v1Doc)
	if _, err := convertV1ToV2(v1Unknown); err == nil {
		t.Fatal("convertV1ToV2 accepted a family with no mapping")
	}

	if _, err := convertV2ToV1(fontV2Document(t, "unknown-v2-id")); err == nil {
		t.Fatal("convertV2ToV1 accepted a catalog ID with no mapping")
	}
}

func TestProductionEmissionLossPolicyAllowsOnlyDeclaredFontFallback(t *testing.T) {
	current := fontV2Document(t, "noto-sans")
	emittedMap := decodeFontTestMap(t, current)
	emittedMap["schemaVersion"] = float64(1)
	emittedCustomization := fontTestObject(t, emittedMap, "customization")
	fontTestObject(t, emittedCustomization, "font")["family"] = "Inter"
	emitted := mustJSON(t, emittedMap)
	restoredMap := decodeFontTestMap(t, current)
	restoredCustomization := fontTestObject(t, restoredMap, "customization")
	fontTestObject(t, restoredCustomization, "font")["family"] = "inter"
	restored := mustJSON(t, restoredMap)

	if err := productionEmissionLossPolicy(current, emitted, restored, 1); err != nil {
		t.Fatalf("declared font fallback rejected: %v", err)
	}

	fontTestObject(t, restoredMap, "personalDetails")["fullName"] = "Changed"
	if err := productionEmissionLossPolicy(current, emitted, mustJSON(t, restoredMap), 1); err == nil {
		t.Fatal("non-font change passed the production emission policy")
	}
	if err := productionEmissionLossPolicy(current, emitted, restored, 2); err == nil {
		t.Fatal("font fallback passed for a target other than v1")
	}
}

func decodeFontTestMap(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	return value
}

func fontTestObject(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("%s is not an object", key)
	}
	return value
}

func normalizeJSONForFontTest(t *testing.T, raw json.RawMessage) []byte {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	out, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("normalize JSON: %v", err)
	}
	return out
}

func TestProductionVersionDeclarationsAreDefensiveCopies(t *testing.T) {
	accepted := AcceptedVersions()
	emitted := EmittedVersions()
	if !bytes.Equal(mustJSON(t, accepted), []byte("[1,2]")) {
		t.Fatalf("accepted versions = %v, want [1 2]", accepted)
	}
	if !bytes.Equal(mustJSON(t, emitted), []byte("[1,2]")) {
		t.Fatalf("emitted versions = %v, want [1 2]", emitted)
	}
	accepted[0] = 99
	emitted[0] = 99
	if AcceptedVersions()[0] != 1 || EmittedVersions()[0] != 1 {
		t.Fatal("production version declaration escaped by reference")
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return raw
}
