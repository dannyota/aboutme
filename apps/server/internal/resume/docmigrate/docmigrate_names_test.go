package docmigrate

import (
	"errors"
	"testing"
)

// TestAcceptWire_RejectsCaseVariantDocumentKeys pins that the wire boundary
// matches resume document property names exactly at every level: the closed
// JSON schema rejects a case variant before any typed decode can fold it.
func TestAcceptWire_RejectsCaseVariantDocumentKeys(t *testing.T) {
	t.Parallel()

	rename := func(object map[string]any, from, to string) {
		value, ok := object[from]
		if !ok {
			t.Fatalf("test document has no %q", from)
		}
		delete(object, from)
		object[to] = value
	}
	for name, vary := range map[string]func(map[string]any){
		"top level": func(doc map[string]any) { rename(doc, "personalDetails", "PersonalDetails") },
		"personal details": func(doc map[string]any) {
			rename(fontTestObject(t, doc, "personalDetails"), "fullName", "FullName")
		},
		"section": func(doc map[string]any) {
			rename(fontTestObject(t, fontTestObject(t, doc, "content"), "projects"), "sectionType", "SectionType")
		},
		"entry": func(doc map[string]any) {
			projects := fontTestObject(t, fontTestObject(t, doc, "content"), "projects")
			entry, ok := projects["entries"].([]any)[0].(map[string]any)
			if !ok {
				t.Fatal("project test entry is not an object")
			}
			rename(entry, "projectId", "projectID")
		},
		"customization": func(doc map[string]any) {
			rename(fontTestObject(t, doc, "customization"), "colors", "Colors")
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := v6Document(t)
			vary(doc)
			_, _, err := NewIdentityProjector().AcceptWire(mustJSON(t, doc), 6)
			if !errors.Is(err, ErrInvalidDocument) {
				t.Fatalf("AcceptWire accepted a case-variant key: %v", err)
			}
		})
	}
	if _, _, err := NewIdentityProjector().AcceptWire(mustJSON(t, v6Document(t)), 6); err != nil {
		t.Fatalf("AcceptWire rejected the exact document: %v", err)
	}
}
