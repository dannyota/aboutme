package auth_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

const evidenceCallbackForTest = "https://aboutme.example/api/v1/github-evidence/callback"

func TestEvidenceTransaction_BindsUserGenerationAndIsOneUse(t *testing.T) {
	q := newTestQueries(t)
	ts := auth.NewTransactionStore(q)
	ctx := context.Background()
	userID := createTestUser(t, q)

	handle, begun, err := ts.BeginEvidence(ctx, userID, 7, evidenceCallbackForTest)
	if err != nil {
		t.Fatalf("BeginEvidence() error = %v", err)
	}
	if len(handle) != 43 || begun.State == "" || begun.State == handle || len(begun.PKCEVerifier) < 43 || begun.Nonce != "" {
		t.Fatalf("BeginEvidence() handle=%d state=%q verifier=%d nonce=%q, want independent random values and no nonce", len(handle), begun.State, len(begun.PKCEVerifier), begun.Nonce)
	}
	got, err := ts.ConsumeEvidence(ctx, handle)
	if err != nil {
		t.Fatalf("ConsumeEvidence() error = %v", err)
	}
	if got.Provider != auth.ProviderGitHub || got.Purpose != auth.PurposeEvidence || got.LinkingUserID != userID ||
		got.EvidenceGeneration != 7 || got.RedirectURI != evidenceCallbackForTest || got.State != begun.State || got.PKCEVerifier != begun.PKCEVerifier {
		t.Fatalf("ConsumeEvidence() = %+v, want the bound user, generation 7, and the exact callback", got)
	}
	if _, err := ts.ConsumeEvidence(ctx, handle); !errors.Is(err, auth.ErrTransactionInvalid) {
		t.Fatalf("second ConsumeEvidence() error = %v, want ErrTransactionInvalid", err)
	}
}

func TestEvidenceTransaction_SignInConsumeRefusesIt(t *testing.T) {
	q := newTestQueries(t)
	ts := auth.NewTransactionStore(q)
	ctx := context.Background()
	handle, _, err := ts.BeginEvidence(ctx, createTestUser(t, q), 0, evidenceCallbackForTest)
	if err != nil {
		t.Fatalf("BeginEvidence() error = %v", err)
	}
	if _, err := ts.Consume(ctx, handle, auth.ProviderGitHub); !errors.Is(err, auth.ErrTransactionInvalid) {
		t.Fatalf("sign-in Consume(evidence handle) error = %v, want ErrTransactionInvalid", err)
	}
	if _, err := ts.ConsumeEvidence(ctx, handle); !errors.Is(err, auth.ErrTransactionInvalid) {
		t.Fatalf("ConsumeEvidence() after a sign-in consume error = %v, want the handle burned", err)
	}
}

func TestEvidenceTransaction_EvidenceConsumeRefusesSignInTransactions(t *testing.T) {
	q := newTestQueries(t)
	ts := auth.NewTransactionStore(q)
	ctx := context.Background()
	userID := createTestUser(t, q)
	for _, purpose := range []auth.Purpose{auth.PurposeLink, auth.PurposeReauth, auth.PurposeLogin} {
		linking := userID
		if purpose == auth.PurposeLogin {
			linking = uuid.Nil
		}
		handle, _, err := ts.Begin(ctx, auth.ProviderGitHub, purpose, linking, "https://aboutme.example/api/v1/auth/github/callback")
		if err != nil {
			t.Fatalf("Begin(%s) error = %v", purpose, err)
		}
		if _, err := ts.ConsumeEvidence(ctx, handle); !errors.Is(err, auth.ErrTransactionInvalid) {
			t.Fatalf("ConsumeEvidence(%s handle) error = %v, want ErrTransactionInvalid", purpose, err)
		}
	}
}

func TestEvidenceTransaction_ExpiresAfterTenMinutes(t *testing.T) {
	q := newTestQueries(t)
	clock := testutil.NewClock(time.Now().Truncate(time.Microsecond))
	ts := auth.NewTransactionStoreForTest(q, clock.Now)
	ctx := context.Background()
	userID := createTestUser(t, q)

	fresh, _, err := ts.BeginEvidence(ctx, userID, 1, evidenceCallbackForTest)
	if err != nil {
		t.Fatalf("BeginEvidence() error = %v", err)
	}
	stale, _, err := ts.BeginEvidence(ctx, userID, 1, evidenceCallbackForTest)
	if err != nil {
		t.Fatalf("BeginEvidence() error = %v", err)
	}
	clock.Advance(10*time.Minute - time.Second)
	if _, err := ts.ConsumeEvidence(ctx, fresh); err != nil {
		t.Fatalf("ConsumeEvidence() just inside ten minutes error = %v", err)
	}
	clock.Advance(time.Second)
	if _, err := ts.ConsumeEvidence(ctx, stale); !errors.Is(err, auth.ErrTransactionInvalid) {
		t.Fatalf("ConsumeEvidence() at ten minutes error = %v, want ErrTransactionInvalid", err)
	}
}

func TestGitHubSignInCallback_RefusesAnEvidenceTransaction(t *testing.T) {
	gh := newGitHubStub(t, withTokenResponse("code-evidence", "sign-in-must-not-exchange"), withUser(uniqueGitHubUserID(t), "octocat"))
	handler, q := newTestService(t, withGitHubEndpoint(gh.URL))
	ts := auth.NewTransactionStore(q)
	handle, tx, err := ts.BeginEvidence(context.Background(), createTestUser(t, q), 0, evidenceCallbackForTest)
	if err != nil {
		t.Fatalf("BeginEvidence() error = %v", err)
	}
	resp := doGet(t, handler, auth.GitHubCallbackPath+"?code=code-evidence&state="+tx.State, requestCookie(auth.OAuthTxCookieName, handle)) //nolint:bodyclose // doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	assertRedirectPath(t, resp.Header.Get("Location"), "/login")
	if got := mustQueryParam(t, resp.Header.Get("Location"), "error"); got != "auth_failed" {
		t.Fatalf("error = %q, want auth_failed", got)
	}
	if cookie := extractCookie(resp, auth.SessionCookieName); cookie != nil && cookie.Value != "" {
		t.Fatal("sign-in callback issued a session for an evidence transaction")
	}
}
