package resumeapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

type exitRateAdmissionBody struct {
	reader io.Reader
	reads  atomic.Int32
}

func (b *exitRateAdmissionBody) Read(p []byte) (int, error) {
	b.reads.Add(1)
	return b.reader.Read(p)
}

func (*exitRateAdmissionBody) Close() error { return nil }

type exitRateAdmissionIdempotency struct {
	idempotencyBoundary
	inspects atomic.Int32
	executes atomic.Int32
}

func (i *exitRateAdmissionIdempotency) Inspect(ctx context.Context, userID uuid.UUID,
	operation string, key uuid.UUID, requestHash [32]byte,
) (resume.StoredResponse, bool, error) {
	i.inspects.Add(1)
	return i.idempotencyBoundary.Inspect(ctx, userID, operation, key, requestHash)
}

func (i *exitRateAdmissionIdempotency) Execute(ctx context.Context, userID uuid.UUID,
	operation string, key uuid.UUID, requestHash [32]byte,
	run func(*store.Queries) (resume.StoredResponse, error),
) (resume.ExecuteResult, error) {
	i.executes.Add(1)
	return i.idempotencyBoundary.Execute(ctx, userID, operation, key, requestHash, run)
}

func TestRateLimitsHold(t *testing.T) {
	h := newResumeAPITestHarness(t)
	clock := testutil.NewClockAtEpoch()
	h.service.clock = clock.Now
	idempotency := &exitRateAdmissionIdempotency{idempotencyBoundary: h.service.idempotency}
	h.service.idempotency = idempotency

	beforeResumes := h.snapshotUserTable(t, "resumes")
	beforeIdempotency := h.snapshotUserTable(t, "idempotency_records")
	accountA := uuid.NewString()
	accountB := uuid.NewString()

	tests := []struct {
		name       string
		requests   int
		retryAfter string
		middleware func(routeChains) api.Middleware
		handler    func(http.ResponseWriter, *http.Request)
		request    func() *http.Request
	}{
		{
			name:       "read",
			requests:   resumeReadRequests,
			retryAfter: "1",
			middleware: func(chains routeChains) api.Middleware { return chains.read },
			handler:    h.service.handleGetResume,
			request: func() *http.Request {
				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/resumes/not-a-uuid", nil)
				req.SetPathValue("id", "not-a-uuid")
				return req
			},
		},
		{
			name:       "write",
			requests:   resumeWriteRequests,
			retryAfter: "1",
			middleware: func(chains routeChains) api.Middleware { return chains.write },
			handler:    h.service.handleUpdateResumeMetadata,
			request: func() *http.Request {
				body := &exitRateAdmissionBody{reader: bytes.NewBufferString(`{"title":"not decoded"}`)}
				req := httptest.NewRequestWithContext(context.Background(), http.MethodPatch,
					"/api/v1/resumes/01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f", nil)
				req.Body = body
				req.ContentLength = -1
				req.SetPathValue("id", "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f")
				return req
			},
		},
		{
			name:       "upload",
			requests:   resumeUploadRequests,
			retryAfter: "180",
			middleware: func(chains routeChains) api.Middleware { return chains.upload },
			handler:    h.service.handleUploadResumePhoto,
			request: func() *http.Request {
				body := &exitRateAdmissionBody{reader: bytes.NewBufferString("--unused--\r\n")}
				req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
					"/api/v1/resumes/01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f/photo", nil)
				req.Body = body
				req.ContentLength = -1
				req.Header.Set("Content-Type", "multipart/form-data; boundary=unused")
				req.SetPathValue("id", "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f")
				return req
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limited := test.middleware(h.service.newRouteChains())(http.HandlerFunc(test.handler))
			send := func(accountID, remoteAddr string) (*httptest.ResponseRecorder, *exitRateAdmissionBody) {
				req := test.request()
				req.RemoteAddr = remoteAddr
				ctx := auth.ContextWithSession(req.Context(), h.session)
				ctx = api.WithAccountID(ctx, accountID)
				req = req.WithContext(ctx)
				recorder := httptest.NewRecorder()
				limited.ServeHTTP(recorder, req)
				var body *exitRateAdmissionBody
				if req.Body != http.NoBody {
					var ok bool
					body, ok = req.Body.(*exitRateAdmissionBody)
					if !ok {
						t.Fatalf("request body type = %T, want *exitRateAdmissionBody", req.Body)
					}
				}
				return recorder, body
			}

			for attempt := 1; attempt <= test.requests; attempt++ {
				recorder, _ := send(accountA, "192.0.2.10:1234")
				if recorder.Code != http.StatusBadRequest {
					t.Fatalf("admitted attempt %d status = %d body=%s, want handler's 400", attempt, recorder.Code, recorder.Body.Bytes())
				}
			}
			rejected, rejectedBody := send(accountA, "192.0.2.10:1234")
			if rejected.Code != http.StatusTooManyRequests {
				t.Fatalf("over-limit status = %d body=%s, want 429", rejected.Code, rejected.Body.Bytes())
			}
			if !bytes.Contains(rejected.Body.Bytes(), []byte(`"code":"rate_limited"`)) {
				t.Fatalf("over-limit body = %s, want rate_limited", rejected.Body.Bytes())
			}
			if got := rejected.Header().Get("Retry-After"); got != test.retryAfter {
				t.Fatalf("Retry-After = %q, want %q", got, test.retryAfter)
			}
			if rejectedBody != nil && rejectedBody.reads.Load() != 0 {
				t.Fatalf("over-limit body reads = %d, want zero", rejectedBody.reads.Load())
			}

			for _, isolated := range []struct {
				name       string
				accountID  string
				remoteAddr string
			}{
				{name: "same account different IP", accountID: accountA, remoteAddr: "192.0.2.11:1234"},
				{name: "different account same IP", accountID: accountB, remoteAddr: "192.0.2.10:1234"},
			} {
				recorder, _ := send(isolated.accountID, isolated.remoteAddr)
				if recorder.Code != http.StatusBadRequest {
					t.Errorf("%s status = %d body=%s, want independent handler 400", isolated.name, recorder.Code, recorder.Body.Bytes())
				}
			}
		})
	}

	if got := h.snapshotUserTable(t, "resumes"); got != beforeResumes {
		t.Fatalf("rate-policy requests changed resume storage: got %q want %q", got, beforeResumes)
	}
	if got := h.snapshotUserTable(t, "idempotency_records"); got != beforeIdempotency {
		t.Fatalf("rate-policy requests changed idempotency storage: got %q want %q", got, beforeIdempotency)
	}
	if idempotency.inspects.Load() != 0 || idempotency.executes.Load() != 0 {
		t.Fatalf("rate-policy requests reached idempotency: inspect=%d execute=%d, want zero",
			idempotency.inspects.Load(), idempotency.executes.Load())
	}
}

type exitRateAdmissionEventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *exitRateAdmissionEventLog) add(event string) {
	l.mu.Lock()
	l.events = append(l.events, event)
	l.mu.Unlock()
}

func (l *exitRateAdmissionEventLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

type exitRateAdmissionBackend struct {
	media.Backend
	events      *exitRateAdmissionEventLog
	putEntered  chan context.Context
	putResponse media.PutOutcome
	putErr      error
	waitForDone bool
	active      atomic.Int32
	puts        atomic.Int32
}

func (b *exitRateAdmissionBackend) Put(ctx context.Context, key, contentType string, body io.Reader, size int64) (media.PutOutcome, error) {
	b.active.Add(1)
	b.puts.Add(1)
	defer b.active.Add(-1)
	if b.events != nil {
		b.events.add("put-start")
		defer b.events.add("put-end")
	}
	if b.putEntered != nil {
		b.putEntered <- ctx
	}
	if b.waitForDone {
		<-ctx.Done()
		return media.PutNotCreated, ctx.Err()
	}
	if b.Backend != nil && b.putErr == nil && b.putResponse == media.PutCreated {
		return b.Backend.Put(ctx, key, contentType, body, size)
	}
	if _, err := io.Copy(io.Discard, body); err != nil {
		return media.PutNotCreated, err
	}
	return b.putResponse, b.putErr
}

func TestMediaAdmissionAndCleanup(t *testing.T) {
	assertPhotoPutDeadlineAuthority(t)
	originalAdmission := taskPhotoAdmission
	t.Cleanup(func() { taskPhotoAdmission = originalAdmission })

	t.Run("five-second Put deadline and synchronous completion", func(t *testing.T) {
		h := newResumeAPITestHarness(t)
		created := h.createResume(t)
		events := &exitRateAdmissionEventLog{}
		entered := make(chan context.Context, 1)
		backend := &exitRateAdmissionBackend{
			Backend: h.service.blobs, events: events, putEntered: entered,
			putResponse: media.PutNotCreated, putErr: errors.New("injected object-store failure"),
		}
		h.service.blobs = backend
		h.service.photoNormalizationDuration = func(time.Duration) { events.add("normalized") }
		taskPhotoAdmission = media.NewPhotoAdmission()

		before := snapshotPhotoUploadState(t, h, created.ID)
		response := exitRateAdmissionUpload(context.Background(), t, h, created.ID, created.Revision,
			makePhotoPNG(t), &photoDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()})
		events.add("response")
		if response.status != http.StatusInternalServerError {
			t.Fatalf("response = %d body=%s, want 500", response.status, response.body)
		}
		putCtx := <-entered
		deadline, ok := putCtx.Deadline()
		if !ok {
			t.Fatal("object Put context has no deadline")
		}
		remaining := time.Until(deadline)
		const deadlineJitter = 500 * time.Millisecond
		if remaining < requiredPhotoPutTimeout-deadlineJitter || remaining > requiredPhotoPutTimeout+deadlineJitter {
			t.Fatalf("object Put deadline remaining = %v, want %v (+/-%v)", remaining, requiredPhotoPutTimeout, deadlineJitter)
		}
		if got := events.snapshot(); fmt.Sprint(got) != "[normalized put-start put-end response]" {
			t.Fatalf("synchronous event order = %v", got)
		}
		if backend.active.Load() != 0 {
			t.Fatalf("active object writes after response = %d, want zero", backend.active.Load())
		}
		assertExitPhotoPermitFree(t)
		assertPhotoUploadState(t, h, created.ID, before)
	})

	t.Run("request cancellation joins object Put", func(t *testing.T) {
		h := newResumeAPITestHarness(t)
		created := h.createResume(t)
		entered := make(chan context.Context, 1)
		backend := &exitRateAdmissionBackend{Backend: h.service.blobs, putEntered: entered, waitForDone: true}
		h.service.blobs = backend
		taskPhotoAdmission = media.NewPhotoAdmission()
		before := snapshotPhotoUploadState(t, h, created.ID)

		ctx, cancel := context.WithCancel(context.Background())
		payload, contentType := exitRateAdmissionMultipart(t, makePhotoPNG(t))
		recorder := &photoDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
		req := exitRateAdmissionRequest(ctx, t, h, created.ID, created.Revision, payload, contentType)
		done := make(chan struct{})
		go func() {
			h.handler.ServeHTTP(recorder, req)
			close(done)
		}()
		putCtx := <-entered
		if _, ok := putCtx.Deadline(); !ok {
			t.Fatal("cancellable object Put has no independent deadline")
		}
		cancel()
		select {
		case <-done:
			response := snapshotHTTPResponse(t, recorder.Result())
			if response.status != http.StatusInternalServerError {
				t.Fatalf("response = %d body=%s, want 500", response.status, response.body)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("handler did not return after canceled object Put exited")
		}
		if backend.active.Load() != 0 {
			t.Fatalf("active object writes after canceled response = %d, want zero", backend.active.Load())
		}
		assertExitPhotoPermitFree(t)
		assertPhotoUploadState(t, h, created.ID, before)
	})

	for _, test := range []struct {
		name          string
		payload       []byte
		contentType   string
		plainRecorder bool
		wantStatus    int
	}{
		{
			name: "read-deadline setup error", payload: makePhotoPNG(t),
			plainRecorder: true, wantStatus: http.StatusInternalServerError,
		},
		{
			name: "multipart decode error", payload: []byte("not-a-complete-multipart"),
			contentType: "multipart/form-data; boundary=broken", wantStatus: http.StatusBadRequest,
		},
		{
			name: "normalization error", payload: []byte("not-an-image"),
			wantStatus: http.StatusUnsupportedMediaType,
		},
	} {
		t.Run(test.name+" releases permit", func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			created := h.createResume(t)
			backend := &exitRateAdmissionBackend{
				Backend: h.service.blobs, putResponse: media.PutNotCreated, putErr: errors.New("Put must not run"),
			}
			h.service.blobs = backend
			taskPhotoAdmission = media.NewPhotoAdmission()
			before := snapshotPhotoUploadState(t, h, created.ID)

			var recorder http.ResponseWriter = &photoDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
			if test.plainRecorder {
				recorder = httptest.NewRecorder()
			}
			response := exitRateAdmissionUploadRaw(context.Background(), t, h, created.ID, created.Revision,
				test.payload, test.contentType, recorder)
			if response.status != test.wantStatus {
				t.Fatalf("response = %d body=%s, want %d", response.status, response.body, test.wantStatus)
			}
			if backend.puts.Load() != 0 {
				t.Fatalf("object Put calls = %d, want zero", backend.puts.Load())
			}
			assertExitPhotoPermitFree(t)
			assertPhotoUploadState(t, h, created.ID, before)
		})
	}

	t.Run("success releases permit after all in-process work", func(t *testing.T) {
		h := newResumeAPITestHarness(t)
		created := h.createResume(t)
		events := &exitRateAdmissionEventLog{}
		backend := &exitRateAdmissionBackend{
			Backend: h.service.blobs, events: events, putResponse: media.PutCreated,
		}
		h.service.blobs = backend
		h.service.photoNormalizationDuration = func(time.Duration) { events.add("normalized") }
		taskPhotoAdmission = media.NewPhotoAdmission()

		response := exitRateAdmissionUpload(context.Background(), t, h, created.ID, created.Revision,
			makePhotoPNG(t), &photoDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()})
		events.add("response")
		if response.status != http.StatusOK {
			t.Fatalf("response = %d body=%s, want 200", response.status, response.body)
		}
		if got := events.snapshot(); fmt.Sprint(got) != "[normalized put-start put-end response]" {
			t.Fatalf("synchronous event order = %v", got)
		}
		if backend.active.Load() != 0 {
			t.Fatalf("active object writes after response = %d, want zero", backend.active.Load())
		}
		assertExitPhotoPermitFree(t)
	})
}

func assertExitPhotoPermitFree(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	release, err := taskPhotoAdmission.Acquire(ctx)
	if err != nil {
		t.Fatalf("photo permit remained held after response: %v", err)
	}
	release()
}

const requiredPhotoPutTimeout = 5 * time.Second

func assertPhotoPutDeadlineAuthority(t *testing.T) {
	t.Helper()
	if photoPutTimeout != requiredPhotoPutTimeout {
		t.Fatalf("production photo Put timeout = %v, Task 11 requires %v", photoPutTimeout, requiredPhotoPutTimeout)
	}
}

func exitRateAdmissionUpload(ctx context.Context, t *testing.T, h *resumeAPITestHarness,
	id uuid.UUID, revision int64, payload []byte, recorder http.ResponseWriter,
) testHTTPResponse {
	t.Helper()
	body, contentType := exitRateAdmissionMultipart(t, payload)
	return exitRateAdmissionUploadRaw(ctx, t, h, id, revision, body, contentType, recorder)
}

func exitRateAdmissionMultipart(t *testing.T, payload []byte) ([]byte, string) {
	t.Helper()
	return photoMultipartBody(t, func(writer *multipart.Writer) {
		part, err := writer.CreateFormFile("file", "photo.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(payload); err != nil {
			t.Fatal(err)
		}
	})
}

func exitRateAdmissionRequest(ctx context.Context, t *testing.T, h *resumeAPITestHarness,
	id uuid.UUID, revision int64, body []byte, contentType string,
) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("/api/v1/resumes/%s/photo", id), bytes.NewReader(body))
	setUniquePhotoDirectRemoteAddr(req)
	req.AddCookie(h.cookie)
	req.Header.Set("Origin", resumeAPITestOrigin)
	req.Header.Set(auth.CSRFHeaderName, h.csrfToken)
	req.Header.Set("Idempotency-Key", uuid.NewString())
	req.Header.Set("If-Match", `"r`+strconv.FormatInt(revision, 10)+`"`)
	req.Header.Set("Content-Type", contentType)
	return req
}

func exitRateAdmissionUploadRaw(ctx context.Context, t *testing.T, h *resumeAPITestHarness,
	id uuid.UUID, revision int64, body []byte, contentType string, recorder http.ResponseWriter,
) testHTTPResponse {
	t.Helper()
	if contentType == "" {
		var wrapped bytes.Buffer
		writer := multipart.NewWriter(&wrapped)
		part, err := writer.CreateFormFile("file", "photo.bin")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(body); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		body = wrapped.Bytes()
		contentType = writer.FormDataContentType()
	}
	req := exitRateAdmissionRequest(ctx, t, h, id, revision, body, contentType)
	h.handler.ServeHTTP(recorder, req)
	resultRecorder, ok := recorder.(interface{ Result() *http.Response })
	if !ok {
		t.Fatalf("recorder %T does not expose a response", recorder)
	}
	response := resultRecorder.Result() //nolint:bodyclose // snapshotHTTPResponse closes the synthetic response body.
	return snapshotHTTPResponse(t, response)
}
