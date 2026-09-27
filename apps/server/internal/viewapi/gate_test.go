package viewapi

// Sign-in-to-view gating and the signedIn field on the public start route
// (docs/design/viewer-analytics/sign-in-to-view.md "Gated routes", "Join
// invite"; AC-VIEW-003).

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	mathrand "math/rand/v2"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/viewcount"
	"github.com/dannyota/aboutme/apps/server/internal/viewpass"
)

// signInQueries answers GetPublicResumeBySlug with a fixed sign-in-to-view
// resume; every other OwnerQueries method is unused.
type signInQueries struct {
	noOwnerQueries
	resume store.Resume
}

func (q signInQueries) GetPublicResumeBySlug(context.Context, string) (store.Resume, error) {
	return q.resume, nil
}

func newSignInGateFixture(t *testing.T, key []byte) (*routeFixture, *http.ServeMux) {
	t.Helper()
	f := &routeFixture{
		now: time.Date(2026, time.September, 26, 3, 0, 0, 0, time.UTC),
		store: &memoryStore{resume: viewcount.LiveResume{
			ID: uuid.MustParse("018f5b6a-9a3e-7c21-8b1e-0000000000b1"), Owner: uuid.MustParse("018f5b6a-9a3e-7c21-8b1e-0000000000a1"),
		}},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	counter, err := viewcount.New(viewcount.Config{
		Store: f.store, Logger: logger, Now: func() time.Time { return f.now }, Random: mathrand.NewChaCha8([32]byte{4}),
	})
	if err != nil {
		t.Fatalf("viewcount.New: %v", err)
	}
	f.counter = counter
	slug := "ada-lovelace"
	queries := signInQueries{resume: store.Resume{
		ID: f.store.resume.ID, Slug: &slug, Live: true, SignInToView: true, ViewPassEpoch: 1,
	}}
	service, err := New(Dependencies{
		Counter: counter, Queries: queries, Sessions: auth.NewSessionManager(nil), PublicOrigin: testOrigin,
		TrustedProxies: api.LoopbackTrustedProxies(), Now: func() time.Time { return f.now }, Logger: logger,
		ViewPassKey: key,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.mux = http.NewServeMux()
	service.RegisterRoutes(f.mux)
	return f, f.mux
}

func TestPublicStart_SignInToView_RequiresPass(t *testing.T) {
	key := bytes.Repeat([]byte{0x44}, 32)
	f, _ := newSignInGateFixture(t, key)

	noPass := f.request(http.MethodPost, startPath, "{}", nil)
	if noPass.Code != http.StatusNotFound || errorCode(t, noPass) != "public_not_found" {
		t.Fatalf("no-pass start = %d %q, want 404 public_not_found", noPass.Code, errorCode(t, noPass))
	}

	wrongEpoch := f.request(http.MethodPost, startPath, "{}", func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: viewpass.CookieName, Value: viewpass.Seal(key, f.store.resume.ID, 0, f.now.Add(time.Hour))})
	})
	if wrongEpoch.Code != http.StatusNotFound {
		t.Fatalf("old-epoch pass start = %d, want 404", wrongEpoch.Code)
	}

	valid := f.request(http.MethodPost, startPath, "{}", func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: viewpass.CookieName, Value: viewpass.Seal(key, f.store.resume.ID, 1, f.now.Add(time.Hour))})
	})
	if valid.Code != http.StatusOK {
		t.Fatalf("valid-pass start = %d %q, want 200", valid.Code, valid.Body.String())
	}
}

// TestPublicStart_SignedInField_FalseWithNoSession proves the response
// carries signedIn=false for an anonymous request. A live true case needs a
// database-backed session (SessionManager.Authenticate), so it is covered by
// the auth package's session tests, not here.
func TestPublicStart_SignedInField_FalseWithNoSession(t *testing.T) {
	f := newRouteFixture(t)
	w := f.request(http.MethodPost, startPath, "{}", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("start status = %d %q, want 200", w.Code, w.Body.String())
	}
	var response struct {
		SignedIn bool `json:"signedIn"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode start response %q: %v", w.Body.String(), err)
	}
	if response.SignedIn {
		t.Error("signedIn = true with no session cookie, want false")
	}
}
