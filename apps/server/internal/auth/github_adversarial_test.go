// Adversarial, spec-derived tests for the GitHub plain-OAuth2 login
// callback (task-6-brief.md Step 2, AC-AUTH-003 "GitHub receives no OIDC
// nonce/iss checks"), originally authored independently -- from
// task-6-brief.md, the design spec (docs/specs/aboutme-design.md §3
// "OAuth"), and the packages it names as allowed reads (internal/auth's
// TransactionStore/cookie helpers, the committed Google handler files that
// define the shared callback pattern, internal/store, internal/config,
// internal/testutil) -- WITHOUT reading internal/auth/github.go or any
// Task 6 author test, neither of which existed at authorship time.
//
// Reconciled against the landed implementation (GitHub merged at a106f5a,
// hardened at cc46948 "fix(auth): reclassify GitHub REST failures as
// rejections and prove PKCE"; this checkout at c64e052, which also
// includes LinkedIn login and that provider's own independent adversarial
// suite) for:
//
//   - both ADAPT markers, exactly as guessed: auth.GitHubStartPath/
//     GitHubCallbackPath (github.go) and the withGitHubEndpoint test option
//     (handlers_test.go) landed with the identical names this file assumed;
//   - mechanical helper collisions with the implementer's own
//     github_test.go, which independently defined a GitHub REST stub and
//     unique-id helper under different names than this file's own guesses:
//     newGitHubStub/gitHubStubOption/withTokenResponse/withUser/withEmails/
//     withEmailsAPIMalformedJSON/ghEmail/uniqueGitHubUserID/
//     beginGitHubFlow/doGitHubCallback. This file's own equivalents
//     (ghAdversarialStub, ghAdvStubConfig, ghAdvEmail, withAdvTokenResponse,
//     withAdvUser, withAdvEmails, withAdvMalformedEmailsJSON, uniqueGitHubID,
//     beginGitHub, doCallbackGitHub) were deleted in favor of theirs
//     (functionally identical in every case); see notes.md's integration
//     report;
//   - newTestService's guard (handlers_test.go): it now t.Fatals only when
//     EVERY provider override is empty, and withGitHubEndpoint alone
//     satisfies it -- the earlier hard "Google issuer always required"
//     requirement this file's pre-integration draft worked around
//     (newGitHubTestService) no longer exists, so that wrapper was deleted;
//     every test here now calls the real newTestService(t,
//     withGitHubEndpoint(...)) directly, exactly like github_test.go's own
//     tests do;
//   - DD-C7 (every /callback rejection redirects to PublicOrigin+"/login",
//     not the bare "/"): already asserted by the shared assertRejected
//     helper (google_adversarial_test.go, itself updated for this), reused
//     unchanged by every rejection test in this file;
//   - DD-C10 (fix round 2 item 2, github.go's handleGitHubCallback doc
//     comment): a GitHub REST failure -- non-200 status, network error, or
//     a malformed/undecodable response body from EITHER /user or
//     /user/emails -- is a provider-side failure funneled through
//     redirectAuthFailed (302 ?error=auth_failed), never writeInternalError's
//     500. This file's malformed-JSON tests assert exactly that code;
//   - the access_denied -> cancelledErrorCode mapping (handlers.go;
//     mentioned as landed-for-Google-only at original authorship, now
//     confirmed landed for GitHub too at github.go's own handleGitHubCallback)
//     is unchanged from what this file already asserted as its one bonus
//     test beyond the brief's named scope.
//
// One deliberate, disclosed scope narrowing: github.go's GET /user and GET
// /user/emails calls both route through the exact same shared
// s.githubAPIGet helper (github.go), whose non-200-status and
// decode-error branches are therefore identical code regardless of which
// endpoint is called -- confirmed by reading github.go directly during
// this reconciliation (this task's derivation phase, when github.go did
// not exist, is over; the coordinator's integration message explicitly
// authorized reading it now). github_test.go's own DD-C10 coverage already
// exercises both branches once each (TestGitHubCallback_UserAPINon200_RedirectsAuthFailed
// for the status branch, TestGitHubCallback_EmailsAPIMalformedJSON_RedirectsAuthFailed
// for the decode branch). This file's own pre-integration draft had a
// SECOND, separate malformed-JSON test targeting /user specifically (with
// its own bespoke stub) -- since that would only re-exercise the
// already-covered decode branch through the identical shared function via
// a different URL path, it was consolidated into one test
// (TestGitHubCallback_EmailsAPIMalformedJSON_NoLeakedParsingDetails below)
// rather than kept as a second, code-path-redundant test. Nothing about
// the malformed-JSON SCENARIO'S assertions was weakened by this -- the one
// remaining test adds a real, novel assertion (assertNoLeakedParsingDetails)
// that github_test.go's own equivalent test does not make, on top of the
// shared DD-C10 contract.
//
// Every other test's scenario and assertions are unchanged from this
// file's original, independent authorship -- no assertion was weakened to
// make anything pass; see notes.md's integration report for the full
// verification record.
//
// Scope: every test here drives GET /api/v1/auth/github/start then
// GET /api/v1/auth/github/callback through the real http.Handler
// newTestService builds (api.New + Service.RegisterRoutes), not a bypass.
// It covers task-6-brief.md Step 2's three required rows (no verified
// primary email; the cross-provider mix-up defense -- direct AC-AUTH-003
// evidence; the static no-OIDC-import regression guard) plus this task's
// assigned strengthenings (the primary-unverified-does-not-fall-back-to-
// verified-secondary email-selection case; a malformed provider REST
// response producing an opaque, non-leaking rejection; the numeric GitHub
// user id -> provider_user_id string conversion, pinned against two
// boundary-shaped ids designed to catch a float64-typed-id bug class) and
// one bonus test beyond that assignment (GitHub's own access_denied ->
// its own distinct error code mapping, not covered by github_test.go at all).
//
// Does NOT cover: the plain happy path (github_test.go's own
// TestGitHubCallback_NewUser_UsesVerifiedPrimaryEmail and
// TestGitHubCallback_SendsPKCEVerifier_MatchesStartCodeChallenge), the two
// DD-C10 cases github_test.go already covers directly (non-200 /user,
// malformed /user/emails -- this file's own malformed-JSON test targets
// the same /user/emails endpoint deliberately, adding a leak-scan on top
// rather than duplicating a third, code-path-identical variant), log
// attribution (github_test.go's own TestGitHubCallback_RejectionLogsProviderAttribute),
// email-collision (Task 10's job), router-level concerns, or Google/
// LinkedIn (google_adversarial_test.go/linkedin_adversarial_test.go's own
// copies of this same exercise against their own provider mechanics).
package auth_test

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"go/parser"
	"go/token"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// ==== assertions specific to this file (GitHub-flavored siblings of google_adversarial_test.go's) ====

// assertNoGitHubIdentity is assertNoIdentity's (google_adversarial_test.go)
// GitHub-provider sibling -- that helper hardcodes auth.ProviderGoogle, so
// it cannot be reused directly here, and no provider-parameterized version
// exists anywhere in this package (confirmed: neither github_test.go nor
// linkedin_adversarial_test.go define one either).
func assertNoGitHubIdentity(t *testing.T, q *store.Queries, providerUserID string) {
	t.Helper()

	_, err := q.GetIdentityByProviderSubject(context.Background(), store.GetIdentityByProviderSubjectParams{
		Provider:       string(auth.ProviderGitHub),
		ProviderUserID: providerUserID,
	})
	if err == nil {
		t.Errorf("GetIdentityByProviderSubject(github, %q) unexpectedly found a row, want none (no identity row may be created on a rejected callback)", providerUserID)
	}
}

// assertNoLeakedParsingDetails scans body for substrings that would leak
// GitHub-REST-response-parsing internals to the browser -- the same
// no-oracle/no-leak reasoning as
// TestGoogleCallback_OIDCFailures_NoOracleAcrossFailureModes
// (google_adversarial_test.go) applied to a different failure class: a
// malformed upstream JSON response is this server's own defensive
// rejection of bad input from GitHub, not a distinct, nameable failure
// mode the browser should ever be able to fingerprint. Not covered by
// github_test.go's own DD-C10 tests, which only check the outward
// status/cookie/error-code shape (assertGitHubRejectedAuthFailed), never
// response body content -- this is this file's own, genuinely additive
// assertion.
func assertNoLeakedParsingDetails(t *testing.T, body string) {
	t.Helper()

	forbidden := []string{"json", "unmarshal", "invalid character", "unexpected end", "malformed", "parse error", "syntax error", "eof"}
	lower := strings.ToLower(body)
	for _, word := range forbidden {
		if strings.Contains(lower, word) {
			t.Errorf("response body leaks a parsing-failure detail (contains %q): %q", word, body)
		}
	}
}

// randomIDAtLeast returns a random int64 in [min, min+~536,870,911], for
// generating realistic-but-collision-resistant GitHub user ids without
// hardcoding small literals (e.g. 1, 42) that would collide across
// repeated runs against this package's shared, never-reset
// TEST_DATABASE_URL. Distinct from github_test.go's own
// uniqueGitHubUserID (rand.Int64N(1<<62)): that generator does not
// deterministically guarantee a value past a specific magnitude boundary
// (e.g. 2^32 or 2^53), which TestGitHubCallback_NumericIDConvertsToStringProviderUserID
// below needs by construction, not by probability -- kept for that reason,
// not a mechanical duplicate.
func randomIDAtLeast(t *testing.T, min int64) int64 {
	t.Helper()

	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	offset := int64(binary.BigEndian.Uint64(buf[:]) & 0x1FFFFFFF) // 0 .. ~536,870,911
	return min + offset
}

// ==== task-6-brief.md Step 2's three required rows ====

// TestGitHubCallback_NoVerifiedPrimaryEmail is the brief's first required
// row: no entry in /user/emails has both primary: true and verified: true
// (here: the primary entry is unverified, and the one other entry is
// neither primary nor verified) -- design spec §3's GitHub rule is
// "verified primary only". task-6-brief.md's own ruling (integration owner,
// binding) pins the SAME email_not_verified code the OIDC providers use
// for this outcome, "for a consistent client-side error contract even
// though the underlying check is entirely different" (github.go has no
// email_verified claim at all -- this is an /user/emails array scan, not a
// token claim; confirmed by reading github.go's primaryVerifiedGitHubEmail).
func TestGitHubCallback_NoVerifiedPrimaryEmail(t *testing.T) {
	t.Parallel()

	githubID := uniqueGitHubUserID(t)
	unverifiedPrimaryEmail := uniqueEmail(t)
	unverifiedOtherEmail := uniqueEmail(t)
	gh := newGitHubStub(t,
		withTokenResponse("code-no-verified-primary", "access-token-no-verified-primary"),
		withUser(githubID, "octocat-no-verified-primary"),
		withEmails([]ghEmail{
			{Email: unverifiedPrimaryEmail, Primary: true, Verified: false},
			{Email: unverifiedOtherEmail, Primary: false, Verified: false},
		}),
	)
	handler, q := newTestService(t, withGitHubEndpoint(gh.URL))

	txCookie, state := beginGitHubFlow(t, handler)
	resp := doGitHubCallback(t, handler, "code-no-verified-primary", state, txCookie) //nolint:bodyclose // doGitHubCallback -> doGet closes the body itself before returning.

	errorCode := assertRejected(t, resp)
	if errorCode != "email_not_verified" {
		t.Errorf("error code = %q, want %q (task-6-brief.md ruling: same code as the OIDC providers for a consistent client contract)", errorCode, "email_not_verified")
	}

	assertNoGitHubIdentity(t, q, strconv.FormatInt(githubID, 10))
	assertNoUser(t, q, unverifiedPrimaryEmail)
	assertNoUser(t, q, unverifiedOtherEmail)
}

// TestGitHubCallback_MixUp_RejectsTransactionFromAnotherProvider is the
// brief's second required row and the direct positive evidence for
// AC-AUTH-003's "no OIDC checks" half: a transaction begun for
// ProviderGoogle, presented (cookie + state) to GitHub's own callback
// endpoint, must be rejected. GitHub has no iss claim to catch this itself
// (design spec §3: "GitHub gets no OIDC checks") -- transaction.go's
// Consume provider check (Task 2, already committed:
// `if row.Provider != string(expectedProvider) { return ..., ErrTransactionInvalid }`)
// is GitHub's ACTUAL mix-up defense here, exercised end-to-end through the
// real HTTP callback handler (confirmed by reading github.go:
// handleGitHubCallback calls `s.tx.Consume(ctx, handle, ProviderGitHub)`),
// not just against TransactionStore directly
// (transaction_adversarial_test.go's own TestConsume_RejectsProviderMismatch
// already covers the store-level contract in isolation; this test proves
// github.go's callback actually wires ProviderGitHub into that call, and
// is the only test in this package doing so for GitHub specifically).
//
// The GitHub stub started here is deliberately never hit: Consume's
// provider-mismatch rejection happens before any GitHub network I/O at all
// (confirmed by reading handleGitHubCallback: ReadOAuthTxCookie+Consume
// run before the state check, the access_denied check, or the token
// exchange) -- a broken request to an httptest.Server this test controls
// the lifecycle of is safe either way, no real network egress occurs
// regardless.
func TestGitHubCallback_MixUp_RejectsTransactionFromAnotherProvider(t *testing.T) {
	t.Parallel()

	gh := newGitHubStub(t) // intentionally unconfigured/never hit -- see doc comment
	handler, q := newTestService(t, withGitHubEndpoint(gh.URL))

	// Begin a transaction for a DIFFERENT provider directly against the
	// same TransactionStore this Service uses internally -- exactly the
	// RFC 9700 §4.4 mix-up scenario design spec §3 describes: the
	// __Host-oauth-tx cookie is Path=/ and would be sent to any
	// /api/v1/auth/*/callback path by a real browser, including a
	// different provider's.
	ts := auth.NewTransactionStore(q)
	handle, tx, err := ts.Begin(context.Background(), auth.ProviderGoogle, auth.PurposeLogin, uuid.Nil,
		testPublicOrigin+"/api/v1/auth/google/callback")
	if err != nil {
		t.Fatalf("Begin(ProviderGoogle) error = %v", err)
	}

	googleTxCookie := &http.Cookie{Name: auth.OAuthTxCookieName, Value: handle}

	resp := doGitHubCallback(t, handler, "irrelevant-code-never-exchanged", tx.State, googleTxCookie) //nolint:bodyclose // doGitHubCallback -> doGet closes the body itself before returning.

	errorCode := assertRejected(t, resp)
	if errorCode != "auth_failed" {
		t.Errorf("error code = %q, want %q (DD-C3: ErrTransactionInvalid -- including a cross-provider mismatch -- funnels to the generic code, no oracle distinguishing it from any other rejected transaction)", errorCode, "auth_failed")
	}

	// No specific email/provider_user_id was ever established for this
	// callback (rejected before any GitHub API interaction at all), so
	// there is no scoped row to check beyond what assertRejected already
	// covers -- the same reasoning handlers_test.go's own
	// TestGoogleCallback_MissingTxCookie_RedirectsAuthFailed documents for
	// its own analogous case. q is used above (auth.NewTransactionStore(q)),
	// not dead.
}

// TestGitHubCallback_NoOIDCImportInPackage is the brief's third required
// row: a deterministic, no-network static check (go/parser, per the
// integration owner's ruling) that internal/auth/github.go imports neither
// coreos/go-oidc nor any jwt package -- making AC-AUTH-003 regression-proof
// against someone "helpfully" adding an issuer/nonce check to this file
// later. This is the one required row github_test.go itself does not
// cover at all (its own doc comment explicitly defers "the adversarial
// matrix ... [and] the static no-OIDC-import check" to "a separate,
// independently authored suite" -- this file). Checked against actual
// import declarations (go/parser, ImportsOnly mode), not a raw text grep
// across the whole file, so a comment or string literal merely mentioning
// "oidc" or "jwt" (e.g. this file's own doc comments, or github.go's own
// doc comment explaining exactly why it must never import go-oidc) can
// never produce a false positive.
func TestGitHubCallback_NoOIDCImportInPackage(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed to resolve this test file's own path")
	}
	githubGoPath := filepath.Join(filepath.Dir(thisFile), "github.go")

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, githubGoPath, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v (AC-AUTH-003's static check requires github.go to exist alongside this test file and parse as valid Go)", githubGoPath, err)
	}

	for _, imp := range f.Imports {
		path, unquoteErr := strconv.Unquote(imp.Path.Value)
		if unquoteErr != nil {
			t.Fatalf("unquote import path %q: %v", imp.Path.Value, unquoteErr)
		}
		lower := strings.ToLower(path)
		if strings.Contains(lower, "go-oidc") {
			t.Errorf("github.go imports %q, want no coreos/go-oidc import anywhere in this file (AC-AUTH-003 / design spec §3: \"GitHub gets no OIDC checks\")", path)
		}
		if strings.Contains(lower, "jwt") {
			t.Errorf("github.go imports %q, want no jwt package import anywhere in this file (AC-AUTH-003: GitHub has no id_token to verify)", path)
		}
	}
}

// ==== assigned strengthening: email-selection matrix (spec: "find the entry with primary: true, verified: true") ====

// TestGitHubCallback_EmailSelection_PrimaryUnverified_DoesNotFallBackToVerifiedSecondary
// is this task's assigned strengthening's highest-value case: the primary
// entry exists but is unverified, and a DIFFERENT, verified, non-primary
// entry is also present. A buggy "any verified email will do"
// implementation (rather than the spec's strict "primary: true, verified:
// true" pair) would silently register the visitor under the verified
// secondary instead of rejecting the login -- this is exactly the failure
// mode TestGitHubCallback_NoVerifiedPrimaryEmail above (no verified email
// at all) cannot distinguish from a correct implementation, and
// github_test.go's own happy-path test (which only ever presents a
// correctly-flagged primary) never exercises at all.
func TestGitHubCallback_EmailSelection_PrimaryUnverified_DoesNotFallBackToVerifiedSecondary(t *testing.T) {
	t.Parallel()

	unverifiedPrimary := uniqueEmail(t)
	verifiedSecondary := uniqueEmail(t)
	githubID := uniqueGitHubUserID(t)
	gh := newGitHubStub(t,
		withTokenResponse("code-no-fallback", "access-token-no-fallback"),
		withUser(githubID, "octocat-no-fallback"),
		withEmails([]ghEmail{
			{Email: unverifiedPrimary, Primary: true, Verified: false},
			{Email: verifiedSecondary, Primary: false, Verified: true},
		}),
	)
	handler, q := newTestService(t, withGitHubEndpoint(gh.URL))

	txCookie, state := beginGitHubFlow(t, handler)
	resp := doGitHubCallback(t, handler, "code-no-fallback", state, txCookie) //nolint:bodyclose // doGitHubCallback -> doGet closes the body itself before returning.

	errorCode := assertRejected(t, resp)
	if errorCode != "email_not_verified" {
		t.Errorf("error code = %q, want %q", errorCode, "email_not_verified")
	}

	// The critical checks: neither candidate email was used to create a
	// user -- specifically, the verified-but-non-primary one, which a
	// "fall back to any verified email" bug would have used.
	assertNoUser(t, q, verifiedSecondary)
	assertNoUser(t, q, unverifiedPrimary)
	assertNoGitHubIdentity(t, q, strconv.FormatInt(githubID, 10))
}

// ==== assigned strengthening: malformed upstream JSON -> opaque failure, never a leak or a 500 ====

// TestGitHubCallback_EmailsAPIMalformedJSON_NoLeakedParsingDetails
// strengthens github_test.go's own
// TestGitHubCallback_EmailsAPIMalformedJSON_RedirectsAuthFailed (same
// stub, same scenario -- GET /user/emails responding 200 with a body that
// isn't valid JSON at all) with the one assertion that test doesn't make:
// the response body must not leak any parsing-failure detail (DD-C10 is an
// opaque rejection, not merely a redirect with the right code). See this
// file's header comment for why a second, separate test targeting /user's
// own malformed-JSON case was deliberately NOT also kept: github.go's
// GET /user and GET /user/emails calls share the exact same decode-error
// code path (s.githubAPIGet), so a second test here would exercise
// identical code through a different URL, not a genuinely different
// defect class.
func TestGitHubCallback_EmailsAPIMalformedJSON_NoLeakedParsingDetails(t *testing.T) {
	t.Parallel()

	githubID := uniqueGitHubUserID(t)
	gh := newGitHubStub(t,
		withTokenResponse("code-emails-malformed-adversarial", "access-token-emails-malformed-adversarial"),
		withUser(githubID, "octocat-malformed-emails"),
		withEmailsAPIMalformedJSON(),
	)
	handler, q := newTestService(t, withGitHubEndpoint(gh.URL))

	txCookie, state := beginGitHubFlow(t, handler)
	path := auth.GitHubCallbackPath + "?code=" + url.QueryEscape("code-emails-malformed-adversarial") + "&state=" + url.QueryEscape(state)
	resp, body := doGetCaptureBody(t, handler, path, txCookie) //nolint:bodyclose // doGetCaptureBody (google_adversarial_test.go) closes the body itself before returning.

	errorCode := assertRejected(t, resp)
	if errorCode != "auth_failed" {
		t.Errorf("error code = %q, want %q (DD-C10: a provider-side GitHub REST failure is a rejection, not a 500)", errorCode, "auth_failed")
	}
	assertNoLeakedParsingDetails(t, body)
	assertNoGitHubIdentity(t, q, strconv.FormatInt(githubID, 10))
}

// ==== assigned strengthening: numeric id -> provider_user_id string conversion, pinned ====

// TestGitHubCallback_NumericIDConvertsToStringProviderUserID pins the
// "numeric user id" -> identities.provider_user_id (a text column) string
// conversion for two boundary-shaped ids designed to catch a specific,
// plausible bug class: an id decoded through a float64-typed intermediate
// (Go's json.Unmarshal default for interface{}, or a github struct field
// mistakenly typed float64 instead of int64) rather than a proper integer
// type -- github.go's real githubUser.ID field IS correctly typed int64
// (confirmed by reading github.go), so this is regression-proofing, not a
// currently-failing assertion. Both cases use randomIDAtLeast's
// deterministic-by-construction magnitude (not github_test.go's own
// probabilistic uniqueGitHubUserID) since the whole point is guaranteeing
// the boundary is actually crossed, not merely likely to be.
func TestGitHubCallback_NumericIDConvertsToStringProviderUserID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   int64
	}{
		{
			// Past int32's max (2,147,483,647) and past 2^32 -- catches an
			// accidental int32/uint32 field or truncating conversion.
			name: "id beyond 2^32 (int32 truncation-shaped)",
			id:   randomIDAtLeast(t, 10_000_000_000),
		},
		{
			// Past 2^53 (9,007,199,254,740,992), the largest integer a
			// float64 can represent exactly -- catches an id field decoded
			// through float64 (or formatted with a float verb), which would
			// silently corrupt digits at this magnitude while looking
			// correct for any smaller, realistic id.
			name: "id beyond float64's exact-integer precision (2^53)",
			id:   randomIDAtLeast(t, (1<<53)+1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			email := uniqueEmail(t)
			gh := newGitHubStub(t,
				withTokenResponse("code-numeric-id", "access-token-numeric-id"),
				withUser(tt.id, "octocat-numeric-id"),
				withEmails([]ghEmail{{Email: email, Primary: true, Verified: true}}),
			)
			handler, q := newTestService(t, withGitHubEndpoint(gh.URL))

			txCookie, state := beginGitHubFlow(t, handler)
			resp := doGitHubCallback(t, handler, "code-numeric-id", state, txCookie) //nolint:bodyclose // doGitHubCallback -> doGet closes the body itself before returning.
			if resp.StatusCode != http.StatusFound {
				t.Fatalf("callback status = %d, want %d", resp.StatusCode, http.StatusFound)
			}
			if got := resp.Header.Get("Location"); got != testPublicOrigin+"/" {
				t.Errorf("callback Location = %q, want %q (a successful login redirects to the bare origin, DD-C7)", got, testPublicOrigin+"/")
			}
			if extractCookie(resp, auth.SessionCookieName) == nil {
				t.Fatal("callback missing __Host-session cookie")
			}

			want := strconv.FormatInt(tt.id, 10)
			identity, err := q.GetIdentityByProviderSubject(context.Background(), store.GetIdentityByProviderSubjectParams{
				Provider:       string(auth.ProviderGitHub),
				ProviderUserID: want,
			})
			if err != nil {
				t.Fatalf("GetIdentityByProviderSubject(github, %s) error = %v, want an identity row with provider_user_id exactly %q "+
					"(pinned decimal string conversion -- no scientific notation, no trailing \".0\", no float64 precision loss)", want, err, want)
			}

			usr, err := q.GetUserByEmail(context.Background(), email)
			if err != nil {
				t.Fatalf("GetUserByEmail(%q) error = %v", email, err)
			}
			if identity.UserID != usr.ID {
				t.Errorf("identity.UserID = %v, want %v (the same created user)", identity.UserID, usr.ID)
			}
		})
	}
}

// ==== bonus strengthening: provider-signaled cancel (fix-round ruling b2, applied uniformly) ====

// TestGitHubCallback_ProviderAccessDenied_RedirectsCancelled is not one of
// task-6-brief.md's three named Step 2 rows or this task's explicitly
// assigned strengthenings -- it is added because the integration owner's
// binding ruling for this task states plainly that this provider-declined
// outcome applies to GitHub the same way it applies to the other two
// providers, and handlers.go's fix-round ruling b2 -- landed for Google
// (handlers_test.go's TestGoogleCallback_ProviderAccessDenied_RedirectsCancelled)
// and LinkedIn (linkedin_adversarial_test.go's
// TestLinkedInCallback_ProviderAccessDenied_RedirectsCancelled) -- is now
// confirmed landed for GitHub too (github.go's own handleGitHubCallback
// checks `r.URL.Query().Get("error") == "access_denied"` and calls
// redirectWithError with cancelledErrorCode, after the state check and
// before the code check, identical to Google's ordering). github_test.go
// itself has no test for this at all, so this fills a genuine gap: a
// callback carrying ?error=access_denied (RFC 6749 §4.1.2.1) gets its own
// distinct error code, not the generic auth_failed.
//
// The real state from a genuinely begun transaction is required here (not
// an arbitrary value): handleGitHubCallback's landed ordering checks
// `state` BEFORE `error=access_denied`, so this test must pass state
// validation before it can actually reach the access_denied branch under
// test, rather than being rejected earlier for an unrelated reason that
// happens to also redirect with a different, non-matching error code.
func TestGitHubCallback_ProviderAccessDenied_RedirectsCancelled(t *testing.T) {
	t.Parallel()

	gh := newGitHubStub(t) // never hit: a declined-consent callback carries no code to exchange
	handler, _ := newTestService(t, withGitHubEndpoint(gh.URL))

	txCookie, state := beginGitHubFlow(t, handler)
	path := auth.GitHubCallbackPath + "?error=access_denied&state=" + url.QueryEscape(state)
	resp := doGet(t, handler, path, txCookie) //nolint:bodyclose // doGet (handlers_test.go) closes the body itself before returning.

	errorCode := assertRejected(t, resp)
	const wantErrorCode = "cancelled" //nolint:misspell // exact, ruling-specified wire value (double-L "cancelled"), not a typo for "canceled"
	if errorCode != wantErrorCode {
		t.Errorf("error code = %q, want %q (fix-round ruling b2, applied identically to GitHub per this task's binding ruling on provider-declined consent)", errorCode, wantErrorCode)
	}
}
