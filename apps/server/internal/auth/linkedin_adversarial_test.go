// Adversarial, spec-derived tests for the LinkedIn OIDC login callback
// (task-5-brief.md, AC-AUTH-002 "LinkedIn registration without verified
// email rejected"), originally authored independently -- from
// task-5-brief.md, the design spec (docs/specs/aboutme-design.md §3
// "OAuth"), and the packages it names as allowed reads (internal/auth's
// TransactionStore/cookie/session-cookie helpers, oidctest, the committed
// Google handler files that define the shared callback pattern,
// internal/store, internal/config, internal/testutil) -- WITHOUT reading
// internal/auth/linkedin.go or any Task 5 author test, neither of which
// existed at authorship time.
//
// Reconciled against the landed implementation (LinkedIn merged at
// ee315b0/97005f3, "Merge branch 'task-5-linkedin'"; this checkout at
// cc46948, which also includes GitHub login and shared-funnel hardening)
// for:
//
//   - both ADAPT markers, exactly as guessed: auth.LinkedInStartPath/
//     LinkedInCallbackPath (handlers.go) and the withLinkedInIssuer test
//     option (handlers_test.go) landed with the identical names this file
//     assumed;
//   - mechanical helper collisions with the implementer's own
//     linkedin_test.go, which independently defined four helpers under
//     the exact same names this file also (independently) chose:
//     uniqueLinkedInSubject, beginLinkedIn, doLinkedInCallback,
//     assertNoLinkedInIdentity. This file's own copies were deleted in
//     favor of theirs (identical bodies in every case); see notes.md's
//     integration report;
//   - a genuine TEST NAME collision, not just a helper one:
//     linkedin_test.go's own TestLinkedInCallback_RegistrationEmailRule
//     (its Step 1 TDD test, written from the same brief pseudocode this
//     file also started from) is the exact same Go identifier this file
//     originally used for its own, independently-authored version of the
//     same four-case matrix. Renamed this file's copy to
//     TestLinkedInCallback_RegistrationEmailRuleAdversarial to keep BOTH
//     -- two independently-derived tests of AC-AUTH-002's core rule are
//     the point of the phase's adversarial-review workflow, not
//     redundancy to collapse into one;
//   - newTestService's guard (handlers_test.go): it t.Fatals unless at
//     least one of withGoogleIssuer/withGitHubEndpoint is also supplied,
//     even for a LinkedIn-only test (linkedin_test.go's own tests already
//     work around this by passing withGoogleIssuer(p.URL) alongside
//     withLinkedInIssuer(p.URL), pointed at the same in-process provider,
//     since a LinkedIn-only test never actually dials Google). Every
//     newTestService call in this file now does the same;
//   - reused beginLinkedInTransaction (linkedin_test.go) in place of this
//     file's own inline TransactionStore.Begin call in the Step 2 link
//     carve-out test -- equivalent glue, not a scenario change.
//
// No test assertion or scenario was weakened in reconciliation. Every
// change above is glue (names, construction plumbing) or a rename forced
// by a Go duplicate-declaration error; see notes.md's integration report
// for the full list.
//
// Landed behavior facts this reconciliation encodes (binding rulings
// applied after this file's original authorship):
//
//   - DD-C7: every /callback REJECTION redirects to PublicOrigin+"/login"
//     (not the bare "/"), carrying ?error=<code> -- already asserted by
//     the shared assertRejected helper (google_adversarial_test.go, now
//     itself updated to check this), reused unchanged by every rejection
//     test in this file;
//   - DD-C12 (interim, pending Task 10): an unclaimed LinkedIn identity
//     plus purpose=link attaches to the linking user via CreateIdentity,
//     never creating a new user row -- exactly this file's own Step 2
//     carve-out test's expectation, now reconciled to call the
//     implementer's own beginLinkedInTransaction helper;
//   - the provider access_denied -> cancelledErrorCode mapping (handlers.go;
//     mentioned as "in-flight" at original authorship) is landed,
//     unchanged from what this file already asserted.
//
// Scope: every test here drives GET /api/v1/auth/linkedin/start then
// GET /api/v1/auth/linkedin/callback through the real http.Handler
// newTestService builds (api.New + Service.RegisterRoutes), not a bypass
// -- except TestLinkedInCallback_PurposeLink_AllowsUnverifiedEmail, which
// constructs its transaction directly via TransactionStore.Begin (via the
// shared beginLinkedInTransaction helper) per the brief's own carve-out
// instruction, since the authenticated "start a link" HTTP surface
// belongs to Task 10.
//
// It covers task-5-brief.md's three steps:
//
//   - Step 1's four-case registration email-rule matrix (the heart of
//     AC-AUTH-002), independently re-derived alongside (not instead of)
//     linkedin_test.go's own copy -- see the rename note above;
//   - Step 2's linking carve-out: purpose=link with EmailVerified: nil
//     must still succeed and attach the identity to the existing user;
//   - Step 3's standard OIDC adversarial matrix (wrong issuer/audience/
//     signature/nonce/expiry) plus spec-implied strengthenings
//     (state-mismatch, missing-tx-cookie, cross-failure no-oracle
//     equality, and the LinkedIn-specific access_denied -> cancelledErrorCode
//     check) mirroring google_adversarial_test.go's own table -- genuinely
//     new coverage: linkedin_test.go's own doc comment explicitly defers
//     these to "a separate, independently authored suite" (i.e. this
//     file).
//
// Does NOT cover: GitHub (Task 6's own copy of this table), the full
// Task 10 three-way login-resolution branch (NewUser | ExistingIdentity |
// EmailCollision) beyond the matrix's own new-user creation case,
// router-level concerns, or linkedin_test.go's own DD-C12
// account-mismatch/missing-linking-user safety-net tests (complementary,
// not duplicated here).
package auth_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/auth/oidctest"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// assertLinkedInLoginAccepted asserts the properties a *successful*
// registration/login/link callback must have: the exact
// PublicOrigin+"/" redirect target (DD-C7 pins this apart from a
// rejection's PublicOrigin+"/login", never the same literal), a
// non-empty __Host-session cookie, and the __Host-oauth-tx cookie
// cleared exactly like a rejection also requires (ruling 1 applies to
// every exit path, not just rejections). Not a mechanical collision with
// anything in linkedin_test.go (which inlines its own equivalent checks
// at each of its three success-path call sites instead of a shared
// helper) -- kept as a genuinely novel, non-colliding helper.
func assertLinkedInLoginAccepted(t *testing.T, resp *http.Response) {
	t.Helper()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want %d (redirect on a successful login)", resp.StatusCode, http.StatusFound)
	}
	if got := resp.Header.Get("Location"); got != testPublicOrigin+"/" {
		t.Errorf("callback Location = %q, want %q (DD-C7: a successful callback redirects to the bare origin, never /login)", got, testPublicOrigin+"/")
	}

	sessionCookie := extractCookie(resp, auth.SessionCookieName)
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("callback response missing a non-empty __Host-session cookie on a successful login")
	}

	tx := extractCookie(resp, auth.OAuthTxCookieName)
	if tx == nil {
		t.Fatal("callback response missing a __Host-oauth-tx Set-Cookie header clearing it on success (ruling 1)")
	}
	if tx.MaxAge >= 0 {
		t.Errorf("__Host-oauth-tx cookie MaxAge = %d on a successful callback, want negative (cleared)", tx.MaxAge)
	}
}

// ==== task-5-brief.md Step 1: the registration email-rule matrix -- the
// heart of AC-AUTH-002 ====

// TestLinkedInCallback_RegistrationEmailRuleAdversarial is task-5-brief.md
// Step 1's required table, implemented per its own pseudocode with two
// deliberate adaptations from the brief's illustrative snippet, both to
// keep every subtest's assertions unambiguous against this package's
// shared, never-reset live TEST_DATABASE_URL (the same reasoning
// google_adversarial_test.go's uniqueSubject/uniqueEmail doc comments
// already establish for this file's sibling suite):
//
//   - the brief's pseudocode reuses the literal "a@example.com" across
//     three of its four cases; this test instead generates a fresh
//     uniqueEmail(t) per case, so a rejected case's assertNoUser check is
//     never confused by an earlier case (e.g. "verified email present")
//     having already inserted that same literal address;
//   - a fresh oidctest.Provider and a fresh uniqueLinkedInSubject(t) per
//     case, exactly as the brief's own comment instructs ("fresh oidctest
//     provider + fresh subject per case (avoid identities collision
//     across subtests)").
//
// Named ...Adversarial (not the brief's literal
// TestLinkedInCallback_RegistrationEmailRule) because linkedin_test.go's
// own Step 1 TDD test already claims that exact identifier -- see this
// file's header comment. Scenario and assertions are otherwise unchanged
// from original, independent authorship. See notes.md's ambiguities
// section for the full reasoning.
func TestLinkedInCallback_RegistrationEmailRuleAdversarial(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		emailPresent  bool
		emailVerified *bool // nil = claim absent -- never treated as true
		wantCreated   bool
	}{
		{"verified email present", true, ptrTrue(), true},
		{"unverified email present", true, ptrFalse(), false},
		{"email present, verified claim absent", true, nil, false},
		{"email absent entirely", false, nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := oidctest.NewProvider(t)
			handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

			subject := uniqueLinkedInSubject(t)
			var email string
			if tc.emailPresent {
				email = uniqueEmail(t)
			}

			txCookie, state, nonce := beginLinkedIn(t, handler)
			p.RegisterCode("code-"+tc.name, oidctest.Claims{
				Subject:       subject,
				Email:         email,
				EmailVerified: tc.emailVerified,
				Nonce:         nonce, // pass every OTHER check so only the email rule is under test
			})

			resp := doLinkedInCallback(t, handler, "code-"+tc.name, state, txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

			if !tc.wantCreated {
				errorCode := assertRejected(t, resp)
				if errorCode != "email_not_verified" {
					t.Errorf("error code = %q, want %q (design spec §3 LinkedIn row: absent email_verified is NEVER treated as true)", errorCode, "email_not_verified")
				}
				assertNoLinkedInIdentity(t, q, subject)
				if tc.emailPresent {
					assertNoUser(t, q, email)
				}
				return
			}

			assertLinkedInLoginAccepted(t, resp)

			identity, err := q.GetIdentityByProviderSubject(context.Background(), store.GetIdentityByProviderSubjectParams{
				Provider:       string(auth.ProviderLinkedIn),
				ProviderUserID: subject,
			})
			if err != nil {
				t.Fatalf("GetIdentityByProviderSubject(linkedin, %q) error = %v, want a created identity row", subject, err)
			}
			usr, err := q.GetUserByEmail(context.Background(), email)
			if err != nil {
				t.Fatalf("GetUserByEmail(%q) error = %v, want a created user row", email, err)
			}
			if identity.UserID != usr.ID {
				t.Errorf("identity.UserID = %v, want %v (the same created user)", identity.UserID, usr.ID)
			}
		})
	}
}

// ==== task-5-brief.md Step 2: the linking carve-out ====

// TestLinkedInCallback_PurposeLink_AllowsUnverifiedEmail is
// task-5-brief.md Step 2: a purpose=link transaction with EmailVerified:
// nil -- the exact claims shape
// TestLinkedInCallback_RegistrationEmailRuleAdversarial's "email present,
// verified claim absent" case rejects for registration -- must succeed
// for linking, attaching the identity to the existing user. This is the
// brief's own stated highest-value case: "the test that would catch an
// over-eager fix that makes the email-verified check unconditional."
//
// Per the brief, this test constructs the transaction directly via
// TransactionStore.Begin (through the implementer's own
// beginLinkedInTransaction helper, linkedin_test.go -- reused rather than
// duplicated, since it does exactly what this test needs) rather than
// driving an authenticated GET .../start?purpose=link: that HTTP surface
// belongs to Task 10. Everything downstream of that -- the
// GET .../callback handling itself, including the purpose=link branch
// under test -- IS Task 5's own scope and is driven for real, through the
// same http.Handler every other test in this file uses.
func TestLinkedInCallback_PurposeLink_AllowsUnverifiedEmail(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	existingUserID := createTestUser(t, q)
	txCookie, tx := beginLinkedInTransaction(t, q, auth.PurposeLink, existingUserID)

	subject := uniqueLinkedInSubject(t)
	email := uniqueEmail(t) // present, but EmailVerified is nil -- see doc comment above
	p.RegisterCode("code-link", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: nil,
		Nonce:         tx.Nonce, // the real, server-generated nonce for this transaction
	})

	resp := doLinkedInCallback(t, handler, "code-link", tx.State, txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

	assertLinkedInLoginAccepted(t, resp)

	identity, err := q.GetIdentityByProviderSubject(context.Background(), store.GetIdentityByProviderSubjectParams{
		Provider:       string(auth.ProviderLinkedIn),
		ProviderUserID: subject,
	})
	if err != nil {
		t.Fatalf("GetIdentityByProviderSubject(linkedin, %q) error = %v, want a created identity row linking to the existing user", subject, err)
	}
	if identity.UserID != existingUserID {
		t.Errorf("linked identity.UserID = %v, want %v (the existing user the link transaction named)", identity.UserID, existingUserID)
	}

	// The carve-out attaches to the existing account; it must not also
	// spawn a brand-new user row from the claims email.
	assertNoUser(t, q, email)
}

// ==== task-5-brief.md Step 3: the standard OIDC adversarial matrix,
// re-run against LinkedIn's own issuer/config wiring ====

// TestLinkedInCallback_RejectsWrongIssuer mirrors
// TestGoogleCallback_RejectsWrongIssuer (google_adversarial_test.go):
// go-oidc's own issuer verification -- not application code -- must
// reject a token whose "iss" doesn't match the discovered LinkedIn
// provider, through linkedin.go's own discovery/verify wiring
// independently of google.go's.
func TestLinkedInCallback_RejectsWrongIssuer(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	subject := uniqueLinkedInSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginLinkedIn(t, handler)
	p.RegisterCode("code-wrong-issuer", oidctest.Claims{
		Subject: subject, Email: email, EmailVerified: ptrTrue(),
		Nonce: nonce, Issuer: "https://evil.example",
	})

	resp := doLinkedInCallback(t, handler, "code-wrong-issuer", state, txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoLinkedInIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestLinkedInCallback_RejectsWrongAudience mirrors
// TestGoogleCallback_RejectsWrongAudience: go-oidc must reject a token
// whose "aud" doesn't match the Service's configured LinkedIn client id.
func TestLinkedInCallback_RejectsWrongAudience(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	subject := uniqueLinkedInSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginLinkedIn(t, handler)
	p.RegisterCode("code-wrong-audience", oidctest.Claims{
		Subject: subject, Email: email, EmailVerified: ptrTrue(),
		Nonce: nonce, Audience: "some-other-client-id",
	})

	resp := doLinkedInCallback(t, handler, "code-wrong-audience", state, txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoLinkedInIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestLinkedInCallback_RejectsTamperedSignature mirrors
// TestGoogleCallback_RejectsTamperedSignature: go-oidc must reject a
// token signed with a key other than the one this LinkedIn-issuer
// oidctest.Provider published at /jwks.json.
func TestLinkedInCallback_RejectsTamperedSignature(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	subject := uniqueLinkedInSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginLinkedIn(t, handler)
	p.RegisterCode("code-tampered-signature", oidctest.Claims{
		Subject: subject, Email: email, EmailVerified: ptrTrue(),
		Nonce: nonce, SigningKey: generateOtherRSAKey(t),
	})

	resp := doLinkedInCallback(t, handler, "code-tampered-signature", state, txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoLinkedInIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestLinkedInCallback_RejectsNonceMismatch mirrors
// TestGoogleCallback_RejectsNonceMismatch -- the brief's own
// "highest-value regression test in this table" reasoning applies
// identically here: go-oidc never checks nonce itself, so this is caught
// only by handleLinkedInCallback's own manual comparison, an easy line to
// omit when copy-adapting google.go's callback into a new file.
func TestLinkedInCallback_RejectsNonceMismatch(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	subject := uniqueLinkedInSubject(t)
	email := uniqueEmail(t)
	txCookie, state, _ := beginLinkedIn(t, handler) // the real nonce is deliberately discarded
	p.RegisterCode("code-nonce-mismatch", oidctest.Claims{
		Subject: subject, Email: email, EmailVerified: ptrTrue(),
		Nonce: "wrong-nonce-does-not-match-the-real-transaction",
	})

	resp := doLinkedInCallback(t, handler, "code-nonce-mismatch", state, txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoLinkedInIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestLinkedInCallback_RejectsExpiredIDToken mirrors
// TestGoogleCallback_RejectsExpiredIDToken: go-oidc must reject a token
// whose "exp" is already in the past.
func TestLinkedInCallback_RejectsExpiredIDToken(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	subject := uniqueLinkedInSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginLinkedIn(t, handler)
	p.RegisterCode("code-expired", oidctest.Claims{
		Subject: subject, Email: email, EmailVerified: ptrTrue(),
		Nonce: nonce, ExpiresAt: time.Now().Add(-1 * time.Hour),
	})

	resp := doLinkedInCallback(t, handler, "code-expired", state, txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoLinkedInIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// ==== strengthenings mirroring google_adversarial_test.go's own table,
// applied here because handleLinkedInCallback is structurally a near-
// duplicate of handleGoogleCallback (and therefore shares its bug
// classes) ====

// TestLinkedInCallback_RejectsStateMismatch mirrors
// TestGoogleCallback_RejectsStateMismatch: the OAuth `state` parameter
// (RFC 6749 §10.12) must be checked against what Begin generated for this
// exact transaction, independent of the __Host-oauth-tx cookie and PKCE.
func TestLinkedInCallback_RejectsStateMismatch(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	subject := uniqueLinkedInSubject(t)
	email := uniqueEmail(t)
	txCookie, _, nonce := beginLinkedIn(t, handler) // deliberately discard the real state
	p.RegisterCode("code-state-mismatch", oidctest.Claims{
		Subject: subject, Email: email, EmailVerified: ptrTrue(), Nonce: nonce,
	})

	resp := doLinkedInCallback(t, handler, "code-state-mismatch", "attacker-supplied-wrong-state", txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoLinkedInIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestLinkedInCallback_RejectsMissingTxCookie mirrors
// TestGoogleCallback_RejectsMissingTxCookie: a callback with a plausible
// code and state but no __Host-oauth-tx cookie at all must be rejected
// the same way any other invalid transaction is.
func TestLinkedInCallback_RejectsMissingTxCookie(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	subject := uniqueLinkedInSubject(t)
	email := uniqueEmail(t)
	_, state, nonce := beginLinkedIn(t, handler) // begin a real transaction, never attach its cookie below
	p.RegisterCode("code-missing-cookie", oidctest.Claims{
		Subject: subject, Email: email, EmailVerified: ptrTrue(), Nonce: nonce,
	})

	resp := doLinkedInCallback(t, handler, "code-missing-cookie", state) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoLinkedInIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestLinkedInCallback_ProviderAccessDenied_RedirectsCancelled encodes the
// binding integration-owner ruling (DD-C4/landed cancelledErrorCode): a
// callback carrying ?error=access_denied (RFC 6749 §4.1.2.1, the visitor
// declining consent at the provider) must redirect with the distinct
// cancelledErrorCode, not the generic auth_failed one -- tested here,
// independently of Google's own equivalent, specifically to catch
// linkedin.go forgetting to mirror this check. Reuses assertRejected
// (which already asserts DD-C7's /login redirect target, cleared tx
// cookie, and no session cookie) rather than re-deriving those checks
// inline.
func TestLinkedInCallback_ProviderAccessDenied_RedirectsCancelled(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	txCookie, state, _ := beginLinkedIn(t, handler)

	path := auth.LinkedInCallbackPath + "?error=access_denied&state=" + url.QueryEscape(state)
	resp := doGet(t, handler, path, txCookie) //nolint:bodyclose // doGet (handlers_test.go) closes the body itself before returning.

	errorCode := assertRejected(t, resp)
	if errorCode != "cancelled" { //nolint:misspell // exact, ruling-specified wire value (double-L "cancelled"), matching handlers.go's own cancelledErrorCode const, not a typo for "canceled"
		t.Errorf("error code = %q, want %q (provider access_denied maps to the distinct code below, not the generic auth_failed)", errorCode, "cancelled") //nolint:misspell // same wire value as above
	}
}

// TestLinkedInCallback_OIDCFailures_NoOracleAcrossFailureModes mirrors
// TestGoogleCallback_OIDCFailures_NoOracleAcrossFailureModes: the five
// distinct OIDC verification failures above must be indistinguishable to
// the browser (no oracle) -- same error code, same response body, and
// that shared code must be neither email_not_verified nor leak any
// verification-internals keyword.
func TestLinkedInCallback_OIDCFailures_NoOracleAcrossFailureModes(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	otherKey := generateOtherRSAKey(t)

	cases := map[string]func(nonce string) oidctest.Claims{
		"wrong issuer": func(nonce string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueLinkedInSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: nonce, Issuer: "https://evil.example",
			}
		},
		"wrong audience": func(nonce string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueLinkedInSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: nonce, Audience: "some-other-client-id",
			}
		},
		"tampered signature": func(nonce string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueLinkedInSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: nonce, SigningKey: otherKey,
			}
		},
		"nonce mismatch": func(string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueLinkedInSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: "wrong-nonce-does-not-match-the-real-transaction",
			}
		},
		"expired id token": func(nonce string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueLinkedInSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: nonce, ExpiresAt: time.Now().Add(-1 * time.Hour),
			}
		},
	}

	errorCodes := make(map[string]string, len(cases))
	bodies := make(map[string]string, len(cases))
	for name, buildClaims := range cases {
		code := "code-li-oracle-" + name

		txCookie, state, nonce := beginLinkedIn(t, handler)
		p.RegisterCode(code, buildClaims(nonce))

		path := auth.LinkedInCallbackPath + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
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
		t.Error("OIDC verification failures reused the email_not_verified error code, want a distinct generic code -- these five are cryptographic/protocol failures, not the email_verified claim check")
	}

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
