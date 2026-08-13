package resumeapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func TestEveryMutation_CSRFMatrix(t *testing.T) {
	h := newResumeAPITestHarness(t)
	beforeResumes := h.snapshotUserTable(t, "resumes")
	beforeRecords := h.snapshotUserTable(t, "idempotency_records")
	beforeObjects := snapshotObjectKeys(t, h)

	for _, route := range registeredRoutes() {
		if !route.Mutation {
			continue
		}
		var canonicalBody []byte
		for _, test := range []struct {
			name   string
			token  string
			origin string
			ref    string
		}{
			{name: "missing token", origin: resumeAPITestOrigin},
			{name: "wrong token", token: strings.Repeat("A", len(h.csrfToken)), origin: resumeAPITestOrigin},
			{name: "wrong token length", token: "x", origin: resumeAPITestOrigin},
			{name: "foreign origin", token: h.csrfToken, origin: "https://foreign.example.test"},
			{name: "foreign referer", token: h.csrfToken, ref: "https://foreign.example.test/path"},
			{name: "missing origin and referer", token: h.csrfToken},
		} {
			t.Run(route.Operation+"/"+test.name, func(t *testing.T) {
				response := mutationSecurityRequest(t, h, route, test.token, test.origin, test.ref, "")
				assertRouteError(t, response, http.StatusForbidden, "csrf_rejected")
				if canonicalBody == nil {
					canonicalBody = append([]byte(nil), response.body...)
				} else if !bytes.Equal(response.body, canonicalBody) {
					t.Fatalf("CSRF body = %s, want byte-identical %s", response.body, canonicalBody)
				}
			})
		}
	}
	assertSecurityMatrixStateUnchanged(t, h, beforeResumes, beforeRecords, beforeObjects)
}

func TestIdempotency_CSRFRetryReusesKey(t *testing.T) {
	h := newResumeAPITestHarness(t)
	key := uuid.NewString()
	body := `{"title":"CSRF retry"}`

	request := func(token string) testHTTPResponse {
		t.Helper()
		req, err := http.NewRequestWithContext(h.ctx, http.MethodPost, h.server.URL+apiResumePath, strings.NewReader(body))
		if err != nil {
			t.Fatalf("build create request: %v", err)
		}
		req.AddCookie(h.cookie)
		req.Header.Set("Origin", resumeAPITestOrigin)
		req.Header.Set(auth.CSRFHeaderName, token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		req.Header.Set(wireVersionHeader, "2")
		response, err := h.client.Do(req)
		if err != nil {
			t.Fatalf("perform create request: %v", err)
		}
		return snapshotHTTPResponse(t, response)
	}

	rejected := request("wrong")
	assertRouteError(t, rejected, http.StatusForbidden, "csrf_rejected")
	if resumes := countResumeTestRows(t, h, "resumes"); resumes != 0 {
		t.Fatalf("rejected request created %d resumes, want 0", resumes)
	}
	if records := countResumeTestRows(t, h, "idempotency_records"); records != 0 {
		t.Fatalf("rejected request created %d idempotency records, want 0", records)
	}

	accepted := request(h.csrfToken)
	if accepted.status != http.StatusCreated {
		t.Fatalf("valid retry status = %d, want 201 (body=%s)", accepted.status, accepted.body)
	}
	if resumes := countResumeTestRows(t, h, "resumes"); resumes != 1 {
		t.Fatalf("valid retry left %d resumes, want exactly 1", resumes)
	}
	if records := countResumeTestRows(t, h, "idempotency_records"); records != 1 {
		t.Fatalf("valid retry left %d idempotency records, want exactly 1", records)
	}
}

func TestEveryMutation_MediaTypeMatrix(t *testing.T) {
	h := newResumeAPITestHarness(t)
	beforeResumes := h.snapshotUserTable(t, "resumes")
	beforeRecords := h.snapshotUserTable(t, "idempotency_records")
	beforeObjects := snapshotObjectKeys(t, h)

	for _, route := range registeredRoutes() {
		if !route.Mutation {
			continue
		}
		t.Run(route.Operation, func(t *testing.T) {
			response := mutationSecurityRequest(t, h, route, h.csrfToken, resumeAPITestOrigin, "", "text/plain")
			wantStatus, wantCode := http.StatusBadRequest, "request_invalid"
			if route.Upload {
				wantStatus, wantCode = http.StatusUnsupportedMediaType, "media_type_unsupported"
			}
			assertRouteError(t, response, wantStatus, wantCode)
		})
	}
	assertSecurityMatrixStateUnchanged(t, h, beforeResumes, beforeRecords, beforeObjects)
}

func TestPathTraversalInRouteParams(t *testing.T) {
	h := newResumeAPITestHarness(t)
	doc := loadMinimalDocument(t)
	doc.Content = map[string]schema.Section{
		"work": schema.NewWorkSection(nil, nil, []schema.WorkEntry{{ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60"}}),
	}
	doc.Customization.Layout.Sections.Main = []string{"work"}
	doc.Customization.Layout.Sections.Sidebar = []string{}
	created, err := h.resumes.Create(h.ctx, h.userID, "path matrix", doc)
	if err != nil {
		t.Fatalf("create path fixture: %v", err)
	}
	before := snapshotStoredResumeRow(t, h, created.ID)
	beforeRecords := h.snapshotUserTable(t, "idempotency_records")

	hostile := []struct {
		name    string
		segment string
	}{
		{name: "dot dot", segment: "%2e%2e"},
		{name: "encoded dot dot text", segment: "%252e%252e"},
		{name: "nul", segment: "%00"},
		{name: "newline", segment: "%0A"},
		{name: "overlong", segment: url.PathEscape(strings.Repeat("a", 1024))},
		{name: "non uuid", segment: "not-a-uuid"},
	}
	for _, test := range hostile {
		t.Run("id/"+test.name, func(t *testing.T) {
			response := rawSecurityRequest(t, h, http.MethodGet, apiResumePath+"/"+test.segment, nil, false)
			assertVocabularyPathRejection(t, response)
		})
		t.Run("sectionKey/"+test.name, func(t *testing.T) {
			response := rawSecurityRequest(t, h, http.MethodPatch,
				fmt.Sprintf("%s/%s/entries/%s", apiResumePath, created.ID, test.segment),
				strings.NewReader(`{"entry":{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61"}}`), true)
			if response.status != http.StatusBadRequest {
				t.Fatalf("section path status = %d body=%s, want 400", response.status, response.body)
			}
			assertRouteError(t, response, http.StatusBadRequest, "request_invalid")
		})
		t.Run("entryId/"+test.name, func(t *testing.T) {
			response := rawSecurityRequest(t, h, http.MethodDelete,
				fmt.Sprintf("%s/%s/entries/work/%s", apiResumePath, created.ID, test.segment), nil, true)
			assertVocabularyPathRejection(t, response)
		})
	}
	if after := snapshotStoredResumeRow(t, h, created.ID); !reflect.DeepEqual(after, before) {
		t.Fatalf("hostile paths changed resume: before=%+v after=%+v", before, after)
	}
	if after := h.snapshotUserTable(t, "idempotency_records"); after != beforeRecords {
		t.Fatalf("hostile paths changed idempotency records: before=%q after=%q", beforeRecords, after)
	}
}

func TestStrictDecodeRejectsNullScalarsWithoutWriting(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	beforeResumes := h.snapshotUserTable(t, "resumes")
	beforeRecords := h.snapshotUserTable(t, "idempotency_records")

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		match  int64
	}{
		{name: "create title", method: http.MethodPost, path: apiResumePath, body: `{"title":null}`},
		{name: "metadata title", method: http.MethodPatch, path: apiResumePath + "/" + created.ID.String(), body: `{"title":null}`, match: created.Revision},
		{name: "section displayName", method: http.MethodPatch, path: apiResumePath + "/" + created.ID.String() + "/sections/work", body: `{"displayName":null}`, match: created.Revision},
		{name: "section displayName wrong type", method: http.MethodPatch, path: apiResumePath + "/" + created.ID.String() + "/sections/work", body: `{"displayName":42}`, match: created.Revision},
		{name: "section iconKey wrong type", method: http.MethodPatch, path: apiResumePath + "/" + created.ID.String() + "/sections/work", body: `{"iconKey":{}}`, match: created.Revision},
		{name: "structure displayName", method: http.MethodPatch, path: apiResumePath + "/" + created.ID.String() + "/structure", body: `{"commands":[{"op":"createSection","key":"work","sectionType":"work","displayName":null,"column":"main","index":0}]}`, match: created.Revision},
		{name: "structure displayName wrong type", method: http.MethodPatch, path: apiResumePath + "/" + created.ID.String() + "/structure", body: `{"commands":[{"op":"createSection","key":"work","sectionType":"work","displayName":42,"column":"main","index":0}]}`, match: created.Revision},
		{name: "structure iconKey wrong type", method: http.MethodPatch, path: apiResumePath + "/" + created.ID.String() + "/structure", body: `{"commands":[{"op":"createSection","key":"work","sectionType":"work","iconKey":{},"column":"main","index":0}]}`, match: created.Revision},
		{name: "crop x", method: http.MethodPatch, path: apiResumePath + "/" + created.ID.String() + "/photo", body: `{"crop":{"x":null,"y":0,"width":1,"height":1}}`, match: created.Revision},
		{name: "crop y", method: http.MethodPatch, path: apiResumePath + "/" + created.ID.String() + "/photo", body: `{"crop":{"x":0,"y":null,"width":1,"height":1}}`, match: created.Revision},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := resumeRequest(t, h, test.method, test.path, test.body, test.match, uuid.New(), "2")
			assertRouteError(t, response, http.StatusUnprocessableEntity, "document_invalid")
		})
	}
	if after := h.snapshotUserTable(t, "resumes"); after != beforeResumes {
		t.Fatalf("null scalar rejection changed resumes: before=%q after=%q", beforeResumes, after)
	}
	if after := h.snapshotUserTable(t, "idempotency_records"); after != beforeRecords {
		t.Fatalf("null scalar rejection changed idempotency records: before=%q after=%q", beforeRecords, after)
	}
}

func TestCrossUser_EveryRoute_IndistinguishableFromMissing(t *testing.T) {
	h := newResumeAPITestHarness(t)
	doc := loadMinimalDocument(t)
	doc.Content = map[string]schema.Section{
		"work": schema.NewWorkSection(nil, nil, []schema.WorkEntry{{ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60"}}),
	}
	doc.Customization.Layout.Sections.Main = []string{"work"}
	doc.Customization.Layout.Sections.Sidebar = []string{}
	created, err := h.resumes.Create(h.ctx, h.userID, "authorization matrix", doc)
	if err != nil {
		t.Fatalf("create authorization fixture: %v", err)
	}
	foreignCookie, foreignToken := issueAdditionalTestSession(t, h)
	unknown := uuid.New()
	before := snapshotStoredResumeRow(t, h, created.ID)
	beforeObjects := snapshotObjectKeys(t, h)

	for _, route := range registeredRoutes() {
		if route.Pattern == apiResumePath {
			continue
		}
		t.Run(route.Operation, func(t *testing.T) {
			real := crossUserRouteRequest(t, h, route, created.ID, foreignCookie, foreignToken)
			missing := crossUserRouteRequest(t, h, route, unknown, foreignCookie, foreignToken)
			if real.status != missing.status || !bytes.Equal(real.body, missing.body) {
				t.Fatalf("real = %d %s; missing = %d %s", real.status, real.body, missing.status, missing.body)
			}
			if real.status != http.StatusNotFound {
				t.Fatalf("cross-user status = %d body=%s, want 404", real.status, real.body)
			}
			if !reflect.DeepEqual(stableSecurityHeaders(real.header), stableSecurityHeaders(missing.header)) {
				t.Fatalf("stable headers differ: real=%v missing=%v", stableSecurityHeaders(real.header), stableSecurityHeaders(missing.header))
			}
			realRequestID := real.header.Get("X-Request-ID")
			missingRequestID := missing.header.Get("X-Request-ID")
			if _, parseErr := uuid.Parse(realRequestID); parseErr != nil || realRequestID == missingRequestID {
				t.Fatalf("request IDs real=%q missing=%q", realRequestID, missingRequestID)
			}
			if _, parseErr := uuid.Parse(missingRequestID); parseErr != nil {
				t.Fatalf("missing request ID %q: %v", missingRequestID, parseErr)
			}
		})
	}
	if after := snapshotStoredResumeRow(t, h, created.ID); !reflect.DeepEqual(after, before) {
		t.Fatalf("cross-user matrix changed owner row: before=%+v after=%+v", before, after)
	}
	if after := snapshotObjectKeys(t, h); !reflect.DeepEqual(after, beforeObjects) {
		t.Fatalf("cross-user matrix changed objects: before=%v after=%v", beforeObjects, after)
	}
}

func TestCrossUser_NoStateLeak(t *testing.T) {
	h := newResumeAPITestHarness(t)
	foreignCookie, foreignToken := issueAdditionalTestSession(t, h)
	route := routeByOperationForTest(t, "updateResumeCustomization")
	unknown := uuid.New()
	rejected := testHTTPResponse{}
	for attempt := 0; attempt < resumeWriteRequests+20; attempt++ {
		response := crossUserRouteRequest(t, h, route, unknown, foreignCookie, foreignToken)
		if response.status == http.StatusTooManyRequests {
			rejected = response
			break
		}
		if response.status != http.StatusNotFound {
			t.Fatalf("foreign attempt %d = %d body=%s, want 404", attempt+1, response.status, response.body)
		}
	}
	if rejected.status == 0 {
		t.Fatal("foreign write budget did not reject within its burst plus bounded refill allowance")
	}
	assertRouteError(t, rejected, http.StatusTooManyRequests, "rate_limited")
	owner := crossUserRouteRequest(t, h, route, unknown, h.cookie, h.csrfToken)
	if owner.status != http.StatusNotFound {
		t.Fatalf("owner request after foreign budget exhausted = %d body=%s, want independent 404", owner.status, owner.body)
	}
}

func mutationSecurityRequest(t *testing.T, h *resumeAPITestHarness, route routeSpec,
	token, origin, referer, contentType string,
) testHTTPResponse {
	t.Helper()
	body := routeSecurityBody(route)
	req, err := http.NewRequestWithContext(h.ctx, route.Method,
		h.server.URL+concreteRoutePath(route.Pattern), body)
	if err != nil {
		t.Fatalf("build %s request: %v", route.Operation, err)
	}
	req.AddCookie(h.cookie)
	if token != "" {
		req.Header.Set(auth.CSRFHeaderName, token)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	req.Header.Set("Idempotency-Key", uuid.NewString())
	if route.Operation != "createResume" {
		req.Header.Set("If-Match", `"r1"`)
	}
	if contentType == "" {
		values := defaultMutationContentType(route)
		if len(values) > 0 {
			contentType = values[0]
		}
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	response, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("perform %s request: %v", route.Operation, err)
	}
	return snapshotHTTPResponse(t, response)
}

func issueAdditionalTestSession(t *testing.T, h *resumeAPITestHarness) (*http.Cookie, string) {
	t.Helper()
	user, err := h.queries.CreateUser(h.ctx, store.CreateUserParams{
		Email: "cross-user-" + uuid.NewString() + "@example.test", Name: "cross user",
	})
	if err != nil {
		t.Fatalf("create cross user: %v", err)
	}
	raw, session, err := auth.NewSessionManager(h.queries).Issue(h.ctx, user.ID, "cross-user-test", "127.0.0.1")
	if err != nil {
		t.Fatalf("issue cross-user session: %v", err)
	}
	return &http.Cookie{Name: "__Host-session", Value: raw}, base64.RawURLEncoding.EncodeToString(session.CSRFSecret)
}

func crossUserRouteRequest(t *testing.T, h *resumeAPITestHarness, route routeSpec, target uuid.UUID,
	cookie *http.Cookie, csrfToken string,
) testHTTPResponse {
	t.Helper()
	path := strings.ReplaceAll(route.Pattern, "{id}", target.String())
	path = strings.ReplaceAll(path, "{sectionKey}", "work")
	path = strings.ReplaceAll(path, "{entryId}", "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60")
	body, contentType := crossUserRouteBody(t, route)
	req, err := http.NewRequestWithContext(h.ctx, route.Method, h.server.URL+path, body)
	if err != nil {
		t.Fatalf("build cross-user %s: %v", route.Operation, err)
	}
	req.AddCookie(cookie)
	if route.Mutation {
		req.Header.Set("Origin", resumeAPITestOrigin)
		req.Header.Set(auth.CSRFHeaderName, csrfToken)
		req.Header.Set("Idempotency-Key", uuid.NewString())
		req.Header.Set("If-Match", `"r1"`)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
	}
	response, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("perform cross-user %s: %v", route.Operation, err)
	}
	return snapshotHTTPResponse(t, response)
}

func crossUserRouteBody(t *testing.T, route routeSpec) (io.Reader, string) {
	t.Helper()
	if route.Upload {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", "photo.png")
		if err != nil {
			t.Fatalf("create cross-user multipart: %v", err)
		}
		if _, err := part.Write(makePhotoPNG(t)); err != nil {
			t.Fatalf("write cross-user multipart: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("close cross-user multipart: %v", err)
		}
		return bytes.NewReader(body.Bytes()), writer.FormDataContentType()
	}
	body := routeSecurityBody(route)
	contentType := ""
	if route.Mutation && route.Method != http.MethodDelete {
		contentType = "application/json"
	}
	return body, contentType
}

func stableSecurityHeaders(source http.Header) http.Header {
	stable := source.Clone()
	for _, name := range []string{"Date", "X-Request-ID"} {
		stable.Del(name)
	}
	return stable
}

func routeByOperationForTest(t *testing.T, operation string) routeSpec {
	t.Helper()
	for _, route := range registeredRoutes() {
		if route.Operation == operation {
			return route
		}
	}
	t.Fatalf("route %q not registered", operation)
	return routeSpec{}
}

func routeSecurityBody(route routeSpec) io.Reader {
	switch route.Operation {
	case "createResume":
		return strings.NewReader(`{"title":"security matrix"}`)
	case "updateResumeMetadata":
		return strings.NewReader(`{"title":"security matrix"}`)
	case "upsertResumeEntry":
		return strings.NewReader(`{"entry":{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60"}}`)
	case "updateResumeSection":
		return strings.NewReader(`{"displayName":"security matrix"}`)
	case "updateResumeStructure":
		return strings.NewReader(`{"commands":[{"op":"reorderColumn","column":"main","keys":[]}]}`)
	case "updateResumePersonalDetails":
		return strings.NewReader(`{"details":[]}`)
	case "updateResumeCustomization":
		return strings.NewReader(`{"deltas":[{"op":"set","path":"font.baseSizePx","value":16}]}`)
	case "uploadResumePhoto":
		return strings.NewReader("--test--\r\n")
	case "updateResumePhotoCrop":
		return strings.NewReader(`{"crop":null}`)
	default:
		return nil
	}
}

func rawSecurityRequest(t *testing.T, h *resumeAPITestHarness, method, path string, body io.Reader,
	mutation bool,
) testHTTPResponse {
	t.Helper()
	req, err := http.NewRequestWithContext(h.ctx, method, h.server.URL+path, body)
	if err != nil {
		t.Fatalf("build hostile path request: %v", err)
	}
	req.AddCookie(h.cookie)
	if mutation {
		req.Header.Set("Origin", resumeAPITestOrigin)
		req.Header.Set(auth.CSRFHeaderName, h.csrfToken)
		req.Header.Set("Idempotency-Key", uuid.NewString())
		req.Header.Set("If-Match", `"r1"`)
		if method != http.MethodDelete {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	response, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("perform hostile path request: %v", err)
	}
	return snapshotHTTPResponse(t, response)
}

func assertVocabularyPathRejection(t *testing.T, response testHTTPResponse) {
	t.Helper()
	if response.status != http.StatusBadRequest && response.status != http.StatusNotFound {
		t.Fatalf("path status = %d body=%s, want 400 or 404", response.status, response.body)
	}
	var envelope errorEnvelope
	if err := json.Unmarshal(response.body, &envelope); err != nil {
		t.Fatalf("decode path error: %v", err)
	}
	if _, production := productionErrorVocabulary[envelope.Error.Code]; !production {
		if _, generic := genericErrorVocabulary[envelope.Error.Code]; !generic {
			t.Fatalf("path error code = %q, want closed vocabulary", envelope.Error.Code)
		}
	}
}

func snapshotObjectKeys(t *testing.T, h *resumeAPITestHarness) []string {
	t.Helper()
	objects, _, err := h.service.blobs.ListPage(h.ctx, "resumes/", "", 100)
	if err != nil {
		t.Fatalf("list objects: %v", err)
	}
	keys := make([]string, len(objects))
	for index, object := range objects {
		keys[index] = object.Key
	}
	return keys
}

func assertSecurityMatrixStateUnchanged(t *testing.T, h *resumeAPITestHarness,
	resumes, records string, objects []string,
) {
	t.Helper()
	if after := h.snapshotUserTable(t, "resumes"); after != resumes {
		t.Fatalf("security rejections changed resumes: before=%q after=%q", resumes, after)
	}
	if after := h.snapshotUserTable(t, "idempotency_records"); after != records {
		t.Fatalf("security rejections changed idempotency records: before=%q after=%q", records, after)
	}
	if after := snapshotObjectKeys(t, h); !reflect.DeepEqual(after, objects) {
		t.Fatalf("security rejections changed objects: before=%v after=%v", objects, after)
	}
}
