// Adversarial, spec-derived tests for the Google OIDC login callback
// (task-4-brief.md Step 3), originally authored independently -- from the
// brief, the design spec (docs/specs/aboutme-design.md §3 "OAuth"), and the
// packages it names as allowed reads (internal/auth's
// TransactionStore/cookie helpers, Task 2; internal/auth/oidctest, Task 3;
// internal/user; internal/store; internal/config) -- WITHOUT reading
// internal/auth/google.go or handlers.go, neither of which existed at
// authorship time.
//
// Reconciled against the landed implementation (commit 0fca4fa,
// "feat(auth): add Google OIDC login"; integrated at 7adfcb5) for exactly
// one seam -- the service-construction ADAPT marker -- plus mechanical
// helper collisions with the implementer's own google_test.go/
// handlers_test.go (doGet, extractCookie, mustQueryParam, newTestService,
// withGoogleIssuer, ptrTrue): this file reuses all of those rather than
// duplicating them. No test assertion or scenario was weakened in
// reconciliation; see notes.md's integration report for the one place a
// genuinely new (not colliding) helper -- doGetCaptureBody -- was added,
// because the shared doGet deliberately discards the response body and one
// assertion here needs it.
//
// Scope: every test here drives GET /api/v1/auth/google/start then
// GET /api/v1/auth/google/callback through the same http.Handler
// google_test.go/handlers_test.go build (api.New + Service.RegisterRoutes),
// not a bypass. It covers the brief's required six-row table (unverified
// email; wrong issuer; wrong audience; tampered signature; nonce mismatch
// -- go-oidc does NOT check this one, making it the highest-value
// regression test here; expired id_token) plus three spec-implied
// strengthenings: state-mismatch and missing-tx-cookie are rejected the
// same way any other invalid transaction is, and the five OIDC-
// verification failure classes are indistinguishable to the browser (no
// oracle), mirroring this phase's already-established
// TestConsume_NoOracleAcrossFailureModes contract for
// TransactionStore.Consume (transaction_adversarial_test.go). Binding
// rulings DD-C3 (generic ?error=auth_failed for every rejection not
// plan-pinned; email_not_verified stays distinct) and DD-C4 (302 for every
// callback rejection; __Host-oauth-tx always cleared; __Host-session never
// set on a rejection) both hold throughout -- the implementation's
// authFailedErrorCode ("auth_failed", handlers.go) and its single
// redirectAuthFailed/redirectWithError funnel match them exactly.
//
// Does NOT cover: the happy path (new-user create, existing-identity
// login -- google_test.go's job), email-collision rejection (Task 10's
// job; resolveGoogleUser is still Task 4's documented stub), router-level
// concerns (router_test.go's job), or GitHub/LinkedIn (Task 5/6's own
// copies of this same table against their own provider mechanics).
package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/auth/oidctest"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// ptrFalse is ptrTrue's (google_test.go) missing counterpart -- not
// defined anywhere else in this package, so it stays here rather than
// colliding with an existing helper.
func ptrFalse() *bool {
	b := false
	return &b
}

// generateOtherRSAKey returns a fresh RSA-2048 key distinct from any
// oidctest.Provider's own key, for TestGoogleCallback_RejectsTamperedSignature.
func generateOtherRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating test RSA key: %v", err)
	}
	return key
}

// doGetCaptureBody is doGet's sibling for the one assertion in this file
// that needs the response body
// (TestGoogleCallback_OIDCFailures_NoOracleAcrossFailureModes's leak/
// equality check). The shared doGet (handlers_test.go) deliberately
// discards the body unread -- every other test in this package only needs
// headers/cookies -- so it cannot be reused for that one check without
// losing the capability; every other test in this file uses the shared
// doGet unchanged.
func doGetCaptureBody(t *testing.T, handler http.Handler, path string, cookies ...*http.Cookie) (*http.Response, string) {
	t.Helper()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("read response body: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("close response body: %v", err)
	}
	return resp, string(body)
}

// ==== flow helpers ====

// beginGoogle drives GET auth.GoogleStartPath (via the shared doGet) and
// returns the __Host-oauth-tx cookie it set, the state query param, and
// the nonce query param from its redirect Location. The nonce is generated
// server-side (Begin) and is otherwise invisible to a test -- it is never
// echoed back through a cookie, only embedded in the provider authorize
// URL exactly like state (oidc.Nonce is an oauth2.AuthCodeOption, the same
// mechanism state itself uses; this is how a real OIDC client recovers it
// too, not a test-only backdoor). Tests that need every OTHER verification
// dimension to succeed so they can isolate one specific failure (e.g.
// TestGoogleCallback_RejectsUnverifiedEmail must get past nonce/issuer/
// audience/signature/expiry checks to actually reach the email_verified
// branch) register their oidctest.Claims with this real nonce;
// TestGoogleCallback_RejectsNonceMismatch is the deliberate exception.
func beginGoogle(t *testing.T, handler http.Handler) (txCookie *http.Cookie, state, nonce string) {
	t.Helper()

	start := doGet(t, handler, auth.GoogleStartPath) //nolint:bodyclose // doGet (handlers_test.go) closes the body itself before returning.
	if start.StatusCode != http.StatusFound {
		t.Fatalf("GET %s status = %d, want %d (a redirect to the provider)", auth.GoogleStartPath, start.StatusCode, http.StatusFound)
	}

	txCookie = extractCookie(start, auth.OAuthTxCookieName)
	if txCookie == nil {
		t.Fatalf("GET %s did not set the %s cookie", auth.GoogleStartPath, auth.OAuthTxCookieName)
	}

	loc := start.Header.Get("Location")
	state = mustQueryParam(t, loc, "state")
	nonce = mustQueryParam(t, loc, "nonce")

	return txCookie, state, nonce
}

// doCallback drives GET auth.GoogleCallbackPath?code=...&state=... (via
// the shared doGet), attaching cookies (typically just the __Host-oauth-tx
// cookie beginGoogle returned; omitted entirely for
// TestGoogleCallback_RejectsMissingTxCookie's case).
func doCallback(t *testing.T, handler http.Handler, code, state string, cookies ...*http.Cookie) *http.Response {
	t.Helper()

	path := auth.GoogleCallbackPath + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
	return doGet(t, handler, path, cookies...) //nolint:bodyclose // doGet (handlers_test.go) closes the body itself before returning.
}

// ==== shared assertions ====

// assertRejected asserts every property a /callback rejection must have
// regardless of *why* it was rejected (DD-C4):
//
//   - a 302 redirect, never a bare 200 or a raw JSON error body -- every
//     sibling task in this phase (task-5/6/10-brief.md) pins /callback
//     rejections to exactly this shape, reasoning that /callback is a
//     top-level browser navigation, not an API call a JS client parses;
//   - a non-empty ?error= query param on the Location so the frontend has
//     something to show the visitor;
//   - no __Host-session cookie -- a rejected callback must never leave the
//     visitor authenticated;
//   - the __Host-oauth-tx cookie cleared exactly the way
//     auth.ClearOAuthTxCookie does (empty value, negative Max-Age) -- DD-C4
//     requires clearing it on every failure path so a dead transaction
//     cookie never lingers in the browser.
//
// Returns the extracted error code for callers that want to assert
// something about it specifically (the email_not_verified pin, or the
// no-oracle comparison across the five OIDC verification failure classes).
func assertRejected(t *testing.T, resp *http.Response) (errorCode string) {
	t.Helper()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want %d (every /callback rejection is a redirect, never a raw JSON error or a 200)", resp.StatusCode, http.StatusFound)
	}

	loc := resp.Header.Get("Location")
	errorCode = mustQueryParam(t, loc, "error")
	assertRedirectPath(t, loc, "/login") // DD-C7: every rejection targets PublicOrigin+"/login", never the bare "/"

	if sc := extractCookie(resp, auth.SessionCookieName); sc != nil {
		t.Errorf("response set a %s cookie on a rejected callback (value=%q), want none -- a rejected callback must never authenticate the visitor", auth.SessionCookieName, sc.Value)
	}

	tx := extractCookie(resp, auth.OAuthTxCookieName)
	if tx == nil {
		t.Fatalf("response did not clear the %s cookie on a rejected callback (no Set-Cookie for it at all) -- DD-C4 requires ClearOAuthTxCookie on every failure path", auth.OAuthTxCookieName)
	}
	if tx.MaxAge >= 0 {
		t.Errorf("%s cookie MaxAge = %d on a rejected callback, want negative (cleared, matching auth.ClearOAuthTxCookie)", auth.OAuthTxCookieName, tx.MaxAge)
	}
	if tx.Value != "" {
		t.Errorf("%s cookie Value = %q on a rejected callback, want empty (cleared)", auth.OAuthTxCookieName, tx.Value)
	}

	return errorCode
}

// assertNoIdentity asserts no identities row exists for
// (provider=google, provider_user_id=providerUserID) -- the row a
// successful callback would have created.
func assertNoIdentity(t *testing.T, q *store.Queries, providerUserID string) {
	t.Helper()

	_, err := q.GetIdentityByProviderSubject(context.Background(), store.GetIdentityByProviderSubjectParams{
		Provider:       string(auth.ProviderGoogle),
		ProviderUserID: providerUserID,
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetIdentityByProviderSubject(google, %q) error = %v, want pgx.ErrNoRows (no identity row may be created on a rejected callback)", providerUserID, err)
	}
}

// assertNoUser asserts no users row exists with the given email -- the row
// a successful new-user callback would have created.
func assertNoUser(t *testing.T, q *store.Queries, email string) {
	t.Helper()

	_, err := q.GetUserByEmail(context.Background(), email)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetUserByEmail(%q) error = %v, want pgx.ErrNoRows (no user row may be created on a rejected callback)", email, err)
	}
}

// uniqueSubject and uniqueEmail generate collision-proof identifiers so
// each test's assertNoIdentity/assertNoUser check is unambiguous even
// though every test in this file shares one live TEST_DATABASE_URL
// (matching createTestUser's convention in transaction_test.go).
func uniqueSubject(t *testing.T) string {
	t.Helper()
	return "g-sub-" + uuid.NewString()
}

func uniqueEmail(t *testing.T) string {
	t.Helper()
	return uuid.NewString() + "@example.com"
}

// ==== task-4-brief.md Step 3's required six-row table ====

// TestGoogleCallback_RejectsUnverifiedEmail is the brief's first required
// row: design spec §3 pins Google's rule as "require email_verified ==
// true", and the error code itself (not just rejection) is plan-pinned and
// distinct from DD-C3's generic auth_failed -- handlers.go's
// emailNotVerifiedErrorCode confirms the literal.
func TestGoogleCallback_RejectsUnverifiedEmail(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginGoogle(t, handler)
	p.RegisterCode("code-unverified", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrFalse(),
		Nonce:         nonce, // must pass every OTHER check so only email_verified fails
	})

	resp := doCallback(t, handler, "code-unverified", state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	errorCode := assertRejected(t, resp)
	if errorCode != "email_not_verified" {
		t.Errorf("error code = %q, want %q (design spec §3: Google requires email_verified == true)", errorCode, "email_not_verified")
	}

	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestGoogleCallback_RejectsWrongIssuer proves go-oidc's own issuer
// verification -- not application code -- rejects a token whose "iss"
// doesn't match the discovered provider, and that the rejection carries
// this file's full failure-path contract (no session cookie, cleared tx
// cookie, no rows created). oidctest_test.go's TestClaims_IssuerOverride
// already proves the raw go-oidc verifier rejects this shape directly
// against the mock; this test proves the Service's own callback handler
// surfaces that rejection correctly end-to-end over HTTP.
func TestGoogleCallback_RejectsWrongIssuer(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginGoogle(t, handler)
	p.RegisterCode("code-wrong-issuer", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
		Issuer:        "https://evil.example",
	})

	resp := doCallback(t, handler, "code-wrong-issuer", state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestGoogleCallback_RejectsWrongAudience proves go-oidc rejects a token
// whose "aud" doesn't match the Service's configured GoogleClientID.
func TestGoogleCallback_RejectsWrongAudience(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginGoogle(t, handler)
	p.RegisterCode("code-wrong-audience", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
		Audience:      "some-other-client-id",
	})

	resp := doCallback(t, handler, "code-wrong-audience", state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestGoogleCallback_RejectsTamperedSignature proves go-oidc rejects a
// token signed with a key other than the one the provider published at
// /jwks.json -- the harness's stand-in for a forged or tampered id_token.
func TestGoogleCallback_RejectsTamperedSignature(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginGoogle(t, handler)
	p.RegisterCode("code-tampered-signature", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
		SigningKey:    generateOtherRSAKey(t),
	})

	resp := doCallback(t, handler, "code-tampered-signature", state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestGoogleCallback_RejectsNonceMismatch is the highest-value regression
// test in this table (task-4-brief.md Step 3): go-oidc's Verify never
// inspects the "nonce" claim at all (oidctest_test.go's
// TestProvider_NonceRoundTrips doc comment; confirmed against go-oidc's
// own verify.go, and against handlers.go's own comment: "go-oidc does NOT
// check nonce automatically"), so a token whose nonce doesn't match the
// one Begin generated and stored server-side (transaction.go's
// Transaction.Nonce) is only caught by handleGoogleCallback's manual
// `idToken.Nonce != tx.Nonce` comparison -- exactly the kind of check an
// implementer can silently forget, since every other check in this table
// fails "for free" via go-oidc. This test deliberately discards
// beginGoogle's real nonce and registers a fixed, unrelated one instead:
// with overwhelming probability (the real nonce is 32 crypto/rand bytes)
// that is not the one Begin actually generated for this transaction, which
// is exactly the mismatch under test.
func TestGoogleCallback_RejectsNonceMismatch(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, state, _ := beginGoogle(t, handler) // the real nonce is deliberately discarded below
	p.RegisterCode("code-nonce-mismatch", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         "wrong-nonce-does-not-match-the-real-transaction",
	})

	resp := doCallback(t, handler, "code-nonce-mismatch", state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestGoogleCallback_RejectsExpiredIDToken proves go-oidc rejects a token
// whose "exp" is already in the past. oidctest's
// TestProvider_ExpiresIn_IndependentOfIDTokenExpiry (Task 3) already
// guards that the mock's OAuth2 access-token expires_in can't
// accidentally launder this past exp at the token-exchange layer, so a
// rejection observed here can only be go-oidc's own id_token expiry check.
func TestGoogleCallback_RejectsExpiredIDToken(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginGoogle(t, handler)
	p.RegisterCode("code-expired", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
		ExpiresAt:     time.Now().Add(-1 * time.Hour),
	})

	resp := doCallback(t, handler, "code-expired", state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// ==== strengthenings the spec implies but the brief's table doesn't spell out ====

// TestGoogleCallback_RejectsStateMismatch: the state query param the
// provider echoes back must match the state Begin stored in the
// transaction (transaction.go's Transaction.State) even when the tx cookie
// itself is present and valid and the code would otherwise exchange
// successfully -- otherwise an attacker who can get their own
// authorization code accepted by the victim's browser (e.g. via a
// captured/replayed redirect) could bind it to the victim's transaction.
// This is a distinct check from the __Host-oauth-tx cookie's own CSRF
// protection (RFC 6749 §10.12's classic state-parameter defense, checked
// explicitly by handleGoogleCallback), not a restatement of it.
func TestGoogleCallback_RejectsStateMismatch(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, _, nonce := beginGoogle(t, handler) // deliberately discard the real state
	p.RegisterCode("code-state-mismatch", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
	})

	resp := doCallback(t, handler, "code-state-mismatch", "attacker-supplied-wrong-state", txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestGoogleCallback_RejectsMissingTxCookie: a /callback request that
// carries a plausible code and state but no __Host-oauth-tx cookie at all
// (e.g. a bookmarked/replayed callback URL, or a cookie dropped by a
// privacy setting) must be rejected the same way any other invalid
// transaction is -- never treated as "no transaction to check against, so
// skip straight to token exchange".
func TestGoogleCallback_RejectsMissingTxCookie(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	_, state, nonce := beginGoogle(t, handler) // begin a real transaction, but never attach its cookie below
	p.RegisterCode("code-missing-cookie", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
	})

	resp := doCallback(t, handler, "code-missing-cookie", state) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestGoogleCallback_OIDCFailures_NoOracleAcrossFailureModes strengthens
// TestGoogleCallback_Rejects{WrongIssuer,WrongAudience,TamperedSignature,
// NonceMismatch,ExpiredIDToken} together: it isn't enough that each
// independently redirects with a non-empty ?error= code -- the code (and
// the response body) must also be indistinguishable across all five
// distinct OIDC verification failures. This mirrors the phase's
// already-established "no oracle" contract for TransactionStore.Consume's
// ErrTransactionInvalid (transaction_adversarial_test.go's
// TestConsume_NoOracleAcrossFailureModes, task-2-brief.md) and is exactly
// what DD-C3 requires: a generic OIDC failure that instead surfaced, say,
// "?error=signature_invalid" only for the tampered-key case would hand an
// attacker a working oracle to fingerprint exactly which defense they
// still need to defeat.
func TestGoogleCallback_OIDCFailures_NoOracleAcrossFailureModes(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL))

	otherKey := generateOtherRSAKey(t)

	// Each case builds its Claims from the real nonce beginGoogle returns
	// (the same "recover it from the redirect Location" mechanism
	// beginGoogle's own doc comment explains) EXCEPT "nonce mismatch",
	// which deliberately ignores it -- matching
	// TestGoogleCallback_RejectsNonceMismatch's own approach, so that every
	// other case in this table fails on exactly one dimension (issuer/
	// audience/signature/expiry) rather than accidentally also failing
	// nonce verification first and masking which check actually fired.
	cases := map[string]func(nonce string) oidctest.Claims{
		"wrong issuer": func(nonce string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: nonce, Issuer: "https://evil.example",
			}
		},
		"wrong audience": func(nonce string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: nonce, Audience: "some-other-client-id",
			}
		},
		"tampered signature": func(nonce string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: nonce, SigningKey: otherKey,
			}
		},
		"nonce mismatch": func(string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: "wrong-nonce-does-not-match-the-real-transaction",
			}
		},
		"expired id token": func(nonce string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: nonce, ExpiresAt: time.Now().Add(-1 * time.Hour),
			}
		},
	}

	errorCodes := make(map[string]string, len(cases))
	bodies := make(map[string]string, len(cases))
	for name, buildClaims := range cases {
		code := "code-oracle-" + name

		txCookie, state, nonce := beginGoogle(t, handler)
		p.RegisterCode(code, buildClaims(nonce))

		path := auth.GoogleCallbackPath + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
		resp, body := doGetCaptureBody(t, handler, path, txCookie) //nolint:bodyclose // doGetCaptureBody closes the body itself before returning.

		errorCodes[name] = assertRejected(t, resp)
		bodies[name] = body
	}

	var firstName, firstCode, firstBody string
	for name, code := range errorCodes {
		if firstName == "" {
			firstName, firstCode, firstBody = name, code, bodies[name]
			continue
		}
		if code != firstCode {
			t.Errorf("error code for %q = %q, want %q (same as %q) -- distinct OIDC failure classes must not be distinguishable to the browser", name, code, firstCode, firstName)
		}
		if bodies[name] != firstBody {
			t.Errorf("response body for %q differs from %q's -- distinct OIDC failure classes must not produce distinguishable response bodies", name, firstName)
		}
	}

	if firstCode == "email_not_verified" {
		t.Error("OIDC verification failures reused the email_not_verified error code, want a distinct generic code -- these five are cryptographic/protocol failures, not the email_verified claim check, and conflating them would misreport the failure to the visitor")
	}

	// Defense in depth beyond the cross-case body-equality check above: no
	// individual body should leak which specific OIDC check failed, even
	// if (contrary to the check above) every body happened to still agree
	// with each other.
	forbidden := []string{"issuer", "audience", "signature", "nonce", "expired", "expire"}
	for name, body := range bodies {
		lower := strings.ToLower(body)
		for _, word := range forbidden {
			if strings.Contains(lower, word) {
				t.Errorf("%s: response body contains %q, want no OIDC-verification internals leaked to the browser (body=%q)", name, word, body)
			}
		}
	}
}
