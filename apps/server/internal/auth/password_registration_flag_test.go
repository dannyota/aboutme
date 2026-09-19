package auth_test

// With email-and-password sign-up turned off, POST /auth/password/register is
// not registered and returns the uniform not-found response without any work,
// while pending registrations still verify and the other password routes are
// unchanged.

import (
	"context"
	"net/http"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
)

func newRegistrationOffEnv(t *testing.T) *passwordEnv {
	t.Helper()
	return newPasswordEnvWith(t, func(opts *auth.PasswordServiceOptions) {
		opts.RegistrationDisabled = true
	})
}

func TestPasswordRegister_DisabledIsUniformNotFoundWithoutWork(t *testing.T) {
	e := newRegistrationOffEnv(t)
	email := newEmail()

	resp, body := e.request(t, http.MethodPost, auth.PasswordRegisterPath, jsonBody(t, map[string]string{ //nolint:bodyclose // request closes the body itself before returning.
		"name": "Ada Lovelace", "email": email, "password": testPassword,
	}))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body=%s)", resp.StatusCode, body)
	}
	unknown, unknownBody := e.request(t, http.MethodPost, "/api/v1/auth/password/not-a-route", "{}") //nolint:bodyclose // request closes the body itself before returning.
	if unknown.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown route status = %d, want 404", unknown.StatusCode)
	}
	assertErrorCode(t, body, "not_found")
	assertErrorCode(t, unknownBody, "not_found")
	// No handler ran: nothing was stored and no mail was queued.
	assertNoRegistrationForEmail(t, e.pool, email)
	var jobs int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM auth_email_jobs j JOIN password_registrations r ON r.id = j.registration_id WHERE r.email = $1`, email).Scan(&jobs); err != nil {
		t.Fatalf("count mail jobs: %v", err)
	}
	if jobs != 0 {
		t.Fatalf("%d mail jobs queued, want 0", jobs)
	}
}

func TestPasswordVerify_StillWorksWithRegistrationDisabled(t *testing.T) {
	e := newRegistrationOffEnv(t)
	email := newEmail()
	token, _ := e.createRegistration(t, email, "Ada", testPassword)

	resp, body := e.request(t, http.MethodPost, auth.PasswordVerifyPath, jsonBody(t, map[string]string{"token": token.Raw})) //nolint:bodyclose // request closes the body itself before returning.
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("verify status = %d, want 204 (body=%s)", resp.StatusCode, body)
	}
	if _, err := e.q.GetUserByCanonicalEmail(context.Background(), email); err != nil {
		t.Fatalf("user not created by verify: %v", err)
	}
}

func TestPasswordLoginAndForgot_UnchangedWithRegistrationDisabled(t *testing.T) {
	e := newRegistrationOffEnv(t)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	email := e.userEmail(t, userID)

	login, body := e.request(t, http.MethodPost, auth.PasswordLoginPath, jsonBody(t, map[string]string{"email": email, "password": testPassword})) //nolint:bodyclose // request closes the body itself before returning.
	if login.StatusCode != http.StatusNoContent {
		t.Fatalf("login status = %d, want 204 (body=%s)", login.StatusCode, body)
	}
	forgot, body := e.request(t, http.MethodPost, auth.PasswordForgotPath, jsonBody(t, map[string]string{"email": email})) //nolint:bodyclose // request closes the body itself before returning.
	if forgot.StatusCode != http.StatusAccepted {
		t.Fatalf("forgot status = %d, want 202 (body=%s)", forgot.StatusCode, body)
	}
}
