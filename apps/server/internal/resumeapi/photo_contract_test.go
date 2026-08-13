package resumeapi

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/media"
)

func makePhotoPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for y := range 2 {
		for x := range 3 {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(30 + x*20), G: uint8(60 + y*30), B: 90, A: 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func (h *resumeAPITestHarness) uploadPhotoRequest(t *testing.T, id uuid.UUID, revision int64, key, filename string, payload []byte) testHTTPResponse {
	t.Helper()
	body, contentType := photoMultipartBody(t, func(writer *multipart.Writer) {
		part, err := writer.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, writeErr := part.Write(payload); writeErr != nil {
			t.Fatal(writeErr)
		}
	})
	req, err := http.NewRequestWithContext(h.ctx, http.MethodPost, h.server.URL+fmt.Sprintf("/api/v1/resumes/%s/photo", id), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(h.cookie)
	req.Header.Set("Origin", resumeAPITestOrigin)
	req.Header.Set(auth.CSRFHeaderName, h.csrfToken)
	req.Header.Set("Idempotency-Key", key)
	req.Header.Set("If-Match", fmt.Sprintf(`"r%d"`, revision))
	req.Header.Set("Content-Type", contentType)
	response, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return snapshotHTTPResponse(t, response)
}

func photoMultipartBody(t *testing.T, build func(*multipart.Writer)) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	build(writer)
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func (h *resumeAPITestHarness) directPhotoUploadRequest(t *testing.T, id uuid.UUID, revision int64,
	body []byte, contentType string, contentLength int64, chunked bool, recorder http.ResponseWriter,
) testHTTPResponse {
	t.Helper()
	req, err := http.NewRequestWithContext(h.ctx, http.MethodPost,
		fmt.Sprintf("/api/v1/resumes/%s/photo", id), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	setUniquePhotoDirectRemoteAddr(req)
	req.ContentLength = contentLength
	if chunked {
		req.TransferEncoding = []string{"chunked"}
	}
	req.AddCookie(h.cookie)
	req.Header.Set("Origin", resumeAPITestOrigin)
	req.Header.Set(auth.CSRFHeaderName, h.csrfToken)
	req.Header.Set("Idempotency-Key", uuid.NewString())
	req.Header.Set("If-Match", fmt.Sprintf(`"r%d"`, revision))
	req.Header.Set("Content-Type", contentType)
	h.handler.ServeHTTP(recorder, req)
	responseRecorder, ok := recorder.(interface {
		Result() *http.Response
	})
	if !ok {
		t.Fatalf("recorder %T does not expose a response", recorder)
	}
	response := responseRecorder.Result() //nolint:bodyclose // snapshotHTTPResponse closes this synthetic response body.
	return snapshotHTTPResponse(t, response)
}

type photoUploadState struct {
	resume  wireStoredRow
	records string
	objects []string
}

var photoDirectIPCounter atomic.Uint32

func setUniquePhotoDirectRemoteAddr(req *http.Request) {
	octet := photoDirectIPCounter.Add(1)%250 + 1
	req.RemoteAddr = fmt.Sprintf("192.0.2.%d:1234", octet)
}

func snapshotPhotoUploadState(t *testing.T, h *resumeAPITestHarness, id uuid.UUID) photoUploadState {
	t.Helper()
	return photoUploadState{
		resume:  snapshotStoredResumeRow(t, h, id),
		records: h.snapshotUserTable(t, "idempotency_records"),
		objects: snapshotObjectKeys(t, h),
	}
}

func assertPhotoUploadState(t *testing.T, h *resumeAPITestHarness, id uuid.UUID, want photoUploadState) {
	t.Helper()
	if got := snapshotPhotoUploadState(t, h, id); !reflect.DeepEqual(got, want) {
		t.Fatalf("photo rejection changed state:\n got=%+v\nwant=%+v", got, want)
	}
}

type photoDeadlineRecorder struct {
	*httptest.ResponseRecorder
	deadline    time.Time
	deadlineSet atomic.Bool
}

func (w *photoDeadlineRecorder) SetReadDeadline(deadline time.Time) error {
	w.deadline = deadline
	w.deadlineSet.Store(true)
	return nil
}

type photoDeadlineObservedBody struct {
	deadlineSet *atomic.Bool
	read        atomic.Bool
}

func (b *photoDeadlineObservedBody) Read([]byte) (int, error) {
	b.read.Store(true)
	if !b.deadlineSet.Load() {
		return 0, errors.New("body read before streaming deadline")
	}
	return 0, errors.New("simulated read deadline")
}

func (*photoDeadlineObservedBody) Close() error { return nil }

func TestPhotoMultipartBoundaryRealHandler(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	valid, validType := photoMultipartBody(t, func(writer *multipart.Writer) {
		part, err := writer.CreateFormFile("file", "photo.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(makePhotoPNG(t)); err != nil {
			t.Fatal(err)
		}
	})
	missing, missingType := photoMultipartBody(t, func(writer *multipart.Writer) {
		if err := writer.WriteField("caption", "not a file"); err != nil {
			t.Fatal(err)
		}
	})
	duplicate, duplicateType := photoMultipartBody(t, func(writer *multipart.Writer) {
		for index := range 2 {
			part, err := writer.CreateFormFile("file", fmt.Sprintf("photo-%d.png", index))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write(makePhotoPNG(t)); err != nil {
				t.Fatal(err)
			}
		}
	})
	fileOverflow, fileOverflowType := photoMultipartBody(t, func(writer *multipart.Writer) {
		part, err := writer.CreateFormFile("file", "large.bin")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(bytes.Repeat([]byte{'x'}, photoFileBytes+1)); err != nil {
			t.Fatal(err)
		}
	})
	requestOverflow := append(append([]byte(nil), valid...),
		bytes.Repeat([]byte{'x'}, int(photoRequestBytes+1)-len(valid))...)

	for _, test := range []struct {
		name          string
		body          []byte
		contentType   string
		contentLength int64
		status        int
	}{
		{name: "empty body", contentType: "multipart/form-data; boundary=empty", status: http.StatusBadRequest},
		{name: "missing file part", body: missing, contentType: missingType, contentLength: int64(len(missing)), status: http.StatusBadRequest},
		{name: "duplicate file parts", body: duplicate, contentType: duplicateType, contentLength: int64(len(duplicate)), status: http.StatusBadRequest},
		{name: "short body with larger declared length", body: valid[:len(valid)-8], contentType: validType, contentLength: int64(len(valid)), status: http.StatusBadRequest},
		{name: "observed request overflow with understated length", body: requestOverflow, contentType: validType, contentLength: 1, status: http.StatusRequestEntityTooLarge},
		{name: "file overflow", body: fileOverflow, contentType: fileOverflowType, contentLength: int64(len(fileOverflow)), status: http.StatusRequestEntityTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := snapshotPhotoUploadState(t, h, created.ID)
			response := h.directPhotoUploadRequest(t, created.ID, created.Revision,
				test.body, test.contentType, test.contentLength, false,
				&photoDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()})
			if response.status != test.status {
				t.Fatalf("response = %d body=%s, want %d", response.status, response.body, test.status)
			}
			assertPhotoUploadState(t, h, created.ID, before)
		})
	}

	for _, test := range []struct {
		name          string
		contentLength int64
		chunked       bool
	}{
		{name: "admitted chunked body", contentLength: -1, chunked: true},
		{name: "admitted understated content length", contentLength: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			resume := h.createResume(t)
			response := h.directPhotoUploadRequest(t, resume.ID, resume.Revision,
				valid, validType, test.contentLength, test.chunked,
				&photoDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()})
			if response.status != http.StatusOK {
				t.Fatalf("response = %d body=%s, want 200", response.status, response.body)
			}
		})
	}
}

func TestPhotoStreamingReadDeadlinePrecedesBodyRead(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	recorder := &photoDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	body := &photoDeadlineObservedBody{deadlineSet: &recorder.deadlineSet}
	started := time.Now()
	req := httptest.NewRequestWithContext(h.ctx, http.MethodPost,
		fmt.Sprintf("/api/v1/resumes/%s/photo", created.ID), nil)
	setUniquePhotoDirectRemoteAddr(req)
	req.Body = body
	req.ContentLength = -1
	req.TransferEncoding = []string{"chunked"}
	req.AddCookie(h.cookie)
	req.Header.Set("Origin", resumeAPITestOrigin)
	req.Header.Set(auth.CSRFHeaderName, h.csrfToken)
	req.Header.Set("Idempotency-Key", uuid.NewString())
	req.Header.Set("If-Match", `"r1"`)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=deadline")
	h.handler.ServeHTTP(recorder, req)
	if !body.read.Load() || !recorder.deadlineSet.Load() {
		t.Fatalf("deadline set=%v body read=%v, want deadline before read", recorder.deadlineSet.Load(), body.read.Load())
	}
	remaining := recorder.deadline.Sub(started)
	if remaining < photoReadTimeout-time.Second || remaining > photoReadTimeout+time.Second {
		t.Fatalf("read deadline = %v after start, want %v", remaining, photoReadTimeout)
	}
}

func (h *resumeAPITestHarness) getPhotoRequest(t *testing.T, id uuid.UUID, ifNoneMatch string) testHTTPResponse {
	t.Helper()
	req, err := http.NewRequestWithContext(h.ctx, http.MethodGet, h.server.URL+fmt.Sprintf("/api/v1/resumes/%s/photo", id), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(h.cookie)
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	response, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return snapshotHTTPResponse(t, response)
}

func TestPhotoUploadGetCropDeleteContractAndReplay(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	source := makePhotoPNG(t)
	key := uuid.NewString()

	uploaded := h.uploadPhotoRequest(t, created.ID, created.Revision, key, "client-name.webp", source)
	if uploaded.status != http.StatusOK || uploaded.header.Get("ETag") != `"r2"` {
		t.Fatalf("upload = %d headers=%v body=%s", uploaded.status, uploaded.header, uploaded.body)
	}
	revision, document := decodedWrittenDocument(t, uploaded)
	if revision != "2" || document.PersonalDetails.Photo == nil || document.PersonalDetails.Photo.Crop != nil {
		t.Fatalf("uploaded document revision=%q photo=%#v", revision, document.PersonalDetails.Photo)
	}
	photoKey := document.PersonalDetails.Photo.Key
	if ext, err := media.ParsePhotoKey(created.ID, photoKey); err != nil || ext != "jpg" {
		t.Fatalf("stored photo key %q ext=%q err=%v", photoKey, ext, err)
	}

	got := h.getPhotoRequest(t, created.ID, "")
	if got.status != http.StatusOK || got.header.Get("Content-Type") != "image/jpeg" || got.header.Get("ETag") == "" || got.header.Get(wireVersionHeader) != "" {
		t.Fatalf("GET = %d headers=%v body=%x", got.status, got.header, got.body)
	}
	if bytes.Equal(got.body, source) {
		t.Fatal("GET returned the unnormalized source container")
	}
	conditional := h.getPhotoRequest(t, created.ID, got.header.Get("ETag"))
	if conditional.status != http.StatusNotModified || len(conditional.body) != 0 || conditional.header.Get("Content-Type") != "" {
		t.Fatalf("conditional GET = %d headers=%v body=%x", conditional.status, conditional.header, conditional.body)
	}

	cropKey := uuid.NewString()
	cropBody := bytes.NewBufferString(`{"crop":{"x":0.1,"y":0.2,"width":0.7,"height":0.6}}`)
	cropped := h.mutationRequest(t, http.MethodPatch, fmt.Sprintf("/api/v1/resumes/%s/photo", created.ID), cropBody, 2, cropKey)
	if cropped.status != http.StatusOK || cropped.header.Get("ETag") != `"r3"` {
		t.Fatalf("crop = %d headers=%v body=%s", cropped.status, cropped.header, cropped.body)
	}
	_, croppedDocument := decodedWrittenDocument(t, cropped)
	if croppedDocument.PersonalDetails.Photo == nil || croppedDocument.PersonalDetails.Photo.Key != photoKey || croppedDocument.PersonalDetails.Photo.Crop == nil {
		t.Fatalf("cropped photo = %#v", croppedDocument.PersonalDetails.Photo)
	}
	replay := h.mutationRequest(t, http.MethodPatch, fmt.Sprintf("/api/v1/resumes/%s/photo", created.ID), bytes.NewBufferString(`{"crop":{"x":0.1,"y":0.2,"width":0.7,"height":0.6}}`), 2, cropKey)
	if replay.status != http.StatusOK || replay.header.Get("ETag") != `"r3"` || !bytes.Equal(replay.body, cropped.body) {
		t.Fatalf("crop replay = %d headers=%v body=%s", replay.status, replay.header, replay.body)
	}

	deleteKey := uuid.NewString()
	deleted := h.mutationRequest(t, http.MethodDelete, fmt.Sprintf("/api/v1/resumes/%s/photo", created.ID), nil, 3, deleteKey)
	if deleted.status != http.StatusNoContent || deleted.header.Get("ETag") != `"r4"` || deleted.header.Get(wireVersionHeader) == "" || len(deleted.body) != 0 {
		t.Fatalf("delete = %d headers=%v body=%s", deleted.status, deleted.header, deleted.body)
	}
	missing := h.getPhotoRequest(t, created.ID, "")
	if missing.status != http.StatusNotFound || !bytes.Contains(missing.body, []byte(`"media_not_found"`)) {
		t.Fatalf("GET after delete = %d body=%s", missing.status, missing.body)
	}
	var queued string
	if err := h.pool.QueryRow(h.ctx, `SELECT object_key FROM media_deletion_jobs WHERE resume_id = $1`, created.ID).Scan(&queued); err != nil {
		t.Fatalf("read deletion job: %v", err)
	}
	if queued != photoKey {
		t.Fatalf("queued key = %q, want %q", queued, photoKey)
	}
	replayedDelete := h.mutationRequest(t, http.MethodDelete, fmt.Sprintf("/api/v1/resumes/%s/photo", created.ID), nil, 3, deleteKey)
	if replayedDelete.status != http.StatusNoContent || replayedDelete.header.Get("ETag") != `"r4"` || len(replayedDelete.body) != 0 {
		t.Fatalf("delete replay = %d headers=%v body=%s", replayedDelete.status, replayedDelete.header, replayedDelete.body)
	}
	var jobCount int
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM media_deletion_jobs WHERE resume_id = $1`, created.ID).Scan(&jobCount); err != nil {
		t.Fatal(err)
	}
	if jobCount != 1 {
		t.Fatalf("deletion jobs after replay = %d", jobCount)
	}
}

func TestPhotoAbsentCropAndDeleteAreMediaNotFound(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	path := fmt.Sprintf("/api/v1/resumes/%s/photo", created.ID)
	for _, test := range []struct {
		method string
		body   io.Reader
	}{
		{http.MethodPatch, bytes.NewBufferString(`{"crop":null}`)},
		{http.MethodDelete, nil},
	} {
		response := h.mutationRequest(t, test.method, path, test.body, created.Revision, uuid.NewString())
		if response.status != http.StatusNotFound || !bytes.Contains(response.body, []byte(`"media_not_found"`)) {
			t.Errorf("%s absent photo = %d body=%s", test.method, response.status, response.body)
		}
	}
}

func TestPhotoUploadReplayReuseAndStaleCandidateCompensation(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	source := makePhotoPNG(t)
	key := uuid.NewString()

	first := h.uploadPhotoRequest(t, created.ID, created.Revision, key, "first.png", source)
	if first.status != http.StatusOK {
		t.Fatalf("first upload = %d body=%s", first.status, first.body)
	}
	replay := h.uploadPhotoRequest(t, created.ID, created.Revision, key, "renamed.bin", source)
	if replay.status != http.StatusOK || !bytes.Equal(replay.body, first.body) || replay.header.Get("ETag") != first.header.Get("ETag") {
		t.Fatalf("replay = %d headers=%v body=%s", replay.status, replay.header, replay.body)
	}
	objects, _, err := h.service.blobs.ListPage(h.ctx, "resumes/"+created.ID.String()+"/", "", 10)
	if err != nil || len(objects) != 1 {
		t.Fatalf("objects after replay = %#v err=%v", objects, err)
	}

	changed := append([]byte(nil), source...)
	changed[len(changed)-1] ^= 1
	reused := h.uploadPhotoRequest(t, created.ID, created.Revision, key, "first.png", changed)
	if reused.status != http.StatusConflict || !bytes.Contains(reused.body, []byte(`"idempotency_key_reuse"`)) {
		t.Fatalf("key reuse = %d body=%s", reused.status, reused.body)
	}
	objects, _, err = h.service.blobs.ListPage(h.ctx, "resumes/"+created.ID.String()+"/", "", 10)
	if err != nil || len(objects) != 1 {
		t.Fatalf("objects after key reuse = %#v err=%v", objects, err)
	}

	stale := h.uploadPhotoRequest(t, created.ID, created.Revision, uuid.NewString(), "stale.png", source)
	if stale.status != http.StatusPreconditionFailed || !bytes.Contains(stale.body, []byte(`"revision_mismatch"`)) {
		t.Fatalf("stale upload = %d body=%s", stale.status, stale.body)
	}
	objects, _, err = h.service.blobs.ListPage(h.ctx, "resumes/"+created.ID.String()+"/", "", 10)
	if err != nil || len(objects) != 1 {
		t.Fatalf("objects after stale compensation = %#v err=%v", objects, err)
	}
}

func TestPhotoReplacementClearsCropAndQueuesTransactionReadKey(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	source := makePhotoPNG(t)
	first := h.uploadPhotoRequest(t, created.ID, created.Revision, uuid.NewString(), "first.png", source)
	if first.status != http.StatusOK {
		t.Fatalf("first upload = %d body=%s", first.status, first.body)
	}
	_, firstDoc := decodedWrittenDocument(t, first)
	oldKey := firstDoc.PersonalDetails.Photo.Key
	path := fmt.Sprintf("/api/v1/resumes/%s/photo", created.ID)
	cropped := h.mutationRequest(t, http.MethodPatch, path,
		bytes.NewBufferString(`{"crop":{"x":0,"y":0,"width":1,"height":1}}`), 2, uuid.NewString())
	if cropped.status != http.StatusOK {
		t.Fatalf("crop = %d body=%s", cropped.status, cropped.body)
	}
	replaced := h.uploadPhotoRequest(t, created.ID, 3, uuid.NewString(), "second.png", source)
	if replaced.status != http.StatusOK {
		t.Fatalf("replacement = %d body=%s", replaced.status, replaced.body)
	}
	_, replacedDoc := decodedWrittenDocument(t, replaced)
	if replacedDoc.PersonalDetails.Photo == nil || replacedDoc.PersonalDetails.Photo.Key == oldKey || replacedDoc.PersonalDetails.Photo.Crop != nil {
		t.Fatalf("replacement photo = %#v old=%q", replacedDoc.PersonalDetails.Photo, oldKey)
	}
	var queued string
	if err := h.pool.QueryRow(h.ctx, `SELECT object_key FROM media_deletion_jobs WHERE resume_id = $1`, created.ID).Scan(&queued); err != nil {
		t.Fatalf("read replacement job: %v", err)
	}
	if queued != oldKey {
		t.Fatalf("replacement queued %q, want %q", queued, oldKey)
	}
}

func TestPhotoInvalidStoredKeyFailsClosedBeforeBackendIO(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	invalid := []byte(`{"key":"not-a-photo-key"}`)
	if _, err := h.pool.Exec(h.ctx,
		`UPDATE resumes SET personal_details = jsonb_set(personal_details, '{photo}', $2::jsonb) WHERE id = $1`,
		created.ID, invalid); err != nil {
		t.Fatalf("seed invalid photo: %v", err)
	}
	backend := &photoBackendStub{}
	h.service.blobs = backend
	invariants := 0
	h.service.photoKeyInvariant = func() { invariants++ }

	got := h.getPhotoRequest(t, created.ID, "")
	if got.status != http.StatusInternalServerError || len(backend.gets) != 0 || len(backend.puts) != 0 || len(backend.deletes) != 0 || invariants != 1 {
		t.Fatalf("invalid GET = %d gets=%d puts=%d deletes=%d invariants=%d body=%s", got.status, len(backend.gets), len(backend.puts), len(backend.deletes), invariants, got.body)
	}
	crop := h.mutationRequest(t, http.MethodPatch, fmt.Sprintf("/api/v1/resumes/%s/photo", created.ID),
		bytes.NewBufferString(`{"crop":null}`), created.Revision, uuid.NewString())
	if crop.status != http.StatusInternalServerError || invariants != 2 || len(backend.gets) != 0 || len(backend.puts) != 0 || len(backend.deletes) != 0 {
		t.Fatalf("invalid crop = %d invariants=%d gets=%d puts=%d deletes=%d body=%s", crop.status, invariants, len(backend.gets), len(backend.puts), len(backend.deletes), crop.body)
	}
}

func TestPhotoReplacementQueueFailureRollsBackAndCompensatesCandidate(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	first := h.uploadPhotoRequest(t, created.ID, created.Revision, uuid.NewString(), "first.png", makePhotoPNG(t))
	if first.status != http.StatusOK {
		t.Fatalf("first upload = %d body=%s", first.status, first.body)
	}
	_, firstDoc := decodedWrittenDocument(t, first)
	oldKey := firstDoc.PersonalDetails.Photo.Key
	h.service.resumes = enqueueFailureStore{Store: h.resumes, err: errors.New("queue unavailable")}

	replaced := h.uploadPhotoRequest(t, created.ID, 2, uuid.NewString(), "second.png", makePhotoPNG(t))
	if replaced.status != http.StatusInternalServerError {
		t.Fatalf("replacement = %d body=%s", replaced.status, replaced.body)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Revision != 2 || stored.Doc.PersonalDetails.Photo == nil || stored.Doc.PersonalDetails.Photo.Key != oldKey {
		t.Fatalf("stored after queue failure revision=%d photo=%#v", stored.Revision, stored.Doc.PersonalDetails.Photo)
	}
	objects, _, err := h.service.blobs.ListPage(h.ctx, "resumes/"+created.ID.String()+"/", "", 10)
	if err != nil || len(objects) != 1 || objects[0].Key != oldKey {
		t.Fatalf("objects after rollback = %#v err=%v", objects, err)
	}
}

func TestPhotoUnknownPutStopsBeforeDatabaseAndNeverDeletes(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	backend := &photoBackendStub{outcomes: []media.PutOutcome{media.PutUnknown}, errors: []error{errors.New("unknown remote outcome")}}
	h.service.blobs = backend
	h.service.photoRandom = bytes.NewReader(bytes.Repeat([]byte{0x05}, 16))

	response := h.uploadPhotoRequest(t, created.ID, created.Revision, uuid.NewString(), "photo.png", makePhotoPNG(t))
	if response.status != http.StatusInternalServerError {
		t.Fatalf("unknown Put response = %d body=%s", response.status, response.body)
	}
	if len(backend.puts) != 1 || len(backend.deletes) != 0 {
		t.Fatalf("unknown Put calls=%d deletes=%v", len(backend.puts), backend.deletes)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Revision != created.Revision || stored.Doc.PersonalDetails.Photo != nil {
		t.Fatalf("stored after unknown Put revision=%d photo=%#v", stored.Revision, stored.Doc.PersonalDetails.Photo)
	}
	var records int
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM idempotency_records WHERE user_id = $1`, h.userID).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if records != 0 {
		t.Fatalf("idempotency records after unknown Put = %d", records)
	}
}

func TestPhotoExpiredCandidateNeverExecutesAndIsCompensated(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	base := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	afterPutCalls := -1
	backend := &photoBackendStub{outcomes: []media.PutOutcome{media.PutCreated}, errors: []error{nil}}
	backend.onPut = func() { afterPutCalls = 0 }
	h.service.blobs = backend
	h.service.photoRandom = bytes.NewReader(bytes.Repeat([]byte{0x06}, 16))
	h.service.clock = func() time.Time {
		if afterPutCalls < 0 {
			return base
		}
		afterPutCalls++
		if afterPutCalls == 1 {
			return base
		}
		return base.Add(photoCandidateLifetime + time.Second)
	}

	response := h.uploadPhotoRequest(t, created.ID, created.Revision, uuid.NewString(), "photo.png", makePhotoPNG(t))
	if response.status != http.StatusInternalServerError {
		t.Fatalf("expired candidate response = %d body=%s", response.status, response.body)
	}
	if len(backend.puts) != 1 || len(backend.deletes) != 1 || backend.deletes[0] != backend.puts[0].key {
		t.Fatalf("expired candidate puts=%#v deletes=%v", backend.puts, backend.deletes)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Revision != created.Revision || stored.Doc.PersonalDetails.Photo != nil {
		t.Fatalf("stored after expiry revision=%d photo=%#v", stored.Revision, stored.Doc.PersonalDetails.Photo)
	}
	var records int
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM idempotency_records WHERE user_id = $1`, h.userID).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if records != 0 {
		t.Fatalf("idempotency records after expiry = %d", records)
	}
}

func TestPhotoConcurrentUploadsLeaveOnlyCASWinnerObject(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	source := makePhotoPNG(t)
	start := make(chan struct{})
	responses := make([]testHTTPResponse, 2)
	var wg sync.WaitGroup
	for i := range responses {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			responses[index] = h.uploadPhotoRequest(t, created.ID, created.Revision, uuid.NewString(), fmt.Sprintf("photo-%d.png", index), source)
		}(i)
	}
	close(start)
	wg.Wait()
	statuses := map[int]int{}
	for _, response := range responses {
		statuses[response.status]++
	}
	if statuses[http.StatusOK] != 1 || statuses[http.StatusPreconditionFailed] != 1 {
		t.Fatalf("concurrent statuses = %v", statuses)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Doc.PersonalDetails.Photo == nil {
		t.Fatal("winning document has no photo")
	}
	objects, _, err := h.service.blobs.ListPage(h.ctx, "resumes/"+created.ID.String()+"/", "", 10)
	if err != nil || len(objects) != 1 || objects[0].Key != stored.Doc.PersonalDetails.Photo.Key {
		t.Fatalf("winner photo=%#v objects=%#v err=%v", stored.Doc.PersonalDetails.Photo, objects, err)
	}
}

func TestPhotoUnknownAndForeignResumeAreIndistinguishable(t *testing.T) {
	owner := newResumeAPITestHarness(t)
	foreignOwner := newResumeAPITestHarness(t)
	foreign := foreignOwner.createResume(t)
	unknown := uuid.New()
	source := makePhotoPNG(t)

	for _, id := range []uuid.UUID{unknown, foreign.ID} {
		got := owner.getPhotoRequest(t, id, "")
		if got.status != http.StatusNotFound || !bytes.Contains(got.body, []byte(`"resume_not_found"`)) {
			t.Errorf("GET %s = %d body=%s", id, got.status, got.body)
		}
		crop := owner.mutationRequest(t, http.MethodPatch, fmt.Sprintf("/api/v1/resumes/%s/photo", id),
			bytes.NewBufferString(`{"crop":null}`), 1, uuid.NewString())
		if crop.status != http.StatusNotFound || !bytes.Contains(crop.body, []byte(`"resume_not_found"`)) {
			t.Errorf("PATCH %s = %d body=%s", id, crop.status, crop.body)
		}
		upload := owner.uploadPhotoRequest(t, id, 1, uuid.NewString(), "photo.png", source)
		if upload.status != http.StatusNotFound || !bytes.Contains(upload.body, []byte(`"resume_not_found"`)) {
			t.Errorf("POST %s = %d body=%s", id, upload.status, upload.body)
		}
		objects, _, err := owner.service.blobs.ListPage(owner.ctx, "resumes/"+id.String()+"/", "", 10)
		if err != nil || len(objects) != 0 {
			t.Errorf("objects for rejected %s = %#v err=%v", id, objects, err)
		}
	}
}

func TestPhotoNormalizeErrorsUseClosedContract(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	for _, test := range []struct {
		name   string
		input  []byte
		status int
		code   string
		reason string
	}{
		{"unsupported", []byte("GIF89a"), http.StatusUnsupportedMediaType, "media_type_unsupported", ""},
		{"recognized malformed", []byte{0xff, 0xd8, 0xff}, http.StatusUnprocessableEntity, "media_invalid", "malformed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := h.uploadPhotoRequest(t, created.ID, created.Revision, uuid.NewString(), "photo.bin", test.input)
			if response.status != test.status || !bytes.Contains(response.body, []byte(`"`+test.code+`"`)) {
				t.Fatalf("response = %d body=%s", response.status, response.body)
			}
			if test.reason != "" && (!bytes.Contains(response.body, []byte(`"reason":"`+test.reason+`"`)) || bytes.Contains(response.body, []byte("EOF"))) {
				t.Fatalf("invalid details leaked or missing: %s", response.body)
			}
		})
	}
}
