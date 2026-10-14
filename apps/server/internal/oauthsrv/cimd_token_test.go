package oauthsrv

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/google/uuid"
)

func TestCIMDCodeExchangeAndRefreshUseOnlyStoredIdentity(t *testing.T) {
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	f := newCodeFixture(t, now)
	id := "https://client.example/" + uuid.NewString()
	if _, err := f.pool.Exec(context.Background(), `UPDATE oauth_clients SET metadata_url=$1 WHERE id=$2`, id, f.clientID); err != nil {
		t.Fatal(err)
	}
	// No resolver exists and the startup flag is false throughout token use.
	form := url.Values{"client_id": {id}, "code": {f.code}, "redirect_uri": {"http://127.0.0.1:20090/callback"}, "code_verifier": {f.verifier}}
	alias := url.Values{}
	for key, values := range form {
		alias[key] = append([]string(nil), values...)
	}
	alias.Set("client_id", f.clientID.String())
	if _, known := f.s.grantRateClientID(context.Background(), url.Values{"grant_type": {"authorization_code"}, "client_id": {f.clientID.String()}}); known {
		t.Fatal("internal UUID alias consumed a known-client grant bucket")
	}
	if internalID, known := f.s.grantRateClientID(context.Background(), url.Values{"grant_type": {"authorization_code"}, "client_id": {id}}); !known || internalID != f.clientID {
		t.Fatal("URL client did not use its internal grant bucket")
	}
	if _, err := f.s.exchangeAuthorizationCode(context.Background(), alias); !errors.Is(err, errOAuthUnknownClient) {
		t.Fatalf("UUID alias exchange: %v", err)
	}
	wrongPort := url.Values{}
	for key, values := range form {
		wrongPort[key] = append([]string(nil), values...)
	}
	wrongPort.Set("redirect_uri", "http://127.0.0.1:20091/callback")
	if _, err := f.s.exchangeAuthorizationCode(context.Background(), wrongPort); !errors.Is(err, errOAuthInvalidGrant) {
		t.Fatal("code accepted another loopback port")
	}
	response, err := f.s.exchangeAuthorizationCode(context.Background(), form)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.rotateRefreshToken(context.Background(), response.RefreshToken, []string{f.clientID.String()}); !errors.Is(err, errOAuthInvalidGrant) {
		t.Fatalf("UUID alias refresh: %v", err)
	}
	if _, err := f.s.rotateRefreshToken(context.Background(), response.RefreshToken, []string{"https://other.example/metadata"}); !errors.Is(err, errOAuthInvalidGrant) {
		t.Fatalf("wrong-client refresh: %v", err)
	}
	rotated, err := f.s.rotateRefreshToken(context.Background(), response.RefreshToken, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	final, err := f.s.rotateRefreshToken(context.Background(), rotated.RefreshToken, nil)
	if err != nil {
		t.Fatal("omitted refresh identity failed", err)
	}
	if err := f.s.revokeToken(context.Background(), final.RefreshToken, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.rotateRefreshToken(context.Background(), final.RefreshToken, []string{id}); !errors.Is(err, errOAuthInvalidGrant) {
		t.Fatal("revoked CIMD refresh accepted")
	}
}

func TestCIMDTokenUnknownAndMalformedIdentityErrors(t *testing.T) {
	f := newCodeFixture(t, time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC))
	for _, tc := range []struct {
		id                      string
		codeError, refreshError error
	}{
		{"https://client.example/missing", errOAuthUnknownClient, errOAuthUnknownClient},
		{"https://client.example:443/metadata", errOAuthInvalidClient, errOAuthInvalidGrant},
		{"https://client.example/metadata?x=1", errOAuthInvalidClient, errOAuthInvalidGrant},
		{"garbage", errOAuthInvalidClient, errOAuthInvalidGrant},
	} {
		_, err := f.s.exchangeAuthorizationCode(context.Background(), url.Values{"client_id": {tc.id}, "code": {f.code}})
		if !errors.Is(err, tc.codeError) {
			t.Fatalf("code identity error: %v", err)
		}
		_, err = f.s.rotateRefreshToken(context.Background(), "not-a-token", []string{tc.id})
		if !errors.Is(err, tc.refreshError) {
			t.Fatalf("refresh identity error: %v", err)
		}
	}
}

func TestCIMDTokenFormNeverAcceptsClientCredentials(t *testing.T) {
	s := &Service{}
	for _, field := range []string{"client_secret", "client_assertion", "client_assertion_type"} {
		r := httptest.NewRequest(http.MethodPost, "https://aboutme.example/oauth/token", strings.NewReader("grant_type=unsupported&"+field+"=ignored"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		s.HandleToken(w, r)
		if w.Code != 400 || !strings.Contains(w.Body.String(), `"error":"invalid_request"`) {
			t.Fatal("client credential accepted")
		}
	}
}

func TestCIMDApprovedAuthoritySurvivesServiceRestartWithFlagOff(t *testing.T) {
	s, q, user, request := cimdAuthorizeHarness(t)
	ctx := context.Background()
	verifier := strings.Repeat("v", 43)
	sum := sha256.Sum256([]byte(verifier))
	request.CodeChallenge = base64.RawURLEncoding.EncodeToString(sum[:])
	session := issueTestSession(t, s.pool, user.ID, s.clock(), nil)
	redirect, approvalErr := s.ConsentDecision(ctx, session, ConsentDecision{ConsentQuery: request, Decision: "approve"})
	if approvalErr != nil {
		t.Fatal(approvalErr)
	}
	result, parseErr := url.Parse(redirect)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	client, clientErr := q.GetOAuthClientByMetadataURL(ctx, request.PublicClientID)
	if clientErr != nil {
		t.Fatal(clientErr)
	}
	grant, grantErr := q.GetLiveOAuthGrant(ctx, store.GetLiveOAuthGrantParams{UserID: user.ID, ClientID: client.ID})
	if grantErr != nil {
		t.Fatal(grantErr)
	}
	// Reconstruct all process-local OAuth state without a resolver or flag.
	restarted, restartErr := NewService(ctx, ServiceDependencies{
		Pool: s.pool, Queries: store.New(s.pool), Clock: s.clock, Entropy: s.entropy,
		PublicOrigin: s.publicOrigin, RegisterAdmission: s.registerAdmission,
		TokenAdmission: s.tokenAdmission, LiveGrantLimit: s.liveGrantLimit,
	})
	if restartErr != nil {
		t.Fatal(restartErr)
	}
	tokens, exchangeErr := restarted.exchangeAuthorizationCode(ctx, url.Values{
		"client_id": {request.PublicClientID}, "code": {result.Query().Get("code")},
		"redirect_uri": {request.RedirectURI}, "code_verifier": {verifier},
	})
	if exchangeErr != nil {
		t.Fatal(exchangeErr)
	}
	var storedClient, storedGrant uuid.UUID
	if bindingErr := s.pool.QueryRow(ctx, `SELECT client_id,grant_id FROM oauth_tokens WHERE user_id=$1 LIMIT 1`, user.ID).Scan(&storedClient, &storedGrant); bindingErr != nil {
		t.Fatal(bindingErr)
	}
	if storedClient != client.ID || storedGrant != grant.ID {
		t.Fatal("restart changed UUID authority")
	}
	rows, listingErr := restarted.queries.ListLiveOAuthGrantsForUser(ctx, store.ListLiveOAuthGrantsForUserParams{UserID: user.ID, LimitRows: 10})
	if listingErr != nil || len(rows) != 1 || rows[0].MetadataUrl == nil || *rows[0].MetadataUrl != request.PublicClientID {
		t.Fatal("flag-off owner grant listing lost URL identity", listingErr)
	}
	rotated, refreshErr := restarted.rotateRefreshToken(ctx, tokens.RefreshToken, []string{request.PublicClientID})
	if refreshErr != nil {
		t.Fatal(refreshErr)
	}
	if revokeErr := restarted.revokeToken(ctx, rotated.RefreshToken, ""); revokeErr != nil {
		t.Fatal(revokeErr)
	}
	if _, rejectedErr := restarted.rotateRefreshToken(ctx, rotated.RefreshToken, []string{request.PublicClientID}); !errors.Is(rejectedErr, errOAuthInvalidGrant) {
		t.Fatal("revoked restarted authority refreshed")
	}
}
