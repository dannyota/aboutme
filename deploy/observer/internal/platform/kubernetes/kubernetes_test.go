package kubernetes

import (
	"context"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dannyota/aboutme/deploy/observer/internal/platform"
)

// newTLSServer starts an httptest TLS server that serves body for any GET
// request to the pods path, and returns a Config with Host, Port, TokenFile,
// and CAFile pointed at it, as a real in-cluster client would be configured.
func newTLSServer(t *testing.T, body []byte) *Lister {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/pods") {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer canary-token" {
			t.Fatalf("Authorization header = %q, want Bearer canary-token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(body); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.crt")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(caFile, pemBytes, 0o600); err != nil {
		t.Fatalf("write CA file: %v", err)
	}
	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte("canary-token\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("split server host/port: %v", err)
	}

	l, err := New(Config{
		Namespace: "aboutme",
		Host:      host,
		Port:      port,
		TokenFile: tokenFile,
		CAFile:    caFile,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return l
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "kubernetes", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// TestRunningMapsCountsAndReplicas covers the component mapping, an ignored
// container, a non-running container, and replicas counted with the
// earliest start (docs/design/deployment-transparency/document.md,
// "Component mapping").
func TestRunningMapsCountsAndReplicas(t *testing.T) {
	l := newTLSServer(t, fixture(t, "pod_list.json"))

	got, err := l.Running(context.Background())
	if err != nil {
		t.Fatalf("Running: %v", err)
	}

	want := []platform.Image{
		{
			Component:    platform.Caddy,
			Digest:       "sha256:f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7",
			Replicas:     1,
			RunningSince: time.Date(2026, 9, 20, 9, 59, 0, 0, time.UTC),
		},
		{
			Component:    platform.Maintenance,
			Digest:       "sha256:f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7",
			Replicas:     1,
			RunningSince: time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC),
		},
		{
			Component:    platform.Server,
			Digest:       "sha256:a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5",
			Replicas:     2,
			RunningSince: time.Date(2026, 9, 20, 9, 55, 0, 0, time.UTC),
		},
		{
			Component:    platform.Web,
			Digest:       "sha256:e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6",
			Replicas:     1,
			RunningSince: time.Date(2026, 9, 20, 10, 5, 0, 0, time.UTC),
		},
	}
	if len(got) != len(want) {
		t.Fatalf("Running: got %d images, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("image %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestRunningFailsOnInvalidImageID asserts a running container whose imageID
// has no valid "@sha256:<hex>" suffix fails the whole run
// (docs/design/deployment-transparency/README.md, "Kubernetes").
func TestRunningFailsOnInvalidImageID(t *testing.T) {
	l := newTLSServer(t, fixture(t, "pod_list_invalid_digest.json"))

	if _, err := l.Running(context.Background()); err == nil {
		t.Fatal("Running: got nil error for a running container with no valid image digest")
	}
}

// TestDigestFromImageID is the pure form of the imageID digest extraction
// rule.
func TestDigestFromImageID(t *testing.T) {
	valid := "sha256:a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5"
	cases := []struct {
		imageID string
		want    string
		wantOK  bool
	}{
		{"docker-pullable://ghcr.io/dannyota/aboutme-server@" + valid, valid, true},
		{"containerd://ghcr.io/dannyota/aboutme-server@" + valid, valid, true},
		{"containerd://" + valid, "", false},
		{"", "", false},
		{"docker-pullable://ghcr.io/dannyota/aboutme-server@sha256:not-hex", "", false},
	}
	for _, c := range cases {
		got, ok := digestFromImageID(c.imageID)
		if got != c.want || ok != c.wantOK {
			t.Errorf("digestFromImageID(%q) = (%q, %v), want (%q, %v)", c.imageID, got, ok, c.want, c.wantOK)
		}
	}
}
