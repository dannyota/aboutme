package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/directrender"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

func TestPublicRenderConfigRejectsSwappedOriginsAndUnsafeDigests(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name, environment, publicOrigin, renderOrigin, appDigest, rendererDigest string
	}{
		{"development config value", "dev", "https://aboutme.example", "http://127.0.0.1:20030", "sha256:app", "sha256:renderer"},
		{"development parser value", "development", "https://aboutme.example", "http://127.0.0.1:20030", "sha256:app", "sha256:renderer"},
		{"production config value", "prod", "https://aboutme.example", "http://127.0.0.1:3000", "sha256:app", "sha256:renderer"},
		{"staging", "staging", "https://aboutme.example", "http://127.0.0.1:3000", "sha256:app", "sha256:renderer"},
		{"swapped", "dev", "http://127.0.0.1:20030", "https://aboutme.example", "sha256:app", "sha256:renderer"},
		{"blank app digest", "dev", "https://aboutme.example", "http://127.0.0.1:20030", "", "sha256:renderer"},
		{"control renderer digest", "dev", "https://aboutme.example", "http://127.0.0.1:20030", "sha256:app", "sha256:renderer\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parsePublicRuntime(test.publicOrigin, test.renderOrigin, test.environment, test.appDigest, test.rendererDigest)
			if test.name == "development config value" || test.name == "development parser value" || test.name == "production config value" || test.name == "staging" {
				if err != nil {
					t.Fatalf("parsePublicRuntime() error = %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("parsePublicRuntime() error = nil, want rejection")
			}
		})
	}
}

func TestReadinessRenderRequestPostsValidMinimalPublicResume(t *testing.T) {
	origin, err := publicresume.ParsePublicOrigin("https://aboutme.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	renderOrigin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	var body []byte
	renderer := directrender.New(renderOrigin, &http.Client{Transport: readinessRoundTrip(func(request *http.Request) (*http.Response, error) {
		var readErr error
		body, readErr = io.ReadAll(request.Body)
		if readErr != nil {
			return nil, readErr
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(bytes.NewBufferString("<!doctype html>"))}, nil
	})})
	if err := renderer.Probe(context.Background(), readinessRenderRequest(origin)); err != nil {
		t.Fatalf("Probe() error = %v", err)
	}

	var envelope struct {
		PublicResume publicresume.PublicResume `json:"publicResume"`
		Mode         string                    `json:"mode"`
		Origin       string                    `json:"canonicalOrigin"`
		Discovery    bool                      `json:"discoveryEnabled"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Mode != directrender.PublicRenderMode || envelope.Origin != "https://aboutme.example" || envelope.Discovery {
		t.Fatalf("render envelope = %#v", envelope)
	}
	resume := envelope.PublicResume
	if resume.Slug != "readiness-probe" || resume.Revision != "1" || resume.Lng != "en" || resume.DownloadEnabled || resume.Document.SchemaVersion != schema.CurrentVersion || resume.Document.PersonalDetails.FullName != "Readiness Probe" {
		t.Fatalf("PublicResume contract fields = %#v", resume)
	}
	var contentEnvelope struct {
		PublicResume struct {
			Document struct {
				Content map[string]struct {
					SectionType string `json:"sectionType"`
					Entries     []struct {
						ID string `json:"id"`
					} `json:"entries"`
				} `json:"content"`
			} `json:"document"`
		} `json:"publicResume"`
	}
	if err := json.Unmarshal(body, &contentEnvelope); err != nil {
		t.Fatal(err)
	}
	profile, ok := contentEnvelope.PublicResume.Document.Content["profile"]
	if !ok || profile.SectionType != string(schema.Profile) || len(profile.Entries) != 1 || profile.Entries[0].ID != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("PublicContent = %#v", contentEnvelope.PublicResume.Document.Content)
	}
	customization := resume.Document.Customization
	if customization.Font.Family != schema.Inter || customization.Font.BaseSizePx != 14 || customization.Colors.Primary != "#1a1a1a" || customization.Colors.Text != "#1a1a1a" || customization.Colors.Background != "#ffffff" || customization.Spacing.SectionGap != 16 || customization.Spacing.EntryGap != 8 || customization.Spacing.LineHeight != 1.4 || customization.Heading.Style != schema.Normal || customization.Heading.ShowRule || customization.Layout.Columns != 1 || len(customization.Layout.Sections.Main) != 1 || customization.Layout.Sections.Main[0] != "profile" || len(customization.Layout.Sections.Sidebar) != 0 || customization.SectionDisplay.Skill.Style != schema.Text || customization.SectionDisplay.Language.Style != schema.Text || customization.PageFormat != schema.A4 || customization.DateFormat != schema.MmYyyy {
		t.Fatalf("PublicCustomization does not satisfy the closed minimal contract: %#v", customization)
	}
}

type readinessRoundTrip func(*http.Request) (*http.Response, error)

func (f readinessRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// TestServe_DrainsInFlightRequestBeforeReturning is a regression test for
// graceful shutdown: canceling the context must not cut off a request
// that's already being handled. serve must wait for it to finish (up to
// shutdownTimeout) before returning, exactly like a SIGTERM during a real
// deploy is supposed to drain in-flight work rather than abort it.
func TestServe_DrainsInFlightRequestBeforeReturning(t *testing.T) {
	t.Parallel()

	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	var completed atomic.Bool

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release // held open until the test explicitly releases it
		completed.Store(true)
		w.WriteHeader(http.StatusOK)
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- serve(ctx, logger, ln, handler)
	}()

	reqDone := make(chan error, 1)
	go func() {
		// Deliberately context.Background(), not ctx: this is the client's
		// request context, independent of the server's shutdown-trigger
		// context above. Tying it to ctx would cancel the in-flight request
		// the moment cancel() fires below, defeating the point of this test.
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+ln.Addr().String()+"/", nil)
		if err != nil {
			reqDone <- err
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			err = resp.Body.Close()
		}
		reqDone <- err
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never started; request did not reach the server")
	}

	// Trigger shutdown while the handler is blocked mid-request.
	cancel()

	// serve() must not return while the in-flight request is still being
	// held open — give shutdown a moment to (incorrectly) race ahead before
	// we release the handler, so a bug that aborts in-flight work would
	// show up as serveDone firing before we release() below.
	select {
	case <-serveDone:
		t.Fatal("serve() returned before the in-flight request completed — shutdown did not drain")
	case <-time.After(100 * time.Millisecond):
	}
	if completed.Load() {
		t.Fatal("handler completed before being released — test setup is broken")
	}

	close(release)

	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("serve() error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve() did not return after the in-flight request completed")
	}

	if !completed.Load() {
		t.Error("in-flight request never completed")
	}
	if err := <-reqDone; err != nil {
		t.Errorf("client request error: %v", err)
	}
}
