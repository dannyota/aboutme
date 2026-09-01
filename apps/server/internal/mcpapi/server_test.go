package mcpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/oauthsrv"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/resumeapi"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const maxMCPRequestBytes = 4 << 20

type recordingAgentExecutor struct {
	mu        sync.Mutex
	calls     []resumeapi.AgentCall
	responses map[resumeapi.AgentOperation]resumeapi.AgentResponse
}

func (e *recordingAgentExecutor) ExecuteAgent(_ context.Context, _ resumeapi.AgentPrincipal,
	call resumeapi.AgentCall,
) resumeapi.AgentResponse {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, call)
	if response, ok := e.responses[call.Operation]; ok {
		return response
	}
	return resumeapi.AgentResponse{Status: http.StatusOK, Header: make(http.Header), Body: []byte(`{"data":{}}`)}
}

func (e *recordingAgentExecutor) snapshotCalls() []resumeapi.AgentCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]resumeapi.AgentCall(nil), e.calls...)
}

type bearerRoundTripper struct {
	token  string
	cookie string
}

func (t bearerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+t.token)
	if t.cookie != "" {
		clone.Header.Set("Cookie", t.cookie)
	}
	return http.DefaultTransport.RoundTrip(clone)
}

func connectMCPClient(t *testing.T, endpoint, token, cookie string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "aboutme-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: endpoint,
		HTTPClient: &http.Client{Transport: bearerRoundTripper{
			token: token, cookie: cookie,
		}},
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}, nil)
	if err != nil {
		t.Fatalf("connect MCP client: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func mustTestMCPRates(t *testing.T) *RatePolicies {
	t.Helper()
	rates, err := NewRatePolicies(testMCPRateConfig())
	if err != nil {
		t.Fatalf("NewRatePolicies: %v", err)
	}
	return rates
}

func TestServer_GoSDKListsExactlyFifteenToolsAndCallsRead(t *testing.T) {
	h := newBearerHarness(t, "resumes:read resumes:write")
	raw, _ := h.createToken(t, oauthsrv.TokenKindAccess)
	executor := &recordingAgentExecutor{responses: map[resumeapi.AgentOperation]resumeapi.AgentResponse{
		resumeapi.AgentListResumes: {
			Status: http.StatusOK, Header: make(http.Header),
			Body: []byte(`{"data":[{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f","revision":"1"}]}`),
		},
	}}
	handler, err := NewServer(ServerDependencies{Bearer: h.bearer, Resumes: executor, Rates: mustTestMCPRates(t), MaxRequestBodyBytes: maxMCPRequestBytes})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	session := connectMCPClient(t, httpServer.URL, raw, "__Host-session=must-not-be-read")

	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := make([]string, len(listed.Tools))
	for i, tool := range listed.Tools {
		names[i] = tool.Name
		if tool.InputSchema == nil || tool.OutputSchema == nil {
			t.Fatalf("tool %q omitted generated input/output schema", tool.Name)
		}
	}
	sort.Strings(names)
	want := []string{
		"create_resume", "delete_entry", "delete_photo", "delete_resume", "get_photo",
		"get_resume", "list_resumes", "update_customization", "update_personal_details",
		"update_photo_crop", "update_resume_metadata", "update_section", "update_structure",
		"upload_photo", "upsert_entry",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("tool names = %v, want %v", names, want)
	}

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_resumes"})
	if err != nil || result.IsError {
		t.Fatalf("CallTool list_resumes = %#v, error = %v", result, err)
	}
	calls := executor.snapshotCalls()
	if len(calls) != 1 || calls[0].Operation != resumeapi.AgentListResumes {
		t.Fatalf("executor calls = %#v", calls)
	}
}

func TestServer_WriteScopeDeniedBeforeResumeStateAndCookieCannotAuthenticate(t *testing.T) {
	h := newBearerHarness(t, "resumes:read")
	raw, _ := h.createToken(t, oauthsrv.TokenKindAccess)
	executor := &recordingAgentExecutor{}
	handler, err := NewServer(ServerDependencies{Bearer: h.bearer, Resumes: executor, Rates: mustTestMCPRates(t), MaxRequestBodyBytes: maxMCPRequestBytes})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	session := connectMCPClient(t, httpServer.URL, raw, "__Host-session=ignored")

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "create_resume", Arguments: map[string]any{"title": "forbidden"},
	})
	if err != nil || !result.IsError || len(result.Content) != 1 {
		t.Fatalf("write result = %#v, error = %v", result, err)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok || text.Text != "scope_denied" {
		t.Fatalf("write error content = %#v", result.Content)
	}
	if calls := executor.snapshotCalls(); len(calls) != 0 {
		t.Fatalf("scope-denied write reached executor: %#v", calls)
	}
	oversizedPhoto := base64.StdEncoding.EncodeToString(make([]byte, maxDecodedPhotoBytes+1))
	photoResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "upload_photo", Arguments: map[string]any{
			"resume_id": uuid.NewString(), "revision": "1", "data_base64": oversizedPhoto,
		},
	})
	if err != nil || !photoResult.IsError || toolErrorText(photoResult) != "scope_denied" {
		t.Fatalf("read-only oversized photo = %#v, error = %v", photoResult, err)
	}

	body := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, httpServer.URL, body)
	if err != nil {
		t.Fatalf("build cookie-only request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Cookie", "__Host-session=must-not-authenticate")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("cookie-only request: %v", err)
	}
	responseBody, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		t.Fatalf("read cookie-only response: %v", readErr)
	}
	if response.StatusCode != http.StatusUnauthorized || string(responseBody) != `{"error":"unauthorized"}` {
		t.Fatalf("cookie-only response = %d %s", response.StatusCode, responseBody)
	}
}

func TestServer_CreateUpsertGetChainsRevisionAndClosesStaleConflict(t *testing.T) {
	h := newBearerHarness(t, "resumes:read resumes:write")
	raw, _ := h.createToken(t, oauthsrv.TokenKindAccess)
	projector := docmigrate.NewIdentityProjector()
	backend, err := media.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	public, err := h.q.GetPublicState(context.Background())
	if err != nil {
		t.Fatalf("GetPublicState: %v", err)
	}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: public.DiscoveryGeneration})
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	resumes := resumeapi.New(resume.NewStore(h.pool, projector), resume.NewIdempotencyStore(h.pool), projector, backend,
		resumeapi.Options{
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Clock: h.clock.Now,
			Coordinator: coordinator, RecoveryPool: h.pool,
		})
	handler, err := NewServer(ServerDependencies{Bearer: h.bearer, Resumes: resumes, Rates: mustTestMCPRates(t), MaxRequestBodyBytes: maxMCPRequestBytes})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	session := connectMCPClient(t, httpServer.URL, raw, "")

	rawDocument, err := os.ReadFile("../../../../packages/schema/fixtures/minimal.json")
	if err != nil {
		t.Fatalf("read minimal fixture: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(rawDocument, &document); err != nil {
		t.Fatalf("decode minimal fixture: %v", err)
	}
	var otherDocument schema.Resume
	if err := json.Unmarshal(rawDocument, &otherDocument); err != nil {
		t.Fatalf("decode other-user document: %v", err)
	}
	otherUser, err := h.q.CreateUser(context.Background(), store.CreateUserParams{
		Email: "other-" + uuid.NewString() + "@example.test", Name: "Other owner",
	})
	if err != nil {
		t.Fatalf("CreateUser other: %v", err)
	}
	t.Cleanup(func() { _, _ = h.pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", otherUser.ID) })
	otherResume, err := resume.NewStore(h.pool, projector).Create(context.Background(), otherUser.ID, "Other resume", otherDocument)
	if err != nil {
		t.Fatalf("create other-user resume: %v", err)
	}
	crossUser, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_resume", Arguments: map[string]any{"resume_id": otherResume.ID.String()},
	})
	if err != nil || !crossUser.IsError || toolErrorText(crossUser) != "not_found" {
		t.Fatalf("cross-user get = %#v, error = %v", crossUser, err)
	}
	invalidID, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_resume", Arguments: map[string]any{"resume_id": "not-a-uuid"},
	})
	if err != nil || !invalidID.IsError || toolErrorText(invalidID) != "validation_failed" {
		t.Fatalf("invalid-id get = %#v, error = %v", invalidID, err)
	}
	document["content"] = map[string]any{
		"work": map[string]any{"sectionType": "work", "iconKey": "briefcase", "entries": []any{}},
	}
	customization := document["customization"].(map[string]any)
	layout := customization["layout"].(map[string]any)
	sections := layout["sections"].(map[string]any)
	sections["main"] = []any{"work"}

	created, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "create_resume", Arguments: map[string]any{"title": "MCP lifecycle", "document": document},
	})
	if err != nil || created.IsError {
		t.Fatalf("create_resume = %#v, error = %v", created, err)
	}
	createdOutput := decodeStructuredMutation(t, created)
	resumeID, ok := createdOutput.State["id"].(string)
	if !ok || resumeID == "" || createdOutput.Revision != "1" {
		t.Fatalf("create output = %#v", createdOutput)
	}

	entryID := "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61"
	upserted, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "upsert_entry", Arguments: map[string]any{
			"resume_id": resumeID, "revision": createdOutput.Revision, "section_key": "work",
			"entry": map[string]any{"id": entryID, "description": `<script>alert(1)</script><p>safe</p>`},
		},
	})
	if err != nil || upserted.IsError {
		t.Fatalf("upsert_entry = %#v, error = %v", upserted, err)
	}
	upsertedOutput := decodeStructuredMutation(t, upserted)
	if upsertedOutput.Revision != "2" {
		t.Fatalf("upsert revision = %q", upsertedOutput.Revision)
	}
	sanitized, err := json.Marshal(upsertedOutput.State)
	if err != nil || bytes.Contains(sanitized, []byte("alert(1)")) || !bytes.Contains(sanitized, []byte("safe")) {
		t.Fatalf("sanitized state = %s, error = %v", sanitized, err)
	}

	got, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_resume", Arguments: map[string]any{"resume_id": resumeID},
	})
	if err != nil || got.IsError {
		t.Fatalf("get_resume = %#v, error = %v", got, err)
	}
	gotState := decodeStructuredState(t, got)
	if gotState["revision"] != "2" {
		t.Fatalf("get revision = %#v", gotState["revision"])
	}

	stale, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "update_resume_metadata", Arguments: map[string]any{
			"resume_id": resumeID, "revision": "1", "title": "stale",
		},
	})
	if err != nil || !stale.IsError || len(stale.Content) != 1 {
		t.Fatalf("stale update = %#v, error = %v", stale, err)
	}
	text, ok := stale.Content[0].(*mcp.TextContent)
	if !ok || text.Text != "revision_conflict" {
		t.Fatalf("stale content = %#v", stale.Content)
	}

	metadata := callMutationTool(t, session, "update_resume_metadata", map[string]any{
		"resume_id": resumeID, "revision": "2", "title": "MCP renamed",
	})
	if metadata.Revision != "3" || metadata.State["title"] != "MCP renamed" {
		t.Fatalf("metadata output = %#v", metadata)
	}
	deletedEntry := callMutationTool(t, session, "delete_entry", map[string]any{
		"resume_id": resumeID, "revision": "3", "section_key": "work", "entry_id": entryID,
	})
	if deletedEntry.Revision != "4" {
		t.Fatalf("delete entry output = %#v", deletedEntry)
	}
	updatedSection := callMutationTool(t, session, "update_section", map[string]any{
		"resume_id": resumeID, "revision": "4", "section_key": "work", "display_name": "Agent experience",
		"icon_key": nil, "entry_order": []any{},
	})
	if updatedSection.Revision != "5" {
		t.Fatalf("update section output = %#v", updatedSection)
	}
	sectionState, err := json.Marshal(updatedSection.State)
	if err != nil || bytes.Contains(sectionState, []byte("briefcase")) || !bytes.Contains(sectionState, []byte("Agent experience")) {
		t.Fatalf("update section state = %s, error = %v", sectionState, err)
	}
	updatedStructure := callMutationTool(t, session, "update_structure", map[string]any{
		"resume_id": resumeID, "revision": "5",
		"commands": []any{map[string]any{
			"op": "createSection", "key": "skills", "sectionType": "skill",
			"displayName": "Skills", "column": "main", "index": 1,
		}},
	})
	if updatedStructure.Revision != "6" {
		t.Fatalf("update structure output = %#v", updatedStructure)
	}
	updatedDetails := callMutationTool(t, session, "update_personal_details", map[string]any{
		"resume_id": resumeID, "revision": "6",
		"personal_details": map[string]any{"fullName": "Agent User", "headline": "Writer", "details": []any{}},
	})
	if updatedDetails.Revision != "7" {
		t.Fatalf("update personal details output = %#v", updatedDetails)
	}
	updatedCustomization := callMutationTool(t, session, "update_customization", map[string]any{
		"resume_id": resumeID, "revision": "7",
		"deltas": []any{map[string]any{"op": "set", "path": "colors.primary", "value": "#112233"}},
	})
	if updatedCustomization.Revision != "8" {
		t.Fatalf("update customization output = %#v", updatedCustomization)
	}
	uploaded := callMutationTool(t, session, "upload_photo", map[string]any{
		"resume_id": resumeID, "revision": "8", "data_base64": base64.StdEncoding.EncodeToString(testPhotoPNG(t)),
	})
	if uploaded.Revision != "9" {
		t.Fatalf("upload photo output = %#v", uploaded)
	}
	photo, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_photo", Arguments: map[string]any{"resume_id": resumeID},
	})
	if err != nil || photo.IsError {
		t.Fatalf("get_photo = %#v, error = %v", photo, err)
	}
	photoRaw, err := json.Marshal(photo.StructuredContent)
	if err != nil {
		t.Fatalf("marshal photo output: %v", err)
	}
	var photoData photoOutput
	if err := json.Unmarshal(photoRaw, &photoData); err != nil ||
		!strings.HasPrefix(photoData.ContentType, "image/") || photoData.DataBase64 == "" {
		t.Fatalf("photo output = %#v, error = %v", photoData, err)
	}
	cropped := callMutationTool(t, session, "update_photo_crop", map[string]any{
		"resume_id": resumeID, "revision": "9",
		"crop": map[string]any{"x": 0.1, "y": 0.2, "width": 0.7, "height": 0.6},
	})
	if cropped.Revision != "10" {
		t.Fatalf("update photo crop output = %#v", cropped)
	}
	deletedPhoto := callMutationTool(t, session, "delete_photo", map[string]any{
		"resume_id": resumeID, "revision": "10",
	})
	if deletedPhoto.Revision != "11" {
		t.Fatalf("delete photo output = %#v", deletedPhoto)
	}
	listed, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_resumes"})
	if err != nil || listed.IsError {
		t.Fatalf("list_resumes = %#v, error = %v", listed, err)
	}
	deletedResume, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "delete_resume", Arguments: map[string]any{"resume_id": resumeID, "revision": "11"},
	})
	if err != nil || deletedResume.IsError {
		t.Fatalf("delete_resume = %#v, error = %v", deletedResume, err)
	}
	deleteRaw, err := json.Marshal(deletedResume.StructuredContent)
	if err != nil {
		t.Fatalf("marshal delete output: %v", err)
	}
	var deletion deleteResumeOutput
	if err := json.Unmarshal(deleteRaw, &deletion); err != nil || !deletion.Deleted ||
		deletion.ID != resumeID || deletion.Revision != "11" {
		t.Fatalf("delete output = %#v, error = %v", deletion, err)
	}
}

func callMutationTool(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) mutationOutput {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil || result.IsError {
		t.Fatalf("%s = %#v, tool error = %q, error = %v", name, result, toolErrorText(result), err)
	}
	return decodeStructuredMutation(t, result)
}

func toolErrorText(result *mcp.CallToolResult) string {
	if result == nil || len(result.Content) != 1 {
		return ""
	}
	text, _ := result.Content[0].(*mcp.TextContent)
	if text == nil {
		return ""
	}
	return text.Text
}

func testPhotoPNG(t *testing.T) []byte {
	t.Helper()
	imageData := image.NewRGBA(image.Rect(0, 0, 3, 2))
	imageData.Set(0, 0, color.RGBA{R: 255, A: 255})
	var body bytes.Buffer
	if err := png.Encode(&body, imageData); err != nil {
		t.Fatalf("encode test photo: %v", err)
	}
	return body.Bytes()
}

func decodeStructuredMutation(t *testing.T, result *mcp.CallToolResult) mutationOutput {
	t.Helper()
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured mutation: %v", err)
	}
	var output mutationOutput
	if err := json.Unmarshal(raw, &output); err != nil {
		t.Fatalf("decode structured mutation: %v (raw=%s)", err, raw)
	}
	return output
}

func decodeStructuredState(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured state: %v", err)
	}
	var output resumeStateOutput
	if err := json.Unmarshal(raw, &output); err != nil {
		t.Fatalf("decode structured state: %v (raw=%s)", err, raw)
	}
	return output.State
}

func TestServer_RejectsBatchAndEnforcesFourMiBBoundary(t *testing.T) {
	h := newBearerHarness(t, "resumes:read")
	raw, _ := h.createToken(t, oauthsrv.TokenKindAccess)
	handler, err := NewServer(ServerDependencies{Bearer: h.bearer, Resumes: &recordingAgentExecutor{}, Rates: mustTestMCPRates(t), MaxRequestBodyBytes: maxMCPRequestBytes})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)

	for _, tc := range []struct {
		name       string
		body       []byte
		wantStatus int
		wantBody   string
	}{
		{"batch", []byte(`[{"jsonrpc":"2.0","id":1,"method":"tools/list"}]`), http.StatusBadRequest, `{"error":"invalid_request"}`},
		{"exact boundary is not too large", bytes.Repeat([]byte(" "), maxMCPRequestBytes), http.StatusBadRequest, ""},
		{"one byte over", bytes.Repeat([]byte(" "), maxMCPRequestBytes+1), http.StatusRequestEntityTooLarge, `{"error":"payload_too_large"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, httpServer.URL, bytes.NewReader(tc.body))
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			request.Header.Set("Authorization", "Bearer "+raw)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Accept", "application/json, text/event-stream")
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatalf("perform request: %v", err)
			}
			body, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr != nil {
				t.Fatalf("read response: %v", readErr)
			}
			if response.StatusCode != tc.wantStatus {
				t.Fatalf("response = %d %s, want %d", response.StatusCode, body, tc.wantStatus)
			}
			if tc.wantBody != "" && string(body) != tc.wantBody {
				t.Fatalf("response body = %q, want %q", body, tc.wantBody)
			}
			if tc.name == "exact boundary is not too large" && bytes.Contains(body, []byte("payload_too_large")) {
				t.Fatalf("exact boundary returned payload_too_large: %s", body)
			}
		})
	}
}

func TestServer_JSONOnlyAcceptAndUnknownToolAreRejectedWithoutExecution(t *testing.T) {
	h := newBearerHarness(t, "resumes:read resumes:write")
	raw, _ := h.createToken(t, oauthsrv.TokenKindAccess)
	executor := &recordingAgentExecutor{}
	handler, err := NewServer(ServerDependencies{Bearer: h.bearer, Resumes: executor, Rates: mustTestMCPRates(t), MaxRequestBodyBytes: maxMCPRequestBytes})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)

	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, httpServer.URL,
		bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	if err != nil {
		t.Fatalf("build JSON-only request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+raw)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("perform JSON-only request: %v", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("JSON-only Accept status = %d, want 400", response.StatusCode)
	}

	session := connectMCPClient(t, httpServer.URL, raw, "")
	result, callErr := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "publish_resume"})
	if callErr == nil || result != nil {
		t.Fatalf("unknown tool result = %#v, error = %v", result, callErr)
	}
	if calls := executor.snapshotCalls(); len(calls) != 0 {
		t.Fatalf("unknown tool reached executor: %#v", calls)
	}
}

func TestServer_UploadPhotoRejectsDecodedBytesOverExistingCeiling(t *testing.T) {
	h := newBearerHarness(t, "resumes:write")
	raw, _ := h.createToken(t, oauthsrv.TokenKindAccess)
	executor := &recordingAgentExecutor{}
	handler, err := NewServer(ServerDependencies{Bearer: h.bearer, Resumes: executor, Rates: mustTestMCPRates(t), MaxRequestBodyBytes: maxMCPRequestBytes})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	session := connectMCPClient(t, httpServer.URL, raw, "")

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "upload_photo", Arguments: map[string]any{
			"resume_id": uuid.NewString(), "revision": "1",
			"data_base64": base64.StdEncoding.EncodeToString(make([]byte, maxDecodedPhotoBytes+1)),
		},
	})
	if err != nil || !result.IsError || toolErrorText(result) != "payload_too_large" {
		t.Fatalf("oversized decoded photo = %#v, error = %v", result, err)
	}
	if calls := executor.snapshotCalls(); len(calls) != 0 {
		t.Fatalf("oversized decoded photo reached executor: %#v", calls)
	}
}

func TestServer_SchemaFailuresUseClosedValidationError(t *testing.T) {
	h := newBearerHarness(t, "resumes:read")
	raw, _ := h.createToken(t, oauthsrv.TokenKindAccess)
	handler, err := NewServer(ServerDependencies{Bearer: h.bearer, Resumes: &recordingAgentExecutor{}, Rates: mustTestMCPRates(t), MaxRequestBodyBytes: maxMCPRequestBytes})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	session := connectMCPClient(t, httpServer.URL, raw, "")

	for _, arguments := range []map[string]any{{}, {"resume_id": 42}} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
			Name: "get_resume", Arguments: arguments,
		})
		if err != nil || !result.IsError || toolErrorText(result) != "validation_failed" {
			t.Fatalf("schema failure for %#v = %#v, error = %v", arguments, result, err)
		}
	}
}

func TestServer_ToolCallRateLimitReturnsClosedHTTPError(t *testing.T) {
	h := newBearerHarness(t, "resumes:read")
	raw, _ := h.createToken(t, oauthsrv.TokenKindAccess)
	cfg := testMCPRateConfig()
	cfg.TokenRequests = 1
	cfg.UserRequests = 10
	rates, err := NewRatePolicies(cfg)
	if err != nil {
		t.Fatalf("NewRatePolicies: %v", err)
	}
	handler, err := NewServer(ServerDependencies{
		Bearer: h.bearer, Resumes: &recordingAgentExecutor{}, Rates: rates,
		MaxRequestBodyBytes: maxMCPRequestBytes,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_resumes","arguments":{}}}`
	for i := 1; i <= 2; i++ {
		request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+raw)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if i == 1 && recorder.Code != http.StatusOK {
			t.Fatalf("first tool call status = %d body=%s", recorder.Code, recorder.Body.String())
		}
		if i == 2 {
			if recorder.Code != http.StatusTooManyRequests || recorder.Header().Get("Retry-After") != "60" || recorder.Body.String() != `{"error":"rate_limited"}` {
				t.Fatalf("tool limit+1 = %d Retry-After %q body %q", recorder.Code, recorder.Header().Get("Retry-After"), recorder.Body.String())
			}
		}
	}
}
