package schema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func verifiedExample(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "testdata", "examples", "verified.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func field(t *testing.T, doc map[string]any, path ...string) (map[string]any, string) {
	t.Helper()
	node := doc
	for _, key := range path[:len(path)-1] {
		switch next := node[key].(type) {
		case map[string]any:
			node = next
		case []any:
			first, ok := next[0].(map[string]any)
			if !ok {
				t.Fatalf("%v: not an object list", path)
			}
			node = first
		default:
			t.Fatalf("%v: no %s", path, key)
		}
	}
	return node, path[len(path)-1]
}

func withOwner(t *testing.T, doc map[string]any, owner string, path ...string) {
	t.Helper()
	node, key := field(t, doc, path...)
	value, ok := node[key].(string)
	if !ok || !strings.Contains(value, "dannyota") {
		t.Fatalf("%v holds no dannyota value: %v", path, node[key])
	}
	node[key] = strings.Replace(value, "dannyota", owner, 1)
}

func validate(t *testing.T, doc map[string]any) error {
	t.Helper()
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return Validate(b)
}

var ownedFields = map[string][]string{
	"source_repository": {"source_repository"},
	"image":             {"components", "image"},
	"signer_workflow":   {"components", "running_images", "signature", "signer_workflow"},
	"commit link":       {"components", "running_images", "links", "commit"},
	"release link":      {"components", "running_images", "links", "release"},
	"build link":        {"components", "running_images", "links", "build"},
	"provenance link":   {"components", "running_images", "links", "provenance"},
	"sbom link":         {"components", "running_images", "links", "sbom"},
}

func TestBothOwnersValidateInEveryField(t *testing.T) {
	for _, owner := range []string{"dannyota", "dntmakers"} {
		b, err := os.ReadFile(filepath.Join("..", "testdata", "examples", "verified.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := Validate([]byte(strings.ReplaceAll(string(b), "dannyota", owner))); err != nil {
			t.Errorf("%s document: %v", owner, err)
		}
	}
	for name, path := range ownedFields {
		t.Run(name, func(t *testing.T) {
			doc := verifiedExample(t)
			withOwner(t, doc, "dntmakers", path...)
			if err := validate(t, doc); err != nil {
				t.Errorf("a dntmakers %s alone did not validate: %v", name, err)
			}
		})
	}
}

func TestOtherOwnersAreRefusedInEveryField(t *testing.T) {
	for name, path := range ownedFields {
		for _, owner := range []string{"someone", "dannyota-x", "Dntmakers"} {
			t.Run(name+" "+owner, func(t *testing.T) {
				doc := verifiedExample(t)
				withOwner(t, doc, owner, path...)
				if validate(t, doc) == nil {
					t.Errorf("accepted %s %q", name, owner)
				}
			})
		}
	}
}

func TestImageMustNameItsComponent(t *testing.T) {
	for _, image := range []string{"ghcr.io/dntmakers/aboutme-web", "ghcr.io/dannyota/aboutme-caddy", "ghcr.io/dntmakers/aboutme-observer"} {
		doc := verifiedExample(t)
		node, key := field(t, doc, "components", "image")
		node[key] = image
		if validate(t, doc) == nil {
			t.Errorf("the server component accepted %s", image)
		}
	}
}
