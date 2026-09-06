package accountapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// TestExportRouteRejectsUnsupportedMethod catches a route that admits a
// mutating account-export request or masks its method contract behind auth.
func TestExportRouteRejectsUnsupportedMethod(t *testing.T) {
	mux := http.NewServeMux()
	(&Service{}).RegisterRoutes(mux)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodPost, ExportPath, nil))

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
	if got := recorder.Header().Get("Allow"); got != http.MethodGet {
		t.Fatalf("Allow = %q, want %q", got, http.MethodGet)
	}
}

func TestExportRouteRequiresCookieSession(t *testing.T) {
	h := newExportTestHarness(t)
	recorder := httptest.NewRecorder()
	h.mux.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, ExportPath, nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestExportRouteExportsAnEmptyResumeCollection(t *testing.T) {
	h := newExportTestHarness(t)
	response := h.request(t, http.MethodGet, ExportPath, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	var output struct {
		Data struct {
			Resumes []json.RawMessage `json:"resumes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &output); err != nil {
		t.Fatalf("decode empty export: %v", err)
	}
	if output.Data.Resumes == nil || len(output.Data.Resumes) != 0 {
		t.Fatalf("resumes = %#v, want an empty array", output.Data.Resumes)
	}
}

func TestExportRouteProducesPortableFrozenAttachment(t *testing.T) {
	h := newExportTestHarness(t)
	credentialSentinel := "credential-sentinel"
	avatarSentinel := "avatar-key-sentinel"
	if _, err := h.pool.Exec(h.ctx, "UPDATE users SET avatar_key = $1 WHERE id = $2", avatarSentinel, h.user.ID); err != nil {
		t.Fatalf("seed avatar sentinel: %v", err)
	}
	if _, err := h.pool.Exec(h.ctx, `INSERT INTO password_credentials (user_id, encoded_hash) VALUES ($1, $2)`, h.user.ID, []byte(credentialSentinel)); err != nil {
		t.Fatalf("seed credential sentinel: %v", err)
	}
	for _, identity := range []struct{ provider, subject string }{
		{"github", "subject-sentinel-github"},
		{"google", "subject-sentinel-google"},
		{"github", "subject-sentinel-github-duplicate"},
	} {
		if _, err := h.q.CreateIdentity(h.ctx, store.CreateIdentityParams{UserID: h.user.ID, Provider: identity.provider, ProviderUserID: identity.subject}); err != nil {
			t.Fatalf("seed identity: %v", err)
		}
	}

	photoBytes := exportTestPNG(t)
	photo := h.insertResume(t, "", true, photoBytes)
	h.insertResume(t, "draft one", false, nil)
	h.insertResume(t, "draft two", false, nil)

	response := h.request(t, http.MethodGet, ExportPath, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != exportCacheControl {
		t.Errorf("Cache-Control = %q, want %q", got, exportCacheControl)
	}
	if got := response.Header().Get("Content-Disposition"); got != exportAttachment {
		t.Errorf("Content-Disposition = %q, want %q", got, exportAttachment)
	}
	if got := response.Header().Get(exportSchemaHeader); got != "2" {
		t.Errorf("schema header = %q, want 2", got)
	}

	var output struct {
		Data struct {
			Account struct {
				LinkedProviders []string `json:"linkedProviders"`
			} `json:"account"`
			Resumes []struct {
				ID       uuid.UUID      `json:"id"`
				Document map[string]any `json:"document"`
				Photo    *struct {
					MediaType string `json:"mediaType"`
					Data      string `json:"data"`
					Crop      any    `json:"crop"`
				} `json:"photo"`
			} `json:"resumes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &output); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	if got := output.Data.Account.LinkedProviders; len(got) != 2 || got[0] != "github" || got[1] != "google" {
		t.Fatalf("linkedProviders = %#v, want first-occurrence unique providers", got)
	}
	if len(output.Data.Resumes) != 3 {
		t.Fatalf("resume count = %d, want 3", len(output.Data.Resumes))
	}
	if output.Data.Resumes[0].ID != photo.ID || output.Data.Resumes[0].Photo == nil {
		t.Fatalf("first resume = %#v, want the ordered photo resume", output.Data.Resumes[0])
	}
	personalDetails, ok := output.Data.Resumes[0].Document["personalDetails"].(map[string]any)
	if !ok {
		t.Fatal("document.personalDetails was not an object")
	}
	if _, exists := personalDetails["photo"]; exists {
		t.Fatal("document.personalDetails.photo was exported")
	}
	if got := output.Data.Resumes[0].Photo.MediaType; got != "image/png" {
		t.Errorf("photo mediaType = %q, want image/png", got)
	}
	if got, err := base64.StdEncoding.DecodeString(output.Data.Resumes[0].Photo.Data); err != nil || !bytes.Equal(got, photoBytes) {
		t.Errorf("portable photo = %x, %v; want original PNG bytes", got, err)
	}
	for _, sentinel := range []string{credentialSentinel, avatarSentinel, "subject-sentinel"} {
		if bytes.Contains(response.Body.Bytes(), []byte(sentinel)) || strings.Contains(h.logs.String(), sentinel) {
			t.Errorf("sentinel %q escaped the portable export boundary", sentinel)
		}
	}
}

func TestExportRouteRejectsQueryBodyAndClosedHeaders(t *testing.T) {
	for _, test := range []struct {
		name   string
		path   string
		body   io.Reader
		header string
	}{
		{name: "query", path: ExportPath + "?x=1"},
		{name: "body", path: ExportPath, body: strings.NewReader("x")},
		{name: "chunked body", path: ExportPath, body: exportTestUnknownLengthReader{Reader: strings.NewReader("x")}},
		{name: "authorization header", path: ExportPath, header: "Authorization"},
		{name: "schema header", path: ExportPath, header: exportSchemaHeader},
		{name: "conditional header", path: ExportPath, header: "If-None-Match"},
		{name: "idempotency header", path: ExportPath, header: "Idempotency-Key"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newExportTestHarness(t)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, test.path, test.body)
			req.RemoteAddr = "127.0.0.1:3210"
			req.AddCookie(&http.Cookie{Name: "__Host-session", Value: h.token})
			if test.header != "" {
				req.Header.Set(test.header, "present")
			}
			recorder := httptest.NewRecorder()
			h.mux.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s, want 400", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestExportRouteFailsBeforeHeadersWhenReferencedPhotoIsMissing(t *testing.T) {
	h := newExportTestHarness(t)
	row := h.insertResume(t, "photo missing", false, nil)
	document := exportTestDocument()
	document.PersonalDetails.Photo = &schema.Photo{Key: "resumes/" + row.ID.String() + "/photo-00000000000000000000000000000000.png"}
	personal, err := json.Marshal(document.PersonalDetails)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(h.ctx, "UPDATE resumes SET personal_details = $1 WHERE id = $2", personal, row.ID); err != nil {
		t.Fatalf("set missing photo reference: %v", err)
	}

	response := h.request(t, http.MethodGet, ExportPath, nil)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s, want 503", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Disposition") != "" || response.Header().Get("Content-Length") != "" {
		t.Fatalf("failed export exposed success headers: %#v", response.Header())
	}
}

func TestMarshalExportPayloadRejectsTheTwelveMiBCap(t *testing.T) {
	_, err := marshalExportPayload(exportData{Account: exportProfile{Name: strings.Repeat("x", exportMaxBytes)}})
	if err == nil {
		t.Fatal("marshalExportPayload() error = nil, want size-limit failure")
	}
}

func TestProjectExportLanguageReturnsAStringForCurrentNullAndInvalidValues(t *testing.T) {
	current := "EN-us"
	invalid := "not a language tag!"
	for _, test := range []struct {
		name  string
		value *string
		want  string
	}{
		{name: "current", value: &current, want: "en-US"},
		{name: "null", want: "und"},
		{name: "invalid", value: &invalid, want: "und"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := projectExportLanguage(test.value); got != test.want {
				t.Fatalf("projectExportLanguage(%v) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

func TestValidateExportResumeCountRejectsUnexpectedFourthRow(t *testing.T) {
	if err := validateExportResumeCount(make([]store.Resume, 3)); err != nil {
		t.Fatalf("three resumes error = %v", err)
	}
	if err := validateExportResumeCount(make([]store.Resume, 4)); err == nil {
		t.Fatal("four resumes error = nil, want bounded export failure")
	}
}

func TestExportRouteProjectsInvalidStoredLanguageToUnd(t *testing.T) {
	h := newExportTestHarness(t)
	row := h.insertResume(t, "invalid language", false, nil)
	if _, err := h.pool.Exec(h.ctx, "UPDATE resumes SET lng = $1 WHERE id = $2", "not a language tag!", row.ID); err != nil {
		t.Fatalf("set invalid language: %v", err)
	}
	response := h.request(t, http.MethodGet, ExportPath, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", response.Code, response.Body.String())
	}
	var output struct {
		Data struct {
			Resumes []struct {
				Lng string `json:"lng"`
			} `json:"resumes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &output); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	if got := output.Data.Resumes[0].Lng; got != "und" {
		t.Fatalf("lng = %q, want und", got)
	}
}

func TestExportRouteRateLimitIsIndependentAndAccountScoped(t *testing.T) {
	h := newExportTestHarness(t)
	for attempt := 1; attempt <= exportRequestsPerMinute; attempt++ {
		if response := h.request(t, http.MethodGet, ExportPath, nil); response.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want 200", attempt, response.Code)
		}
	}
	if response := h.request(t, http.MethodGet, ExportPath, nil); response.Code != http.StatusTooManyRequests {
		t.Fatalf("limited request status = %d, want 429", response.Code)
	}
}

func TestExportPhotoRejectsUnreadableOversizedAndUnknownMedia(t *testing.T) {
	resumeID := uuid.New()
	ref := &storedPhotoReference{Key: "resumes/" + resumeID.String() + "/photo-00000000000000000000000000000000.png", Crop: json.RawMessage("null")}
	for _, test := range []struct {
		name      string
		reader    io.ReadCloser
		mediaType string
	}{
		{name: "reader failure", reader: &exportTestReadCloser{Reader: exportTestFailReader{}}, mediaType: "image/png"},
		{name: "close failure", reader: &exportTestReadCloser{Reader: strings.NewReader("png"), closeErr: errors.New("close")}, mediaType: "image/png"},
		{name: "oversized", reader: io.NopCloser(bytes.NewReader(make([]byte, media.MaxObjectBytes+1))), mediaType: "image/png"},
		{name: "unknown type", reader: io.NopCloser(strings.NewReader("bytes")), mediaType: "image/webp"},
		{name: "truncated png", reader: io.NopCloser(strings.NewReader("not a png")), mediaType: "image/png"},
		{name: "extension MIME mismatch", reader: io.NopCloser(bytes.NewReader(exportTestPNG(t))), mediaType: "image/jpeg"},
		{name: "PNG over edge limit", reader: io.NopCloser(bytes.NewReader(exportTestPNGDimensions(t, exportPNGMaxEdge+1, 1))), mediaType: "image/png"},
		{name: "nil reader", mediaType: "image/png"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &Service{media: exportTestMedia{reader: test.reader, mediaType: test.mediaType}}
			if _, err := service.exportPhoto(context.Background(), resumeID, ref); err == nil {
				t.Fatal("exportPhoto() error = nil, want failure")
			}
		})
	}
}

func TestExportPhotoClosesAnObjectReturnedWithGetError(t *testing.T) {
	resumeID := uuid.New()
	body := &exportTestReadCloser{Reader: strings.NewReader("ignored")}
	service := &Service{media: exportTestMedia{reader: body, err: errors.New("get failed")}}
	ref := &storedPhotoReference{Key: exportTestPhotoKey(resumeID), Crop: json.RawMessage("null")}
	if _, err := service.exportPhoto(context.Background(), resumeID, ref); err == nil || !body.closed {
		t.Fatalf("exportPhoto() error=%v closed=%v, want closed error body", err, body.closed)
	}
}

func TestExportPhotoCancellationClosesAndJoinsBlockedReader(t *testing.T) {
	resumeID := uuid.New()
	body := newExportTestBlockingBody()
	service := &Service{media: exportTestMedia{reader: body, mediaType: "image/png"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := service.exportPhoto(ctx, resumeID, &storedPhotoReference{Key: exportTestPhotoKey(resumeID), Crop: json.RawMessage("null")})
		done <- err
	}()
	<-body.started
	cancel()
	select {
	case err := <-done:
		if err == nil || !body.wasClosed() {
			t.Fatalf("exportPhoto() error=%v closed=%v, want a canceled closed read", err, body.wasClosed())
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("canceled exportPhoto did not close and join the blocked reader")
	}
}

func TestExportRequestInvalidRejectsAnUnknownLengthBodyWithoutReadingIt(t *testing.T) {
	body := newExportTestBlockingBody()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, ExportPath, body)
	done := make(chan bool, 1)
	go func() { done <- exportRequestInvalid(req) }()
	select {
	case invalid := <-done:
		if !invalid {
			t.Fatal("unknown-length body was accepted")
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("request validation blocked reading an unknown-length body")
	}
}

func TestExportAttachmentKeepsTheDatabaseSnapshotWhilePhotoReadIsGated(t *testing.T) {
	h := newExportTestHarness(t)
	row := h.insertResume(t, "original title", true, exportTestPNG(t))
	gate := &exportTestGatedMedia{delegate: h.media, reached: make(chan struct{}), release: make(chan struct{})}
	h.service.media = gate
	done := make(chan struct {
		payload []byte
		err     error
	}, 1)
	go func() {
		payload, err := h.service.exportAttachment(context.Background(), h.user.ID)
		done <- struct {
			payload []byte
			err     error
		}{payload, err}
	}()
	<-gate.reached
	if _, err := h.pool.Exec(h.ctx, "UPDATE users SET name = $1 WHERE id = $2", "changed name", h.user.ID); err != nil {
		t.Fatalf("change profile after snapshot: %v", err)
	}
	if _, err := h.pool.Exec(h.ctx, "UPDATE resumes SET title = $1 WHERE id = $2", "changed title", row.ID); err != nil {
		t.Fatalf("change resume after snapshot: %v", err)
	}
	close(gate.release)
	result := <-done
	if result.err != nil {
		t.Fatalf("export attachment: %v", result.err)
	}
	var output struct {
		Data struct {
			Account struct {
				Name string `json:"name"`
			} `json:"account"`
			Resumes []struct {
				Title string `json:"title"`
			} `json:"resumes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(result.payload, &output); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	if output.Data.Account.Name != "Export test" || output.Data.Resumes[0].Title != "original title" {
		t.Fatalf("snapshot export = account=%q resume=%q, want original values", output.Data.Account.Name, output.Data.Resumes[0].Title)
	}
}

type exportTestHarness struct {
	ctx     context.Context
	pool    *store.Pool
	q       *store.Queries
	user    store.User
	token   string
	media   media.Backend
	mux     *http.ServeMux
	logs    *bytes.Buffer
	service *Service
}

func newExportTestHarness(t *testing.T) *exportTestHarness {
	t.Helper()
	ctx := context.Background()
	pool, err := store.NewPool(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	q := store.New(pool)
	user, err := q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.test", Name: "Export test"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		if _, cleanupErr := pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID); cleanupErr != nil {
			t.Errorf("delete user: %v", cleanupErr)
		}
	})
	backend, err := media.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("new media backend: %v", err)
	}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	logs := new(bytes.Buffer)
	service, err := New(Dependencies{
		Pool: pool, Sessions: auth.NewSessionManager(q), Coordinator: coordinator, Media: backend,
		Projector: docmigrate.NewIdentityProjector(), Logger: slog.New(slog.NewJSONHandler(logs, nil)),
		PublicOrigin: "https://aboutme.test", Now: func() time.Time { return time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	token, _, err := auth.NewSessionManager(q).Issue(ctx, user.ID, "", "127.0.0.1")
	if err != nil {
		t.Fatalf("issue session: %v", err)
	}
	mux := http.NewServeMux()
	service.RegisterRoutes(mux)
	return &exportTestHarness{ctx: ctx, pool: pool, q: q, user: user, token: token, media: backend, mux: mux, logs: logs, service: service}
}

func (h *exportTestHarness) request(t *testing.T, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, body)
	req.RemoteAddr = "127.0.0.1:3210"
	req.AddCookie(&http.Cookie{Name: "__Host-session", Value: h.token})
	recorder := httptest.NewRecorder()
	h.mux.ServeHTTP(recorder, req)
	return recorder
}

func (h *exportTestHarness) insertResume(t *testing.T, title string, withPhoto bool, photoBytes []byte) store.Resume {
	t.Helper()
	document := exportTestDocument()
	var key string
	if withPhoto {
		key = "resumes/" + uuid.NewString() + "/photo-00000000000000000000000000000000.png"
		document.PersonalDetails.Photo = &schema.Photo{Key: key, Crop: &schema.PhotoCrop{X: 0, Y: 0, Width: 1, Height: 1}}
	}
	personal, err := json.Marshal(document.PersonalDetails)
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(document.Content)
	if err != nil {
		t.Fatal(err)
	}
	customization, err := json.Marshal(document.Customization)
	if err != nil {
		t.Fatal(err)
	}
	row, err := h.q.CreateResume(h.ctx, store.CreateResumeParams{
		UserID: h.user.ID, Title: title, SchemaVersion: docmigrate.CurrentVersion,
		PersonalDetails: personal, Content: content, Customization: customization,
	})
	if err != nil {
		t.Fatalf("create resume: %v", err)
	}
	if withPhoto {
		key = "resumes/" + row.ID.String() + "/photo-00000000000000000000000000000000.png"
		document.PersonalDetails.Photo.Key = key
		personal, err = json.Marshal(document.PersonalDetails)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.pool.Exec(h.ctx, "UPDATE resumes SET personal_details = $1 WHERE id = $2", personal, row.ID); err != nil {
			t.Fatalf("set photo reference: %v", err)
		}
		if outcome, err := h.media.Put(h.ctx, key, "image/png", bytes.NewReader(photoBytes), int64(len(photoBytes))); err != nil || outcome != media.PutCreated {
			t.Fatalf("put photo outcome=%v err=%v", outcome, err)
		}
	}
	return row
}

func exportTestDocument() schema.Resume {
	name := "Ada"
	return schema.Resume{
		SchemaVersion: int64(schema.CurrentVersion),
		PersonalDetails: schema.PersonalDetails{FullName: &name, Details: []schema.PersonalDetail{{
			ID: "private", IsHidden: true, Type: schema.Email, Value: "hidden-sentinel@example.test",
		}}},
		Content: map[string]schema.Section{},
		Customization: schema.Customization{
			Font:           schema.Font{Family: schema.Inter, BaseSizePx: 14},
			Colors:         schema.Colors{Primary: "#1a1a1a", Text: "#1a1a1a", Background: "#ffffff"},
			Spacing:        schema.Spacing{SectionGap: 16, EntryGap: 8, LineHeight: 1.4},
			Heading:        schema.Heading{Style: schema.Normal, ShowRule: false},
			Layout:         schema.Layout{Columns: 1, Sections: schema.Sections{Main: []string{}, Sidebar: []string{}}},
			SectionDisplay: schema.SectionDisplay{Skill: schema.SkillClass{Style: schema.Text}, Language: schema.LanguageClass{Style: schema.Text}},
			PageFormat:     schema.A4, DateFormat: schema.MmYyyy,
		},
	}
}

func exportTestPNG(t *testing.T) []byte {
	return exportTestPNGDimensions(t, 1, 1)
}

func exportTestPNGDimensions(t *testing.T, width, height int) []byte {
	t.Helper()
	imageData := image.NewRGBA(image.Rect(0, 0, width, height))
	imageData.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	var data bytes.Buffer
	err := png.Encode(&data, imageData)
	if err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

type exportTestMedia struct {
	reader    io.ReadCloser
	mediaType string
	err       error
}

func (m exportTestMedia) Get(context.Context, string) (io.ReadCloser, string, error) {
	return m.reader, m.mediaType, m.err
}

func (exportTestMedia) Put(context.Context, string, string, io.Reader, int64) (media.PutOutcome, error) {
	return media.PutNotCreated, errors.New("unused")
}

func (exportTestMedia) Delete(context.Context, string) error { return errors.New("unused") }

func (exportTestMedia) ListPage(context.Context, string, string, int) ([]media.Object, string, error) {
	return nil, "", errors.New("unused")
}

type exportTestReadCloser struct {
	io.Reader
	closeErr error
	closed   bool
}

func (r *exportTestReadCloser) Close() error { r.closed = true; return r.closeErr }

type exportTestFailReader struct{}

func (exportTestFailReader) Read([]byte) (int, error) { return 0, errors.New("read") }

type exportTestUnknownLengthReader struct{ io.Reader }

func exportTestPhotoKey(resumeID uuid.UUID) string {
	return "resumes/" + resumeID.String() + "/photo-00000000000000000000000000000000.png"
}

type exportTestBlockingBody struct {
	started chan struct{}
	closed  chan struct{}
}

func newExportTestBlockingBody() *exportTestBlockingBody {
	return &exportTestBlockingBody{started: make(chan struct{}), closed: make(chan struct{})}
}

func (b *exportTestBlockingBody) Read([]byte) (int, error) {
	select {
	case <-b.started:
	default:
		close(b.started)
	}
	<-b.closed
	return 0, io.EOF
}

func (b *exportTestBlockingBody) Close() error {
	select {
	case <-b.closed:
	default:
		close(b.closed)
	}
	return nil
}

func (b *exportTestBlockingBody) wasClosed() bool {
	select {
	case <-b.closed:
		return true
	default:
		return false
	}
}

type exportTestGatedMedia struct {
	delegate media.Backend
	reached  chan struct{}
	release  chan struct{}
}

func (m *exportTestGatedMedia) Get(ctx context.Context, key string) (io.ReadCloser, string, error) {
	select {
	case <-m.reached:
	default:
		close(m.reached)
	}
	select {
	case <-m.release:
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}
	return m.delegate.Get(ctx, key)
}

func (m *exportTestGatedMedia) Put(ctx context.Context, key, mediaType string, body io.Reader, size int64) (media.PutOutcome, error) {
	return m.delegate.Put(ctx, key, mediaType, body, size)
}

func (m *exportTestGatedMedia) Delete(ctx context.Context, key string) error {
	return m.delegate.Delete(ctx, key)
}

func (m *exportTestGatedMedia) ListPage(ctx context.Context, prefix, cursor string, limit int) ([]media.Object, string, error) {
	return m.delegate.ListPage(ctx, prefix, cursor, limit)
}
