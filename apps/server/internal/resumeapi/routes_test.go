package resumeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
)

func TestRouteInventoryIsCompleteAndImplemented(t *testing.T) {
	t.Parallel()

	routes := registeredRoutes()
	if len(routes) != 16 {
		t.Fatalf("registered route count = %d, want 16", len(routes))
	}
	mutations := 0
	var got []string
	for _, route := range routes {
		got = append(got, route.Method+" "+route.Pattern)
		if route.Mutation {
			mutations++
		}
	}
	if mutations != 13 {
		t.Fatalf("mutation route count = %d, want 13", mutations)
	}
	sort.Strings(got)
	want := []string{
		http.MethodGet + " /api/v1/resumes",
		http.MethodPost + " /api/v1/resumes",
		http.MethodPost + " /api/v1/resumes/{id}/publish",
		http.MethodDelete + " /api/v1/resumes/{id}",
		http.MethodGet + " /api/v1/resumes/{id}",
		http.MethodPatch + " /api/v1/resumes/{id}",
		http.MethodDelete + " /api/v1/resumes/{id}/entries/{sectionKey}/{entryId}",
		http.MethodPatch + " /api/v1/resumes/{id}/entries/{sectionKey}",
		http.MethodPatch + " /api/v1/resumes/{id}/sections/{sectionKey}",
		http.MethodPatch + " /api/v1/resumes/{id}/structure",
		http.MethodPatch + " /api/v1/resumes/{id}/personal-details",
		http.MethodPatch + " /api/v1/resumes/{id}/customization",
		http.MethodDelete + " /api/v1/resumes/{id}/photo",
		http.MethodGet + " /api/v1/resumes/{id}/photo",
		http.MethodPatch + " /api/v1/resumes/{id}/photo",
		http.MethodPost + " /api/v1/resumes/{id}/photo",
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("routes = %v, want %v", got, want)
	}
}

func TestRecordPhotoKeyInvariantEmitsOnlySignalAndRequestID(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	calls := 0
	service := &Service{
		logger:            slog.New(slog.NewTextHandler(&logs, nil)),
		photoKeyInvariant: func() { calls++ },
	}
	service.recordPhotoKeyInvariant(context.Background())
	if calls != 1 {
		t.Fatalf("metric calls = %d, want 1", calls)
	}
	if strings.Contains(logs.String(), "resumes/") || strings.Contains(logs.String(), "photo-") {
		t.Fatalf("invariant log contains an object key: %s", logs.String())
	}
}

func TestEveryMutationRegistersExactlyOneConcreteOperation(t *testing.T) {
	t.Parallel()

	want := map[string]operationKind{
		"createResume":                operationCreate,
		"updateResumeMetadata":        operationMetadata,
		"deleteResume":                operationDelete,
		"publishResume":               operationPublish,
		"upsertResumeEntry":           operationAggregate,
		"deleteResumeEntry":           operationAggregate,
		"updateResumeSection":         operationAggregate,
		"updateResumeStructure":       operationAggregate,
		"updateResumePersonalDetails": operationAggregate,
		"updateResumeCustomization":   operationAggregate,
		"uploadResumePhoto":           operationPhotoCandidate,
		"updateResumePhotoCrop":       operationAggregate,
		"deleteResumePhoto":           operationAggregate,
	}
	for _, route := range registeredRoutes() {
		kind, isMutation := want[route.Operation]
		if !route.Mutation {
			if route.OperationKind != operationNone || route.OperationKind.build(&Service{}) != nil {
				t.Errorf("read %s registers operation kind %v", route.Operation, route.OperationKind)
			}
			continue
		}
		if !isMutation {
			t.Errorf("mutation %s missing from expected operation map", route.Operation)
			continue
		}
		if route.OperationKind != kind {
			t.Errorf("%s operation kind = %v, want %v", route.Operation, route.OperationKind, kind)
		}
		built := route.OperationKind.build(&Service{})
		if built == nil {
			t.Errorf("%s operation factory returned nil", route.Operation)
		}
		if route.Operation == "updateResumeMetadata" {
			if _, ok := built.(resumeMetadataMutation); !ok {
				t.Errorf("%s operation factory = %T, want resumeMetadataMutation", route.Operation, built)
			}
		}
	}
}

func TestRouteWireVersionPolicyIsComplete(t *testing.T) {
	t.Parallel()

	for _, route := range registeredRoutes() {
		wantAccepts := route.Mutation || route.Operation == "listResumes" || route.Operation == "getResume"
		if route.AcceptsWireVersion != wantAccepts {
			t.Errorf("%s AcceptsWireVersion = %v, want %v", route.Operation, route.AcceptsWireVersion, wantAccepts)
		}
		wantEmits := route.Operation != "getResumePhoto" && route.Operation != "deleteResume"
		if route.EmitsWireVersion != wantEmits {
			t.Errorf("%s EmitsWireVersion = %v, want %v", route.Operation, route.EmitsWireVersion, wantEmits)
		}
	}
}

func TestRouteInventoryEqualsOpenAPI(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../../../docs/api/openapi.yaml")
	if err != nil {
		t.Fatalf("read OpenAPI: %v", err)
	}
	if bytes.Contains(raw, []byte("not_implemented")) {
		t.Fatal("OpenAPI declares the construction-only sentinel")
	}
	type operation struct {
		OperationID string `yaml:"operationId"`
	}
	type pathItem struct {
		Get    *operation `yaml:"get"`
		Post   *operation `yaml:"post"`
		Patch  *operation `yaml:"patch"`
		Delete *operation `yaml:"delete"`
	}
	var document struct {
		Paths map[string]pathItem `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode OpenAPI: %v", err)
	}
	var declared []string
	for path, operations := range document.Paths {
		if !strings.HasPrefix(path, "/resumes") {
			continue
		}
		for method, operation := range map[string]*operation{
			http.MethodGet: operations.Get, http.MethodPost: operations.Post,
			http.MethodPatch: operations.Patch, http.MethodDelete: operations.Delete,
		} {
			if operation != nil {
				declared = append(declared, method+" /api/v1"+path)
			}
		}
	}
	var registered []string
	for _, route := range registeredRoutes() {
		registered = append(registered, route.Method+" "+route.Pattern)
	}
	sort.Strings(declared)
	sort.Strings(registered)
	if !reflect.DeepEqual(registered, declared) {
		t.Fatalf("registered routes = %v, OpenAPI routes = %v", registered, declared)
	}
}

func TestNoRouteAnswers501(t *testing.T) {
	h := newResumeAPITestHarness(t)

	for _, route := range registeredRoutes() {
		route := route
		path := concreteRoutePath(route.Pattern)
		t.Run(route.Method+" "+path, func(t *testing.T) {
			unauthenticated := h.request(t, route.Method, path, nil, false, false)
			assertRouteError(t, unauthenticated, http.StatusUnauthorized, "session_required")

			if route.Mutation {
				var body io.Reader
				if route.Upload {
					body = strings.NewReader("--test--\r\n")
				} else if route.Method != http.MethodDelete {
					body = strings.NewReader(`{}`)
				}
				missingToken := buildHarnessRequest(t, h, route, path, body, true, false)
				assertRouteError(t, missingToken, http.StatusForbidden, "csrf_rejected")
			}

			var body io.Reader
			if route.Upload {
				body = strings.NewReader("--test--\r\n")
			} else if route.Mutation && route.Method != http.MethodDelete {
				body = strings.NewReader(`{}`)
			}
			response := buildHarnessRequest(t, h, route, path, body, true, route.Mutation)
			if response.status == http.StatusNotImplemented || bytes.Contains(response.body, []byte("not_implemented")) {
				t.Fatalf("implemented route returned a construction sentinel: %d %s", response.status, response.body)
			}
		})
	}

	methodNotAllowed := h.request(t, http.MethodPut, apiResumePath, nil, false, false)
	assertRouteError(t, methodNotAllowed, http.StatusMethodNotAllowed, "method_not_allowed")
}

func buildHarnessRequest(t *testing.T, h *resumeAPITestHarness, route routeSpec, path string, body io.Reader,
	authenticated, csrf bool,
) testHTTPResponse {
	t.Helper()
	req, err := http.NewRequestWithContext(h.ctx, route.Method, h.server.URL+path, body)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if authenticated {
		req.AddCookie(h.cookie)
	}
	if route.Mutation {
		req.Header.Set("Origin", resumeAPITestOrigin)
		if csrf {
			req.Header.Set(auth.CSRFHeaderName, h.csrfToken)
		}
		req.Header.Set("Idempotency-Key", uuid.NewString())
		if route.Operation != "createResume" {
			req.Header.Set("If-Match", `"r1"`)
		}
		if route.Upload {
			req.Header.Set("Content-Type", "multipart/form-data; boundary=test")
		} else if route.Method != http.MethodDelete {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	return snapshotHTTPResponse(t, resp)
}

func assertRouteError(t *testing.T, response testHTTPResponse, status int, code string) {
	t.Helper()
	if response.status != status {
		t.Fatalf("status = %d, want %d (body=%s)", response.status, status, response.body)
	}
	var envelope errorEnvelope
	if err := json.Unmarshal(response.body, &envelope); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if envelope.Error.Code != code {
		t.Fatalf("error code = %q, want %q", envelope.Error.Code, code)
	}
}

func TestMutationHeaderContract_AllRoutes(t *testing.T) {
	h := newResumeAPITestHarness(t)
	beforeResumes := h.snapshotUserTable(t, "resumes")
	beforeIdempotency := h.snapshotUserTable(t, "idempotency_records")

	for _, route := range registeredRoutes() {
		if !route.Mutation {
			continue
		}
		path := concreteRoutePath(route.Pattern)
		for _, tc := range []struct {
			name       string
			key        []string
			match      []string
			version    []string
			wantStatus int
			wantCode   string
		}{
			{"missing key", nil, nil, nil, http.StatusBadRequest, "idempotency_key_required"},
			{"invalid key", []string{"not-a-uuid"}, nil, nil, http.StatusBadRequest, "idempotency_key_invalid"},
			{"duplicate key", []string{uuid.NewString(), uuid.NewString()}, nil, nil, http.StatusBadRequest, "idempotency_key_invalid"},
			{"folded key", []string{uuid.NewString() + ", " + uuid.NewString()}, nil, nil, http.StatusBadRequest, "idempotency_key_invalid"},
		} {
			t.Run(route.Operation+"/"+tc.name, func(t *testing.T) {
				response := mutationHeaderRequest(t, h, route, path, tc.key, tc.match, tc.version)
				assertRouteError(t, response, tc.wantStatus, tc.wantCode)
			})
		}

		validKey := []string{uuid.NewString()}
		if route.Operation == "createResume" {
			for _, test := range []struct {
				name  string
				match []string
			}{
				{name: "unsupported match", match: []string{`"r1"`}},
				{name: "duplicate match", match: []string{`"r1"`, `"r1"`}},
				{name: "folded match", match: []string{`"r1", "r1"`}},
			} {
				t.Run(route.Operation+"/"+test.name, func(t *testing.T) {
					response := mutationHeaderRequest(t, h, route, path, validKey, test.match, nil)
					assertRouteError(t, response, http.StatusBadRequest, "precondition_not_supported")
				})
			}
		} else {
			response := mutationHeaderRequest(t, h, route, path, validKey, nil, nil)
			assertRouteError(t, response, http.StatusPreconditionRequired, "precondition_required")
			for _, test := range []struct {
				name  string
				match []string
			}{
				{name: "wildcard match", match: []string{"*"}},
				{name: "bare match", match: []string{"42"}},
				{name: "wrong tag match", match: []string{`"42"`}},
				{name: "weak match", match: []string{`W/"r42"`}},
				{name: "empty match", match: []string{""}},
				{name: "duplicate match", match: []string{`"r1"`, `"r1"`}},
				{name: "folded match", match: []string{`"r1", "r1"`}},
			} {
				t.Run(route.Operation+"/"+test.name, func(t *testing.T) {
					response := mutationHeaderRequest(t, h, route, path, validKey, test.match, nil)
					assertRouteError(t, response, http.StatusBadRequest, "precondition_malformed")
				})
			}
		}
		validMatch := []string(nil)
		if route.Operation != "createResume" {
			validMatch = []string{`"r1"`}
		}
		for _, test := range []struct {
			name    string
			version []string
		}{
			{name: "unsupported version", version: []string{"999999"}},
			{name: "duplicate version", version: []string{"2", "2"}},
			{name: "folded version", version: []string{"2, 2"}},
		} {
			t.Run(route.Operation+"/"+test.name, func(t *testing.T) {
				response := mutationHeaderRequest(t, h, route, path, validKey, validMatch, test.version)
				assertRouteError(t, response, http.StatusBadRequest, "unsupported_schema_version")
			})
		}

		baseContentType := defaultMutationContentType(route)
		if len(baseContentType) == 0 {
			baseContentType = []string{"application/json"}
		}
		for _, test := range []struct {
			name        string
			contentType []string
		}{
			{name: "duplicate content type", contentType: []string{baseContentType[0], baseContentType[0]}},
			{name: "folded content type", contentType: []string{baseContentType[0] + ", " + baseContentType[0]}},
		} {
			wantStatus := http.StatusBadRequest
			wantCode := "request_invalid"
			if route.Upload {
				wantStatus = http.StatusUnsupportedMediaType
				wantCode = "media_type_unsupported"
			}
			t.Run(route.Operation+"/"+test.name, func(t *testing.T) {
				response := mutationHeaderAndContentTypeRequest(t, h, route, path, validKey, validMatch, nil, test.contentType)
				assertRouteError(t, response, wantStatus, wantCode)
			})
		}
	}
	if got := h.snapshotUserTable(t, "resumes"); got != beforeResumes {
		t.Fatalf("header rejections changed resume rows: before=%q after=%q", beforeResumes, got)
	}
	if got := h.snapshotUserTable(t, "idempotency_records"); got != beforeIdempotency {
		t.Fatalf("header rejections changed idempotency rows: before=%q after=%q", beforeIdempotency, got)
	}
}

func mutationHeaderRequest(t *testing.T, h *resumeAPITestHarness, route routeSpec, path string,
	key, match, version []string,
) testHTTPResponse {
	t.Helper()
	return mutationHeaderAndContentTypeRequest(t, h, route, path, key, match, version, defaultMutationContentType(route))
}

func defaultMutationContentType(route routeSpec) []string {
	switch {
	case route.Upload:
		return []string{"multipart/form-data; boundary=test"}
	case route.Method != http.MethodDelete:
		return []string{"application/json"}
	default:
		return nil
	}
}

func mutationHeaderAndContentTypeRequest(t *testing.T, h *resumeAPITestHarness, route routeSpec, path string,
	key, match, version, contentType []string,
) testHTTPResponse {
	t.Helper()
	var body io.Reader
	if route.Upload {
		body = strings.NewReader("--test--\r\n")
	} else if route.Method != http.MethodDelete {
		body = strings.NewReader(`{}`)
	}
	req, err := http.NewRequestWithContext(h.ctx, route.Method, h.server.URL+path, body)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.AddCookie(h.cookie)
	req.Header.Set("Origin", resumeAPITestOrigin)
	req.Header.Set(auth.CSRFHeaderName, h.csrfToken)
	for _, value := range key {
		req.Header.Add("Idempotency-Key", value)
	}
	for _, value := range match {
		req.Header.Add("If-Match", value)
	}
	for _, value := range version {
		req.Header.Add(wireVersionHeader, value)
	}
	for _, value := range contentType {
		req.Header.Add("Content-Type", value)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	return snapshotHTTPResponse(t, resp)
}
