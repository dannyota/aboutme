package resumeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/printsnapshot"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func TestPDFRouteIsRegistered(t *testing.T) {
	t.Parallel()

	for _, route := range registeredRoutes() {
		if route.Method == http.MethodGet && route.Pattern == apiResumePath+"/{id}/pdf" && route.Operation == "downloadResumePDF" {
			return
		}
	}
	t.Fatal("GET /api/v1/resumes/{id}/pdf is not registered")
}

type pdfQueueFunc func(context.Context, renderjob.Request) (renderjob.Result, error)

func (f pdfQueueFunc) Render(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
	return f(ctx, request)
}

type pdfResumeReader struct {
	resumeBoundary
	get func(context.Context, uuid.UUID, uuid.UUID) (resume.Resume, error)
}

func (s pdfResumeReader) Get(ctx context.Context, userID, resumeID uuid.UUID) (resume.Resume, error) {
	return s.get(ctx, userID, resumeID)
}

type pdfBackend struct {
	media.Backend
	gets        []string
	body        io.ReadCloser
	contentType string
	err         error
}

func (b *pdfBackend) Get(_ context.Context, key string) (io.ReadCloser, string, error) {
	b.gets = append(b.gets, key)
	return b.body, b.contentType, b.err
}

func pdfRequest(t *testing.T, method string, resumeID uuid.UUID, body io.Reader) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, apiResumePath+"/"+resumeID.String()+"/pdf", body)
	req.SetPathValue("id", resumeID.String())
	req = req.WithContext(auth.ContextWithSession(req.Context(), store.Session{UserID: uuid.MustParse("10000000-0000-4000-8000-000000000001")}))
	return req
}

func pdfService(row resume.Resume, queue PrintQueue, backend media.Backend) *Service {
	return &Service{
		resumes: pdfResumeReader{get: func(_ context.Context, userID, resumeID uuid.UUID) (resume.Resume, error) {
			if userID != row.UserID || resumeID != row.ID {
				return resume.Resume{}, resume.ErrNotFound
			}
			return row, nil
		}},
		blobs: backend, printQueue: queue,
	}
}

func TestPDFGetAndHeadEmitFixedDownloadResponse(t *testing.T) {
	t.Parallel()

	userID := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	resumeID := uuid.MustParse("20000000-0000-4000-8000-000000000001")
	document := loadMinimalDocument(t)
	frozenName := "Frozen name"
	document.PersonalDetails.FullName = &frozenName
	row := resume.Resume{ID: resumeID, UserID: userID, Revision: 7, Doc: document}
	backend := &pdfBackend{}
	pdfBytes := []byte("%PDF-1.7\nfrozen")
	queue := pdfQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
		if request.Format != renderjob.PDF || request.ValidateGeneration != nil {
			t.Fatalf("render request = %+v", request)
		}
		snapshot, err := request.Prepare(ctx)
		if err != nil {
			t.Fatalf("Prepare() error = %v", err)
		}
		if snapshot.ResumeID != resumeID || snapshot.Revision != 7 || snapshot.SchemaVersion != int(schema.CurrentVersion) || snapshot.PublicGeneration != 0 {
			t.Fatalf("snapshot bindings = %+v", snapshot)
		}
		var envelope struct {
			PublicGeneration *string `json:"publicGeneration"`
			Document         struct {
				PersonalDetails struct {
					FullName string `json:"fullName"`
				} `json:"personalDetails"`
			} `json:"document"`
		}
		if err := json.Unmarshal(snapshot.Payload, &envelope); err != nil {
			t.Fatalf("decode frozen payload: %v", err)
		}
		if envelope.Document.PersonalDetails.FullName != frozenName || envelope.PublicGeneration != nil {
			t.Fatalf("frozen envelope = %+v", envelope)
		}
		return renderjob.Result{Bytes: append([]byte(nil), pdfBytes...), Revision: snapshot.Revision}, nil
	})
	service := pdfService(row, queue, backend)

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			service.handleDownloadResumePDF(recorder, pdfRequest(t, method, resumeID, nil))
			response := recorder.Result()
			body, readErr := io.ReadAll(response.Body)
			closeErr := response.Body.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status = %d body=%s", response.StatusCode, body)
			}
			if response.Header.Get("Content-Type") != "application/pdf" || response.Header.Get("Content-Disposition") != `attachment; filename="resume.pdf"` || response.Header.Get("Cache-Control") != api.CacheControlNoStore {
				t.Fatalf("headers = %v", response.Header)
			}
			if response.Header.Get("Content-Length") != strconv.Itoa(len(pdfBytes)) {
				t.Fatalf("Content-Length = %q", response.Header.Get("Content-Length"))
			}
			if response.Header.Get(wireVersionHeader) != "" {
				t.Fatalf("%s = %q, want absent", wireVersionHeader, response.Header.Get(wireVersionHeader))
			}
			if method == http.MethodHead && len(body) != 0 {
				t.Fatalf("HEAD body = %q, want empty", body)
			}
			if method == http.MethodGet && !bytes.Equal(body, pdfBytes) {
				t.Fatalf("GET body = %q, want %q", body, pdfBytes)
			}
		})
	}
	if len(backend.gets) != 0 {
		t.Fatalf("photo backend calls = %v, want none", backend.gets)
	}
}

func TestPDFFrozenSnapshotSurvivesLaterEdit(t *testing.T) {
	t.Parallel()

	userID := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	resumeID := uuid.MustParse("20000000-0000-4000-8000-000000000010")
	frozenName := "Frozen name"
	document := loadMinimalDocument(t)
	document.PersonalDetails.FullName = &frozenName
	current := resume.Resume{ID: resumeID, UserID: userID, Revision: 7, Doc: document}
	queue := pdfQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
		snapshot, err := request.Prepare(ctx)
		if err != nil {
			t.Fatal(err)
		}
		laterName := "Later edit"
		current.Revision = 8
		current.Doc.PersonalDetails.FullName = &laterName
		var frozen struct {
			Revision string `json:"revision"`
			Document struct {
				PersonalDetails struct {
					FullName string `json:"fullName"`
				} `json:"personalDetails"`
			} `json:"document"`
		}
		if err := json.Unmarshal(snapshot.Payload, &frozen); err != nil {
			t.Fatal(err)
		}
		if snapshot.Revision != 7 || frozen.Revision != "7" || frozen.Document.PersonalDetails.FullName != frozenName {
			t.Fatalf("snapshot = %+v, envelope = %+v", snapshot, frozen)
		}
		return renderjob.Result{Bytes: []byte("%PDF-frozen"), Revision: snapshot.Revision}, nil
	})
	service := &Service{
		resumes: pdfResumeReader{get: func(context.Context, uuid.UUID, uuid.UUID) (resume.Resume, error) {
			return current, nil
		}},
		printQueue: queue,
	}
	recorder := httptest.NewRecorder()
	service.handleDownloadResumePDF(recorder, pdfRequest(t, http.MethodGet, resumeID, nil))
	if recorder.Code != http.StatusOK || current.Revision != 8 {
		t.Fatalf("response = %d, current revision = %d", recorder.Code, current.Revision)
	}
}

type pdfRedeemingRenderer struct {
	queue    *renderjob.Queue
	redeemed chan renderjob.Snapshot
}

func (r *pdfRedeemingRenderer) Render(ctx context.Context, navigation renderjob.Navigation) ([]byte, error) {
	snapshot, err := r.queue.Redeem(ctx, renderjob.Redemption{
		ResumeID: navigation.ResumeID, JobID: navigation.JobID,
		Audience: "nuxt-print", Capability: navigation.Capability,
	})
	if err != nil {
		return nil, err
	}
	r.redeemed <- snapshot
	return []byte("%PDF-owner"), nil
}

func TestPDFDefaultResumeFullNameReachesRenderer(t *testing.T) {
	t.Parallel()

	projector := docmigrate.NewIdentityProjector()
	service := &Service{projector: projector}
	replacement := map[string]json.RawMessage{
		"fullName": json.RawMessage(`"Synthetic Owner"`),
		"details":  json.RawMessage(`[]`),
	}
	document, err := service.applyAtWireVersion(
		defaultResumeDocument(), docmigrate.CurrentVersion,
		func(wire json.RawMessage) (json.RawMessage, error) {
			return replaceWirePersonalDetails(wire, replacement)
		},
	)
	if err != nil {
		t.Fatalf("apply accepted Full name update: %v", err)
	}
	if document.PersonalDetails.FullName == nil || *document.PersonalDetails.FullName != "Synthetic Owner" {
		t.Fatal("accepted Full name was not present in the current document")
	}

	owner := resume.Resume{
		ID:       uuid.MustParse("20000000-0000-4000-8000-000000000012"),
		UserID:   uuid.MustParse("10000000-0000-4000-8000-000000000001"),
		Revision: 2, Doc: document,
	}
	envelope, err := printsnapshot.FromOwner(owner, nil, "")
	if err != nil {
		t.Fatalf("FromOwner(default resume after Full name): %v", err)
	}
	wantPayload, err := printsnapshot.Marshal(envelope)
	if err != nil {
		t.Fatalf("Marshal(default owner envelope): %v", err)
	}

	renderer := &pdfRedeemingRenderer{redeemed: make(chan renderjob.Snapshot, 1)}
	queue, err := renderjob.New(renderjob.Config{Renderer: renderer})
	if err != nil {
		t.Fatalf("construct real render queue: %v", err)
	}
	renderer.queue = queue
	t.Cleanup(func() {
		if err := queue.Close(); err != nil {
			t.Errorf("close render queue: %v", err)
		}
	})

	recorder := httptest.NewRecorder()
	pdfService(owner, queue, nil).handleDownloadResumePDF(recorder, pdfRequest(t, http.MethodGet, owner.ID, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("owner PDF status = %d, want 200", recorder.Code)
	}
	select {
	case snapshot := <-renderer.redeemed:
		if snapshot.ResumeID != owner.ID || snapshot.Revision != owner.Revision ||
			snapshot.SchemaVersion != int(docmigrate.CurrentVersion) || snapshot.PublicGeneration != 0 ||
			!bytes.Equal(snapshot.Payload, wantPayload) {
			t.Fatalf("redeemed snapshot bindings invalid: id=%v revision=%d schema=%d public=%d bytes=%d",
				snapshot.ResumeID, snapshot.Revision, snapshot.SchemaVersion,
				snapshot.PublicGeneration, len(snapshot.Payload))
		}
	default:
		t.Fatal("renderer was not reached")
	}
}

func TestPDFRouteSessionAndOwnerPrivacyMatrix(t *testing.T) {
	h := newResumeAPITestHarness(t)
	owner := h.createResume(t)
	foreignUser, err := h.queries.CreateUser(h.ctx, store.CreateUserParams{
		Email: "pdf-foreign-" + uuid.NewString() + "@example.test", Name: "PDF foreign owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := h.resumes.Create(h.ctx, foreignUser.ID, "Foreign", loadMinimalDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	queueCalls := 0
	h.service.printQueue = pdfQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
		queueCalls++
		snapshot, prepareErr := request.Prepare(ctx)
		if prepareErr != nil {
			return renderjob.Result{}, prepareErr
		}
		return renderjob.Result{Bytes: []byte("%PDF-owner"), Revision: snapshot.Revision}, nil
	})

	unauthenticated := h.request(t, http.MethodGet, apiResumePath+"/"+owner.ID.String()+"/pdf", nil, false, false)
	assertRouteError(t, unauthenticated, http.StatusUnauthorized, "session_required")
	if queueCalls != 0 {
		t.Fatalf("unauthenticated request reached queue %d times", queueCalls)
	}

	invalidRequest, err := http.NewRequestWithContext(h.ctx, http.MethodGet, h.server.URL+apiResumePath+"/"+owner.ID.String()+"/pdf", nil)
	if err != nil {
		t.Fatal(err)
	}
	invalidRequest.AddCookie(&http.Cookie{Name: "__Host-session", Value: "invalid"})
	invalidResponse, err := h.client.Do(invalidRequest)
	if err != nil {
		t.Fatal(err)
	}
	invalid := snapshotHTTPResponse(t, invalidResponse)
	assertRouteError(t, invalid, http.StatusUnauthorized, "session_required")
	if queueCalls != 0 {
		t.Fatalf("invalid session reached queue %d times", queueCalls)
	}

	missing := h.request(t, http.MethodGet, apiResumePath+"/"+uuid.NewString()+"/pdf", nil, true, false)
	foreignResponse := h.request(t, http.MethodGet, apiResumePath+"/"+foreign.ID.String()+"/pdf", nil, true, false)
	assertRouteError(t, missing, http.StatusNotFound, "resume_not_found")
	assertRouteError(t, foreignResponse, http.StatusNotFound, "resume_not_found")
	if !bytes.Equal(missing.body, foreignResponse.body) {
		t.Fatalf("missing body = %s, foreign body = %s", missing.body, foreignResponse.body)
	}

	owned := h.request(t, http.MethodGet, apiResumePath+"/"+owner.ID.String()+"/pdf", nil, true, false)
	if owned.status != http.StatusOK || !bytes.Equal(owned.body, []byte("%PDF-owner")) {
		t.Fatalf("owner response = %d %q", owned.status, owned.body)
	}
}

func TestPDFRejectsUnsupportedRequestOptionsBeforeQueue(t *testing.T) {
	t.Parallel()

	resumeID := uuid.MustParse("20000000-0000-4000-8000-000000000002")
	row := resume.Resume{ID: resumeID, UserID: uuid.MustParse("10000000-0000-4000-8000-000000000001"), Revision: 1, Doc: loadMinimalDocument(t)}
	queueCalls := 0
	service := pdfService(row, pdfQueueFunc(func(context.Context, renderjob.Request) (renderjob.Result, error) {
		queueCalls++
		return renderjob.Result{}, nil
	}), nil)

	tests := []struct {
		name   string
		body   io.Reader
		mutate func(*http.Request)
	}{
		{name: "query", mutate: func(r *http.Request) { r.URL.RawQuery = "download=1" }},
		{name: "empty query marker", mutate: func(r *http.Request) { r.URL.ForceQuery = true }},
		{name: "body", body: strings.NewReader("x")},
		{name: "unknown length body", body: strings.NewReader("x"), mutate: func(r *http.Request) { r.ContentLength = -1 }},
		{name: "schema negotiation", mutate: func(r *http.Request) { r.Header.Set(wireVersionHeader, "2") }},
		{name: "conditional", mutate: func(r *http.Request) { r.Header.Set("If-None-Match", `"r1"`) }},
		{name: "compressed", mutate: func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := pdfRequest(t, http.MethodGet, resumeID, test.body)
			if test.mutate != nil {
				test.mutate(req)
			}
			recorder := httptest.NewRecorder()
			service.handleDownloadResumePDF(recorder, req)
			if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"request_invalid"`) {
				t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
	if queueCalls != 0 {
		t.Fatalf("queue calls = %d, want 0", queueCalls)
	}
}

type pdfBlockingRequestBody struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *pdfBlockingRequestBody) Read([]byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	<-b.release
	return 0, errors.New("request body must not be read")
}

func (*pdfBlockingRequestBody) Close() error { return nil }

func TestPDFRejectsFramedBodiesWithoutReading(t *testing.T) {
	t.Parallel()

	resumeID := uuid.MustParse("20000000-0000-4000-8000-000000000011")
	row := resume.Resume{ID: resumeID, UserID: uuid.MustParse("10000000-0000-4000-8000-000000000001"), Revision: 1, Doc: loadMinimalDocument(t)}
	queueCalls := 0
	service := pdfService(row, pdfQueueFunc(func(context.Context, renderjob.Request) (renderjob.Result, error) {
		queueCalls++
		return renderjob.Result{}, nil
	}), nil)

	for _, test := range []struct {
		name             string
		transferEncoding []string
	}{
		{name: "negative content length"},
		{name: "chunked", transferEncoding: []string{"chunked"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := &pdfBlockingRequestBody{started: make(chan struct{}), release: make(chan struct{})}
			req := pdfRequest(t, http.MethodGet, resumeID, nil)
			req.Body = body
			req.ContentLength = -1
			req.TransferEncoding = test.transferEncoding
			recorder := httptest.NewRecorder()
			done := make(chan struct{})
			go func() {
				service.handleDownloadResumePDF(recorder, req)
				close(done)
			}()

			select {
			case <-body.started:
				close(body.release)
				<-done
				t.Fatal("handler read a body whose framing already required rejection")
			case <-done:
				close(body.release)
			case <-time.After(time.Second):
				close(body.release)
				<-done
				t.Fatal("handler blocked while validating request framing")
			}
			if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"request_invalid"`) {
				t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
	if queueCalls != 0 {
		t.Fatalf("queue calls = %d, want 0", queueCalls)
	}
}

func TestPDFPreparationClassifiesOwnerAbsenceWithoutLeakingErrors(t *testing.T) {
	t.Parallel()

	resumeID := uuid.MustParse("20000000-0000-4000-8000-000000000003")
	rawQueueError := "queue-private-sentinel"
	queue := pdfQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
		if _, err := request.Prepare(ctx); !errors.Is(err, errOwnerPDFPreparation) {
			t.Fatalf("Prepare() error = %v, want sanitized preparation failure", err)
		}
		return renderjob.Result{}, errors.New(rawQueueError)
	})
	service := &Service{
		resumes: pdfResumeReader{get: func(context.Context, uuid.UUID, uuid.UUID) (resume.Resume, error) {
			return resume.Resume{}, resume.ErrNotFound
		}},
		printQueue: queue,
	}
	recorder := httptest.NewRecorder()
	service.handleDownloadResumePDF(recorder, pdfRequest(t, http.MethodGet, resumeID, nil))
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), `"code":"resume_not_found"`) {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), rawQueueError) {
		t.Fatalf("response leaked queue error: %s", recorder.Body.String())
	}
}

func TestPDFQueueAndOutputFailuresAreGenericAndBounded(t *testing.T) {
	t.Parallel()

	resumeID := uuid.MustParse("20000000-0000-4000-8000-000000000004")
	row := resume.Resume{ID: resumeID, UserID: uuid.MustParse("10000000-0000-4000-8000-000000000001"), Revision: 1, Doc: loadMinimalDocument(t)}
	queueWithOutput := func(output []byte) PrintQueue {
		return pdfQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
			snapshot, err := request.Prepare(ctx)
			if err != nil {
				return renderjob.Result{}, err
			}
			return renderjob.Result{Bytes: output, Revision: snapshot.Revision}, nil
		})
	}
	tests := []struct {
		name  string
		queue PrintQueue
	}{
		{name: "absent queue"},
		{name: "opaque queue failure", queue: pdfQueueFunc(func(context.Context, renderjob.Request) (renderjob.Result, error) {
			return renderjob.Result{}, errors.New("browser-private-sentinel")
		})},
		{name: "empty output", queue: queueWithOutput(nil)},
		{name: "oversized output", queue: queueWithOutput(make([]byte, maxOwnerPDFBytes+1))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			pdfService(row, test.queue, nil).handleDownloadResumePDF(recorder, pdfRequest(t, http.MethodGet, resumeID, nil))
			if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Retry-After") != "1" || !strings.Contains(recorder.Body.String(), `"code":"internal_error"`) {
				t.Fatalf("response = %d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), "private-sentinel") {
				t.Fatalf("response leaked downstream error: %s", recorder.Body.String())
			}
		})
	}
}

func TestPDFPhotoIsValidatedBoundedAndClosed(t *testing.T) {
	t.Parallel()

	userID := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	resumeID := uuid.MustParse("20000000-0000-4000-8000-000000000005")
	key, err := media.NewPhotoKey(bytes.NewReader(bytes.Repeat([]byte{0x42}, 16)), resumeID, "png")
	if err != nil {
		t.Fatal(err)
	}
	document := loadMinimalDocument(t)
	document.PersonalDetails.Photo = &schema.Photo{Key: key}
	row := resume.Resume{ID: resumeID, UserID: userID, Revision: 4, Doc: document}
	body := &trackingReadCloser{Reader: bytes.NewReader(makePhotoPNG(t))}
	backend := &pdfBackend{body: body, contentType: "image/png"}
	queue := pdfQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
		snapshot, prepareErr := request.Prepare(ctx)
		if prepareErr != nil {
			t.Fatalf("Prepare() error = %v", prepareErr)
		}
		var envelope struct {
			Document struct {
				PersonalDetails struct {
					Photo *struct {
						URL string `json:"url"`
					} `json:"photo"`
				} `json:"personalDetails"`
			} `json:"document"`
		}
		if err := json.Unmarshal(snapshot.Payload, &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Document.PersonalDetails.Photo == nil || !strings.HasPrefix(envelope.Document.PersonalDetails.Photo.URL, "data:image/png;base64,") {
			t.Fatalf("photo URL = %+v", envelope.Document.PersonalDetails.Photo)
		}
		return renderjob.Result{Bytes: []byte("%PDF"), Revision: snapshot.Revision}, nil
	})
	recorder := httptest.NewRecorder()
	pdfService(row, queue, backend).handleDownloadResumePDF(recorder, pdfRequest(t, http.MethodGet, resumeID, nil))
	if recorder.Code != http.StatusOK || !body.closed {
		t.Fatalf("response = %d, photo closed = %v", recorder.Code, body.closed)
	}
	if len(backend.gets) != 1 || backend.gets[0] != key {
		t.Fatalf("backend gets = %v", backend.gets)
	}
}

func TestPDFRejectsInvalidPhotoBeforeBackendRead(t *testing.T) {
	t.Parallel()

	resumeID := uuid.MustParse("20000000-0000-4000-8000-000000000006")
	document := loadMinimalDocument(t)
	document.PersonalDetails.Photo = &schema.Photo{Key: "resumes/foreign/photo-secret.png"}
	row := resume.Resume{ID: resumeID, UserID: uuid.MustParse("10000000-0000-4000-8000-000000000001"), Revision: 1, Doc: document}
	backend := &pdfBackend{}
	queue := pdfQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
		if _, err := request.Prepare(ctx); !errors.Is(err, errOwnerPDFPreparation) {
			t.Fatalf("Prepare() error = %v, want sanitized preparation failure", err)
		}
		return renderjob.Result{}, errors.New("masked")
	})
	recorder := httptest.NewRecorder()
	pdfService(row, queue, backend).handleDownloadResumePDF(recorder, pdfRequest(t, http.MethodGet, resumeID, nil))
	if recorder.Code != http.StatusServiceUnavailable || len(backend.gets) != 0 {
		t.Fatalf("response = %d, backend gets = %v", recorder.Code, backend.gets)
	}
}

func TestPDFPhotoFailuresStayOpaqueAndCloseBodies(t *testing.T) {
	t.Parallel()

	resumeID := uuid.MustParse("20000000-0000-4000-8000-000000000008")
	key, err := media.NewPhotoKey(bytes.NewReader(bytes.Repeat([]byte{0x32}, 16)), resumeID, "png")
	if err != nil {
		t.Fatal(err)
	}
	document := loadMinimalDocument(t)
	document.PersonalDetails.Photo = &schema.Photo{Key: key}
	row := resume.Resume{ID: resumeID, UserID: uuid.MustParse("10000000-0000-4000-8000-000000000001"), Revision: 1, Doc: document}

	tests := []struct {
		name        string
		body        *trackingReadCloser
		contentType string
		getErr      error
	}{
		{name: "backend failure", getErr: errors.New("photo-private-sentinel")},
		{name: "extension mismatch", body: &trackingReadCloser{Reader: strings.NewReader("not read")}, contentType: "image/jpeg"},
		{name: "overflow", body: &trackingReadCloser{Reader: bytes.NewReader(make([]byte, printsnapshot.MaxPhotoBytes+1))}, contentType: "image/png"},
		{name: "invalid image", body: &trackingReadCloser{Reader: strings.NewReader("not an image")}, contentType: "image/png"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &pdfBackend{contentType: test.contentType, err: test.getErr}
			if test.body != nil {
				backend.body = test.body
			}
			queue := pdfQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
				if _, err := request.Prepare(ctx); !errors.Is(err, errOwnerPDFPreparation) {
					t.Fatalf("Prepare() error = %v, want sanitized preparation failure", err)
				}
				return renderjob.Result{}, errors.New("queue-masked-sentinel")
			})
			recorder := httptest.NewRecorder()
			pdfService(row, queue, backend).handleDownloadResumePDF(recorder, pdfRequest(t, http.MethodGet, resumeID, nil))
			if recorder.Code != http.StatusServiceUnavailable || strings.Contains(recorder.Body.String(), "sentinel") {
				t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
			}
			if test.body != nil && !test.body.closed {
				t.Fatal("photo body was not closed")
			}
		})
	}
}

func TestPDFPassesCancellationToRenderOnce(t *testing.T) {
	t.Parallel()

	resumeID := uuid.MustParse("20000000-0000-4000-8000-000000000009")
	row := resume.Resume{ID: resumeID, UserID: uuid.MustParse("10000000-0000-4000-8000-000000000001"), Revision: 1, Doc: loadMinimalDocument(t)}
	queueCalls := 0
	queue := pdfQueueFunc(func(ctx context.Context, _ renderjob.Request) (renderjob.Result, error) {
		queueCalls++
		<-ctx.Done()
		return renderjob.Result{}, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := pdfRequest(t, http.MethodGet, resumeID, nil).WithContext(auth.ContextWithSession(ctx, store.Session{UserID: row.UserID}))
	recorder := httptest.NewRecorder()
	pdfService(row, queue, nil).handleDownloadResumePDF(recorder, req)
	if recorder.Code != http.StatusServiceUnavailable || queueCalls != 1 {
		t.Fatalf("response = %d, queue calls = %d", recorder.Code, queueCalls)
	}
}

type trackingReadCloser struct {
	io.Reader
	closed bool
}

func (b *trackingReadCloser) Close() error {
	b.closed = true
	return nil
}

type blockingPhotoBody struct {
	started chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (b *blockingPhotoBody) Read([]byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	<-b.closed
	return 0, errors.New("photo-read-private-sentinel")
}

func (b *blockingPhotoBody) Close() error {
	select {
	case <-b.closed:
	default:
		close(b.closed)
	}
	return nil
}

func TestPDFCancellationClosesAndJoinsPhotoRead(t *testing.T) {
	t.Parallel()

	resumeID := uuid.MustParse("20000000-0000-4000-8000-000000000007")
	key, err := media.NewPhotoKey(bytes.NewReader(bytes.Repeat([]byte{0x24}, 16)), resumeID, "png")
	if err != nil {
		t.Fatal(err)
	}
	document := loadMinimalDocument(t)
	document.PersonalDetails.Photo = &schema.Photo{Key: key}
	row := resume.Resume{ID: resumeID, UserID: uuid.MustParse("10000000-0000-4000-8000-000000000001"), Revision: 1, Doc: document}
	body := &blockingPhotoBody{started: make(chan struct{}), closed: make(chan struct{})}
	backend := &pdfBackend{body: body, contentType: "image/png"}
	queueCalls := 0
	queue := pdfQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
		queueCalls++
		jobCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() {
			_, prepareErr := request.Prepare(jobCtx)
			done <- prepareErr
		}()
		<-body.started
		cancel()
		if prepareErr := <-done; prepareErr == nil {
			t.Fatal("Prepare() error = nil after cancellation")
		}
		return renderjob.Result{}, context.Canceled
	})
	recorder := httptest.NewRecorder()
	pdfService(row, queue, backend).handleDownloadResumePDF(recorder, pdfRequest(t, http.MethodGet, resumeID, nil))
	if recorder.Code != http.StatusServiceUnavailable || queueCalls != 1 {
		t.Fatalf("response = %d, queue calls = %d", recorder.Code, queueCalls)
	}
	select {
	case <-body.closed:
	default:
		t.Fatal("photo body was not closed before handler returned")
	}
}

func TestPDFAdmissionLimitsAccountAndIPIndependently(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	request := func(t *testing.T, handler http.Handler, accountID, remoteAddr string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		req.RemoteAddr = remoteAddr
		req = req.WithContext(api.WithAccountID(req.Context(), accountID))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder
	}
	newHandler := func() http.Handler {
		service := New(nil, nil, nil, nil, Options{Clock: func() time.Time { return now }})
		return service.wrapPDFAdmission(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	}

	accountHandler := newHandler()
	for index := range ownerPDFRequestsPerMinute {
		got := request(t, accountHandler, "account-a", "192.0.2."+strconv.Itoa(index+1)+":1234")
		if got.Code != http.StatusNoContent {
			t.Fatalf("account request %d = %d", index+1, got.Code)
		}
	}
	if got := request(t, accountHandler, "account-a", "198.51.100.1:1234"); got.Code != http.StatusTooManyRequests || got.Header().Get("Retry-After") == "" {
		t.Fatalf("account overflow = %d headers=%v", got.Code, got.Header())
	}

	ipHandler := newHandler()
	for index := range ownerPDFRequestsPerMinute {
		got := request(t, ipHandler, "account-"+strconv.Itoa(index), "203.0.113.1:1234")
		if got.Code != http.StatusNoContent {
			t.Fatalf("IP request %d = %d", index+1, got.Code)
		}
	}
	if got := request(t, ipHandler, "account-extra", "203.0.113.1:1234"); got.Code != http.StatusTooManyRequests || got.Header().Get("Retry-After") == "" {
		t.Fatalf("IP overflow = %d headers=%v", got.Code, got.Header())
	}
}
