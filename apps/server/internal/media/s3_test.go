package media_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/media"
)

// Sentinel credential values: unique strings that must never surface in any
// returned error or captured log output, whatever the remote service does.
const (
	sentinelAccessKeyID = "SENTINEL-AKID-9f3a7c11d2e5"
	// Split the fake value so secret scanners do not mistake this leak-test
	// fixture for an assigned credential.
	sentinelSecret = "SENTINEL-" + "SECRET-4b8d0e6a91c7"
)

func stubConfig(endpoint string) media.S3Config {
	return media.S3Config{
		Bucket:          "stub-bucket",
		Region:          "us-east-1",
		Endpoint:        endpoint,
		AccessKeyID:     sentinelAccessKeyID,
		SecretAccessKey: sentinelSecret,
		ForcePathStyle:  true,
	}
}

func newStubBackend(t *testing.T, handler http.Handler) (media.Backend, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	b, err := media.NewS3(context.Background(), stubConfig(server.URL))
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}
	return b, server
}

// xmlError renders an S3-style error body. Code and message deliberately
// carry attacker-controlled text in the leakage tests.
func xmlError(code, message string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><Error><Code>` + code + `</Code><Message>` + message + `</Message></Error>`
}

func TestNewS3_ConfigValidation(t *testing.T) {
	t.Parallel()
	valid := stubConfig("http://127.0.0.1:20091")

	cases := []struct {
		name    string
		mutate  func(*media.S3Config)
		wantErr bool
	}{
		{"valid custom endpoint", func(c *media.S3Config) {}, false},
		{"valid aws mode", func(c *media.S3Config) {
			c.Endpoint, c.AccessKeyID, c.SecretAccessKey, c.ForcePathStyle = "", "", "", false
		}, false},
		{"missing bucket", func(c *media.S3Config) { c.Bucket = "" }, true},
		{"missing region", func(c *media.S3Config) { c.Region = "" }, true},
		{"endpoint without path style", func(c *media.S3Config) { c.ForcePathStyle = false }, true},
		{"endpoint missing access key", func(c *media.S3Config) { c.AccessKeyID = "" }, true},
		{"endpoint missing secret", func(c *media.S3Config) { c.SecretAccessKey = "" }, true},
		{"relative endpoint", func(c *media.S3Config) { c.Endpoint = "127.0.0.1:20091" }, true},
		{"non-http scheme", func(c *media.S3Config) { c.Endpoint = "ftp://127.0.0.1:20091" }, true},
		{"endpoint with userinfo", func(c *media.S3Config) { c.Endpoint = "http://user:pass@127.0.0.1:20091" }, true},
		{"endpoint without host", func(c *media.S3Config) { c.Endpoint = "http://" }, true},
		{"aws mode with path style", func(c *media.S3Config) {
			c.Endpoint, c.AccessKeyID, c.SecretAccessKey = "", "", ""
		}, true},
		{"aws mode with static keys", func(c *media.S3Config) { c.Endpoint, c.ForcePathStyle = "", false }, true},
		{"aws mode with partial pair", func(c *media.S3Config) {
			c.Endpoint, c.ForcePathStyle, c.SecretAccessKey = "", false, ""
		}, true},
		{"custom endpoint partial pair", func(c *media.S3Config) { c.AccessKeyID = "" }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := valid
			tc.mutate(&cfg)
			_, err := media.NewS3(context.Background(), cfg)
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Errorf("NewS3 err = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil && (strings.Contains(err.Error(), sentinelAccessKeyID) || strings.Contains(err.Error(), sentinelSecret)) {
				t.Errorf("NewS3 error leaks a credential: %v", err)
			}
		})
	}
}

// TestS3_PutOutcomeClassification drives every fault-injection class the
// task file names through a stub service: a received rejection that proves
// the conditional create did not occur is PutNotCreated (412 additionally
// maps to ErrAlreadyExists); any dispatched request without a conclusive
// service result — 5xx or a lost response — is PutUnknown.
func TestS3_PutOutcomeClassification(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		status      int
		body        string
		wantOutcome media.PutOutcome
		wantExists  bool
	}{
		{"collision 412", 412, xmlError("PreconditionFailed", "exists"), media.PutNotCreated, true},
		{"conditional conflict 409", 409, xmlError("ConditionalRequestConflict", "racing"), media.PutNotCreated, false},
		{"definite rejection 400", 400, xmlError("BadDigest", "bad"), media.PutNotCreated, false},
		{"access denied 403", 403, xmlError("AccessDenied", "denied"), media.PutNotCreated, false},
		{"no such bucket 404", 404, xmlError("NoSuchBucket", "missing"), media.PutNotCreated, false},
		{"server error 500", 500, xmlError("InternalError", "oops"), media.PutUnknown, false},
		{"slow down 503", 503, xmlError("SlowDown", "later"), media.PutUnknown, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b, _ := newStubBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				copyExternalTestBody(t, io.Discard, r.Body)
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(tc.status)
				writeExternalTestBody(t, w, tc.body)
			}))
			outcome, err := b.Put(context.Background(), "resumes/x/put.jpg", "image/jpeg", bytes.NewReader([]byte("x")), 1)
			checkPutPair(t, outcome, err)
			if outcome != tc.wantOutcome {
				t.Errorf("outcome = %d, err = %v, want %d", outcome, err, tc.wantOutcome)
			}
			if errors.Is(err, media.ErrAlreadyExists) != tc.wantExists {
				t.Errorf("errors.Is(err, ErrAlreadyExists) = %v, want %v (err %v)", !tc.wantExists, tc.wantExists, err)
			}
		})
	}
}

// TestS3_PutLostResponseIsUnknown: the service accepted (or may have
// accepted) the conditional write but the response never arrived. The
// outcome must be PutUnknown so the request path never deletes a key that
// could belong to a collision winner.
func TestS3_PutLostResponseIsUnknown(t *testing.T) {
	t.Parallel()
	b, _ := newStubBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the complete body — the write may have been applied — then
		// drop the connection without any response.
		copyExternalTestBody(t, io.Discard, r.Body)
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			panic("test response writer does not support hijacking")
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			panic(err)
		}
		closeExternalTestBody(t, conn)
	}))
	outcome, err := b.Put(context.Background(), "resumes/x/lost.jpg", "image/jpeg", bytes.NewReader([]byte("x")), 1)
	checkPutPair(t, outcome, err)
	if outcome != media.PutUnknown {
		t.Errorf("outcome = %d, err = %v, want PutUnknown", outcome, err)
	}
}

// TestS3_PutLostCollisionResponsePreservesWinner models a conditional write
// that collided with an existing object, but whose 412 response was lost. The
// client cannot prove the collision, so it must report PutUnknown and must not
// issue cleanup that could delete the winner.
func TestS3_PutLostCollisionResponsePreservesWinner(t *testing.T) {
	t.Parallel()
	winner := []byte("winner bytes")
	const winnerContentType = "image/png"
	var deletes atomic.Int64
	seenBody := make(chan []byte, 1)
	b, _ := newStubBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			if got := r.Header.Get("If-None-Match"); got != "*" {
				t.Errorf("If-None-Match = %q, want *", got)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read contender body: %v", err)
			}
			seenBody <- body

			// The winner already owns the key. Model a collision without
			// changing its bytes, then lose the 412 before it reaches the client.
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				panic("test response writer does not support hijacking")
			}
			conn, _, err := hijacker.Hijack()
			if err != nil {
				panic(err)
			}
			closeExternalTestBody(t, conn)
		case http.MethodGet:
			w.Header().Set("Content-Type", winnerContentType)
			w.Header().Set("Content-Length", fmt.Sprint(len(winner)))
			w.WriteHeader(http.StatusOK)
			if _, writeErr := w.Write(winner); writeErr != nil {
				t.Errorf("write winner response: %v", writeErr)
			}
		case http.MethodDelete:
			deletes.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))

	contender := []byte("contender bytes")
	outcome, err := b.Put(
		context.Background(),
		"resumes/x/lost-collision.jpg",
		"image/jpeg",
		bytes.NewReader(contender),
		int64(len(contender)),
	)
	checkPutPair(t, outcome, err)
	if outcome != media.PutUnknown {
		t.Errorf("outcome = %d, err = %v, want PutUnknown", outcome, err)
	}
	select {
	case got := <-seenBody:
		if !bytes.Equal(got, contender) {
			t.Errorf("service received %q, want contender %q", got, contender)
		}
	default:
		t.Error("conditional contender never reached the service")
	}
	if got := deletes.Load(); got != 0 {
		t.Errorf("cleanup DELETE requests = %d, want 0", got)
	}
	body, contentType, getErr := b.Get(context.Background(), "resumes/x/lost-collision.jpg")
	if getErr != nil {
		t.Fatalf("Get(winner) error: %v", getErr)
	}
	gotWinner, readErr := io.ReadAll(body)
	closeErr := body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read/close winner: %v / %v", readErr, closeErr)
	}
	if !bytes.Equal(gotWinner, winner) || contentType != winnerContentType {
		t.Errorf("winner = (%q, %q), want unchanged (%q, %q)", gotWinner, contentType, winner, winnerContentType)
	}
}

func TestS3_PutCancellationClassification(t *testing.T) {
	t.Parallel()
	var requests atomic.Int64
	release := make(chan struct{})
	b, _ := newStubBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		// Stall until the client gives up. The explicit release is a bounded
		// fallback for HTTP/1 transports that do not promptly cancel the
		// server-side request context after the client returns.
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() { close(release) })

	// Canceled before dispatch: proved non-create, and no request is sent.
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	outcome, err := b.Put(canceled, "resumes/x/pre.jpg", "image/jpeg", bytes.NewReader([]byte("x")), 1)
	checkPutPair(t, outcome, err)
	if outcome != media.PutNotCreated || !errors.Is(err, context.Canceled) {
		t.Errorf("pre-dispatch outcome = %d, err = %v, want PutNotCreated + context.Canceled", outcome, err)
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("pre-dispatch cancellation sent %d requests, want 0", n)
	}

	// Canceled after dispatch: the service may have accepted the write, so
	// the outcome is unknown.
	ctx, cancel2 := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel2()
	outcome, err = b.Put(ctx, "resumes/x/mid.jpg", "image/jpeg", bytes.NewReader([]byte("x")), 1)
	checkPutPair(t, outcome, err)
	if outcome != media.PutUnknown {
		t.Errorf("mid-request outcome = %d, err = %v, want PutUnknown", outcome, err)
	}
	if n := requests.Load(); n == 0 {
		t.Errorf("mid-request cancellation sent no request; the test proved nothing")
	}
}

func TestS3_GetClassification(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		status       int
		body         string
		wantNotFound bool
	}{
		{"no such key", 404, xmlError("NoSuchKey", "absent"), true},
		{"bare 404", 404, "", true},
		{"denied is not absence", 403, xmlError("AccessDenied", "no"), false},
		{"server error", 500, xmlError("InternalError", "oops"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b, _ := newStubBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(tc.status)
				writeExternalTestBody(t, w, tc.body)
			}))
			_, _, err := b.Get(context.Background(), "resumes/x/get.jpg")
			if err == nil {
				t.Fatalf("Get err = nil, want error")
			}
			if errors.Is(err, media.ErrNotFound) != tc.wantNotFound {
				t.Errorf("errors.Is(ErrNotFound) = %v, want %v (err %v)", !tc.wantNotFound, tc.wantNotFound, err)
			}
		})
	}
}

func TestS3_DeleteClassification(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{"deleted", 204, "", nil},
		{"already absent", 404, xmlError("NoSuchKey", "gone"), media.ErrNotFound},
		{"server error", 500, xmlError("InternalError", "oops"), errors.New("remote failure")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b, _ := newStubBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(tc.status)
				writeExternalTestBody(t, w, tc.body)
			}))
			err := b.Delete(context.Background(), "resumes/x/del.jpg")
			if tc.wantErr == nil && err != nil {
				t.Errorf("Delete err = %v, want nil", err)
			}
			if tc.wantErr != nil && err == nil {
				t.Errorf("Delete err = nil, want non-nil")
			}
			if errors.Is(tc.wantErr, media.ErrNotFound) && !errors.Is(err, media.ErrNotFound) {
				t.Errorf("Delete err = %v, want exactly ErrNotFound", err)
			}
		})
	}
}

// TestS3_ListPagePropagatesWindow asserts the exact listing window the
// backend requests from the service: the validated prefix, the cursor as
// start-after, and the limit as max-keys.
func TestS3_ListPagePropagatesWindow(t *testing.T) {
	t.Parallel()
	var query atomic.Value
	b, _ := newStubBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query.Store(r.URL.Query().Encode())
		w.Header().Set("Content-Type", "application/xml")
		writeExternalTestBody(t, w, `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult><Name>stub-bucket</Name><IsTruncated>true</IsTruncated>
<Contents><Key>resumes/a/k1.jpg</Key><LastModified>2026-08-01T00:00:00.000Z</LastModified></Contents>
<Contents><Key>resumes/a/k2.jpg</Key><LastModified>2026-08-02T00:00:00.000Z</LastModified></Contents>
</ListBucketResult>`)
	}))
	objects, next, err := b.ListPage(context.Background(), "resumes/a/", "resumes/a/k0.jpg", 2)
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	got, ok := query.Load().(string)
	if !ok {
		t.Fatal("request query was not recorded")
	}
	for _, fragment := range []string{"list-type=2", "prefix=resumes%2Fa%2F", "start-after=resumes%2Fa%2Fk0.jpg", "max-keys=2"} {
		if !strings.Contains(got, fragment) {
			t.Errorf("request query %q missing %q", got, fragment)
		}
	}
	if len(objects) != 2 || objects[0].Key != "resumes/a/k1.jpg" || objects[1].Key != "resumes/a/k2.jpg" {
		t.Errorf("objects = %v", objects)
	}
	if objects[0].UpdatedAt.IsZero() || objects[1].UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt not mapped: %v", objects)
	}
	if next != "resumes/a/k2.jpg" {
		t.Errorf("nextCursor = %q, want last key of the truncated page", next)
	}
}

func TestS3_ListPageRejectsContractViolations(t *testing.T) {
	t.Parallel()
	object := func(key, updated string) string {
		lastModified := ""
		if updated != "" {
			lastModified = "<LastModified>" + updated + "</LastModified>"
		}
		return "<Contents><Key>" + key + "</Key>" + lastModified + "</Contents>"
	}
	const timestamp = "2026-08-01T00:00:00.000Z"
	tests := []struct {
		name      string
		prefix    string
		cursor    string
		limit     int
		truncated bool
		contents  string
	}{
		{"neighbor key", "resumes/a/", "", 2, false, object("resumes/b/photo.jpg", timestamp)},
		{"noncanonical key", "resumes/a/", "", 2, false, object("resumes/a//photo.jpg", timestamp)},
		{"key does not advance cursor", "resumes/a/", "resumes/a/m.jpg", 2, false, object("resumes/a/a.jpg", timestamp)},
		{"keys out of order", "resumes/a/", "", 2, false, object("resumes/a/z.jpg", timestamp) + object("resumes/a/a.jpg", timestamp)},
		{"duplicate keys", "resumes/a/", "", 2, false, object("resumes/a/a.jpg", timestamp) + object("resumes/a/a.jpg", timestamp)},
		{"more than limit", "resumes/a/", "", 1, false, object("resumes/a/a.jpg", timestamp) + object("resumes/a/b.jpg", timestamp)},
		{"missing update time", "resumes/a/", "", 2, false, object("resumes/a/a.jpg", "")},
		{"truncated empty page", "resumes/a/", "", 2, true, ""},
		{"truncated short page", "resumes/a/", "", 2, true, object("resumes/a/a.jpg", timestamp)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b, _ := newStubBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				if _, writeErr := fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult><Name>stub-bucket</Name><IsTruncated>%t</IsTruncated>%s</ListBucketResult>`, tt.truncated, tt.contents); writeErr != nil {
					t.Errorf("write list response: %v", writeErr)
				}
			}))
			objects, next, err := b.ListPage(context.Background(), tt.prefix, tt.cursor, tt.limit)
			if err == nil {
				t.Fatalf("ListPage = %v, %q, nil; want fail-closed error", objects, next)
			}
			if objects != nil || next != "" {
				t.Errorf("ListPage on contract violation returned partial page %v, %q", objects, next)
			}
		})
	}
}

// TestS3_SecretsNeverLeak injects the sentinel credentials into the backend
// configuration AND into a hostile failing service response that echoes
// both sentinels back in its error code and message. Neither sentinel may
// appear in any returned error or in captured log output, for any
// operation, including a transport-level failure.
func TestS3_SecretsNeverLeak(t *testing.T) {
	// Not parallel: it captures the process-global log output.
	var logBuf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(prev)

	var deleteRequests atomic.Int64
	hostile := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodDelete {
			deleteRequests.Add(1)
		}
		copyExternalTestBody(t, io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(500)
		// Echo the credentials the request was signed with, plus the raw
		// sentinels, into every field a lazy error path might surface.
		writeExternalTestBody(t, w, xmlError(
			"Denied-"+sentinelAccessKeyID,
			"auth="+r.Header.Get("Authorization")+" secret="+sentinelSecret,
		))
	})
	b, _ := newStubBackend(t, hostile)

	var errs []error
	outcome, err := b.Put(context.Background(), "resumes/x/leak.jpg", "image/jpeg", bytes.NewReader([]byte("x")), 1)
	checkPutPair(t, outcome, err)
	errs = append(errs, err)
	_, _, err = b.Get(context.Background(), "resumes/x/leak.jpg")
	errs = append(errs, err)
	errs = append(errs, b.Delete(context.Background(), "resumes/x/leak.jpg"))
	if got := deleteRequests.Load(); got == 0 {
		t.Error("DeleteObject was not reached")
	}
	_, _, err = b.ListPage(context.Background(), "resumes/x", "", 10)
	errs = append(errs, err)

	// Transport-level failure (connection refused): grab a port that is
	// closed by the time the backend dials it.
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadEndpoint := "http://" + listener.Addr().String()
	closeExternalTestBody(t, listener)
	deadBackend, err := media.NewS3(context.Background(), stubConfig(deadEndpoint))
	if err != nil {
		t.Fatalf("NewS3(dead endpoint): %v", err)
	}
	outcome, err = deadBackend.Put(context.Background(), "resumes/x/dead.jpg", "image/jpeg", bytes.NewReader([]byte("x")), 1)
	checkPutPair(t, outcome, err)
	if outcome != media.PutUnknown {
		t.Errorf("Put(dead endpoint) outcome = %d, want PutUnknown", outcome)
	}
	errs = append(errs, err)

	for i, err := range errs {
		if err == nil {
			t.Errorf("operation %d returned nil error; the leakage assertion proved nothing", i)
			continue
		}
		text := fmt.Sprintf("%+v", err)
		if strings.Contains(text, sentinelAccessKeyID) || strings.Contains(text, sentinelSecret) {
			t.Errorf("operation %d error leaks a credential sentinel: %s", i, text)
		}
	}
	logText := logBuf.String()
	if strings.Contains(logText, sentinelAccessKeyID) || strings.Contains(logText, sentinelSecret) {
		t.Errorf("captured log output leaks a credential sentinel: %s", logText)
	}
}
