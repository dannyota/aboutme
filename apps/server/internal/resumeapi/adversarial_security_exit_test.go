package resumeapi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

type securityExitOpenAPIOperation struct {
	Method       string
	Path         string
	OperationID  string
	ContentTypes []string
}

func TestAuthenticatedResponsesUseExactNoStoreCachePolicy(t *testing.T) {
	const wantCacheControl = "no-store, no-transform"
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)

	perform := func(request *http.Request) testHTTPResponse {
		t.Helper()
		request.Header.Set("Accept-Encoding", "gzip, zstd")
		response, err := h.client.Do(request)
		if err != nil {
			t.Fatalf("perform %s %s: %v", request.Method, request.URL.Path, err)
		}
		return snapshotHTTPResponse(t, response)
	}
	assertPolicy := func(name string, response testHTTPResponse, wantStatus int) {
		t.Helper()
		if response.status != wantStatus {
			t.Fatalf("%s status = %d, want %d (body=%s)", name, response.status, wantStatus, response.body)
		}
		if got := response.header.Get("Cache-Control"); got != wantCacheControl {
			t.Fatalf("%s Cache-Control = %q, want %q", name, got, wantCacheControl)
		}
		if got := response.header.Get("Content-Encoding"); got != "" {
			t.Fatalf("%s Content-Encoding = %q, want empty", name, got)
		}
	}
	readRequest := func(path string, cookie *http.Cookie) *http.Request {
		t.Helper()
		request, err := http.NewRequestWithContext(h.ctx, http.MethodGet, h.server.URL+path, nil)
		if err != nil {
			t.Fatalf("build GET %s: %v", path, err)
		}
		if cookie != nil {
			request.AddCookie(cookie)
		}
		request.Header.Set(wireVersionHeader, "2")
		return request
	}

	resumePath := apiResumePath + "/" + created.ID.String()
	assertPolicy("resume 200", perform(readRequest(resumePath, h.cookie)), http.StatusOK)
	assertPolicy("resume 401", perform(readRequest(resumePath, nil)), http.StatusUnauthorized)

	firstPatch := newAdversarialExitMutationRequest(t, h, http.MethodPatch, resumePath,
		[]byte(`{"title":"cache proof"}`), created.Revision, uuid.NewString(), "2", "application/json")
	assertPolicy("resume PATCH 200", perform(firstPatch), http.StatusOK)
	stalePatch := newAdversarialExitMutationRequest(t, h, http.MethodPatch, resumePath,
		[]byte(`{"title":"stale"}`), created.Revision, uuid.NewString(), "2", "application/json")
	assertPolicy("resume 412", perform(stalePatch), http.StatusPreconditionFailed)

	uploaded := h.uploadPhotoRequest(t, created.ID, created.Revision+1, uuid.NewString(), "cache.png", makePhotoPNG(t))
	if uploaded.status != http.StatusOK {
		t.Fatalf("upload setup status = %d, want 200 (body=%s)", uploaded.status, uploaded.body)
	}
	photoPath := resumePath + "/photo"
	photoRead := perform(readRequest(photoPath, h.cookie))
	assertPolicy("photo 200", photoRead, http.StatusOK)
	conditional := readRequest(photoPath, h.cookie)
	conditional.Header.Set("If-None-Match", photoRead.header.Get("ETag"))
	assertPolicy("photo 304", perform(conditional), http.StatusNotModified)

	missingPhoto := h.createResume(t)
	missingPhotoPath := apiResumePath + "/" + missingPhoto.ID.String() + "/photo"
	assertPolicy("photo error", perform(readRequest(missingPhotoPath, h.cookie)), http.StatusNotFound)
}

func TestEveryRoute_NoSession_401(t *testing.T) {
	h := newResumeAPITestHarness(t)
	before := snapshotSecurityExitState(t, h)
	beforeSessions := h.snapshotUserTable(t, "sessions")

	for _, operation := range securityExitOpenAPIResumeOperations(t) {
		operation := operation
		t.Run(operation.OperationID, func(t *testing.T) {
			response := securityExitRequest(t, h, operation.Method,
				concreteRoutePath(operation.Path), nil, "", nil, false)
			assertRouteError(t, response, http.StatusUnauthorized, "session_required")
			if got := response.header.Get("Cache-Control"); got != api.CacheControlNoStore {
				t.Fatalf("Cache-Control = %q, want %q", got, api.CacheControlNoStore)
			}
		})
	}

	assertSecurityExitState(t, h, before)
	if after := h.snapshotUserTable(t, "sessions"); after != beforeSessions {
		t.Fatalf("unauthenticated route matrix changed sessions: before=%q after=%q", beforeSessions, after)
	}
}

func TestMultipartOnlyOnPhotoUpload(t *testing.T) {
	h := newResumeAPITestHarness(t)
	storageCalls := &securityExitCountingBackend{Backend: h.service.blobs}
	idempotencyCalls := &securityExitCountingIdempotency{delegate: h.service.idempotency}
	h.service.blobs = storageCalls
	h.service.idempotency = idempotencyCalls
	operations := securityExitOpenAPIResumeOperations(t)
	var upload securityExitOpenAPIOperation
	before := snapshotSecurityExitState(t, h)
	beforeCalls := securityExitSnapshotCalls(storageCalls, idempotencyCalls)

	for _, operation := range operations {
		if operation.Method == http.MethodGet {
			continue
		}
		if securityExitHasContentType(operation, "multipart/form-data") {
			if upload.OperationID != "" {
				t.Fatalf("OpenAPI declares multiple multipart mutations: %s and %s", upload.OperationID, operation.OperationID)
			}
			upload = operation
			continue
		}
		t.Run(operation.OperationID+" rejects multipart", func(t *testing.T) {
			response := securityExitMutationRequest(t, h, operation,
				concreteRoutePath(operation.Path), strings.NewReader("--exit--\r\n"),
				"multipart/form-data; boundary=exit", 1, true)
			assertRouteError(t, response, http.StatusBadRequest, "request_invalid")
		})
	}
	if upload.OperationID == "" {
		t.Fatal("OpenAPI declares no multipart resume mutation")
	}
	assertSecurityExitPreflightCalls(t, storageCalls, idempotencyCalls, beforeCalls)
	assertSecurityExitState(t, h, before)

	created := h.createResume(t)
	uploadPath := strings.ReplaceAll(upload.Path, "{id}", created.ID.String())
	beforeJSONRejection := snapshotSecurityExitState(t, h)
	beforeJSONCalls := securityExitSnapshotCalls(storageCalls, idempotencyCalls)
	jsonRejected := securityExitMutationRequest(t, h, upload, uploadPath,
		strings.NewReader(`{"file":"not multipart"}`), "application/json", created.Revision, true)
	assertRouteError(t, jsonRejected, http.StatusUnsupportedMediaType, "media_type_unsupported")
	assertSecurityExitPreflightCalls(t, storageCalls, idempotencyCalls, beforeJSONCalls)
	assertSecurityExitState(t, h, beforeJSONRejection)

	accepted := h.uploadPhotoRequest(t, created.ID, created.Revision, uuid.NewString(), "photo.png", makePhotoPNG(t))
	if accepted.status != http.StatusOK {
		t.Fatalf("declared multipart upload status = %d, want 200 (body=%s)", accepted.status, accepted.body)
	}
}

func TestEveryMutation_MediaTypeMatrix_PreflightIsolation(t *testing.T) {
	h := newResumeAPITestHarness(t)
	storageCalls := &securityExitCountingBackend{Backend: h.service.blobs}
	idempotencyCalls := &securityExitCountingIdempotency{delegate: h.service.idempotency}
	h.service.blobs = storageCalls
	h.service.idempotency = idempotencyCalls
	before := snapshotSecurityExitState(t, h)
	mutations := 0

	for _, operation := range securityExitOpenAPIResumeOperations(t) {
		if operation.Method == http.MethodGet {
			continue
		}
		mutations++
		t.Run(operation.OperationID, func(t *testing.T) {
			var body io.Reader
			if operation.Method != http.MethodDelete {
				body = strings.NewReader(`{}`)
			}
			beforeRequest := securityExitSnapshotCalls(storageCalls, idempotencyCalls)
			response := securityExitMutationRequest(t, h, operation,
				concreteRoutePath(operation.Path), body, "text/plain", 1, true)
			wantStatus, wantCode := http.StatusBadRequest, "request_invalid"
			if securityExitHasContentType(operation, "multipart/form-data") {
				wantStatus, wantCode = http.StatusUnsupportedMediaType, "media_type_unsupported"
			}
			assertRouteError(t, response, wantStatus, wantCode)
			assertSecurityExitPreflightCalls(t, storageCalls, idempotencyCalls, beforeRequest)
		})
	}
	if mutations == 0 {
		t.Fatal("OpenAPI declares no resume mutations")
	}
	assertSecurityExitState(t, h, before)
}

func TestPhotoMethodPoliciesStayDistinct(t *testing.T) {
	h := newResumeAPITestHarness(t)
	storageCalls := &securityExitCountingBackend{Backend: h.service.blobs}
	idempotencyCalls := &securityExitCountingIdempotency{delegate: h.service.idempotency}
	h.service.blobs = storageCalls
	h.service.idempotency = idempotencyCalls
	created := h.createResume(t)
	path := apiResumePath + "/" + created.ID.String() + "/photo"
	upload := securityExitOperationByID(t, "uploadResumePhoto")
	crop := securityExitOperationByID(t, "updateResumePhotoCrop")
	deletePhoto := securityExitOperationByID(t, "deleteResumePhoto")
	body, multipartType := photoMultipartBody(t, func(writer *multipart.Writer) {
		part, err := writer.CreateFormFile("file", "photo.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(makePhotoPNG(t)); err != nil {
			t.Fatal(err)
		}
	})

	before := snapshotSecurityExitState(t, h)
	beforeCalls := securityExitSnapshotCalls(storageCalls, idempotencyCalls)
	missingUploadCSRF := securityExitMutationRequest(t, h, upload, path,
		bytes.NewReader(body), multipartType, created.Revision, false)
	assertRouteError(t, missingUploadCSRF, http.StatusForbidden, "csrf_rejected")
	wrongUploadMedia := securityExitMutationRequest(t, h, upload, path,
		strings.NewReader(`{}`), "application/json", created.Revision, true)
	assertRouteError(t, wrongUploadMedia, http.StatusUnsupportedMediaType, "media_type_unsupported")
	assertSecurityExitPreflightCalls(t, storageCalls, idempotencyCalls, beforeCalls)
	assertSecurityExitState(t, h, before)

	uploaded := h.uploadPhotoRequest(t, created.ID, created.Revision, uuid.NewString(), "photo.png", makePhotoPNG(t))
	if uploaded.status != http.StatusOK {
		t.Fatalf("multipart photo POST = %d, want 200 (body=%s)", uploaded.status, uploaded.body)
	}
	afterUpload := snapshotSecurityExitState(t, h)
	gotPhoto := h.request(t, http.MethodGet, path, nil, true, false)
	if gotPhoto.status != http.StatusOK {
		t.Fatalf("authenticated photo GET without CSRF = %d, want 200 (body=%s)", gotPhoto.status, gotPhoto.body)
	}
	assertSecurityExitState(t, h, afterUpload)
	afterGetCalls := securityExitSnapshotCalls(storageCalls, idempotencyCalls)

	missingCropCSRF := securityExitMutationRequest(t, h, crop, path,
		strings.NewReader(`{"crop":null}`), "application/json", 2, false)
	assertRouteError(t, missingCropCSRF, http.StatusForbidden, "csrf_rejected")
	wrongCropMedia := securityExitMutationRequest(t, h, crop, path,
		strings.NewReader("--exit--\r\n"), "multipart/form-data; boundary=exit", 2, true)
	assertRouteError(t, wrongCropMedia, http.StatusBadRequest, "request_invalid")
	assertSecurityExitPreflightCalls(t, storageCalls, idempotencyCalls, afterGetCalls)
	assertSecurityExitState(t, h, afterUpload)

	release, err := taskPhotoAdmission.Acquire(h.ctx)
	if err != nil {
		t.Fatalf("hold photo admission permit: %v", err)
	}
	permitHeld := true
	defer func() {
		if permitHeld {
			release()
		}
	}()
	cropped := h.mutationRequest(t, http.MethodPatch, path,
		strings.NewReader(`{"crop":null}`), 2, uuid.NewString())
	if cropped.status != http.StatusOK {
		t.Fatalf("JSON crop while upload permit held = %d, want 200 (body=%s)", cropped.status, cropped.body)
	}
	afterCrop := snapshotSecurityExitState(t, h)
	afterCropCalls := securityExitSnapshotCalls(storageCalls, idempotencyCalls)
	busyUpload := h.uploadPhotoRequest(t, created.ID, 3, uuid.NewString(), "photo.png", makePhotoPNG(t))
	assertRouteError(t, busyUpload, http.StatusServiceUnavailable, "media_busy")
	assertSecurityExitPreflightCalls(t, storageCalls, idempotencyCalls, afterCropCalls)
	assertSecurityExitState(t, h, afterCrop)
	release()
	permitHeld = false

	beforeDeleteCalls := securityExitSnapshotCalls(storageCalls, idempotencyCalls)
	missingDeleteCSRF := securityExitMutationRequest(t, h, deletePhoto, path, nil, "", 3, false)
	assertRouteError(t, missingDeleteCSRF, http.StatusForbidden, "csrf_rejected")
	deleteWithBody := securityExitMutationRequest(t, h, deletePhoto, path,
		strings.NewReader(`{}`), "application/json", 3, true)
	assertRouteError(t, deleteWithBody, http.StatusBadRequest, "request_invalid")
	assertSecurityExitPreflightCalls(t, storageCalls, idempotencyCalls, beforeDeleteCalls)
	assertSecurityExitState(t, h, afterCrop)
	bodylessDelete := h.mutationRequest(t, http.MethodDelete, path, nil, 3, uuid.NewString())
	if bodylessDelete.status != http.StatusNoContent || len(bodylessDelete.body) != 0 {
		t.Fatalf("bodyless photo DELETE = %d body=%q, want 204 with empty body", bodylessDelete.status, bodylessDelete.body)
	}

	t.Run("upload rate budget is independent from crop", func(t *testing.T) {
		rateHarness := newResumeAPITestHarness(t)
		rateResume := rateHarness.createResume(t)
		ratePath := apiResumePath + "/" + rateResume.ID.String() + "/photo"
		for attempt := 1; attempt <= resumeUploadRequests+1; attempt++ {
			response := securityExitMutationRequest(t, rateHarness, upload, ratePath,
				strings.NewReader("--test--\r\n"), "multipart/form-data; boundary=test", 1, true)
			wantStatus := http.StatusBadRequest
			wantCode := "request_invalid"
			if attempt == resumeUploadRequests+1 {
				wantStatus = http.StatusTooManyRequests
				wantCode = "rate_limited"
			}
			assertRouteError(t, response, wantStatus, wantCode)
		}
		cropAfterUploadLimit := securityExitMutationRequest(t, rateHarness, crop, ratePath,
			strings.NewReader(`{"crop":null}`), "application/json", 1, true)
		assertRouteError(t, cropAfterUploadLimit, http.StatusNotFound, "media_not_found")
	})
}

func TestSessionRevokedBetweenRequests(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	path := apiResumePath + "/" + created.ID.String()

	first := resumeRequest(t, h, http.MethodPatch, path, `{"title":"before revoke"}`,
		created.Revision, uuid.New(), "2")
	if first.status != http.StatusOK {
		t.Fatalf("first authenticated resume request = %d, want 200 (body=%s)", first.status, first.body)
	}
	beforeRejected := snapshotSecurityExitState(t, h)
	if err := h.service.sessions.Revoke(h.ctx, h.session.ID); err != nil {
		t.Fatalf("revoke session between requests: %v", err)
	}

	second := resumeRequest(t, h, http.MethodPatch, path, `{"title":"after revoke"}`,
		created.Revision+1, uuid.New(), "2")
	assertRouteError(t, second, http.StatusUnauthorized, "session_required")
	if got := second.header.Get("Cache-Control"); got != api.CacheControlNoStore {
		t.Fatalf("revoked-session Cache-Control = %q, want %q", got, api.CacheControlNoStore)
	}
	assertSecurityExitState(t, h, beforeRejected)
}

func TestGetSafeMethodsBypassCSRFButNotAuth(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	operations := securityExitOpenAPIResumeOperations(t)
	before := snapshotSecurityExitState(t, h)
	getPaths := make(map[string]struct{})

	for _, operation := range operations {
		if operation.Method != http.MethodGet {
			continue
		}
		path := strings.ReplaceAll(operation.Path, "{id}", created.ID.String())
		getPaths[path] = struct{}{}
		t.Run(operation.OperationID, func(t *testing.T) {
			withoutAuth := securityExitRequest(t, h, http.MethodGet, path, nil, "", nil, false)
			assertRouteError(t, withoutAuth, http.StatusUnauthorized, "session_required")

			withAuth := securityExitRequest(t, h, http.MethodGet, path, nil, "", h.cookie, false)
			wantStatus := http.StatusOK
			if operation.OperationID == "getResumePhoto" {
				wantStatus = http.StatusNotFound
			}
			if withAuth.status != wantStatus {
				t.Fatalf("authenticated GET without CSRF = %d, want %d (body=%s)", withAuth.status, wantStatus, withAuth.body)
			}
			if operation.OperationID == "downloadResumePDF" {
				if !bytes.Equal(withAuth.body, []byte(resumeAPITestPDF)) {
					t.Fatalf("authenticated PDF body = %q, want fixed test PDF", withAuth.body)
				}
				if withAuth.header.Get("Content-Type") != "application/pdf" ||
					withAuth.header.Get("Content-Disposition") != `attachment; filename="resume.pdf"` {
					t.Fatalf("authenticated PDF headers = %v", withAuth.header)
				}
			}

			head := securityExitRequest(t, h, http.MethodHead, path, nil, "", h.cookie, false)
			if head.status != wantStatus {
				t.Fatalf("authenticated HEAD without CSRF = %d, want %d", head.status, wantStatus)
			}
			if len(head.body) != 0 {
				t.Fatalf("authenticated HEAD body = %q, want empty", head.body)
			}
			if operation.OperationID == "downloadResumePDF" &&
				(head.header.Get("Content-Type") != "application/pdf" ||
					head.header.Get("Content-Disposition") != `attachment; filename="resume.pdf"`) {
				t.Fatalf("authenticated PDF HEAD headers = %v", head.header)
			}
		})
	}

	seenPaths := make(map[string]struct{})
	for _, operation := range operations {
		path := strings.ReplaceAll(operation.Path, "{id}", created.ID.String())
		if _, seen := seenPaths[path]; seen {
			continue
		}
		seenPaths[path] = struct{}{}
		if _, hasGet := getPaths[path]; !hasGet {
			head := securityExitRequest(t, h, http.MethodHead, concreteRoutePath(path), nil, "", h.cookie, false)
			if head.status != http.StatusMethodNotAllowed {
				t.Fatalf("HEAD on mutation-only path %s = %d, want 405", path, head.status)
			}
		}
		response := securityExitRequest(t, h, http.MethodOptions, concreteRoutePath(path), nil, "", nil, false)
		assertRouteError(t, response, http.StatusMethodNotAllowed, "method_not_allowed")
	}
	assertSecurityExitState(t, h, before)
}

func TestErrorBodiesLeakNothing(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	path := apiResumePath + "/" + created.ID.String()
	var responses []struct {
		name     string
		response testHTTPResponse
	}
	add := func(name string, response testHTTPResponse, status int, code string) {
		t.Helper()
		assertRouteError(t, response, status, code)
		responses = append(responses, struct {
			name     string
			response testHTTPResponse
		}{name: name, response: response})
	}

	add("session required", h.request(t, http.MethodGet, path, nil, false, false),
		http.StatusUnauthorized, "session_required")
	add("csrf rejected", securityExitRequest(t, h, http.MethodPost, apiResumePath,
		strings.NewReader(`{"title":"x"}`), "application/json", h.cookie, false),
		http.StatusForbidden, "csrf_rejected")
	add("request invalid", h.request(t, http.MethodGet, apiResumePath+"/not-a-uuid", nil, true, false),
		http.StatusBadRequest, "request_invalid")
	add("resume not found", h.request(t, http.MethodGet, apiResumePath+"/"+uuid.NewString(), nil, true, false),
		http.StatusNotFound, "resume_not_found")
	add("method not allowed", h.request(t, http.MethodOptions, path, nil, false, false),
		http.StatusMethodNotAllowed, "method_not_allowed")
	add("document invalid", resumeRequest(t, h, http.MethodPost, apiResumePath,
		`{"title":null}`, 0, uuid.New(), "2"), http.StatusUnprocessableEntity, "document_invalid")
	add("revision mismatch", resumeRequest(t, h, http.MethodPatch, path,
		`{"title":"stale"}`, created.Revision+1, uuid.New(), "2"),
		http.StatusPreconditionFailed, "revision_mismatch")
	malformedPNG := makePhotoPNG(t)
	malformedPNG = malformedPNG[:len(malformedPNG)-1]
	add("media invalid", h.uploadPhotoRequest(t, created.ID, created.Revision,
		uuid.NewString(), "bad.png", malformedPNG),
		http.StatusUnprocessableEntity, "media_invalid")

	foreignCookie, foreignToken := issueAdditionalTestSession(t, h)
	add("cross user", crossUserRouteRequest(t, h,
		routeByOperationForTest(t, "getResume"), created.ID, foreignCookie, foreignToken),
		http.StatusNotFound, "resume_not_found")

	uploaded := h.uploadPhotoRequest(t, created.ID, created.Revision, uuid.NewString(), "photo.png", makePhotoPNG(t))
	if uploaded.status != http.StatusOK {
		t.Fatalf("upload leak fixture = %d, want 200 (body=%s)", uploaded.status, uploaded.body)
	}
	_, document := decodedWrittenDocument(t, uploaded)
	if document.PersonalDetails.Photo == nil {
		t.Fatal("uploaded leak fixture has no photo")
	}
	objectKey := document.PersonalDetails.Photo.Key
	sentinels := []string{
		"select password_hash from users",
		"adversarial_security_exit.go:777",
		objectKey,
		"aboutme-private-bucket-sentinel",
		"postgres.internal.example:5432",
		h.userID.String(),
	}
	h.service.blobs = &securityExitLeakyGetBackend{
		Backend: h.service.blobs,
		err:     fmt.Errorf("backend failure: %s", strings.Join(sentinels, " | ")),
	}
	add("opaque internal storage error", h.getPhotoRequest(t, created.ID, ""),
		http.StatusInternalServerError, "internal_error")

	for _, test := range responses {
		wire := strings.ToLower(string(test.response.body) + "\n" + fmt.Sprint(test.response.header))
		for _, sentinel := range sentinels {
			if strings.Contains(wire, strings.ToLower(sentinel)) {
				t.Errorf("%s response leaked sentinel %q: status=%d headers=%v body=%s",
					test.name, sentinel, test.response.status, test.response.header, test.response.body)
			}
		}
	}
}

func TestStorageSecretsNeverLeak(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	baseBackend := h.service.blobs
	accessSentinel := "TEST_ACCESS_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	secretSentinel := "TEST_SECRET_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	leakyError := fmt.Errorf("storage credentials access=%s secret=%s", accessSentinel, secretSentinel)
	var logs bytes.Buffer
	h.service.logger = slog.New(slog.NewJSONHandler(&logs, nil))

	invariantMetricCalls := 0
	normalizationMetricCalls := 0
	invariantMetric := func() { invariantMetricCalls++ }
	normalizationMetric := func(time.Duration) { normalizationMetricCalls++ }
	h.service.photoKeyInvariant = invariantMetric
	h.service.photoNormalizationDuration = normalizationMetric

	putBackend := &securityExitLeakyBackend{Backend: baseBackend, putErr: leakyError}
	h.service.blobs = putBackend
	var putResponse testHTTPResponse
	securityExitAssertNoPanic(t, "Put failure", func() {
		putResponse = h.uploadPhotoRequest(t, created.ID, created.Revision,
			uuid.NewString(), "photo.png", makePhotoPNG(t))
	})
	assertRouteError(t, putResponse, http.StatusInternalServerError, "internal_error")
	if putBackend.puts != 1 {
		t.Fatalf("leaky Put calls = %d, want 1", putBackend.puts)
	}
	assertSecurityExitSentinelsAbsent(t, "Put response", putResponse.header, putResponse.body,
		accessSentinel, secretSentinel)

	h.service.blobs = baseBackend
	uploaded := h.uploadPhotoRequest(t, created.ID, created.Revision,
		uuid.NewString(), "photo.png", makePhotoPNG(t))
	if uploaded.status != http.StatusOK {
		t.Fatalf("seed photo = %d, want 200 (body=%s)", uploaded.status, uploaded.body)
	}
	_, document := decodedWrittenDocument(t, uploaded)
	if document.PersonalDetails.Photo == nil {
		t.Fatal("seed photo response omitted photo metadata")
	}
	candidateKey := document.PersonalDetails.Photo.Key

	getBackend := &securityExitLeakyBackend{Backend: baseBackend, getErr: leakyError}
	h.service.blobs = getBackend
	var getResponse testHTTPResponse
	securityExitAssertNoPanic(t, "Get failure", func() {
		getResponse = h.getPhotoRequest(t, created.ID, "")
	})
	assertRouteError(t, getResponse, http.StatusInternalServerError, "internal_error")
	if getBackend.gets != 1 {
		t.Fatalf("leaky Get calls = %d, want 1", getBackend.gets)
	}
	assertSecurityExitSentinelsAbsent(t, "Get response", getResponse.header, getResponse.body,
		accessSentinel, secretSentinel)

	deleteBackend := &securityExitLeakyBackend{Backend: baseBackend, deleteErr: leakyError}
	h.service.blobs = deleteBackend
	securityExitAssertNoPanic(t, "candidate cleanup Delete failure", func() {
		h.service.finalizePhotoCandidate(photoCandidate{Key: candidateKey, Created: true})(
			h.ctx, preparedInput{}, resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, nil)
	})
	if deleteBackend.deletes != 1 {
		t.Fatalf("leaky cleanup Delete calls = %d, want 1", deleteBackend.deletes)
	}
	if !strings.Contains(logs.String(), "resume photo candidate cleanup failed") {
		t.Fatalf("cleanup log = %q, want generic cleanup failure signal", logs.String())
	}
	for _, sentinel := range []string{accessSentinel, secretSentinel, candidateKey, leakyError.Error()} {
		if strings.Contains(logs.String(), sentinel) {
			t.Fatalf("structured log leaked sentinel %q: %s", sentinel, logs.String())
		}
	}
	if invariantMetricCalls != 0 {
		t.Fatalf("key-invariant metric calls = %d, want zero", invariantMetricCalls)
	}
	if normalizationMetricCalls < 2 {
		t.Fatalf("normalization metric calls = %d, want Put failure and seed upload signals", normalizationMetricCalls)
	}
}

func securityExitAssertNoPanic(t *testing.T, operation string, run func()) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("%s panicked: %v", operation, recovered)
		}
	}()
	run()
}

func assertSecurityExitSentinelsAbsent(t *testing.T, operation string, header http.Header, body []byte,
	sentinels ...string,
) {
	t.Helper()
	wire := string(body) + "\n" + fmt.Sprint(header)
	for _, sentinel := range sentinels {
		if strings.Contains(wire, sentinel) {
			t.Fatalf("%s leaked sentinel %q: headers=%v body=%s", operation, sentinel, header, body)
		}
	}
}

type securityExitLeakyBackend struct {
	media.Backend
	putErr    error
	getErr    error
	deleteErr error
	puts      int
	gets      int
	deletes   int
}

func (b *securityExitLeakyBackend) Put(context.Context, string, string, io.Reader, int64) (media.PutOutcome, error) {
	b.puts++
	return media.PutNotCreated, b.putErr
}

func (b *securityExitLeakyBackend) Get(context.Context, string) (io.ReadCloser, string, error) {
	b.gets++
	return nil, "", b.getErr
}

func (b *securityExitLeakyBackend) Delete(context.Context, string) error {
	b.deletes++
	return b.deleteErr
}

type securityExitLeakyGetBackend struct {
	media.Backend
	err error
}

func (b *securityExitLeakyGetBackend) Get(context.Context, string) (io.ReadCloser, string, error) {
	return nil, "", b.err
}

type securityExitCountingBackend struct {
	media.Backend
	puts    int
	gets    int
	deletes int
	lists   int
}

func (b *securityExitCountingBackend) Put(ctx context.Context, key, contentType string,
	body io.Reader, size int64,
) (media.PutOutcome, error) {
	b.puts++
	return b.Backend.Put(ctx, key, contentType, body, size)
}

func (b *securityExitCountingBackend) Get(ctx context.Context, key string) (io.ReadCloser, string, error) {
	b.gets++
	return b.Backend.Get(ctx, key)
}

func (b *securityExitCountingBackend) Delete(ctx context.Context, key string) error {
	b.deletes++
	return b.Backend.Delete(ctx, key)
}

func (b *securityExitCountingBackend) ListPage(ctx context.Context, prefix, cursor string,
	limit int,
) ([]media.Object, string, error) {
	b.lists++
	return b.Backend.ListPage(ctx, prefix, cursor, limit)
}

type securityExitCountingIdempotency struct {
	delegate    idempotencyBoundary
	inspections int
	executions  int
}

func (i *securityExitCountingIdempotency) Inspect(ctx context.Context, userID uuid.UUID,
	operation string, key uuid.UUID, fingerprint [32]byte,
) (resume.StoredResponse, bool, error) {
	i.inspections++
	return i.delegate.Inspect(ctx, userID, operation, key, fingerprint)
}

func (i *securityExitCountingIdempotency) Recheck(ctx context.Context, userID uuid.UUID,
	operation string, key uuid.UUID, fingerprint [32]byte,
) (resume.RecheckResult, error) {
	return i.delegate.Recheck(ctx, userID, operation, key, fingerprint)
}

func (i *securityExitCountingIdempotency) Execute(ctx context.Context, userID uuid.UUID,
	operation string, key uuid.UUID, fingerprint [32]byte,
	run func(*store.Queries) (resume.StoredResponse, error),
) (resume.ExecuteResult, error) {
	i.executions++
	return i.delegate.Execute(ctx, userID, operation, key, fingerprint, run)
}

type securityExitCallCounts struct {
	puts        int
	gets        int
	deletes     int
	lists       int
	inspections int
	executions  int
}

func securityExitSnapshotCalls(storage *securityExitCountingBackend,
	idempotency *securityExitCountingIdempotency,
) securityExitCallCounts {
	return securityExitCallCounts{
		puts: storage.puts, gets: storage.gets, deletes: storage.deletes, lists: storage.lists,
		inspections: idempotency.inspections, executions: idempotency.executions,
	}
}

func assertSecurityExitPreflightCalls(t *testing.T, storage *securityExitCountingBackend,
	idempotency *securityExitCountingIdempotency, want securityExitCallCounts,
) {
	t.Helper()
	if got := securityExitSnapshotCalls(storage, idempotency); got != want {
		t.Fatalf("preflight rejection reached storage or idempotency: got=%+v want=%+v", got, want)
	}
}

type securityExitState struct {
	resumes   string
	records   string
	jobs      string
	objectIDs []string
}

func snapshotSecurityExitState(t *testing.T, h *resumeAPITestHarness) securityExitState {
	t.Helper()
	var jobs string
	if err := h.pool.QueryRow(h.ctx, `
		SELECT coalesce(string_agg(job::text, '|' ORDER BY job::text), '')
		FROM media_deletion_jobs job
		JOIN resumes ON resumes.id = job.resume_id
		WHERE resumes.user_id = $1`, h.userID).Scan(&jobs); err != nil {
		t.Fatalf("snapshot media deletion jobs: %v", err)
	}
	return securityExitState{
		resumes:   h.snapshotUserTable(t, "resumes"),
		records:   h.snapshotUserTable(t, "idempotency_records"),
		jobs:      jobs,
		objectIDs: snapshotObjectKeys(t, h),
	}
}

func assertSecurityExitState(t *testing.T, h *resumeAPITestHarness, want securityExitState) {
	t.Helper()
	if got := snapshotSecurityExitState(t, h); !reflect.DeepEqual(got, want) {
		t.Fatalf("rejected or safe request changed state:\n got=%+v\nwant=%+v", got, want)
	}
}

func securityExitOpenAPIResumeOperations(t *testing.T) []securityExitOpenAPIOperation {
	t.Helper()
	raw, err := os.ReadFile("../../../../docs/api/openapi.yaml")
	if err != nil {
		t.Fatalf("read OpenAPI: %v", err)
	}
	type operation struct {
		OperationID string `yaml:"operationId"`
		RequestBody *struct {
			Content map[string]any `yaml:"content"`
		} `yaml:"requestBody"`
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

	var operations []securityExitOpenAPIOperation
	for path, item := range document.Paths {
		if !strings.HasPrefix(path, "/resumes") {
			continue
		}
		for method, declared := range map[string]*operation{
			http.MethodGet: item.Get, http.MethodPost: item.Post,
			http.MethodPatch: item.Patch, http.MethodDelete: item.Delete,
		} {
			if declared == nil {
				continue
			}
			contentTypes := make([]string, 0)
			if declared.RequestBody != nil {
				for contentType := range declared.RequestBody.Content {
					contentTypes = append(contentTypes, contentType)
				}
				sort.Strings(contentTypes)
			}
			operations = append(operations, securityExitOpenAPIOperation{
				Method: method, Path: "/api/v1" + path,
				OperationID: declared.OperationID, ContentTypes: contentTypes,
			})
		}
	}
	sort.Slice(operations, func(i, j int) bool {
		return operations[i].Method+" "+operations[i].Path < operations[j].Method+" "+operations[j].Path
	})
	if len(operations) == 0 {
		t.Fatal("OpenAPI declares no resume operations")
	}
	return operations
}

func securityExitOperationByID(t *testing.T, operationID string) securityExitOpenAPIOperation {
	t.Helper()
	for _, operation := range securityExitOpenAPIResumeOperations(t) {
		if operation.OperationID == operationID {
			return operation
		}
	}
	t.Fatalf("OpenAPI operation %q not found", operationID)
	return securityExitOpenAPIOperation{}
}

func securityExitHasContentType(operation securityExitOpenAPIOperation, contentType string) bool {
	for _, declared := range operation.ContentTypes {
		if declared == contentType {
			return true
		}
	}
	return false
}

func securityExitMutationRequest(t *testing.T, h *resumeAPITestHarness,
	operation securityExitOpenAPIOperation, path string, body io.Reader, contentType string,
	revision int64, csrf bool,
) testHTTPResponse {
	t.Helper()
	request, err := http.NewRequestWithContext(h.ctx, operation.Method, h.server.URL+path, body)
	if err != nil {
		t.Fatalf("build %s request: %v", operation.OperationID, err)
	}
	request.AddCookie(h.cookie)
	request.Header.Set("Origin", resumeAPITestOrigin)
	if csrf {
		request.Header.Set(auth.CSRFHeaderName, h.csrfToken)
	}
	request.Header.Set("Idempotency-Key", uuid.NewString())
	if operation.Method != http.MethodPost || operation.Path != apiResumePath {
		request.Header.Set("If-Match", fmt.Sprintf(`"r%d"`, revision))
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := h.client.Do(request)
	if err != nil {
		t.Fatalf("perform %s request: %v", operation.OperationID, err)
	}
	return snapshotHTTPResponse(t, response)
}

func securityExitRequest(t *testing.T, h *resumeAPITestHarness, method, path string,
	body io.Reader, contentType string, cookie *http.Cookie, csrf bool,
) testHTTPResponse {
	t.Helper()
	request, err := http.NewRequestWithContext(h.ctx, method, h.server.URL+path, body)
	if err != nil {
		t.Fatalf("build %s %s request: %v", method, path, err)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if csrf {
		request.Header.Set("Origin", resumeAPITestOrigin)
		request.Header.Set(auth.CSRFHeaderName, h.csrfToken)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := h.client.Do(request)
	if err != nil {
		t.Fatalf("perform %s %s request: %v", method, path, err)
	}
	return snapshotHTTPResponse(t, response)
}
