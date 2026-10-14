package oauthsrv

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
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
	for _, claimed := range []string{id, f.clientID.String(), "https://other.example/metadata"} {
		key, known, lookupErr := f.s.grantRateID(context.Background(), url.Values{"grant_type": {"authorization_code"}, "code": {f.code}, "client_id": {claimed}})
		if lookupErr != nil || !known || key != f.grantID {
			t.Fatal("credential did not select its stored grant", lookupErr)
		}
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
	if _, refreshErr := f.s.rotateRefreshToken(context.Background(), response.RefreshToken, []string{f.clientID.String()}); !errors.Is(refreshErr, errOAuthInvalidGrant) {
		t.Fatalf("UUID alias refresh: %v", refreshErr)
	}
	if _, refreshErr := f.s.rotateRefreshToken(context.Background(), response.RefreshToken, []string{"https://other.example/metadata"}); !errors.Is(refreshErr, errOAuthInvalidGrant) {
		t.Fatalf("wrong-client refresh: %v", refreshErr)
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
		r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "https://aboutme.example/oauth/token", strings.NewReader("grant_type=unsupported&"+field+"=ignored"))
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

func TestTokenSharedClientGrantAdmissionIsolatesOwners(t *testing.T) {
	for _, mode := range []string{"cimd on", "cimd off", "dcr"} {
		for _, credential := range []string{"code", "refresh"} {
			t.Run(mode+" "+credential, func(t *testing.T) {
				ctx := t.Context()
				s, q, client, owner := newAuthorizeHarness(t)
				request := consentQuery(client.ID, "resumes:read")
				publicID := client.ID.String()
				if mode != "dcr" {
					resolver, _ := cimdTLSResolver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						if _, err := w.Write([]byte(cimdFixture(r.URL.Path))); err != nil {
							return
						}
					}), s.clock)
					s.cimd, s.cimdEnabled = resolver, true
					publicID = "https://example.com/" + uuid.NewString()
					request.PublicClientID, request.ClientID = publicID, uuid.Nil
					request.RedirectURI = "http://localhost:20095/callback"
					request.address = netip.MustParseAddr("8.8.8.8")
					t.Cleanup(func() {
						if _, err := s.pool.Exec(context.Background(), "DELETE FROM oauth_clients WHERE metadata_url=$1", publicID); err != nil {
							t.Error(err)
						}
					})
				}
				second, err := q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.test", Name: "Second owner"})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if _, err := s.pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", second.ID); err != nil {
						t.Error(err)
					}
				})
				var entropy []byte
				for block := byte(1); block <= 32; block++ {
					entropy = append(entropy, bytes.Repeat([]byte{block}, 32)...)
				}
				s.entropy = bytes.NewReader(entropy)
				verifier := strings.Repeat("v", 43)
				challenge := sha256.Sum256([]byte(verifier))
				request.CodeChallenge = base64.RawURLEncoding.EncodeToString(challenge[:])
				codes := make([]string, 2)
				grants := make([]uuid.UUID, 2)
				for i, user := range []store.User{owner, second} {
					session := issueTestSession(t, s.pool, user.ID, s.clock(), nil)
					redirect, err := s.ConsentDecision(ctx, session, ConsentDecision{ConsentQuery: request, Decision: "approve"})
					if err != nil {
						t.Fatal(err)
					}
					codes[i] = urlMustParse(t, redirect).Query().Get("code")
					digest, err := ParseCode(codes[i])
					if err != nil {
						t.Fatal(err)
					}
					row, err := q.GetOAuthAuthorizationCodeByDigest(ctx, digest[:])
					if err != nil {
						t.Fatal(err)
					}
					grants[i] = row.GrantID
				}
				if grants[0] == grants[1] {
					t.Fatal("owners share a grant")
				}
				s.cimdEnabled = mode == "cimd on"
				policies, err := NewRatePolicies(testOAuthRateConfig())
				if err != nil {
					t.Fatal(err)
				}
				s.tokenAdmission = policies
				post := func(form url.Values, address string, status int) *httptest.ResponseRecorder {
					t.Helper()
					r := rateRequest("/oauth/token", "application/x-www-form-urlencoded", form.Encode(), address)
					w := httptest.NewRecorder()
					s.HandleToken(w, r)
					if w.Code != status {
						t.Fatalf("token status %d, want %d", w.Code, status)
					}
					return w
				}
				codeForm := func(raw string) url.Values {
					return url.Values{"grant_type": {"authorization_code"}, "client_id": {publicID}, "code": {raw}, "redirect_uri": {request.RedirectURI}, "code_verifier": {verifier}}
				}
				// Guesses from one shared address stay below the separate token-IP cap.
				for i := range 10 {
					form := codeForm("bad")
					form.Set("code_verifier", "bad")
					post(form, "8.8.8.8", http.StatusBadRequest)
					unknown := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{byte(i + 100)}, 32))
					post(codeForm(unknown), "8.8.4.4", http.StatusBadRequest)
					post(url.Values{"grant_type": {"refresh_token"}, "client_id": {publicID}, "refresh_token": {refreshTokenPrefix + unknown}}, "9.9.9.9", http.StatusBadRequest)
				}
				if len(policies.grants.entries) != 0 || len(policies.grants.overflow.pending) != 0 || policies.grants.overflow.failures != 0 {
					t.Fatal("unknown guesses created grant debt")
				}
				var ownRefresh string
				if credential == "refresh" {
					w := post(codeForm(codes[0]), "1.1.1.1", http.StatusOK)
					var response tokenResponse
					if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
						t.Fatal(err)
					}
					ownRefresh = response.RefreshToken
				}
				other, err := q.CreateOAuthClient(ctx, store.CreateOAuthClientParams{ClientName: "Other client", RedirectURIs: []byte(`["https://agent.example/callback"]`), CreatedAt: s.clock()})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if _, err := q.DeleteOAuthClient(context.Background(), other.ID); err != nil {
						t.Error(err)
					}
				})
				target, err := q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: second.ID, ClientID: other.ID, Scopes: "resumes:read", CreatedAt: s.clock()})
				if err != nil {
					t.Fatal(err)
				}
				for range 10 {
					form := codeForm(codes[0])
					if credential == "refresh" {
						form = url.Values{"grant_type": {"refresh_token"}, "refresh_token": {ownRefresh}}
					}
					form.Set("client_id", other.ID.String())
					post(form, "1.0.0.1", http.StatusBadRequest)
				}
				if policies.grants.entries[target.ID] != nil {
					t.Fatal("wrong client redirected grant debt")
				}
				if policies.grants.entries[grants[0]] == nil {
					t.Fatal("stored credential did not select its grant")
				}
				blocked := codeForm(codes[0])
				if credential == "refresh" {
					blocked = url.Values{"grant_type": {"refresh_token"}, "refresh_token": {ownRefresh}}
				}
				w := post(blocked, "1.0.0.1", http.StatusTooManyRequests)
				if w.Header().Get("Retry-After") != "900" {
					t.Fatal("grant refusal lost its window")
				}
				if credential == "code" {
					digest, err := ParseCode(codes[0])
					if err != nil {
						t.Fatal(err)
					}
					row, err := q.GetOAuthAuthorizationCodeByDigest(ctx, digest[:])
					if err != nil || row.ConsumedAt != nil {
						t.Fatal("refused admission consumed code", err)
					}
				} else {
					_, digest, err := ParseToken(ownRefresh)
					if err != nil {
						t.Fatal(err)
					}
					row, err := q.GetOAuthTokenAuthorityByDigest(ctx, digest[:])
					if err != nil || row.OAuthToken.SupersededAt != nil || row.OAuthToken.RevokedAt != nil {
						t.Fatal("refused admission mutated refresh", err)
					}
					explicit := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {ownRefresh}, "client_id": {publicID}}
					post(explicit, "1.0.0.1", http.StatusTooManyRequests)
				}
				policies.grants.maxKeys = 1
				for i := range 10 {
					unknown := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{byte(i + 120)}, 32))
					post(codeForm(unknown), "4.2.2.1", http.StatusBadRequest)
					post(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshTokenPrefix + unknown}, "client_id": {publicID}}, "4.2.2.2", http.StatusBadRequest)
				}
				if len(policies.grants.entries) != 1 || len(policies.grants.overflow.pending) != 0 || policies.grants.overflow.failures != 0 {
					t.Fatal("unknown guesses created overflow debt at capacity")
				}
				// The other owner shares the first guesser's address without sharing debt.
				w = post(codeForm(codes[1]), "8.8.8.8", http.StatusOK)
				var response tokenResponse
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				post(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {response.RefreshToken}, "client_id": {publicID}}, "8.8.8.8", http.StatusOK)
			})
		}
	}
}

type grantIdentityQueries struct {
	store.OAuthQueries
	code  store.OAuthAuthorizationCode
	token store.GetOAuthTokenAuthorityByDigestRow
	err   error
	calls int
}

func (q *grantIdentityQueries) GetOAuthAuthorizationCodeByDigest(context.Context, []byte) (store.OAuthAuthorizationCode, error) {
	q.calls++
	return q.code, q.err
}
func (q *grantIdentityQueries) GetOAuthTokenAuthorityByDigest(context.Context, []byte) (store.GetOAuthTokenAuthorityByDigestRow, error) {
	q.calls++
	return q.token, q.err
}

func TestTokenGrantIdentityRequiresStoredCredential(t *testing.T) {
	t.Parallel()
	raw := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{90}, 32))
	grant := uuid.New()
	for _, tc := range []struct {
		name, kind, credential string
		lookupErr              error
		storedKind             string
		want                   bool
	}{
		{"malformed code", "authorization_code", "bad", nil, "", false},
		{"unknown code", "authorization_code", raw, pgx.ErrNoRows, "", false},
		{"retained code", "authorization_code", raw, nil, "", true},
		{"malformed refresh", "refresh_token", "bad", nil, "refresh", false},
		{"access spelling", "refresh_token", accessTokenPrefix + raw, nil, "refresh", false},
		{"unknown refresh", "refresh_token", refreshTokenPrefix + raw, pgx.ErrNoRows, "refresh", false},
		{"wrong stored kind", "refresh_token", refreshTokenPrefix + raw, nil, "access", false},
		{"retained refresh", "refresh_token", refreshTokenPrefix + raw, nil, "refresh", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := &grantIdentityQueries{code: store.OAuthAuthorizationCode{GrantID: grant}, token: store.GetOAuthTokenAuthorityByDigestRow{OAuthToken: store.OAuthToken{GrantID: grant, Kind: tc.storedKind}}, err: tc.lookupErr}
			s := &Service{queries: q}
			form := url.Values{"grant_type": {tc.kind}, "code": {tc.credential}, "refresh_token": {tc.credential}, "client_id": {uuid.NewString()}}
			id, known, err := s.grantRateID(t.Context(), form)
			if err != nil || known != tc.want || known && id != grant {
				t.Fatalf("credential admission: known %v, error %v", known, err)
			}
		})
	}
}

func TestTokenGrantLookupFailureStopsBeforeReservationOrMutation(t *testing.T) {
	t.Parallel()
	raw := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{90}, 32))
	for _, form := range []url.Values{
		{"grant_type": {"authorization_code"}, "code": {raw}, "client_id": {uuid.NewString()}, "redirect_uri": {"https://agent.example/callback"}, "code_verifier": {strings.Repeat("v", 43)}},
		{"grant_type": {"refresh_token"}, "refresh_token": {refreshTokenPrefix + raw}},
	} {
		q := &grantIdentityQueries{err: errors.New("database unavailable")}
		recorder := &grantResultRecorder{}
		s := &Service{queries: q, tokenAdmission: recorder, clock: cimdTestNow}
		w := postTokenForm(t, s, form.Encode())
		if w.Code != http.StatusInternalServerError || q.calls != 1 || len(recorder.snapshot()) != 0 {
			t.Fatal("lookup failure did not stop before admission")
		}
	}
}
