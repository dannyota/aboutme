package resumeapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestSectionPatchMetadataOrderAndPlacementIsolation(t *testing.T) {
	t.Parallel()
	raw := entryTestDocument(t)
	before := decodeEntryTestDocument(t, raw)
	beforeCustomization, err := json.Marshal(before.Customization)
	if err != nil {
		t.Fatalf("marshal customization: %v", err)
	}

	display := "Experience"
	icon := "briefcase"
	changed, err := applySectionPatch(raw, "work", sectionPatch{
		DisplayName: optionalString{Present: true, Value: &display},
		IconKey:     optionalString{Present: true, Value: &icon},
		EntryOrder:  []string{"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61", "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60"},
		HasOrder:    true,
	})
	if err != nil {
		t.Fatalf("patch section: %v", err)
	}
	doc := decodeEntryTestDocument(t, changed)
	section := doc.Content["work"]
	if section.DisplayName == nil || *section.DisplayName != display || section.IconKey == nil || *section.IconKey != icon {
		t.Fatalf("metadata = display %v icon %v", section.DisplayName, section.IconKey)
	}
	if got := section.WorkEntries; got[0].ID != "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61" || got[1].ID != "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60" {
		t.Fatalf("entry order = %#v", got)
	}
	afterCustomization, marshalErr := json.Marshal(doc.Customization)
	if marshalErr != nil {
		t.Fatalf("marshal changed customization: %v", marshalErr)
	}
	if !bytes.Equal(beforeCustomization, afterCustomization) {
		t.Fatalf("customization changed:\nbefore %s\nafter  %s", beforeCustomization, afterCustomization)
	}
	preserved, err := applySectionPatch(changed, "work", sectionPatch{
		EntryOrder: []string{"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60", "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61"},
		HasOrder:   true,
	})
	if err != nil {
		t.Fatalf("order-only patch: %v", err)
	}
	preservedSection := decodeEntryTestDocument(t, preserved).Content["work"]
	if preservedSection.DisplayName == nil || *preservedSection.DisplayName != display || preservedSection.IconKey == nil || *preservedSection.IconKey != icon {
		t.Fatalf("absent metadata fields did not preserve values: display %v icon %v", preservedSection.DisplayName, preservedSection.IconKey)
	}

	empty := ""
	cleared, err := applySectionPatch(changed, "work", sectionPatch{
		DisplayName: optionalString{Present: true, Value: &empty},
		IconKey:     optionalString{Present: true, Value: nil},
	})
	if err != nil {
		t.Fatalf("clear metadata: %v", err)
	}
	section = decodeEntryTestDocument(t, cleared).Content["work"]
	if section.DisplayName == nil || *section.DisplayName != "" || section.IconKey != nil {
		t.Fatalf("cleared metadata = display %v icon %v", section.DisplayName, section.IconKey)
	}
}

func TestSectionRouteContract(t *testing.T) {
	for _, route := range sectionRoutes() {
		if route.Handler == nil {
			t.Fatalf("%s %s has no handler", route.Method, route.Pattern)
		}
	}
	h := newResumeAPITestHarness(t)
	created := createEntryContractResume(t, h)
	path := fmt.Sprintf("/api/v1/resumes/%s/sections/work", created.ID)
	response := h.mutationRequest(t, http.MethodPatch, path,
		bytes.NewBufferString(`{"displayName":"","iconKey":null,"entryOrder":["01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60"]}`),
		created.Revision, uuid.NewString())
	if response.status != http.StatusOK {
		t.Fatalf("section patch status = %d body=%s", response.status, response.body)
	}
	revision, document := decodedWrittenDocument(t, response)
	section := document.Content["work"]
	if revision != "2" || section.DisplayName == nil || *section.DisplayName != "" || section.IconKey != nil {
		t.Fatalf("section response revision=%q display=%v icon=%v", revision, section.DisplayName, section.IconKey)
	}
}

func TestSectionPatchRejectsNonPermutation(t *testing.T) {
	t.Parallel()
	raw := entryTestDocument(t)
	for name, order := range map[string][]string{
		"drop":      {"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60"},
		"add":       {"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60", "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61", "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e99"},
		"duplicate": {"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60", "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := applySectionPatch(raw, "work", sectionPatch{EntryOrder: order, HasOrder: true}); err == nil {
				t.Fatalf("order %v accepted", order)
			}
		})
	}
}
