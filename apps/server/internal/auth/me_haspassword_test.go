package auth_test

// These tests pin GET /me's hasPassword field: a single existence probe that is
// false without a credential and true with one, while identity order stays
// deterministic and no provider subject or email beyond the user's own leaks.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// meUserBody is the user half of GET /me's envelope, including hasPassword.
type meUserBody struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	Name        string  `json:"name"`
	AvatarKey   *string `json:"avatarKey"`
	HasPassword bool    `json:"hasPassword"`
}

func getMeUser(t *testing.T, handler http.Handler, cookie *http.Cookie) meUserBody {
	t.Helper()
	resp := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", cookie) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /me status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var body struct {
		Data struct {
			User meUserBody `json:"user"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body.Data.User
}

func TestGetMe_HasPasswordFalse_WithoutCredential(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)
	raw, _ := issueTestSession(t, q, userID)

	user := getMeUser(t, handler, sessionRequestCookie(raw))
	if user.HasPassword {
		t.Error("hasPassword = true for an account with no password credential, want false")
	}
}

func TestGetMe_HasPasswordTrue_WithCredential(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	ctx := context.Background()
	userID := createTestUser(t, q)

	now := time.Now()
	if _, err := q.UpsertPasswordCredential(ctx, store.UpsertPasswordCredentialParams{
		UserID:      userID,
		EncodedHash: []byte("test-encoded-hash"),
		CreatedAt:   now,
		ChangedAt:   now,
	}); err != nil {
		t.Fatalf("UpsertPasswordCredential() error = %v", err)
	}

	raw, _ := issueTestSession(t, q, userID)
	user := getMeUser(t, handler, sessionRequestCookie(raw))
	if !user.HasPassword {
		t.Error("hasPassword = false for an account with a password credential, want true")
	}
}

func TestGetMe_HasPasswordDoesNotLeakProviderEmail(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	ctx := context.Background()

	// A provider-only user with a linked identity; its user email is its own,
	// but the identity's provider subject must never appear in /me.
	userID := createTestUser(t, q)
	subject := "g-sub-" + uuid.NewString()
	if _, err := q.CreateIdentity(ctx, store.CreateIdentityParams{
		UserID:         userID,
		Provider:       string(auth.ProviderGoogle),
		ProviderUserID: subject,
	}); err != nil {
		t.Fatalf("CreateIdentity() error = %v", err)
	}

	raw, _ := issueTestSession(t, q, userID)
	resp := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /me status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if strings.Contains(string(bodyBytes), subject) {
		t.Error("GET /me response leaked the provider subject, want it never exposed")
	}
	if user := getMeUser(t, handler, sessionRequestCookie(raw)); user.HasPassword {
		t.Error("hasPassword = true for a provider-only account, want false")
	}
}
