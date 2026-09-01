package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func TestOptionalSession_AuthenticatesWhenPresentAndContinuesWhenAbsentOrInvalid(t *testing.T) {
	pool := newTestPool(t)
	queries := store.New(pool)
	userID := createTestUser(t, queries)
	raw, expected := issueTestSession(t, queries, userID)
	sessions := auth.NewSessionManager(queries)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := auth.SessionFromContext(r.Context())
		if !ok {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if session.ID != expected.ID || session.UserID != userID {
			t.Errorf("session in context = %#v, want %s for %s", session, expected.ID, userID)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	handler := auth.OptionalSession(sessions)(inner)

	valid := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/oauth/authorize", nil)
	valid.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: raw})
	validRecorder := httptest.NewRecorder()
	handler.ServeHTTP(validRecorder, valid)
	if validRecorder.Code != http.StatusNoContent {
		t.Fatalf("valid session status = %d, want 204", validRecorder.Code)
	}

	absentRecorder := httptest.NewRecorder()
	handler.ServeHTTP(absentRecorder, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/oauth/authorize", nil))
	if absentRecorder.Code != http.StatusAccepted {
		t.Fatalf("absent session status = %d, want anonymous continuation", absentRecorder.Code)
	}

	invalid := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/oauth/authorize", nil)
	invalid.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "invalid"})
	invalidRecorder := httptest.NewRecorder()
	handler.ServeHTTP(invalidRecorder, invalid)
	if invalidRecorder.Code != http.StatusAccepted {
		t.Fatalf("invalid session status = %d, want anonymous continuation", invalidRecorder.Code)
	}
	cleared := invalidRecorder.Result().Cookies()
	if len(cleared) != 1 || cleared[0].Name != auth.SessionCookieName || cleared[0].MaxAge >= 0 {
		t.Fatalf("invalid session clearing cookies = %#v", cleared)
	}
}
