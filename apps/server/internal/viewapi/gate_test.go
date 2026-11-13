package viewapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	mathrand "math/rand/v2"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/viewcount"
	"danny.vn/aboutme/apps/server/internal/viewpass"
)

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
	f.queries = liveRow(f.store.resume, true, 1)
	service, err := New(Dependencies{
		Counter: counter, Queries: f.queries, Sessions: auth.NewSessionManager(nil), PublicOrigin: testOrigin,
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

func TestPublicStart_StatusesAndCounting(t *testing.T) {
	key := bytes.Repeat([]byte{0x44}, 32)
	tests := []struct {
		name         string
		signInToView bool
		path         string
		withPass     bool
		wantStatus   int
		wantCounted  int32
	}{
		{"public resume", false, startPath, false, http.StatusOK, 1},
		{"sign-in resume without a pass", true, startPath, false, http.StatusNotFound, 0},
		{"sign-in resume with a valid pass", true, startPath, true, http.StatusOK, 1},
		{"unpublished slug", false, "/api/v1/public/resumes/ada-unpublished/views/start", false, http.StatusNotFound, 0},
		{"unknown slug", true, "/api/v1/public/resumes/nobody-here/views/start", true, http.StatusNotFound, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var f *routeFixture
			if test.signInToView {
				f, _ = newSignInGateFixture(t, key)
			} else {
				f = newRouteFixture(t)
			}
			var edit func(*http.Request)
			if test.withPass {
				edit = func(r *http.Request) {
					r.AddCookie(&http.Cookie{Name: viewpass.CookieName, Value: viewpass.Seal(key, f.store.resume.ID, 1, f.now.Add(time.Hour))})
				}
			}
			if test.wantStatus != http.StatusOK {
				w := f.request(http.MethodPost, test.path, "{}", edit)
				if w.Code != test.wantStatus || errorCode(t, w) != "public_not_found" {
					t.Fatalf("start = %d %s, want %d public_not_found", w.Code, w.Body.String(), test.wantStatus)
				}
			} else {
				body := f.startAndSolve(t, edit)
				f.now = f.now.Add(9 * time.Second)
				if w := f.request(http.MethodPost, collectPath, body, edit); w.Code != http.StatusNoContent {
					t.Fatalf("collect = %d, want 204", w.Code)
				}
			}
			if f.queries.reads != 1 {
				t.Errorf("live reads = %d, want 1", f.queries.reads)
			}
			if total := f.totals(t); total.Counted != test.wantCounted {
				t.Errorf("counted = %d, want %d", total.Counted, test.wantCounted)
			}
		})
	}
}

func TestOwnerQueriesReadNoWholeResume(t *testing.T) {
	queries := reflect.TypeFor[OwnerQueries]()
	whole := reflect.TypeFor[store.Resume]()
	for i := range queries.NumMethod() {
		method := queries.Method(i)
		for j := range method.Type.NumOut() {
			if method.Type.Out(j) == whole {
				t.Errorf("OwnerQueries.%s returns store.Resume", method.Name)
			}
		}
	}
}
