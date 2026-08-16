package mailcapture

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/authmail"
)

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testSecret() []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

func testServer(t *testing.T) *Server {
	t.Helper()
	s, err := NewServer(testSecret(), testLogger(t))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return s
}

func bearer(s *Server) string {
	return bearerPrefix + base64.RawURLEncoding.EncodeToString(s.secret[:])
}

func authorizedRequest(t *testing.T, s *Server, method, target string, body io.Reader) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, target, body)
	r.RemoteAddr = "127.0.0.1:55555"
	r.Header.Set("Authorization", bearer(s))
	return r
}

func TestMailCaptureNewServerRejectsBadInputs(t *testing.T) {
	if _, err := NewServer([]byte("short"), testLogger(t)); !errors.Is(err, ErrSecret) {
		t.Fatalf("err = %v, want ErrSecret", err)
	}
	if _, err := NewServer(make([]byte, 32), nil); !errors.Is(err, ErrConfig) {
		t.Fatalf("err = %v, want ErrConfig", err)
	}
}

func TestMailCaptureNewClientRejectsBadInputs(t *testing.T) {
	if _, err := NewClient("http://127.0.0.1:20091", []byte("short"), testLogger(t)); !errors.Is(err, ErrSecret) {
		t.Fatalf("err = %v, want ErrSecret", err)
	}
	if _, err := NewClient("http://127.0.0.1:20091", testSecret(), nil); !errors.Is(err, ErrConfig) {
		t.Fatalf("err = %v, want ErrConfig", err)
	}
	for _, base := range []string{
		"http://example.com:20091",
		"https://example.com",
		"http://0.0.0.0:20091",
		"http://192.168.1.1:20091",
		"ftp://127.0.0.1",
		"",
		"not a url",
	} {
		if _, err := NewClient(base, testSecret(), testLogger(t)); !errors.Is(err, ErrConfig) {
			t.Errorf("NewClient(%q) = %v, want ErrConfig", base, err)
		}
	}
	for _, base := range []string{"http://127.0.0.1:20091", "http://localhost:20091", "https://localhost:20444"} {
		if _, err := NewClient(base, testSecret(), testLogger(t)); err != nil {
			t.Errorf("NewClient(%q) = %v, want nil", base, err)
		}
	}
}

func TestMailCaptureServerRejectsNonLoopback(t *testing.T) {
	s := testServer(t)
	r := httptest.NewRequest(http.MethodPost, "/capture", nil)
	r.RemoteAddr = "203.0.113.9:12345" // non-loopback, never a real source
	r.Header.Set("Authorization", bearer(s))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403", rec.Code)
	}
}

func TestMailCaptureServerRejectsBadAuth(t *testing.T) {
	s := testServer(t)
	cases := []struct {
		name  string
		token string
	}{
		{"missing", ""},
		{"wrong scheme", "Basic abc"},
		{"empty token", bearerPrefix},
		{"wrong secret", bearerPrefix + base64.RawURLEncoding.EncodeToString(make([]byte, 32))},
		{"bad base64", bearerPrefix + "!!!"},
		{"not 32 bytes", bearerPrefix + base64.RawURLEncoding.EncodeToString([]byte("short"))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/messages", nil)
			r.RemoteAddr = "127.0.0.1:55555"
			if tc.token != "" {
				r.Header.Set("Authorization", tc.token)
			}
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, r)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("code = %d, want 401", rec.Code)
			}
		})
	}
}

func TestMailCaptureServerCaptureEndToEnd(t *testing.T) {
	s := testServer(t)
	m := sampleMessage()
	body, _ := json.Marshal(m)

	r := authorizedRequest(t, s, http.MethodPost, "/capture", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("capture code = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}

	// GET returns the message newest-first.
	r = authorizedRequest(t, s, http.MethodGet, "/api/messages", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("get code = %d, want 200", rec.Code)
	}
	var got struct {
		Messages []StoredMessage `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(got.Messages))
	}
	sm := got.Messages[0]
	if sm.Kind != m.Kind || sm.To != m.To || sm.Subject != m.Subject || sm.TextBody != m.TextBody || sm.HTMLBody != m.HTMLBody {
		t.Fatalf("stored = %+v, want %+v", sm, m)
	}
	if sm.ID != 1 {
		t.Fatalf("ID = %d, want 1", sm.ID)
	}

	// DELETE resets.
	r = authorizedRequest(t, s, http.MethodDelete, "/api/messages", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete code = %d, want 204", rec.Code)
	}
	r = authorizedRequest(t, s, http.MethodGet, "/api/messages", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	var after struct {
		Messages []StoredMessage `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &after); err != nil {
		t.Fatalf("decode after reset: %v", err)
	}
	if len(after.Messages) != 0 {
		t.Fatalf("after reset messages = %d, want 0", len(after.Messages))
	}
}

func TestMailCaptureServerCaptureMethodNotAllowed(t *testing.T) {
	s := testServer(t)
	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodPut} {
		r := authorizedRequest(t, s, method, "/capture", nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, r)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /capture = %d, want 405", method, rec.Code)
		}
	}
}

func TestMailCaptureServerCaptureRejectsStrictJSON(t *testing.T) {
	s := testServer(t)
	valid := `{"kind":"verify","to":"a@b.c","subject":"s","text_body":"t","html_body":"h"}`
	cases := []struct {
		name string
		body string
	}{
		{"not an object", `[1,2]`},
		{"array body", `[]`},
		{"duplicate field", `{"kind":"verify","kind":"reset","to":"a@b.c"}`},
		{"unknown field", `{"kind":"verify","to":"a@b.c","x":"y"}`},
		{"trailing content", valid + ` "junk"`},
		{"trailing object", valid + ` {}`},
		{"missing kind", `{"to":"a@b.c"}`},
		{"missing to", `{"kind":"verify"}`},
		{"null kind", `{"kind":null,"to":"a@b.c"}`},
		{"numeric to", `{"kind":"verify","to":5}`},
		{"object to", `{"kind":"verify","to":{}}`},
		{"bogus kind", `{"kind":"spam","to":"a@b.c"}`},
		{"empty to", `{"kind":"verify","to":""}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := authorizedRequest(t, s, http.MethodPost, "/capture", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, r)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("code = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestMailCaptureServerCaptureRejectsOversize(t *testing.T) {
	s := testServer(t)
	// A valid JSON body whose text_body alone exceeds 16 KiB.
	big := strings.Repeat("x", MaxMessageBytes)
	body := `{"kind":"verify","to":"a@b.c","text_body":"` + big + `"}`
	r := authorizedRequest(t, s, http.MethodPost, "/capture", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("code = %d, want 413", rec.Code)
	}
	if s.store.Count() != 0 {
		t.Fatalf("count = %d, want 0", s.store.Count())
	}
}

func TestMailCaptureServerViewerEscapesHTML(t *testing.T) {
	s := testServer(t)
	m := sampleMessage()
	m.To = `<script>alert("to")</script>`
	m.Subject = `<b>bold</b>`
	m.TextBody = `<script>alert("body")</script>`
	m.HTMLBody = `<img src=x onerror=alert(1)>`
	body, _ := json.Marshal(m)

	r := authorizedRequest(t, s, http.MethodPost, "/capture", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("capture = %d, want 202", rec.Code)
	}

	r = authorizedRequest(t, s, http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer = %d, want 200", rec.Code)
	}
	page := rec.Body.String()
	if !strings.Contains(page, "&lt;script&gt;") {
		t.Fatal("viewer does not escape script markup")
	}
	for _, raw := range []string{"<script>alert(\"to\")</script>", "<b>bold</b>", "<img src=x onerror=alert(1)>"} {
		if strings.Contains(page, raw) {
			t.Errorf("viewer leaks raw markup %q", raw)
		}
	}
}

func TestMailCaptureClientSendClassification(t *testing.T) {
	logBuf := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, nil))
	secret := testSecret()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/capture") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth != bearerPrefix+base64.RawURLEncoding.EncodeToString(secret) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/bad"):
			w.WriteHeader(http.StatusBadRequest)
		case strings.HasPrefix(r.URL.Path, "/big"):
			w.WriteHeader(http.StatusRequestEntityTooLarge)
		case strings.HasPrefix(r.URL.Path, "/server-error"):
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	t.Cleanup(srv.Close)

	small := authmail.Message{Kind: authmail.KindVerify, To: "a", Subject: "s", TextBody: "t", HTMLBody: "h"}

	cases := []struct {
		name    string
		baseURL string
		outcome authmail.SendOutcome
	}{
		{"accepted", srv.URL, authmail.SendAccepted},
		{"permanent 400", srv.URL + "/bad", authmail.SendPermanentFailure},
		{"permanent 413", srv.URL + "/big", authmail.SendPermanentFailure},
		{"temporary 500", srv.URL + "/server-error", authmail.SendTemporaryFailure},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewClient(tc.baseURL, secret, logger)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			res, err := c.Send(context.Background(), small)
			if err != nil {
				t.Fatalf("Send: %v", err)
			}
			if res.Outcome != tc.outcome {
				t.Fatalf("outcome = %v, want %v", res.Outcome, tc.outcome)
			}
		})
	}

	// Oversized message is rejected locally as permanent without a request.
	c, err := NewClient(srv.URL, secret, logger)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if res, err := c.Send(context.Background(), authmail.Message{
		Kind: authmail.KindVerify, To: "a", Subject: "b",
		TextBody: strings.Repeat("x", MaxMessageBytes+1),
	}); err != nil {
		t.Fatalf("oversize Send: %v", err)
	} else if res.Outcome != authmail.SendPermanentFailure {
		t.Fatalf("oversize outcome = %v, want permanent", res.Outcome)
	}

	// The secret must never appear in any log line.
	if strings.Contains(logBuf.String(), base64.RawURLEncoding.EncodeToString(secret)) {
		t.Fatalf("logs leak the bearer secret")
	}
}

func TestMailCaptureClientSendTransportFailureIsTemporary(t *testing.T) {
	// A closed listener port yields a transport error: ambiguous, so temporary.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	c, err := NewClient(url, testSecret(), testLogger(t))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	res, err := c.Send(context.Background(), sampleMessage())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Outcome != authmail.SendTemporaryFailure {
		t.Fatalf("outcome = %v, want temporary", res.Outcome)
	}
}

func TestMailCaptureServerLogsNeverIncludeSecret(t *testing.T) {
	buf := &syncBuffer{}
	s, err := NewServer(testSecret(), slog.New(slog.NewTextHandler(buf, nil)))
	if err != nil {
		t.Fatal(err)
	}
	// Force a 401 and a 403; neither path may log the secret.
	r := httptest.NewRequest(http.MethodGet, "/api/messages", nil)
	r.RemoteAddr = "203.0.113.9:12345"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	r2 := httptest.NewRequest(http.MethodGet, "/api/messages", nil)
	r2.RemoteAddr = "127.0.0.1:55555"
	r2.Header.Set("Authorization", bearerPrefix+"not-a-secret")
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, r2)
	if strings.Contains(buf.String(), base64.RawURLEncoding.EncodeToString(s.secret[:])) {
		t.Fatalf("logs leak the bearer secret")
	}
}

// syncBuffer is a concurrency-safe bytes.Buffer used as a log sink.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
