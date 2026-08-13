package resumeapi

import (
	"os"
	"reflect"
	"sort"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCustomizationOpenAPIContract(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../../../docs/api/openapi.yaml")
	if err != nil {
		t.Fatalf("read OpenAPI: %v", err)
	}
	type operationVariant struct {
		Required             []string       `yaml:"required"`
		AdditionalProperties bool           `yaml:"additionalProperties"`
		Properties           map[string]any `yaml:"properties"`
	}
	type deltaSchema struct {
		OneOf []operationVariant `yaml:"oneOf"`
	}
	type response struct {
		Ref string `yaml:"$ref"`
	}
	type operation struct {
		OperationID string              `yaml:"operationId"`
		Responses   map[string]response `yaml:"responses"`
	}
	var document struct {
		Paths map[string]struct {
			Patch operation `yaml:"patch"`
		} `yaml:"paths"`
		Components struct {
			Schemas map[string]deltaSchema `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode OpenAPI: %v", err)
	}

	patch := document.Paths["/resumes/{id}/customization"].Patch
	if patch.OperationID != "updateResumeCustomization" {
		t.Fatalf("operationId = %q", patch.OperationID)
	}
	wantResponses := []string{"200", "400", "401", "403", "404", "405", "409", "412", "413", "422", "428", "429", "500"}
	gotResponses := make([]string, 0, len(patch.Responses))
	for status := range patch.Responses {
		gotResponses = append(gotResponses, status)
	}
	sort.Strings(gotResponses)
	if !reflect.DeepEqual(gotResponses, wantResponses) {
		t.Fatalf("response statuses = %v, want %v", gotResponses, wantResponses)
	}
	if patch.Responses["422"].Ref != "#/components/responses/CustomizationRejected" {
		t.Fatalf("422 response = %q", patch.Responses["422"].Ref)
	}

	variants := document.Components.Schemas["CustomizationDelta"].OneOf
	if len(variants) != 2 {
		t.Fatalf("CustomizationDelta oneOf variants = %d, want 2", len(variants))
	}
	gotRequired := make([][]string, len(variants))
	for i, variant := range variants {
		gotRequired[i] = append([]string(nil), variant.Required...)
		sort.Strings(gotRequired[i])
		if variant.AdditionalProperties {
			t.Fatalf("variant %d permits additional properties", i)
		}
	}
	wantRequired := [][]string{{"op", "path", "value"}, {"op", "path"}}
	for i := range wantRequired {
		sort.Strings(wantRequired[i])
	}
	if !reflect.DeepEqual(gotRequired, wantRequired) {
		t.Fatalf("variant required sets = %v, want %v", gotRequired, wantRequired)
	}
}
