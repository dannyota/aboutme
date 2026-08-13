package resumeapi

import (
	"os"
	"reflect"
	"sort"
	"testing"

	"gopkg.in/yaml.v3"
)

type resumeContractOperation struct {
	OperationID string                            `yaml:"operationId"`
	Responses   map[string]resumeContractResponse `yaml:"responses"`
}

type resumeContractResponse struct {
	Ref     string                             `yaml:"$ref"`
	Content map[string]resumeContractMediaType `yaml:"content"`
}

type resumeContractMediaType struct {
	Schema map[string]any `yaml:"schema"`
}

type resumeContractPath struct {
	Get    *resumeContractOperation `yaml:"get"`
	Post   *resumeContractOperation `yaml:"post"`
	Patch  *resumeContractOperation `yaml:"patch"`
	Delete *resumeContractOperation `yaml:"delete"`
}

func loadResumeContractDocument(t *testing.T) struct {
	Paths      map[string]resumeContractPath `yaml:"paths"`
	Components struct {
		Responses map[string]resumeContractResponse `yaml:"responses"`
		Schemas   map[string]map[string]any         `yaml:"schemas"`
	} `yaml:"components"`
} {
	t.Helper()
	raw, err := os.ReadFile("../../../../docs/api/openapi.yaml")
	if err != nil {
		t.Fatalf("read OpenAPI: %v", err)
	}
	var document struct {
		Paths      map[string]resumeContractPath `yaml:"paths"`
		Components struct {
			Responses map[string]resumeContractResponse `yaml:"responses"`
			Schemas   map[string]map[string]any         `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode OpenAPI: %v", err)
	}
	return document
}

func TestResumeCRUD_OpenAPIContract(t *testing.T) {
	t.Parallel()
	document := loadResumeContractDocument(t)
	operations := []struct {
		name       string
		operation  *resumeContractOperation
		wantID     string
		wantStatus []string
		success    string
		bodyless   bool
	}{
		{name: "list", operation: document.Paths["/resumes"].Get, wantID: "listResumes", wantStatus: []string{"200", "400", "401", "405", "429", "500"}, success: "200"},
		{name: "create", operation: document.Paths["/resumes"].Post, wantID: "createResume", wantStatus: []string{"201", "400", "401", "403", "405", "409", "413", "422", "429", "500"}, success: "201"},
		{name: "read", operation: document.Paths["/resumes/{id}"].Get, wantID: "getResume", wantStatus: []string{"200", "400", "401", "404", "405", "429", "500"}, success: "200"},
		{name: "metadata", operation: document.Paths["/resumes/{id}"].Patch, wantID: "updateResumeMetadata", wantStatus: []string{"200", "400", "401", "403", "404", "405", "409", "412", "413", "422", "428", "429", "500"}, success: "200"},
		{name: "delete", operation: document.Paths["/resumes/{id}"].Delete, wantID: "deleteResume", wantStatus: []string{"204", "400", "401", "403", "404", "405", "409", "412", "413", "428", "429", "500"}, success: "204", bodyless: true},
	}
	for _, test := range operations {
		t.Run(test.name, func(t *testing.T) {
			if test.operation == nil || test.operation.OperationID != test.wantID {
				t.Fatalf("operationId = %v, want %q", test.operation, test.wantID)
			}
			gotStatus := make([]string, 0, len(test.operation.Responses))
			for status := range test.operation.Responses {
				gotStatus = append(gotStatus, status)
			}
			sort.Strings(gotStatus)
			wantStatus := append([]string(nil), test.wantStatus...)
			sort.Strings(wantStatus)
			if !reflect.DeepEqual(gotStatus, wantStatus) {
				t.Fatalf("statuses = %v, want %v", gotStatus, wantStatus)
			}
			response := resolvedResumeContractResponse(t, document.Components.Responses, test.operation.Responses[test.success])
			_, hasJSON := response.Content["application/json"]
			if hasJSON == test.bodyless {
				t.Fatalf("success application/json present = %v, bodyless = %v", hasJSON, test.bodyless)
			}
			for status, declared := range test.operation.Responses {
				if status == test.success {
					continue
				}
				resolved := resolvedResumeContractResponse(t, document.Components.Responses, declared)
				if _, ok := resolved.Content["application/json"]; !ok {
					t.Fatalf("status %s has no application/json error envelope", status)
				}
			}
		})
	}
}

func resolvedResumeContractResponse(t *testing.T, components map[string]resumeContractResponse,
	response resumeContractResponse,
) resumeContractResponse {
	t.Helper()
	if response.Ref == "" {
		return response
	}
	const prefix = "#/components/responses/"
	if len(response.Ref) <= len(prefix) || response.Ref[:len(prefix)] != prefix {
		t.Fatalf("unsupported response ref %q", response.Ref)
	}
	resolved, ok := components[response.Ref[len(prefix):]]
	if !ok {
		t.Fatalf("missing response component %q", response.Ref)
	}
	return resolvedResumeContractResponse(t, components, resolved)
}
