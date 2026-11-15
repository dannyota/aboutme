package auth_test

import (
	"context"
	"net/http"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/store"
)

type endedAccounts struct {
	mu    sync.Mutex
	users []uuid.UUID
}

func (e *endedAccounts) AccountSessionsEndedLocked(userID uuid.UUID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.users = append(e.users, userID)
}

func (e *endedAccounts) requireExactly(t *testing.T, want ...uuid.UUID) {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	if !slices.Equal(e.users, want) {
		t.Fatalf("ended accounts = %v, want %v", e.users, want)
	}
}

func newListenedSessionService(t *testing.T) (http.Handler, *store.Queries, *endedAccounts) {
	t.Helper()
	pool := newTestPool(t)
	svc, err := auth.NewService(testLogger(), config.Config{PublicOrigin: testPublicOrigin}, pool)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	endings := &endedAccounts{}
	svc.SetSessionEndListener(endings)
	return api.New(testLogger(), noopPinger{}, api.Options{}, nil, svc.RegisterRoutes), store.New(pool), endings
}

func TestSessionEndListener_Logout(t *testing.T) {
	handler, q, endings := newListenedSessionService(t)
	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID)
	resp := doJSON(t, handler, http.MethodPost, auth.LogoutPath, testPublicOrigin, csrfTokenFor(sess), "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d", resp.StatusCode)
	}
	endings.requireExactly(t, userID)
}

func TestSessionEndListener_RevokeAllSessions(t *testing.T) {
	handler, q, endings := newListenedSessionService(t)
	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID)
	resp := doJSON(t, handler, http.MethodDelete, auth.SessionsPath, testPublicOrigin, csrfTokenFor(sess), "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke-all status = %d", resp.StatusCode)
	}
	endings.requireExactly(t, userID)
}

func TestSessionEndListener_RevokeOneSession(t *testing.T) {
	handler, q, endings := newListenedSessionService(t)
	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID)
	_, other := issueTestSession(t, q, userID)
	resp := doJSON(t, handler, http.MethodDelete, sessionIDPath(other.ID), testPublicOrigin, csrfTokenFor(sess), "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke-one status = %d", resp.StatusCode)
	}
	endings.requireExactly(t, userID)
}

func TestSessionEndListener_RefusedSignOutEndsNothing(t *testing.T) {
	handler, q, endings := newListenedSessionService(t)
	userID := createTestUser(t, q)
	raw, _ := issueTestSession(t, q, userID)
	resp := doJSON(t, handler, http.MethodPost, auth.LogoutPath, testPublicOrigin, "", "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("logout without CSRF status = %d, want 403", resp.StatusCode)
	}
	endings.requireExactly(t)
}

func TestSessionEndListener_EpochChangeLeavesTheNotificationToItsCaller(t *testing.T) {
	pool := newTestPool(t)
	q := store.New(pool)
	userID := createTestUser(t, q)
	_, sess := issueTestSession(t, q, userID)
	sessions := auth.NewSessionManagerWithPool(pool)
	endings := &endedAccounts{}
	sessions.SetEndListener(endings)
	err := pgx.BeginFunc(context.Background(), pool, func(tx pgx.Tx) error {
		qtx := q.WithTx(tx)
		if _, err := qtx.AdvanceUserAuthEpoch(context.Background(), userID); err != nil {
			return err
		}
		_, err := sessions.ReplaceAfterEpochChangeTx(context.Background(), qtx, userID, sess.ID, nil)
		return err
	})
	if err != nil {
		t.Fatalf("epoch change error = %v", err)
	}
	endings.requireExactly(t)
}

func TestSessionEndListener_PasswordChange(t *testing.T) {
	e := newPasswordEnv(t)
	endings := &endedAccounts{}
	e.svc.SetSessionEndListener(endings)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	raw, sess := e.createSession(t, userID)
	resp, body := e.request(t, http.MethodPut, auth.PasswordMePath, jsonBody(t, map[string]string{"password": "another brand new password"}), //nolint:bodyclose // request closes the body itself before returning.
		withCookie(sessionRequestCookie(raw)), withCSRF(csrfTokenFor(sess)))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("change status = %d body=%s", resp.StatusCode, body)
	}
	endings.requireExactly(t, userID)
}

func TestSessionEndListener_PasswordReset(t *testing.T) {
	e := newPasswordEnv(t)
	endings := &endedAccounts{}
	e.svc.SetSessionEndListener(endings)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	e.createSession(t, userID)
	token := e.createResetToken(t, userID)
	resp, body := e.request(t, http.MethodPost, auth.PasswordResetPath, jsonBody(t, map[string]string{ //nolint:bodyclose // request closes the body itself before returning.
		"token": token.Raw, "password": "a completely different password",
	}))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("reset status = %d body=%s", resp.StatusCode, body)
	}
	endings.requireExactly(t, userID)
}
