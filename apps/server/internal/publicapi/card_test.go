package publicapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/previewcard"
	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
)

const cardVersion = "0123456789abcdef"

var cardPNG = []byte("\x89PNG\r\n\x1a\ncurrent-card")

// fakeCards is a PreviewCards whose current version is fixed.
type fakeCards struct {
	mu          sync.Mutex
	version     string
	stored      *previewcard.StoredCard
	storedCalls int
	builds      []renderjob.Priority
	build       func(context.Context) ([]byte, error)
}

func (c *fakeCards) Version(publicresume.Snapshot) (string, error) { return c.version, nil }

func (c *fakeCards) Stored(context.Context, uuid.UUID) (previewcard.StoredCard, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.storedCalls++
	if c.stored == nil {
		return previewcard.StoredCard{}, false, nil
	}
	return *c.stored, true, nil
}

func (c *fakeCards) Build(ctx context.Context, _ publicresume.Snapshot, priority renderjob.Priority) ([]byte, error) {
	c.mu.Lock()
	c.builds = append(c.builds, priority)
	build := c.build
	c.mu.Unlock()
	if build == nil {
		return cardPNG, nil
	}
	return build(ctx)
}

func newCardHarness(t *testing.T, cards *fakeCards) (*artifactHandlers, *publicServiceStore, *publicstate.Coordinator) {
	t.Helper()
	_, reader, coordinator, backing := publicServiceReaderWithStore(t)
	cache, err := publiccache.New(4, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	queue := artifactQueueFunc(func(context.Context, renderjob.Request) (renderjob.Result, error) {
		t.Fatal("the share-image render queue ran while stored cards are on")
		return renderjob.Result{}, nil
	})
	handlers, err := newArtifactHandlers(ArtifactDependencies{
		Reader: reader, Cache: cache, Queue: queue, AppDigest: "sha256:app", RendererDigest: "sha256:renderer",
		Clock: time.Now, Cards: cards,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handlers, backing, coordinator
}

func cardRequest(t *testing.T, method, path string) *http.Request {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), method, path, nil)
	request.RemoteAddr = "192.0.2.40:1234"
	return request
}

func serveCard(t *testing.T, handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestCardPathAcceptsOnlyAValidSlugAndVersion(t *testing.T) {
	t.Parallel()
	for path, want := range map[string]bool{
		"/api/v1/public/resumes/ada-lovelace/og/0123456789abcdef.png":    true,
		"/api/v1/public/resumes/ada-lovelace/og/0123456789ABCDEF.png":    false,
		"/api/v1/public/resumes/ada-lovelace/og/0123456789abcde.png":     false,
		"/api/v1/public/resumes/ada-lovelace/og/0123456789abcdef0.png":   false,
		"/api/v1/public/resumes/ada-lovelace/og/0123456789abcdef.jpg":    false,
		"/api/v1/public/resumes/ada-lovelace/og/0123456789abcdef":        false,
		"/api/v1/public/resumes/ada/og/0123456789abcdef.png":             false,
		"/api/v1/public/resumes/ada-lovelace/x/og/0123456789abcdef.png":  false,
		"/api/v1/public/resumes/ada-lovelace/og/../0123456789abcdef.png": false,
	} {
		if _, _, got := cardPathParts(path); got != want {
			t.Errorf("cardPathParts(%q) = %t, want %t", path, got, want)
		}
		if got := (&Service{}).Recognizes(path); got != want {
			t.Errorf("Recognizes(%q) = %t, want %t", path, got, want)
		}
	}
}

// With stored cards off, the versioned route is the public 404 and og.png
// keeps the share image, so the server can deploy before the web renderer.
func TestCardRouteIsOffWithoutCards(t *testing.T) {
	t.Parallel()
	handlers, _, _ := newArtifactHarness(t, artifactQueueFunc(func(context.Context, renderjob.Request) (renderjob.Result, error) {
		t.Fatal("render queue ran for the versioned route")
		return renderjob.Result{}, nil
	}), 2, time.Now)
	response := serveCard(t, handlers.card, cardRequest(t, http.MethodGet, "/api/v1/public/resumes/ada-lovelace/og/"+cardVersion+".png"))
	if response.Code != http.StatusNotFound {
		t.Fatalf("versioned route without cards = %d, want 404", response.Code)
	}
}

func TestStoredCurrentCardIsServedAfterTheLiveGate(t *testing.T) {
	t.Parallel()
	cards := &fakeCards{version: cardVersion, stored: &previewcard.StoredCard{Version: cardVersion, PNG: cardPNG}}
	handlers, backing, _ := newCardHarness(t, cards)
	backing.row.SEOGeoEnabled = false
	digest := sha256.Sum256(cardPNG)
	etag := `"` + hex.EncodeToString(digest[:]) + `"`
	for name, handler := range map[string]http.Handler{
		"/og/" + cardVersion + ".png": handlers.card,
		"/og.png":                     handlers.png,
	} {
		path := "/api/v1/public/resumes/ada-lovelace" + name
		get := serveCard(t, handler, cardRequest(t, http.MethodGet, path))
		if get.Code != http.StatusOK || get.Body.String() != string(cardPNG) ||
			get.Header().Get("Content-Type") != "image/png" || get.Header().Get("Cache-Control") != "no-cache, must-revalidate" ||
			get.Header().Get("ETag") != etag || get.Header().Get("X-Robots-Tag") != "noindex, noarchive" {
			t.Fatalf("GET %s = %d %v", name, get.Code, get.Header())
		}
		head := serveCard(t, handler, cardRequest(t, http.MethodHead, path))
		if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") != strconv.Itoa(len(cardPNG)) {
			t.Fatalf("HEAD %s = %d %v", name, head.Code, head.Header())
		}
		conditional := cardRequest(t, http.MethodGet, path)
		conditional.Header.Set("If-None-Match", etag)
		if response := serveCard(t, handler, conditional); response.Code != http.StatusNotModified || response.Body.Len() != 0 {
			t.Fatalf("conditional %s = %d", name, response.Code)
		}
	}
	if len(cards.builds) != 0 {
		t.Fatalf("builds = %d, want none for a stored current card", len(cards.builds))
	}
}

// Only the current version answers; an old version and an unknown one are
// the ordinary public 404, and a stored row of an old version is never
// served.
func TestOnlyTheCurrentVersionAnswers(t *testing.T) {
	t.Parallel()
	cards := &fakeCards{version: cardVersion, stored: &previewcard.StoredCard{Version: "fedcba9876543210", PNG: []byte("\x89PNGstale")}}
	handlers, _, _ := newCardHarness(t, cards)
	old := serveCard(t, handlers.card, cardRequest(t, http.MethodGet, "/api/v1/public/resumes/ada-lovelace/og/fedcba9876543210.png"))
	if old.Code != http.StatusNotFound || strings.Contains(old.Body.String(), "stale") ||
		old.Body.String() != "{\"error\":{\"code\":\"public_not_found\",\"message\":\"public resume not found\"}}\n" {
		t.Fatalf("old version = %d %q, want the public 404", old.Code, old.Body.String())
	}
	if cards.storedCalls != 0 {
		t.Fatalf("stored reads for an old version = %d, want 0", cards.storedCalls)
	}
	current := serveCard(t, handlers.card, cardRequest(t, http.MethodGet, "/api/v1/public/resumes/ada-lovelace/og/"+cardVersion+".png"))
	if current.Code != http.StatusOK || current.Body.String() != string(cardPNG) || len(cards.builds) != 1 || cards.builds[0] != renderjob.PriorityNormal {
		t.Fatalf("current version over a stale row = %d %q builds=%v", current.Code, current.Body.String(), cards.builds)
	}
}

func TestAliasNeverServesAStoredRowOfAnOlderVersion(t *testing.T) {
	t.Parallel()
	cards := &fakeCards{version: cardVersion, stored: &previewcard.StoredCard{Version: "fedcba9876543210", PNG: []byte("\x89PNGstale")}}
	handlers, _, _ := newCardHarness(t, cards)
	response := serveCard(t, handlers.png, cardRequest(t, http.MethodGet, "/api/v1/public/resumes/ada-lovelace/og.png"))
	if response.Code != http.StatusOK || response.Body.String() != string(cardPNG) || len(cards.builds) != 1 {
		t.Fatalf("alias over a stale row = %d %q builds=%v, want the current card built", response.Code, response.Body.String(), cards.builds)
	}
}

func TestCardOfAResumeThatIsNotLiveIsNotRead(t *testing.T) {
	t.Parallel()
	cards := &fakeCards{version: cardVersion, stored: &previewcard.StoredCard{Version: cardVersion, PNG: cardPNG}}
	handlers, backing, _ := newCardHarness(t, cards)
	backing.row.Live = false
	for _, handler := range []http.Handler{handlers.card, handlers.png} {
		for _, path := range []string{"/api/v1/public/resumes/ada-lovelace/og/" + cardVersion + ".png", "/api/v1/public/resumes/ada-lovelace/og.png"} {
			if response := serveCard(t, handler, cardRequest(t, http.MethodGet, path)); response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "PNG") {
				t.Fatalf("card of a resume that is not live = %d %q", response.Code, response.Body.String())
			}
		}
	}
	if cards.storedCalls != 0 || len(cards.builds) != 0 {
		t.Fatalf("stored reads = %d, builds = %d, want none before the gate admits", cards.storedCalls, len(cards.builds))
	}
}

func TestMissingCardIsBuiltOnceAndServed(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		build    func(context.Context) ([]byte, error)
		wantCode int
	}{
		{"stored", func(context.Context) ([]byte, error) { return cardPNG, nil }, http.StatusOK},
		{"stale after render", func(context.Context) ([]byte, error) { return cardPNG, previewcard.ErrStale }, http.StatusOK},
		{"not live", func(context.Context) ([]byte, error) { return nil, previewcard.ErrNotLive }, http.StatusServiceUnavailable},
		{"render failed", func(context.Context) ([]byte, error) { return nil, errors.New("renderer down") }, http.StatusServiceUnavailable},
		{"oversized", func(context.Context) ([]byte, error) { return make([]byte, previewcard.MaxPNGBytes+1), nil }, http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			cards := &fakeCards{version: cardVersion, build: test.build}
			handlers, _, _ := newCardHarness(t, cards)
			response := serveCard(t, handlers.png, cardRequest(t, http.MethodGet, "/api/v1/public/resumes/ada-lovelace/og.png"))
			if response.Code != test.wantCode || (test.wantCode == http.StatusOK) != (response.Body.String() == string(cardPNG)) {
				t.Fatalf("response = %d %q", response.Code, response.Body.String())
			}
			if len(cards.builds) != 1 {
				t.Fatalf("builds = %d, want 1", len(cards.builds))
			}
		})
	}
}

func TestCardBuildsShareTheRenderMissLimit(t *testing.T) {
	t.Parallel()
	cards := &fakeCards{version: cardVersion}
	handlers, _, _ := newCardHarness(t, cards)
	for range publicRenderRequestsPerMinute {
		if response := serveCard(t, handlers.png, cardRequest(t, http.MethodGet, "/api/v1/public/resumes/ada-lovelace/og.png")); response.Code != http.StatusOK {
			t.Fatalf("build within the limit = %d", response.Code)
		}
	}
	if response := serveCard(t, handlers.card, cardRequest(t, http.MethodGet, "/api/v1/public/resumes/ada-lovelace/og/"+cardVersion+".png")); response.Code != http.StatusTooManyRequests {
		t.Fatalf("build past the limit = %d, want 429", response.Code)
	}
}

// A revocation while the card builds cancels the response: no card bytes
// reach the client.
func TestRevocationDuringABuildServesNoCard(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	cards := &fakeCards{version: cardVersion, build: func(ctx context.Context) ([]byte, error) {
		close(started)
		<-ctx.Done()
		return cardPNG, nil
	}}
	handlers, backing, coordinator := newCardHarness(t, cards)
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handlers.card.ServeHTTP(response, cardRequest(t, http.MethodGet, "/api/v1/public/resumes/ada-lovelace/og/"+cardVersion+".png"))
		close(done)
	}()
	<-started
	transition, err := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{{
		ID: backing.row.ID, ExpectedRevision: backing.row.Revision, Class: publicstate.Revoking,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := transition.Close(context.Background(), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("card handler did not end after revocation")
	}
	assertCanceledArtifactResponse(t, response, "current-card")
	if err := transition.Rollback(); err != nil {
		t.Fatal(err)
	}
}

func TestCardRouteRejectsQueriesAndOtherMethods(t *testing.T) {
	t.Parallel()
	handlers, _, _ := newCardHarness(t, &fakeCards{version: cardVersion})
	path := "/api/v1/public/resumes/ada-lovelace/og/" + cardVersion + ".png"
	if response := serveCard(t, handlers.card, cardRequest(t, http.MethodGet, path+"?v=1")); response.Code != http.StatusBadRequest {
		t.Fatalf("query = %d, want 400", response.Code)
	}
	if response := serveCard(t, handlers.card, cardRequest(t, http.MethodPost, path)); response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST = %d %v", response.Code, response.Header())
	}
}
