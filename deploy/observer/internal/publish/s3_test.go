package publish

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"

	"github.com/dannyota/aboutme/deploy/observer/internal/verify"
)

// fakeBucket is a minimal path-style S3 stand-in: enough of PutObject and
// GetObject to exercise Publisher and Cache against the real SDK client.
type fakeBucket struct {
	mu        sync.Mutex
	objects   map[string]put
	forbidden map[string]bool
}

type put struct {
	body         []byte
	contentType  string
	cacheControl string
}

func newFakeBucket(t *testing.T) (*httptest.Server, *fakeBucket) {
	t.Helper()
	fb := &fakeBucket{objects: map[string]put{}, forbidden: map[string]bool{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path // path-style: /<bucket>/<key>
		switch r.Method {
		case http.MethodPut:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read PUT body: %v", err)
			}
			fb.mu.Lock()
			fb.objects[key] = put{body: body, contentType: r.Header.Get("Content-Type"), cacheControl: r.Header.Get("Cache-Control")}
			fb.mu.Unlock()
			w.Header().Set("ETag", `"canary-etag"`)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			fb.mu.Lock()
			obj, ok := fb.objects[key]
			forbidden := fb.forbidden[key]
			fb.mu.Unlock()
			// The observer's role has no s3:ListBucket, so a real bucket
			// answers AccessDenied rather than NoSuchKey for a key it
			// cannot list (docs/design/deployment-transparency/README.md,
			// "Access on AWS").
			if forbidden {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusForbidden)
				if _, err := fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>AccessDenied</Code><Message>Access Denied</Message><RequestId>canary-request-id</RequestId></Error>`); err != nil {
					t.Fatalf("write AccessDenied body: %v", err)
				}
				return
			}
			if !ok {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusNotFound)
				if _, err := fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message><Key>%s</Key><RequestId>canary-request-id</RequestId></Error>`, key); err != nil {
					t.Fatalf("write NoSuchKey body: %v", err)
				}
				return
			}
			w.Header().Set("Content-Type", obj.contentType)
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write(obj.body); err != nil {
				t.Fatalf("write object body: %v", err)
			}
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, fb
}

func TestPublisherPut(t *testing.T) {
	srv, fb := newFakeBucket(t)
	cfg := aws.Config{
		Region:      "ap-southeast-1",
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
	}
	client := NewClient(cfg, srv.URL)
	pub := NewPublisher(client, "aboutme-prod-transparency")

	body := []byte(`{"schema_version":1}`)
	if err := pub.Put(context.Background(), body); err != nil {
		t.Fatalf("Put: %v", err)
	}

	fb.mu.Lock()
	obj, ok := fb.objects["/aboutme-prod-transparency/"+documentKey]
	fb.mu.Unlock()
	if !ok {
		t.Fatal("Put: object was not stored at the well-known path")
	}
	if string(obj.body) != string(body) {
		t.Errorf("Put: body = %q, want %q", obj.body, body)
	}
	if obj.contentType != "application/json" {
		t.Errorf("Put: Content-Type = %q, want application/json", obj.contentType)
	}
	if obj.cacheControl != "public, max-age=30" {
		t.Errorf("Put: Cache-Control = %q, want %q", obj.cacheControl, "public, max-age=30")
	}
}

// TestDocumentKeyMatchesTheServedPath asserts the document is written under
// the path CloudFront serves, since it appends the viewer's path to the S3
// origin rather than always fetching one fixed key
// (docs/design/deployment-transparency/README.md, "Serving and caching").
func TestDocumentKeyMatchesTheServedPath(t *testing.T) {
	if documentKey != ".well-known/deployment.json" {
		t.Errorf("documentKey = %q, want .well-known/deployment.json", documentKey)
	}
}

func TestCacheGetNotFound(t *testing.T) {
	srv, _ := newFakeBucket(t)
	cfg := aws.Config{
		Region:      "ap-southeast-1",
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
	}
	cache := NewCache(NewClient(cfg, srv.URL), "aboutme-prod-transparency")

	digest := "sha256:" + repeatHex("a1", 32)
	_, found, err := cache.Get(context.Background(), digest)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if found {
		t.Fatal("Get: found a digest that was never stored")
	}
}

// TestCacheGetAccessDenied asserts a missing key answered as AccessDenied
// (HTTP 403), as a bucket with no s3:ListBucket grant answers, is treated
// the same as NoSuchKey: found=false with no error
// (docs/design/deployment-transparency/README.md, "Access on AWS").
func TestCacheGetAccessDenied(t *testing.T) {
	srv, fb := newFakeBucket(t)
	cfg := aws.Config{
		Region:      "ap-southeast-1",
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
	}
	cache := NewCache(NewClient(cfg, srv.URL), "aboutme-prod-transparency")

	digest := "sha256:" + repeatHex("e5", 32)
	fb.mu.Lock()
	fb.forbidden["/aboutme-prod-transparency/"+verifiedKey(digest)] = true
	fb.mu.Unlock()

	_, found, err := cache.Get(context.Background(), digest)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if found {
		t.Fatal("Get: found a digest behind an AccessDenied response")
	}
}

func TestCacheGetInvalidDigest(t *testing.T) {
	srv, _ := newFakeBucket(t)
	cfg := aws.Config{
		Region:      "ap-southeast-1",
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
	}
	cache := NewCache(NewClient(cfg, srv.URL), "aboutme-prod-transparency")

	if _, _, err := cache.Get(context.Background(), "not-a-digest"); err == nil {
		t.Fatal("Get: got nil error for a malformed digest")
	}
}

func TestCachePutAndGetRoundTrip(t *testing.T) {
	srv, _ := newFakeBucket(t)
	cfg := aws.Config{
		Region:      "ap-southeast-1",
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
	}
	cache := NewCache(NewClient(cfg, srv.URL), "aboutme-prod-transparency")

	digest := "sha256:" + repeatHex("b2", 32)
	checked := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	want := verify.Evidence{
		Digest: digest,
		Provenance: verify.Provenance{
			Status:    verify.Verified,
			CheckedAt: checked,
			Subjects:  []string{"ghcr.io/dannyota/aboutme-server"},
			Version:   "v0.6.5",
			Commit:    repeatHex("c3", 20),
		},
	}
	if err := cache.Put(context.Background(), want); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, found, err := cache.Get(context.Background(), digest)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found {
		t.Fatal("Get: did not find a digest that was just stored")
	}
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("Get: got %s, want %s", gotJSON, wantJSON)
	}
}

func TestCachePutRefusesUnchecked(t *testing.T) {
	srv, fb := newFakeBucket(t)
	cfg := aws.Config{
		Region:      "ap-southeast-1",
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
	}
	cache := NewCache(NewClient(cfg, srv.URL), "aboutme-prod-transparency")

	digest := "sha256:" + repeatHex("d4", 32)
	err := cache.Put(context.Background(), verify.Evidence{
		Digest:     digest,
		Provenance: verify.Provenance{Status: verify.Unchecked},
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	fb.mu.Lock()
	_, stored := fb.objects["/aboutme-prod-transparency/"+verifiedKey(digest)]
	fb.mu.Unlock()
	if stored {
		t.Fatal("Put: an Unchecked result was stored")
	}
}

func TestCachePutRefusesMalformedDigest(t *testing.T) {
	srv, fb := newFakeBucket(t)
	cfg := aws.Config{
		Region:      "ap-southeast-1",
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
	}
	cache := NewCache(NewClient(cfg, srv.URL), "aboutme-prod-transparency")

	err := cache.Put(context.Background(), verify.Evidence{
		Digest:     "not-a-digest",
		Provenance: verify.Provenance{Status: verify.Verified},
	})
	if err == nil {
		t.Fatal("Put: got nil error for a malformed digest")
	}
	fb.mu.Lock()
	n := len(fb.objects)
	fb.mu.Unlock()
	if n != 0 {
		t.Fatalf("Put: stored %d objects for a refused write", n)
	}
}

func repeatHex(pair string, n int) string {
	out := make([]byte, 0, len(pair)*n)
	for i := 0; i < n; i++ {
		out = append(out, pair...)
	}
	return string(out)
}
