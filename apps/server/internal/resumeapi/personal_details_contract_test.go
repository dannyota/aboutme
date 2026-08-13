package resumeapi

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"sort"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestPersonalDetails_OpenAPIContract(t *testing.T) {
	t.Parallel()
	document := loadResumeContractDocument(t)
	operation := document.Paths["/resumes/{id}/personal-details"].Patch
	if operation == nil || operation.OperationID != "updateResumePersonalDetails" {
		t.Fatalf("operation = %#v, want updateResumePersonalDetails", operation)
	}
	wantStatuses := []string{"200", "400", "401", "403", "404", "405", "409", "412", "413", "422", "428", "429", "500"}
	gotStatuses := make([]string, 0, len(operation.Responses))
	for status := range operation.Responses {
		gotStatuses = append(gotStatuses, status)
	}
	sort.Strings(gotStatuses)
	if !reflect.DeepEqual(gotStatuses, wantStatuses) {
		t.Fatalf("statuses = %v, want %v", gotStatuses, wantStatuses)
	}
	for status, response := range operation.Responses {
		resolved := resolvedResumeContractResponse(t, document.Components.Responses, response)
		if _, ok := resolved.Content["application/json"]; !ok {
			t.Fatalf("status %s has no application/json envelope", status)
		}
	}

	schemaMap := document.Components.Schemas["PersonalDetailsPatch"]
	properties, ok := schemaMap["properties"].(map[string]any)
	if !ok {
		t.Fatalf("PersonalDetailsPatch properties = %#v", schemaMap["properties"])
	}
	gotNames := make([]string, 0, len(properties))
	for name := range properties {
		gotNames = append(gotNames, name)
	}
	sort.Strings(gotNames)
	wantNames := []string{"details", "fullName", "headline"}
	if !reflect.DeepEqual(gotNames, wantNames) || schemaMap["additionalProperties"] != false {
		t.Fatalf("PersonalDetailsPatch names=%v additionalProperties=%v, want closed %v",
			gotNames, schemaMap["additionalProperties"], wantNames)
	}

	compiled := compileResumeContractSchema(t, schemaMap)
	for _, accepted := range []string{`{}`, `{"fullName":"Ada"}`, `{"details":[]}`} {
		if err := validateResumeContractJSON(compiled, accepted); err != nil {
			t.Errorf("PersonalDetailsPatch rejected %s: %v", accepted, err)
		}
	}
	for _, rejected := range []string{
		`{"photo":{"key":"server-owned"}}`,
		`{"crop":{"x":0,"y":0,"width":1,"height":1}}`,
		`{"unknown":true}`,
	} {
		if err := validateResumeContractJSON(compiled, rejected); err == nil {
			t.Errorf("PersonalDetailsPatch accepted forbidden payload %s", rejected)
		}
	}
}

func compileResumeContractSchema(t *testing.T, schemaMap map[string]any) *jsonschema.Schema {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	const location = "https://aboutme.test/openapi/personal-details.schema.json"
	if err := compiler.AddResource(location, schemaMap); err != nil {
		t.Fatalf("add PersonalDetailsPatch schema: %v", err)
	}
	compiled, err := compiler.Compile(location)
	if err != nil {
		t.Fatalf("compile PersonalDetailsPatch schema: %v", err)
	}
	return compiled
}

func validateResumeContractJSON(compiled *jsonschema.Schema, raw string) error {
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return err
	}
	return compiled.Validate(value)
}
