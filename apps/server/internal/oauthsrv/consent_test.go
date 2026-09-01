package oauthsrv

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func consentQuery(clientID uuid.UUID, scope string) ConsentQuery {
	return ConsentQuery{
		ClientID: clientID, RedirectURI: "https://agent.example/callback?fixed=yes", ResponseType: "code", Scope: scope,
		State: "opaque<&state", CodeChallenge: strings.Repeat("A", 43), CodeChallengeMethod: "S256",
	}
}

func TestConsent_ContextAndDecisionRevalidateAndIssueBoundCode(t *testing.T) {
	s, q, client, user := newAuthorizeHarness(t)
	request := consentQuery(client.ID, "resumes:read resumes:write")

	view, err := s.ConsentContext(context.Background(), user.ID, request)
	if err != nil {
		t.Fatalf("ConsentContext: %v", err)
	}
	if view.ClientName != "Authorize agent" || view.Scopes.String() != "resumes:read resumes:write" {
		t.Fatalf("view = %#v, want name and scopes only", view)
	}

	denyRequest := ConsentDecision{ConsentQuery: request, Decision: "deny"}
	denied, err := s.ConsentDecision(context.Background(), user.ID, denyRequest)
	if err != nil {
		t.Fatalf("deny: %v", err)
	}
	denyURL := urlMustParse(t, denied)
	if denyURL.Query().Get("error") != "access_denied" || denyURL.Query().Get("state") != "opaque<&state" || denyURL.Query().Get("code") != "" {
		t.Fatalf("denial redirect = %q", denied)
	}

	approved, err := s.ConsentDecision(context.Background(), user.ID, ConsentDecision{ConsentQuery: request, Decision: "approve"})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	codeRaw := urlMustParse(t, approved).Query().Get("code")
	if len(codeRaw) != 43 {
		t.Fatalf("approved redirect = %q, want 43-byte code", approved)
	}
	digest, err := ParseCode(codeRaw)
	if err != nil {
		t.Fatalf("ParseCode: %v", err)
	}
	code, err := q.GetOAuthAuthorizationCodeByDigest(context.Background(), digest[:])
	if err != nil {
		t.Fatalf("issued code lookup: %v", err)
	}
	if code.ClientID != client.ID || code.UserID != user.ID || code.Scopes != "resumes:read resumes:write" || code.CodeChallenge != strings.Repeat("A", 43) || code.RedirectURI != request.RedirectURI || !code.ExpiresAt.Equal(code.CreatedAt.Add(60*time.Second)) {
		t.Fatalf("code binding = %#v", code)
	}

	forged := request
	forged.RedirectURI = "https://evil.example/callback"
	if _, err := s.ConsentDecision(context.Background(), user.ID, ConsentDecision{ConsentQuery: forged, Decision: "approve"}); !errors.Is(err, ErrConsentInvalid) {
		t.Fatalf("forged redirect error = %v, want ErrConsentInvalid", err)
	}
}

func TestConsent_RefusesEleventhLiveGrant(t *testing.T) {
	s, q, client, user := newAuthorizeHarness(t)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		other, err := q.CreateOAuthClient(ctx, store.CreateOAuthClientParams{ClientName: "Other " + uuid.NewString(), RedirectURIs: client.RedirectURIs, CreatedAt: time.Now().UTC()})
		if err != nil {
			t.Fatalf("CreateOAuthClient(%d): %v", i, err)
		}
		t.Cleanup(func() {
			if _, cleanupErr := q.DeleteOAuthClient(context.Background(), other.ID); cleanupErr != nil {
				t.Errorf("DeleteOAuthClient cleanup: %v", cleanupErr)
			}
		})
		if _, err := q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: user.ID, ClientID: other.ID, Scopes: "resumes:read", CreatedAt: time.Now().UTC()}); err != nil {
			t.Fatalf("UpsertOAuthGrant(%d): %v", i, err)
		}
	}
	_, err := s.ConsentDecision(ctx, user.ID, ConsentDecision{ConsentQuery: consentQuery(client.ID, "resumes:read"), Decision: "approve"})
	if !errors.Is(err, ErrGrantLimit) {
		t.Fatalf("eleventh grant error = %v, want ErrGrantLimit", err)
	}
}

func TestConsent_AllowsTenthLiveGrant(t *testing.T) {
	s, q, client, user := newAuthorizeHarness(t)
	ctx := context.Background()
	for i := 0; i < 9; i++ {
		other, err := q.CreateOAuthClient(ctx, store.CreateOAuthClientParams{ClientName: "Boundary " + uuid.NewString(), RedirectURIs: client.RedirectURIs, CreatedAt: time.Now().UTC()})
		if err != nil {
			t.Fatalf("CreateOAuthClient(%d): %v", i, err)
		}
		t.Cleanup(func() {
			if _, cleanupErr := q.DeleteOAuthClient(context.Background(), other.ID); cleanupErr != nil {
				t.Errorf("DeleteOAuthClient cleanup: %v", cleanupErr)
			}
		})
		if _, err := q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: user.ID, ClientID: other.ID, Scopes: "resumes:read", CreatedAt: time.Now().UTC()}); err != nil {
			t.Fatalf("UpsertOAuthGrant(%d): %v", i, err)
		}
	}
	redirect, err := s.ConsentDecision(ctx, user.ID, ConsentDecision{ConsentQuery: consentQuery(client.ID, "resumes:read"), Decision: "approve"})
	if err != nil || urlMustParse(t, redirect).Query().Get("code") == "" {
		t.Fatalf("tenth grant = %q, %v; want code", redirect, err)
	}
}

func TestConsent_DecisionDetectsClientRowChangeAfterContext(t *testing.T) {
	s, _, client, user := newAuthorizeHarness(t)
	request := consentQuery(client.ID, "resumes:read")
	if _, err := s.ConsentContext(context.Background(), user.ID, request); err != nil {
		t.Fatalf("initial ConsentContext: %v", err)
	}
	if _, err := s.pool.Exec(context.Background(), "UPDATE oauth_clients SET redirect_uris = $2 WHERE id = $1", client.ID, []byte(`["https://agent.example/changed"]`)); err != nil {
		t.Fatalf("change client redirect: %v", err)
	}
	if _, err := s.ConsentDecision(context.Background(), user.ID, ConsentDecision{ConsentQuery: request, Decision: "approve"}); !errors.Is(err, ErrConsentInvalid) {
		t.Fatalf("changed client decision error = %v, want ErrConsentInvalid", err)
	}
}

func TestConsent_ConcurrentApprovalsKeepOneGrantAndIssueTwoCodes(t *testing.T) {
	s, q, client, user := newAuthorizeHarness(t)
	decision := ConsentDecision{ConsentQuery: consentQuery(client.ID, "resumes:read"), Decision: "approve"}
	type result struct {
		redirect string
		err      error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			redirect, err := s.ConsentDecision(context.Background(), user.ID, decision)
			results <- result{redirect, err}
		}()
	}
	codes := make(map[string]bool, 2)
	for range 2 {
		result := <-results
		code := urlMustParse(t, result.redirect).Query().Get("code")
		if result.err != nil || code == "" {
			t.Fatalf("concurrent approval = %#v", result)
		}
		if _, err := ParseCode(code); err != nil {
			t.Fatalf("concurrent code is malformed: %v", err)
		}
		codes[code] = true
	}
	if len(codes) != 2 {
		t.Fatalf("concurrent approvals issued %d distinct codes, want 2", len(codes))
	}
	var storedCodes int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM oauth_authorization_codes WHERE client_id = $1 AND user_id = $2", client.ID, user.ID).Scan(&storedCodes); err != nil || storedCodes != 2 {
		t.Fatalf("stored concurrent codes = %d, %v; want 2", storedCodes, err)
	}
	if count, err := q.CountLiveOAuthGrantsForUser(context.Background(), user.ID); err != nil || count != 1 {
		t.Fatalf("live grants = %d, %v; want 1", count, err)
	}
}

func TestConsent_QueuedRevocationWinsAfterInFlightApproval(t *testing.T) {
	s, q, client, user := newAuthorizeHarness(t)
	ctx := context.Background()
	grant, err := q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: user.ID, ClientID: client.ID, Scopes: "resumes:read", CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("seed grant: %v", err)
	}
	tokenCreatedAt := time.Now().UTC()
	if _, tokenErr := q.CreateOAuthToken(ctx, store.CreateOAuthTokenParams{
		TokenDigest: bytes32(91), Kind: "refresh", FamilyID: uuid.New(), ClientID: client.ID, UserID: user.ID, GrantID: grant.ID,
		CreatedAt: tokenCreatedAt, ExpiresAt: tokenCreatedAt.Add(time.Hour), FamilyExpiresAt: tokenCreatedAt.Add(30*24*time.Hour - time.Nanosecond),
	}); tokenErr != nil {
		t.Fatalf("seed token: %v", tokenErr)
	}
	entropy := &blockingEntropy{started: make(chan struct{}), release: make(chan struct{})}
	s.entropy = entropy

	type approvalResult struct {
		redirect string
		err      error
	}
	approvalDone := make(chan approvalResult, 1)
	go func() {
		redirect, approveErr := s.ConsentDecision(ctx, user.ID, ConsentDecision{ConsentQuery: consentQuery(client.ID, "resumes:read"), Decision: "approve"})
		approvalDone <- approvalResult{redirect, approveErr}
	}()
	<-entropy.started // approval has client → user → grant locks and is before code creation.

	revokeDone := make(chan error, 1)
	revokePID := make(chan int32, 1)
	go func() {
		tx, beginErr := s.pool.Begin(ctx)
		if beginErr != nil {
			revokeDone <- beginErr
			return
		}
		defer func(rollbackCtx context.Context) {
			if rollbackErr := tx.Rollback(rollbackCtx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				t.Errorf("rollback queued revocation transaction: %v", rollbackErr)
			}
		}(context.WithoutCancel(ctx))
		var pid int32
		if pidErr := tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); pidErr != nil {
			revokeDone <- pidErr
			return
		}
		revokePID <- pid
		qtx := store.New(tx)
		if _, lockErr := qtx.GetOAuthClientForUpdate(ctx, client.ID); lockErr != nil {
			revokeDone <- lockErr
			return
		}
		if _, lockErr := qtx.GetUserForUpdate(ctx, user.ID); lockErr != nil {
			revokeDone <- lockErr
			return
		}
		lockedGrant, lockErr := qtx.GetOAuthGrantForUpdate(ctx, grant.ID)
		if lockErr != nil {
			revokeDone <- lockErr
			return
		}
		if _, revokeErr := qtx.RevokeOAuthGrant(ctx, store.RevokeOAuthGrantParams{ID: lockedGrant.ID, RevokedAt: time.Now().UTC()}); revokeErr != nil {
			revokeDone <- revokeErr
			return
		}
		if _, revokeErr := qtx.RevokeOAuthTokensForGrant(ctx, store.RevokeOAuthTokensForGrantParams{GrantID: lockedGrant.ID, RevokedAt: time.Now().UTC()}); revokeErr != nil {
			revokeDone <- revokeErr
			return
		}
		revokeDone <- tx.Commit(ctx)
	}()
	waitForOAuthLock(t, s.pool, <-revokePID)
	close(entropy.release)
	approval := <-approvalDone
	if approval.err != nil {
		t.Fatalf("approval: %v", approval.err)
	}
	if revokeErr := <-revokeDone; revokeErr != nil {
		t.Fatalf("queued revoke: %v", revokeErr)
	}

	code, err := ParseCode(urlMustParse(t, approval.redirect).Query().Get("code"))
	if err != nil {
		t.Fatalf("approval code: %v", err)
	}
	issued, err := q.GetOAuthAuthorizationCodeByDigest(ctx, code[:])
	if err != nil {
		t.Fatalf("issued code lookup: %v", err)
	}
	if _, err := q.GetLiveOAuthGrant(ctx, store.GetLiveOAuthGrantParams{UserID: issued.UserID, ClientID: issued.ClientID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("revoked code still has live grant: %v", err)
	}
	var liveTokens int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM oauth_tokens WHERE grant_id = $1 AND revoked_at IS NULL", grant.ID).Scan(&liveTokens); err != nil || liveTokens != 0 {
		t.Fatalf("live tokens after queued revoke = %d, %v; want 0", liveTokens, err)
	}
}

type blockingEntropy struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *blockingEntropy) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	<-r.release
	for i := range p {
		p[i] = byte(i + 1)
	}
	return len(p), nil
}

var _ io.Reader = (*blockingEntropy)(nil)

func bytes32(value byte) []byte {
	return []byte{value, value, value, value, value, value, value, value, value, value, value, value, value, value, value, value, value, value, value, value, value, value, value, value, value, value, value, value, value, value, value, value}
}

func waitForOAuthLock(t *testing.T, pool *store.Pool, pid int32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waitType *string
		if err := pool.QueryRow(context.Background(), "SELECT wait_event_type FROM pg_stat_activity WHERE pid = $1", pid).Scan(&waitType); err != nil {
			t.Fatalf("inspect queued revocation: %v", err)
		}
		if waitType != nil && *waitType == "Lock" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("revocation backend %d did not queue on the approval lock", pid)
}
