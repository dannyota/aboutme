package resumeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/media"
)

type photoBackendCall struct {
	key         string
	contentType string
	body        []byte
}

type photoNeverReadBody struct{ reads int }

func (b *photoNeverReadBody) Read([]byte) (int, error) {
	b.reads++
	return 0, errors.New("unexpected body read")
}

func (*photoNeverReadBody) Close() error { return nil }

type photoBackendStub struct {
	puts     []photoBackendCall
	gets     []string
	deletes  []string
	outcomes []media.PutOutcome
	errors   []error
	getBody  []byte
	getType  string
	getErr   error
	onPut    func()
}

func (b *photoBackendStub) Put(_ context.Context, key, contentType string, body io.Reader, _ int64) (media.PutOutcome, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return media.PutNotCreated, err
	}
	b.puts = append(b.puts, photoBackendCall{key: key, contentType: contentType, body: raw})
	if b.onPut != nil {
		b.onPut()
	}
	index := len(b.puts) - 1
	return b.outcomes[index], b.errors[index]
}

func (b *photoBackendStub) Get(_ context.Context, key string) (io.ReadCloser, string, error) {
	b.gets = append(b.gets, key)
	return io.NopCloser(bytes.NewReader(b.getBody)), b.getType, b.getErr
}

func (b *photoBackendStub) Delete(_ context.Context, key string) error {
	b.deletes = append(b.deletes, key)
	return nil
}

func (*photoBackendStub) ListPage(context.Context, string, string, int) ([]media.Object, string, error) {
	return nil, "", nil
}

func photoMultipartRequest(t *testing.T, filename string, payload []byte, mutate func(*multipart.Writer) error) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		if err := mutate(writer); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptestNewPhotoRequest(&body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func httptestNewPhotoRequest(body io.Reader) *http.Request {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://example.test/api/v1/resumes/01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f/photo", body)
	if err != nil {
		panic(err)
	}
	return req
}

func TestPhotoMultipartAcceptsOnlyOneBoundedRawFilePart(t *testing.T) {
	payload := []byte{0x00, 0xff, 0x10, 0x0a}
	req := photoMultipartRequest(t, strings.Repeat("a", 251)+".png", payload, nil)
	got, err := decodePhotoMultipart(req)
	if err != nil {
		t.Fatalf("decode admitted multipart: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload = %x, want %x", got, payload)
	}

	tests := []struct {
		name string
		req  func(*testing.T) *http.Request
		code string
	}{
		{"wrong part", func(t *testing.T) *http.Request {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, createErr := writer.CreateFormFile("avatar", "a.png")
			if createErr != nil {
				t.Fatal(createErr)
			}
			if _, writeErr := part.Write(payload); writeErr != nil {
				t.Fatal(writeErr)
			}
			if closeErr := writer.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
			r := httptestNewPhotoRequest(&body)
			r.Header.Set("Content-Type", writer.FormDataContentType())
			return r
		}, "request_invalid"},
		{"extra part", func(t *testing.T) *http.Request {
			return photoMultipartRequest(t, "a.png", payload, func(w *multipart.Writer) error { return w.WriteField("extra", "x") })
		}, "request_invalid"},
		{"filename 256", func(t *testing.T) *http.Request {
			return photoMultipartRequest(t, strings.Repeat("a", 252)+".png", payload, nil)
		}, "request_invalid"},
		{"overlong raw filename with short basename", func(t *testing.T) *http.Request {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			header := textproto.MIMEHeader{
				"Content-Disposition": {`form-data; name="file"; filename="` + strings.Repeat("a", 300) + `/x.png"`},
				"Content-Type":        {"image/png"},
			}
			part, createErr := writer.CreatePart(header)
			if createErr != nil {
				t.Fatal(createErr)
			}
			if _, writeErr := part.Write(payload); writeErr != nil {
				t.Fatal(writeErr)
			}
			if closeErr := writer.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
			r := httptestNewPhotoRequest(&body)
			r.Header.Set("Content-Type", writer.FormDataContentType())
			return r
		}, "request_invalid"},
		{"file over limit", func(t *testing.T) *http.Request {
			return photoMultipartRequest(t, "a.png", bytes.Repeat([]byte{'x'}, photoFileBytes+1), nil)
		}, "media_too_large"},
		{"transfer encoding", func(t *testing.T) *http.Request {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			header := textproto.MIMEHeader{
				"Content-Disposition":       {`form-data; name="file"; filename="a.png"`},
				"Content-Type":              {"image/png"},
				"Content-Transfer-Encoding": {"base64"},
			}
			part, createErr := writer.CreatePart(header)
			if createErr != nil {
				t.Fatal(createErr)
			}
			if _, writeErr := part.Write(payload); writeErr != nil {
				t.Fatal(writeErr)
			}
			if closeErr := writer.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
			r := httptestNewPhotoRequest(&body)
			r.Header.Set("Content-Type", writer.FormDataContentType())
			return r
		}, "request_invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, decodeErr := decodePhotoMultipart(test.req(t))
			var client *clientError
			if !errors.As(decodeErr, &client) || client.Code != test.code {
				t.Fatalf("error = %v, want client code %q", decodeErr, test.code)
			}
		})
	}
}

func TestPhotoMultipartRejectsMalformedAndNonEmptyEpilogue(t *testing.T) {
	for name, body := range map[string]string{
		"truncated": "--boundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.png\"\r\n\r\nraw",
		"epilogue":  "--boundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.png\"\r\n\r\nraw\r\n--boundary--\r\nnot-empty",
	} {
		t.Run(name, func(t *testing.T) {
			req := httptestNewPhotoRequest(strings.NewReader(body))
			req.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")
			_, err := decodePhotoMultipart(req)
			var client *clientError
			if !errors.As(err, &client) || client.Code != "request_invalid" {
				t.Fatalf("error = %v, want request_invalid", err)
			}
		})
	}
}

func TestPhotoCropShapeBoundsAndApplication(t *testing.T) {
	valid, err := decodePhotoCrop([]byte(`{"crop":{"x":0,"y":1,"width":0.25,"height":1}}`))
	if err != nil {
		t.Fatalf("valid crop: %v", err)
	}
	if valid == nil || valid.X != 0 || valid.Y != 1 || valid.Width != 0.25 || valid.Height != 1 {
		t.Fatalf("crop = %#v", valid)
	}
	cleared, err := decodePhotoCrop([]byte(`{"crop":null}`))
	if err != nil || cleared != nil {
		t.Fatalf("clear crop = %#v, %v", cleared, err)
	}

	for _, raw := range []string{
		`{}`, `{"key":"x","crop":null}`, `{"crop":{"x":0,"y":0,"width":1}}`,
		`{"crop":{"x":-0.0001,"y":0,"width":1,"height":1}}`,
		`{"crop":{"x":0,"y":1.0001,"width":1,"height":1}}`,
		`{"crop":{"x":0,"y":0,"width":0,"height":1}}`,
		`{"crop":{"x":0,"y":0,"width":1,"height":1.0001}}`,
		`{"crop":{"x":0,"y":0,"width":1,"height":1,"extra":true}}`,
	} {
		if _, decodeErr := decodePhotoCrop([]byte(raw)); decodeErr == nil {
			t.Errorf("decodePhotoCrop(%s) succeeded", raw)
		}
	}

	resumeID := uuid.MustParse("01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f")
	doc := loadMinimalDocument(t)
	key := fmt.Sprintf("resumes/%s/photo-0123456789abcdef0123456789abcdef.jpg", resumeID)
	doc.PersonalDetails.Photo = &schema.Photo{Key: key}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	changed, oldKey, err := applyPhotoChange(raw, resumeID, valid, false)
	if err != nil || oldKey != key {
		t.Fatalf("apply crop oldKey=%q err=%v", oldKey, err)
	}
	var got schema.Resume
	if err := json.Unmarshal(changed, &got); err != nil {
		t.Fatal(err)
	}
	if got.PersonalDetails.Photo == nil || got.PersonalDetails.Photo.Key != key || got.PersonalDetails.Photo.Crop == nil {
		t.Fatalf("changed photo = %#v", got.PersonalDetails.Photo)
	}
}

func TestPhotoIfNoneMatchIsSingletonStrongTag(t *testing.T) {
	current := `"photo-0123456789abcdef"`
	for _, test := range []struct {
		values []string
		match  bool
		bad    bool
	}{
		{nil, false, false},
		{[]string{current}, true, false},
		{[]string{`""`}, false, false},
		{[]string{`"different"`}, false, false},
		{[]string{"*"}, false, true},
		{[]string{`W/"weak"`}, false, true},
		{[]string{`"a", "b"`}, false, true},
		{[]string{`"a"`, `"b"`}, false, true},
		{[]string{"unquoted"}, false, true},
	} {
		match, err := photoIfNoneMatch(test.values, current)
		if match != test.match || (err != nil) != test.bad {
			t.Errorf("values=%q => match=%v err=%v", test.values, match, err)
		}
	}
}

func TestPhotoCandidateRetriesOnlyProvedCollisionsAndUsesNormalizedExtension(t *testing.T) {
	resumeID := uuid.MustParse("01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f")
	randomness := bytes.Repeat([]byte{0x01}, 16)
	randomness = append(randomness, bytes.Repeat([]byte{0x02}, 16)...)
	backend := &photoBackendStub{
		outcomes: []media.PutOutcome{media.PutNotCreated, media.PutCreated},
		errors:   []error{media.ErrAlreadyExists, nil},
	}
	normalized := media.NormalizedPhoto{Bytes: []byte("normalized"), ContentType: "image/png", Extension: "png"}
	candidate, err := createPhotoCandidate(context.Background(), backend, bytes.NewReader(randomness), resumeID, normalized, time.Now)
	if err != nil {
		t.Fatalf("create candidate: %v", err)
	}
	if !candidate.Created || len(backend.puts) != 2 || candidate.Key != backend.puts[1].key {
		t.Fatalf("candidate=%#v puts=%#v", candidate, backend.puts)
	}
	if _, err := media.ParsePhotoKey(resumeID, candidate.Key); err != nil || !strings.HasSuffix(candidate.Key, ".png") {
		t.Fatalf("candidate key = %q: %v", candidate.Key, err)
	}
	if len(backend.deletes) != 0 || !bytes.Equal(backend.puts[1].body, normalized.Bytes) {
		t.Fatalf("deletes=%v stored=%q", backend.deletes, backend.puts[1].body)
	}
}

func TestPhotoCandidateStopsAtThreeCollisionsAndNeverDeletes(t *testing.T) {
	resumeID := uuid.MustParse("01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f")
	backend := &photoBackendStub{
		outcomes: []media.PutOutcome{media.PutNotCreated, media.PutNotCreated, media.PutNotCreated},
		errors:   []error{media.ErrAlreadyExists, media.ErrAlreadyExists, media.ErrAlreadyExists},
	}
	_, err := createPhotoCandidate(context.Background(), backend, bytes.NewReader(bytes.Repeat([]byte{0x03}, 48)), resumeID,
		media.NormalizedPhoto{Bytes: []byte("x"), ContentType: "image/jpeg", Extension: "jpg"}, time.Now)
	if !errors.Is(err, media.ErrAlreadyExists) || len(backend.puts) != photoPutAttempts || len(backend.deletes) != 0 {
		t.Fatalf("error=%v puts=%d deletes=%v", err, len(backend.puts), backend.deletes)
	}
}

func TestPhotoCandidateUnknownAndDefiniteFailureDoNotRetryOrDelete(t *testing.T) {
	resumeID := uuid.MustParse("01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f")
	for _, outcome := range []media.PutOutcome{media.PutUnknown, media.PutNotCreated} {
		backend := &photoBackendStub{outcomes: []media.PutOutcome{outcome}, errors: []error{errors.New("backend failed")}}
		_, err := createPhotoCandidate(context.Background(), backend, bytes.NewReader(bytes.Repeat([]byte{0x04}, 16)), resumeID,
			media.NormalizedPhoto{Bytes: []byte("x"), ContentType: "image/jpeg", Extension: "jpg"}, time.Now)
		if err == nil || len(backend.puts) != 1 || len(backend.deletes) != 0 {
			t.Errorf("outcome=%v error=%v puts=%d deletes=%v", outcome, err, len(backend.puts), backend.deletes)
		}
	}
}

func TestPhotoBusyWaitsOneSecondAndDoesNotReadBody(t *testing.T) {
	release, err := taskPhotoAdmission.Acquire(context.Background())
	if err != nil {
		t.Fatalf("hold admission: %v", err)
	}
	defer release()

	body := &photoNeverReadBody{}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/resumes/01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f/photo", nil)
	req.Body = body
	req.ContentLength = -1
	req.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	req.Header.Set("Idempotency-Key", uuid.NewString())
	req.Header.Set("If-Match", `"r1"`)
	req.SetPathValue("id", "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f")
	recorder := httptest.NewRecorder()
	started := time.Now()
	(&Service{acceptedVersions: []int32{2}}).handleUploadResumePhoto(recorder, req)
	elapsed := time.Since(started)
	if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Retry-After") != "1" {
		t.Fatalf("busy response = %d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.Bytes())
	}
	if body.reads != 0 {
		t.Fatalf("busy body reads = %d", body.reads)
	}
	if elapsed < 900*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("busy wait = %v, want about one second", elapsed)
	}
}

func TestPhotoDeclaredOverflowDoesNotReadBody(t *testing.T) {
	body := &photoNeverReadBody{}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/resumes/01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f/photo", nil)
	req.Body = body
	req.ContentLength = photoRequestBytes + 1
	req.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	req.Header.Set("Idempotency-Key", uuid.NewString())
	req.Header.Set("If-Match", `"r1"`)
	req.SetPathValue("id", "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f")
	recorder := httptest.NewRecorder()
	(&Service{acceptedVersions: []int32{2}}).handleUploadResumePhoto(recorder, req)
	if recorder.Code != http.StatusRequestEntityTooLarge || !bytes.Contains(recorder.Body.Bytes(), []byte(`"media_too_large"`)) {
		t.Fatalf("declared overflow = %d body=%s", recorder.Code, recorder.Body.Bytes())
	}
	if body.reads != 0 {
		t.Fatalf("declared overflow body reads = %d", body.reads)
	}
}

func TestPhotoWrappedMaxBytesErrorMapsToMediaTooLarge(t *testing.T) {
	err := fmt.Errorf("wrapped intake overflow: %w", &http.MaxBytesError{Limit: photoRequestBytes})
	if got := mapPhotoMultipartError(err); got.Status != http.StatusRequestEntityTooLarge || got.Code != "media_too_large" {
		t.Fatalf("mapped error = %#v", got)
	}
}

func TestPhotoTransportExactByteBoundaries(t *testing.T) {
	exactFile := bytes.Repeat([]byte{'x'}, photoFileBytes)
	req := photoMultipartRequest(t, "photo.bin", exactFile, nil)
	got, err := decodePhotoMultipart(req)
	if err != nil || len(got) != photoFileBytes {
		t.Fatalf("exact file boundary length=%d error=%v", len(got), err)
	}

	buildRequest := func(size int64) *http.Request {
		const prefix = "--b\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.bin\"\r\nX-Pad: "
		const suffix = "\r\n\r\nx\r\n--b--\r\n"
		pad := int(size) - len(prefix) - len(suffix)
		if pad < 0 {
			t.Fatal("invalid transport test size")
		}
		body := prefix + strings.Repeat("a", pad) + suffix
		r := httptestNewPhotoRequest(strings.NewReader(body))
		r.Header.Set("Content-Type", "multipart/form-data; boundary=b")
		r.Body = http.MaxBytesReader(httptest.NewRecorder(), r.Body, photoRequestBytes)
		return r
	}
	admitted, err := decodePhotoMultipart(buildRequest(photoRequestBytes))
	if err != nil || !bytes.Equal(admitted, []byte("x")) {
		t.Fatalf("exact request boundary payload=%q error=%v", admitted, err)
	}
	_, err = decodePhotoMultipart(buildRequest(photoRequestBytes + 1))
	var client *clientError
	if !errors.As(err, &client) || client.Code != "media_too_large" {
		t.Fatalf("request boundary + 1 error=%v", err)
	}
}
