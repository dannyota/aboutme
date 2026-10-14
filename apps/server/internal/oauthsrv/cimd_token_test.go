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
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
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
				second, userErr := q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.test", Name: "Second owner"})
				if userErr != nil {
					t.Fatal(userErr)
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
					redirect, approvalErr := s.ConsentDecision(ctx, session, ConsentDecision{ConsentQuery: request, Decision: "approve"})
					if approvalErr != nil {
						t.Fatal(approvalErr)
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
				policies, policyErr := NewRatePolicies(testOAuthRateConfig())
				if policyErr != nil {
					t.Fatal(policyErr)
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
					if decodeErr := json.Unmarshal(w.Body.Bytes(), &response); decodeErr != nil {
						t.Fatal(decodeErr)
					}
					ownRefresh = response.RefreshToken
				}
				other, clientErr := q.CreateOAuthClient(ctx, store.CreateOAuthClientParams{ClientName: "Other client", RedirectURIs: []byte(`["https://agent.example/callback"]`), CreatedAt: s.clock()})
				if clientErr != nil {
					t.Fatal(clientErr)
				}
				t.Cleanup(func() {
					if _, err := q.DeleteOAuthClient(context.Background(), other.ID); err != nil {
						t.Error(err)
					}
				})
				target, grantErr := q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: second.ID, ClientID: other.ID, Scopes: "resumes:read", CreatedAt: s.clock()})
				if grantErr != nil {
					t.Fatal(grantErr)
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
	databaseErr := errors.New("database unavailable")
	for _, tc := range []struct {
		name, kind, credential string
		lookupErr              error
		storedKind             string
		want                   bool
	}{
		{"malformed code", "authorization_code", "bad", nil, "", false},
		{"unknown code", "authorization_code", raw, pgx.ErrNoRows, "", false},
		{"code database failure", "authorization_code", raw, databaseErr, "", false},
		{"retained code", "authorization_code", raw, nil, "", true},
		{"malformed refresh", "refresh_token", "bad", nil, "refresh", false},
		{"access spelling", "refresh_token", accessTokenPrefix + raw, nil, "refresh", false},
		{"unknown refresh", "refresh_token", refreshTokenPrefix + raw, pgx.ErrNoRows, "refresh", false},
		{"refresh database failure", "refresh_token", refreshTokenPrefix + raw, databaseErr, "refresh", false},
		{"wrong stored kind", "refresh_token", refreshTokenPrefix + raw, nil, "access", false},
		{"retained refresh", "refresh_token", refreshTokenPrefix + raw, nil, "refresh", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := &grantIdentityQueries{code: store.OAuthAuthorizationCode{GrantID: grant}, token: store.GetOAuthTokenAuthorityByDigestRow{OAuthToken: store.OAuthToken{GrantID: grant, Kind: tc.storedKind}}, err: tc.lookupErr}
			s := &Service{queries: q}
			form := url.Values{"grant_type": {tc.kind}, "code": {tc.credential}, "refresh_token": {tc.credential}, "client_id": {uuid.NewString()}}
			id, known, err := s.grantRateID(t.Context(), form)
			wantErr := tc.lookupErr
			if !tc.want && (wantErr == nil || errors.Is(wantErr, pgx.ErrNoRows)) {
				wantErr = errOAuthNoStoredGrant
			}
			if !errors.Is(err, wantErr) || known != tc.want || known && id != grant || !known && id != uuid.Nil {
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
		if w.Code != http.StatusInternalServerError || q.calls != 1 || len(recorder.snapshot()) != 0 ||
			w.Header().Get("Retry-After") != "" ||
			w.Body.String() != `{"error":"server_error","error_description":"The server encountered an error."}` {
			t.Fatal("lookup failure did not stop before admission")
		}
	}
}

func TestTokenNoStoredGrantKeepsOrdinaryValidationErrors(t *testing.T) {
	t.Parallel()
	for _, form := range []url.Values{
		{"grant_type": {"authorization_code"}, "code": {"bad"}, "client_id": {uuid.NewString()}, "redirect_uri": {"https://agent.example/callback"}, "code_verifier": {strings.Repeat("v", 43)}},
		{"grant_type": {"refresh_token"}, "refresh_token": {"bad"}},
	} {
		q := &grantIdentityQueries{err: errors.New("unexpected database lookup")}
		recorder := &grantResultRecorder{}
		s := &Service{queries: q, tokenAdmission: recorder, clock: cimdTestNow}
		w := postTokenForm(t, s, form.Encode())
		assertClosedInvalidGrant(t, w)
		if q.calls != 0 || len(recorder.snapshot()) != 0 {
			t.Fatal("malformed credential reached a lookup or grant reservation")
		}
	}
}

type tokenAdmissionProbe struct {
	*RatePolicies
	addressAdmissions []bool
	grantAdmissions   int
	completions       int
}

func (p *tokenAdmissionProbe) AdmitToken(now time.Time, r *http.Request) (bool, int) {
	allowed, retry := p.RatePolicies.AdmitToken(now, r)
	p.addressAdmissions = append(p.addressAdmissions, allowed)
	return allowed, retry
}

func (p *tokenAdmissionProbe) AdmitGrant(id uuid.UUID, now time.Time) (grantAttempt, bool, int) {
	p.grantAdmissions++
	return p.RatePolicies.AdmitGrant(id, now)
}

func (p *tokenAdmissionProbe) FinishGrant(attempt grantAttempt, result grantAttemptResult) {
	p.completions++
	p.RatePolicies.FinishGrant(attempt, result)
}

type tokenRefusalFixture struct {
	codeFixture
	form       url.Values
	unknown    string
	wrongID    string
	credential string
	probe      *tokenAdmissionProbe
}

func newTokenRefusalFixture(t *testing.T, mode, credential string, overflow bool) *tokenRefusalFixture {
	t.Helper()
	f := &tokenRefusalFixture{codeFixture: newCodeFixture(t, cimdTestNow()), credential: credential}
	publicID := f.clientID.String()
	if mode != "dcr" {
		publicID = "https://client.example/" + f.clientID.String()
		if _, err := f.pool.Exec(t.Context(), "UPDATE oauth_clients SET metadata_url=$1 WHERE id=$2", publicID, f.clientID); err != nil {
			t.Fatal(err)
		}
	}
	f.s.cimdEnabled = mode == "cimd on"
	f.s.clock = func() time.Time { return f.now }
	f.form = url.Values{
		"grant_type": {"authorization_code"}, "client_id": {publicID},
		"code": {f.code}, "redirect_uri": {"http://127.0.0.1:20090/callback"}, "code_verifier": {f.verifier},
	}
	f.unknown = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{233}, 32))
	if credential == "refresh" {
		tokens, err := f.s.exchangeAuthorizationCode(t.Context(), f.form)
		if err != nil {
			t.Fatal(err)
		}
		f.form = url.Values{"grant_type": {"refresh_token"}, "client_id": {publicID}, "refresh_token": {tokens.RefreshToken}}
		f.unknown = refreshTokenPrefix + f.unknown
	}
	other, err := f.q.CreateOAuthClient(t.Context(), store.CreateOAuthClientParams{
		ClientName: "Mismatched client", RedirectURIs: []byte(`["https://other.example/callback"]`), CreatedAt: f.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.wrongID = other.ID.String()
	t.Cleanup(func() {
		if _, cleanupErr := f.q.DeleteOAuthClient(context.Background(), other.ID); cleanupErr != nil {
			t.Error(cleanupErr)
		}
	})
	policies, err := NewRatePolicies(testOAuthRateConfig())
	if err != nil {
		t.Fatal(err)
	}
	if overflow {
		policies.grants.maxKeys = 1
		capacityGrant, grantErr := f.q.UpsertOAuthGrant(t.Context(), store.UpsertOAuthGrantParams{
			UserID: f.userID, ClientID: other.ID, Scopes: "resumes:read", CreatedAt: f.now,
		})
		if grantErr != nil {
			t.Fatal(grantErr)
		}
		attempt, allowed, _ := policies.AdmitGrant(capacityGrant.ID, f.now)
		if !allowed {
			t.Fatal("capacity setup refused")
		}
		policies.FinishGrant(attempt, grantAttemptFailure)
	}
	f.probe = &tokenAdmissionProbe{RatePolicies: policies}
	f.s.tokenAdmission = f.probe
	return f
}

func copyTokenForm(form url.Values) url.Values {
	cloned := make(url.Values, len(form))
	for key, values := range form {
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}

func (f *tokenRefusalFixture) post(t *testing.T, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	r := rateRequest("/oauth/token", "application/x-www-form-urlencoded", form.Encode(), "8.8.8.8")
	w := httptest.NewRecorder()
	f.s.HandleToken(w, r)
	return w
}

func (f *tokenRefusalFixture) unknownForm(form url.Values) url.Values {
	cloned := copyTokenForm(form)
	key := "code"
	if f.credential == "refresh" {
		key = "refresh_token"
	}
	cloned.Set(key, f.unknown)
	return cloned
}

func assertClosedInvalidGrant(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusBadRequest || w.Body.String() != `{"error":"invalid_grant","error_description":"The request is invalid."}` {
		t.Fatal("response differs from the closed invalid_grant")
	}
	want := http.Header{"Content-Type": {"application/json"}, "Cache-Control": {"no-store"}}
	if !reflect.DeepEqual(w.Header(), want) {
		t.Fatal("invalid_grant headers expose admission state")
	}
}

func assertSameTokenResponse(t *testing.T, stored, unknown *httptest.ResponseRecorder) {
	t.Helper()
	if stored.Code != unknown.Code || !bytes.Equal(stored.Body.Bytes(), unknown.Body.Bytes()) || !reflect.DeepEqual(stored.Header(), unknown.Header()) {
		t.Fatal("stored and never-issued candidates have different responses")
	}
}

func (f *tokenRefusalFixture) snapshot(t *testing.T) string {
	t.Helper()
	var state string
	err := f.pool.QueryRow(t.Context(), `SELECT jsonb_build_object(
		'codes', (SELECT jsonb_agg(to_jsonb(c) ORDER BY c.id) FROM oauth_authorization_codes c WHERE grant_id=$1),
		'tokens', (SELECT jsonb_agg(to_jsonb(t) ORDER BY t.id) FROM oauth_tokens t WHERE grant_id=$1),
		'grant', (SELECT to_jsonb(g) FROM oauth_grants g WHERE id=$1),
		'epoch', (SELECT auth_epoch FROM users WHERE id=$2))::text`, f.grantID, f.userID).Scan(&state)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func (f *tokenRefusalFixture) exhaust(t *testing.T) {
	t.Helper()
	for range f.probe.grants.limit {
		attempt, allowed, _ := f.probe.RatePolicies.AdmitGrant(f.grantID, f.now)
		if !allowed {
			return
		}
		f.probe.RatePolicies.FinishGrant(attempt, grantAttemptFailure)
	}
}

func assertGrantThrottle(t *testing.T, w *httptest.ResponseRecorder) int {
	t.Helper()
	seconds, err := strconv.Atoi(w.Header().Get("Retry-After"))
	if w.Code != http.StatusTooManyRequests || err != nil || seconds <= 0 ||
		w.Body.String() != `{"error":"invalid_request","error_description":"The request is invalid."}` {
		t.Fatal("live authority did not receive the ordinary OAuth throttle")
	}
	return seconds
}

func TestTokenGrantRefusalDoesNotExposeCredentialExistence(t *testing.T) {
	for _, mode := range []string{"cimd on", "cimd off", "dcr"} {
		for _, overflow := range []bool{false, true} {
			for _, credential := range []string{"code", "refresh"} {
				t.Run(mode+"/"+credential+"/overflow="+strconv.FormatBool(overflow), func(t *testing.T) {
					f := newTokenRefusalFixture(t, mode, credential, overflow)
					invalid := copyTokenForm(f.form)
					if credential == "code" {
						invalid.Set("code_verifier", strings.Repeat("w", 43))
					} else {
						invalid.Set("client_id", f.wrongID)
					}
					before := f.snapshot(t)
					var baseline *httptest.ResponseRecorder
					for range 11 {
						stored := f.post(t, invalid)
						unknown := f.post(t, f.unknownForm(invalid))
						assertClosedInvalidGrant(t, stored)
						assertSameTokenResponse(t, stored, unknown)
						if baseline == nil {
							baseline = stored
						}
						assertSameTokenResponse(t, stored, baseline)
					}
					if len(f.probe.addressAdmissions) != 22 {
						t.Fatal("paired sequence did not contain twenty-two requests")
					}
					for _, admitted := range f.probe.addressAdmissions {
						if !admitted {
							t.Fatal("address refusal masked grant admission")
						}
					}
					if f.probe.grantAdmissions != 11 || f.probe.completions != 10 {
						t.Fatal("unknown or refused candidates created reservations")
					}
					bucket := f.probe.grants.entries[f.grantID]
					if overflow {
						if bucket != nil || len(f.probe.grants.entries) != 1 {
							t.Fatal("overflow or unknown candidates allocated a key")
						}
						bucket = &f.probe.grants.overflow
					} else if len(f.probe.grants.entries) != 1 || f.probe.grants.overflow.failures != 0 {
						t.Fatal("unknown candidates allocated grant state")
					}
					if bucket == nil || bucket.failures != 10 || len(bucket.pending) != 0 {
						t.Fatal("stored candidates did not retain exactly ten failures")
					}
					wait := assertGrantThrottle(t, f.post(t, f.form))
					if wait != 900 {
						t.Fatal("refusal changed the grant window")
					}
					if credential == "refresh" {
						omitted := copyTokenForm(f.form)
						omitted.Del("client_id")
						assertGrantThrottle(t, f.post(t, omitted))
					}
					if f.snapshot(t) != before || f.probe.completions != 10 {
						t.Fatal("refusal changed authority or finished a nonexistent reservation")
					}
					f.now = f.now.Add(time.Duration(wait) * time.Second)
					afterWait := f.post(t, f.form)
					if credential == "code" {
						assertClosedInvalidGrant(t, afterWait)
					} else if afterWait.Code != http.StatusOK {
						t.Fatal("still-live refresh did not recover after Retry-After")
					}
				})
			}
		}
	}
}

func (f *tokenRefusalFixture) makeInvalid(t *testing.T, reason string) url.Values {
	t.Helper()
	form := copyTokenForm(f.form)
	var statement string
	switch reason {
	case "wrong client":
		form.Set("client_id", f.wrongID)
	case "wrong redirect":
		form.Set("redirect_uri", "http://127.0.0.1:20091/callback")
	case "wrong verifier":
		form.Set("code_verifier", strings.Repeat("w", 43))
	case "wrong resource":
		form.Set("resource", "https://foreign.example/mcp")
	case "expired":
		if f.credential == "code" {
			f.now = f.now.Add(time.Minute)
		} else {
			f.now = f.now.Add(refreshFamilyTTL)
		}
	case "consumed":
		if _, err := f.s.exchangeAuthorizationCode(t.Context(), form); err != nil {
			t.Fatal(err)
		}
	case "superseded":
		if _, err := f.s.rotateRefreshToken(t.Context(), form.Get("refresh_token"), form["client_id"]); err != nil {
			t.Fatal(err)
		}
	case "revoked token":
		statement = "UPDATE oauth_tokens SET revoked_at=created_at WHERE grant_id=$1"
	case "revoked grant":
		statement = "UPDATE oauth_grants SET revoked_at=created_at WHERE id=$1"
	case "stale epoch":
		statement = "UPDATE users SET auth_epoch=auth_epoch+1 WHERE id=(SELECT user_id FROM oauth_grants WHERE id=$1)"
	case "changed scopes":
		statement = "UPDATE oauth_grants SET scopes='resumes:read resumes:write' WHERE id=$1"
	default:
		t.Fatal("unknown invalid-authority case")
	}
	if statement != "" {
		if _, err := f.pool.Exec(t.Context(), statement, f.grantID); err != nil {
			t.Fatal(err)
		}
	}
	return form
}

func TestTokenGrantRefusalKeepsTerminalAndWrongBindingErrors(t *testing.T) {
	for _, mode := range []string{"cimd on", "cimd off", "dcr"} {
		for _, credential := range []string{"code", "refresh"} {
			reasons := []string{"wrong client", "wrong resource", "expired", "revoked grant", "stale epoch"}
			if credential == "code" {
				reasons = append(reasons, "wrong redirect", "wrong verifier", "consumed", "changed scopes")
			} else {
				reasons = append(reasons, "superseded", "revoked token")
			}
			for _, reason := range reasons {
				for _, overflow := range []bool{false, true} {
					t.Run(mode+"/"+credential+"/"+reason+"/overflow="+strconv.FormatBool(overflow), func(t *testing.T) {
						f := newTokenRefusalFixture(t, mode, credential, overflow)
						form := f.makeInvalid(t, reason)
						f.exhaust(t)
						before := f.snapshot(t)
						assertSameTokenResponse(t, f.post(t, form), f.post(t, f.unknownForm(form)))
						assertClosedInvalidGrant(t, f.post(t, form))
						if f.snapshot(t) != before || f.probe.completions != 0 {
							t.Fatal("invalid-authority refusal mutated business state or grant debt")
						}
					})
				}
			}
		}
	}
}

func TestTokenGrantRefusalPreservesOtherErrorPrecedence(t *testing.T) {
	for _, credential := range []string{"code", "refresh"} {
		for _, failure := range []string{"missing field", "unknown field", "unknown client", "malformed client", "internal alias", "wrong resource"} {
			t.Run(credential+"/"+failure, func(t *testing.T) {
				f := newTokenRefusalFixture(t, "cimd off", credential, false)
				form := copyTokenForm(f.form)
				switch failure {
				case "missing field":
					if credential == "code" {
						form.Del("client_id")
					} else {
						form.Set("client_id", "")
					}
				case "unknown field":
					form.Set("client_secret", "ignored")
				case "unknown client":
					form.Set("client_id", "https://unknown.example/metadata")
				case "malformed client":
					form.Set("client_id", "https://invalid.example:443/metadata")
				case "internal alias":
					form.Set("client_id", f.clientID.String())
				case "wrong resource":
					form.Set("resource", "https://foreign.example/mcp")
				}
				before := f.snapshot(t)
				baseline := f.post(t, form)
				f.exhaust(t)
				refused := f.post(t, form)
				assertSameTokenResponse(t, baseline, refused)
				if refused.Header().Get("Retry-After") != "" || f.snapshot(t) != before {
					t.Fatal("refusal overrode ordinary error precedence or mutated authority")
				}
			})
		}
	}
}

type refusalLookupQueries struct {
	store.OAuthQueries
	code   store.OAuthAuthorizationCode
	token  store.GetOAuthTokenAuthorityByDigestRow
	client store.OAuthClient
}

func (q refusalLookupQueries) GetOAuthAuthorizationCodeByDigest(context.Context, []byte) (store.OAuthAuthorizationCode, error) {
	return q.code, nil
}

func (q refusalLookupQueries) GetOAuthTokenAuthorityByDigest(context.Context, []byte) (store.GetOAuthTokenAuthorityByDigestRow, error) {
	return q.token, nil
}

func (q refusalLookupQueries) GetDCRClient(context.Context, uuid.UUID) (store.OAuthClient, error) {
	return q.client, nil
}

func TestTokenGrantRefusalDatabaseFailureIsServerError(t *testing.T) {
	for _, credential := range []string{"code", "refresh"} {
		t.Run(credential, func(t *testing.T) {
			f := newTokenRefusalFixture(t, "dcr", credential, false)
			f.exhaust(t)
			f.s.queries = refusalLookupQueries{
				OAuthQueries: f.q, client: store.OAuthClient{ID: f.clientID},
				code:  store.OAuthAuthorizationCode{GrantID: f.grantID},
				token: store.GetOAuthTokenAuthorityByDigestRow{OAuthToken: store.OAuthToken{GrantID: f.grantID, Kind: "refresh"}},
			}
			// The admission lookup succeeds, but the validation transaction cannot begin.
			failedPool, err := store.NewPool(t.Context(), testutil.RequireMigratedTestDatabaseURL(t))
			if err != nil {
				t.Fatal(err)
			}
			failedPool.Close(t.Context())
			f.s.pool = failedPool
			before := f.snapshot(t)
			w := f.post(t, f.form)
			if w.Code != http.StatusInternalServerError || w.Header().Get("Retry-After") != "" ||
				w.Body.String() != `{"error":"server_error","error_description":"The server encountered an error."}` {
				t.Fatal("validation database failure exposed authority or grant state")
			}
			if f.probe.completions != 0 || f.snapshot(t) != before {
				t.Fatal("database failure mutated authority or finished a nonexistent reservation")
			}
		})
	}
}

func runRefusalRequest(ctx context.Context, s *Service, form url.Values) <-chan *httptest.ResponseRecorder {
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		r := httptest.NewRequestWithContext(ctx, http.MethodPost, "https://aboutme.example/oauth/token", strings.NewReader(form.Encode()))
		r.RemoteAddr = "127.0.0.1:443"
		r.Header.Set(api.TrustedClientIPHeader, "8.8.8.8")
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		s.HandleToken(w, r)
		result <- w
	}()
	return result
}

func awaitRefusalResult(t *testing.T, result <-chan *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case w := <-result:
		return w
	case <-time.After(2 * time.Second):
		t.Fatal("token request did not finish")
		return nil
	}
}

func lockRefusalUser(t *testing.T, f *tokenRefusalFixture) (pgx.Tx, int32) {
	t.Helper()
	tx, err := f.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rollbackTestTx(t, tx) })
	if _, lockErr := tx.Exec(t.Context(), "SELECT id FROM users WHERE id=$1 FOR UPDATE", f.userID); lockErr != nil {
		t.Fatal(lockErr)
	}
	var pid int32
	if pidErr := tx.QueryRow(t.Context(), "SELECT pg_backend_pid()").Scan(&pid); pidErr != nil {
		t.Fatal(pidErr)
	}
	return tx, pid
}

func TestTokenGrantRefusalRechecksConcurrentConsumeAndRotation(t *testing.T) {
	for _, credential := range []string{"code", "refresh"} {
		t.Run(credential, func(t *testing.T) {
			f := newTokenRefusalFixture(t, "dcr", credential, false)
			f.exhaust(t)
			issuer := *f.s
			issuer.tokenAdmission = &registrationAdmissionFake{allowed: true}
			lock, pid := lockRefusalUser(t, f)
			issued := runRefusalRequest(t.Context(), &issuer, f.form)
			waitForBlockedTransactions(t, f.pool, pid, 1)
			refused := runRefusalRequest(t.Context(), f.s, f.form)
			waitForBlockedDescendants(t, f.pool, pid, 2)
			if err := lock.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}
			if awaitRefusalResult(t, issued).Code != http.StatusOK {
				t.Fatal("concurrent credential mutation failed")
			}
			assertClosedInvalidGrant(t, awaitRefusalResult(t, refused))
			var revoked int
			if err := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM oauth_tokens WHERE grant_id=$1 AND revoked_at IS NOT NULL", f.grantID).Scan(&revoked); err != nil {
				t.Fatal(err)
			}
			if revoked != 0 || f.probe.completions != 0 {
				t.Fatal("refused replay revoked a family or finished a reservation")
			}
		})
	}
}

func TestTokenGrantRefusalRechecksConcurrentRevocationAndEpoch(t *testing.T) {
	for _, credential := range []string{"code", "refresh"} {
		for _, change := range []string{"revocation", "epoch"} {
			t.Run(credential+"/"+change, func(t *testing.T) {
				f := newTokenRefusalFixture(t, "dcr", credential, false)
				f.exhaust(t)
				lock, pid := lockRefusalUser(t, f)
				refused := runRefusalRequest(t.Context(), f.s, f.form)
				waitForBlockedTransactions(t, f.pool, pid, 1)
				statement := "UPDATE oauth_grants SET revoked_at=created_at WHERE id=$1"
				if change == "epoch" {
					statement = "UPDATE users SET auth_epoch=auth_epoch+1 WHERE id=(SELECT user_id FROM oauth_grants WHERE id=$1)"
				}
				if _, err := lock.Exec(t.Context(), statement, f.grantID); err != nil {
					t.Fatal(err)
				}
				if err := lock.Commit(t.Context()); err != nil {
					t.Fatal(err)
				}
				before := f.snapshot(t)
				assertClosedInvalidGrant(t, awaitRefusalResult(t, refused))
				if f.snapshot(t) != before || f.probe.completions != 0 {
					t.Fatal("authority-change refusal mutated business state or grant debt")
				}
			})
		}
	}
}

func TestTokenGrantRefusalDatabaseLockFailureIsServerError(t *testing.T) {
	for _, credential := range []string{"code", "refresh"} {
		t.Run(credential, func(t *testing.T) {
			f := newTokenRefusalFixture(t, "dcr", credential, false)
			f.exhaust(t)
			_, pid := lockRefusalUser(t, f)
			before := f.snapshot(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			refused := runRefusalRequest(ctx, f.s, f.form)
			waitForBlockedTransactions(t, f.pool, pid, 1)
			cancel()
			w := awaitRefusalResult(t, refused)
			if w.Code != http.StatusInternalServerError || w.Header().Get("Retry-After") != "" ||
				w.Body.String() != `{"error":"server_error","error_description":"The server encountered an error."}` {
				t.Fatal("validation lock failure exposed authority or grant state")
			}
			if f.probe.completions != 0 || f.snapshot(t) != before {
				t.Fatal("validation lock failure mutated authority or finished a reservation")
			}
		})
	}
}

func TestTokenWrongStoredKindNeverReservesOrRotates(t *testing.T) {
	for _, mode := range []string{"cimd on", "cimd off", "dcr"} {
		t.Run(mode, func(t *testing.T) {
			f := newTokenRefusalFixture(t, mode, "code", false)
			tokens, err := f.s.exchangeAuthorizationCode(t.Context(), f.form)
			if err != nil {
				t.Fatal(err)
			}
			before := f.snapshot(t)
			// A refresh spelling over an issued access digest has the wrong stored kind.
			raw := refreshTokenPrefix + strings.TrimPrefix(tokens.AccessToken, accessTokenPrefix)
			form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {raw}, "client_id": {f.form.Get("client_id")}}
			assertClosedInvalidGrant(t, f.post(t, form))
			form.Del("client_id")
			assertClosedInvalidGrant(t, f.post(t, form))
			if f.snapshot(t) != before || f.probe.grantAdmissions != 0 || f.probe.completions != 0 ||
				len(f.probe.grants.entries) != 0 || f.probe.grants.overflow.failures != 0 || len(f.probe.grants.overflow.pending) != 0 {
				t.Fatal("wrong stored kind created authority or grant debt")
			}
		})
	}
}
