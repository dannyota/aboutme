package mcpapi

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestProjectTools_RegistryListsTheResumeAndProjectToolsExactly(t *testing.T) {
	h := newProjectToolHarness(t, projectToolOldGrant)
	session := startProjectToolServer(t, h)
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := make([]string, len(listed.Tools))
	for i, tool := range listed.Tools {
		names[i] = tool.Name
	}
	sort.Strings(names)
	want := append([]string{
		"create_resume", "delete_entry", "delete_photo", "delete_resume", "get_photo",
		"get_resume", "list_resumes", "update_customization", "update_personal_details",
		"update_photo_crop", "update_resume_metadata", "update_section", "update_structure",
		"upload_photo", "upsert_entry",
	}, projectToolNames...)
	sort.Strings(want)
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("tool names = %v, want %v", names, want)
	}
}

func TestProjectTools_NoToolPublishesAttachesEvidenceOrConnectsAnAccount(t *testing.T) {
	h := newProjectToolHarness(t, projectToolOldGrant)
	session := startProjectToolServer(t, h)
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	forbidden := []string{"publish", "evidence", "account", "connect", "github", "oauth"}
	for _, tool := range listed.Tools {
		for _, word := range forbidden {
			if strings.Contains(strings.ToLower(tool.Name), word) {
				t.Errorf("tool %q is named for %q", tool.Name, word)
			}
		}
		raw, encodeErr := json.Marshal(tool.InputSchema)
		if encodeErr != nil {
			t.Fatalf("encode %s input schema: %v", tool.Name, encodeErr)
		}
		var schema any
		if decodeErr := json.Unmarshal(raw, &schema); decodeErr != nil {
			t.Fatalf("decode %s input schema: %v", tool.Name, decodeErr)
		}
		for _, property := range projectToolSchemaPropertyNames(schema) {
			for _, word := range forbidden {
				if strings.Contains(strings.ToLower(property), word) {
					t.Errorf("tool %q input property %q is named for %q", tool.Name, property, word)
				}
			}
		}
	}
}

func projectToolSchemaPropertyNames(node any) []string {
	var names []string
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			if properties, ok := child.(map[string]any); ok && key == "properties" {
				for name := range properties {
					names = append(names, name)
				}
			}
			names = append(names, projectToolSchemaPropertyNames(child)...)
		}
	case []any:
		for _, child := range value {
			names = append(names, projectToolSchemaPropertyNames(child)...)
		}
	}
	return names
}
