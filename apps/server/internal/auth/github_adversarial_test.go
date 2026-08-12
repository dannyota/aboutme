// These adversarial tests cover GitHub's verified-primary email rule, provider
// binding, absence of OIDC logic, opaque provider failures, numeric IDs, and
// consent denial. See docs/design/security.md.
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

// ==== GitHub-specific assertions ====

// assertNoGitHubIdentity proves rejection created no GitHub identity.
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

// assertNoLeakedParsingDetails rejects upstream parsing details that would let
// a browser fingerprint GitHub failure modes.
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

// randomIDAtLeast avoids shared-database collisions while forcing boundary
// tests above min.
func randomIDAtLeast(t *testing.T, min int64) int64 {
	t.Helper()

	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	offset := int64(binary.BigEndian.Uint64(buf[:]) & 0x1FFFFFFF) // 0 .. ~536,870,911
	return min + offset
}

// ==== verified-primary email selection ====

// TestGitHubCallback_NoVerifiedPrimaryEmail proves that the email list must
// contain one entry with both primary and verified set. The failure uses the
// same email_not_verified wire code as the OIDC providers even though GitHub
// proves the property through its REST email list rather than an ID-token claim.
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
		t.Errorf("error code = %q, want %q (same code as the OIDC providers)", errorCode, "email_not_verified")
	}

	assertNoGitHubIdentity(t, q, strconv.FormatInt(githubID, 10))
	assertNoUser(t, q, unverifiedPrimaryEmail)
	assertNoUser(t, q, unverifiedOtherEmail)
}

// TestGitHubCallback_MixUp_RejectsTransactionFromAnotherProvider proves the
// callback passes ProviderGitHub to transaction consumption. GitHub has no
// issuer claim, so this provider-bound transaction check is its mix-up defense.
//
// The GitHub stub is deliberately never hit: provider mismatch is rejected
// before state validation, consent handling, or token exchange.
func TestGitHubCallback_MixUp_RejectsTransactionFromAnotherProvider(t *testing.T) {
	t.Parallel()

	gh := newGitHubStub(t) // intentionally unconfigured/never hit -- see doc comment
	handler, q := newTestService(t, withGitHubEndpoint(gh.URL))

	// A Path=/ transaction cookie can reach another provider's callback, so
	// begin the transaction for Google and present it to GitHub.
	ts := auth.NewTransactionStore(q)
	handle, tx, err := ts.Begin(context.Background(), auth.ProviderGoogle, auth.PurposeLogin, uuid.Nil,
		testPublicOrigin+"/api/v1/auth/google/callback")
	if err != nil {
		t.Fatalf("Begin(ProviderGoogle) error = %v", err)
	}

	googleTxCookie := requestCookie(auth.OAuthTxCookieName, handle)

	resp := doGitHubCallback(t, handler, "irrelevant-code-never-exchanged", tx.State, googleTxCookie) //nolint:bodyclose // doGitHubCallback -> doGet closes the body itself before returning.

	errorCode := assertRejected(t, resp)
	if errorCode != "auth_failed" {
		t.Errorf("error code = %q, want %q (DD-C3: ErrTransactionInvalid -- including a cross-provider mismatch -- funnels to the generic code, no oracle distinguishing it from any other rejected transaction)", errorCode, "auth_failed")
	}

	// Rejection occurs before GitHub establishes an email or provider user ID,
	// so there is no provider-scoped identity row to query here.
}

// TestGitHubCallback_NoOIDCImportInPackage parses import declarations to prove
// github.go contains no OIDC or JWT dependency. Parsing imports avoids false
// positives from comments or string literals.
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

// ==== verified-primary email selection edge case ====

// TestGitHubCallback_EmailSelection_PrimaryUnverified_DoesNotFallBackToVerifiedSecondary
// rejects a verified secondary email when the primary email is unverified.
// This catches an implementation that accepts any verified entry instead of
// requiring one entry to be both primary and verified.
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

// ==== malformed provider response ====

// TestGitHubCallback_EmailsAPIMalformedJSON_NoLeakedParsingDetails
// strengthens github_test.go's own
// TestGitHubCallback_EmailsAPIMalformedJSON_RedirectsAuthFailed (same
// stub, same scenario -- GET /user/emails responding 200 with a body that
// isn't valid JSON at all) with the one assertion that test doesn't make:
// the response body must not leak any parsing-failure detail. GET /user and
// GET /user/emails share githubAPIGet, so one malformed JSON case proves the
// shared decode-error path.
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

// ==== numeric ID conversion ====

// TestGitHubCallback_NumericIDConvertsToStringProviderUserID pins the
// "numeric user id" -> identities.provider_user_id (a text column) string
// conversion for two boundary-shaped ids designed to catch a specific,
// plausible bug class: an id decoded through a float64-typed intermediate
// (Go's json.Unmarshal default for interface{}, or a github struct field
// mistakenly typed float64 instead of int64) rather than a proper integer
// type. Both cases use randomIDAtLeast so the magnitude boundary is crossed
// by construction.
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

// ==== provider consent denial ====

// TestGitHubCallback_ProviderAccessDenied_RedirectsCancelled proves an
// RFC 6749 access_denied response gets the distinct canceled code rather than
// the generic authentication-failure code.
//
// The callback needs state from a real transaction because state validation
// precedes consent-denial handling.
func TestGitHubCallback_ProviderAccessDenied_RedirectsCancelled(t *testing.T) {
	t.Parallel()

	gh := newGitHubStub(t) // never hit: a declined-consent callback carries no code to exchange
	handler, _ := newTestService(t, withGitHubEndpoint(gh.URL))

	txCookie, state := beginGitHubFlow(t, handler)
	path := auth.GitHubCallbackPath + "?error=access_denied&state=" + url.QueryEscape(state)
	resp := doGet(t, handler, path, txCookie) //nolint:bodyclose // doGet (handlers_test.go) closes the body itself before returning.

	errorCode := assertRejected(t, resp)
	const wantErrorCode = "cancelled" //nolint:misspell // Exact wire value uses double-L "cancelled".
	if errorCode != wantErrorCode {
		t.Errorf("error code = %q, want %q (fix-round ruling b2, applied identically to GitHub per this task's binding ruling on provider-declined consent)", errorCode, wantErrorCode)
	}
}
