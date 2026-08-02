package auth

import (
	"context"
	"log/slog"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// NewSessionManagerForTest builds a SessionManager backed by q that uses
// now instead of the real wall clock, so tests in package auth_test (this
// package's own black-box convention -- see transaction_test.go) can
// exercise Issue/Authenticate's idle/absolute/rotation-age logic
// deterministically with a fake, advanceable clock instead of a real
// sleep.
//
// This lives in export_test.go -- compiled only for `go test`, never
// shipped in the production binary -- rather than as an exported
// production constructor. task-2-report.md's ledger flagged the earlier
// NewTransactionStoreForTest (an exported ForTest constructor in
// transaction.go itself) as a minor, preferring this seam idiom instead;
// this file is that fix applied to SessionManager from the start.
func NewSessionManagerForTest(q *store.Queries, now func() time.Time) *SessionManager {
	return &SessionManager{q: q, now: now}
}

// SessionCookieName exposes the unexported sessionCookieName constant to
// package auth_test, so a black-box test can pin its literal value the
// same way transaction_adversarial_test.go pins OAuthTxCookieName (already
// exported there, since it's part of cookie.go's public API; sessionCookieName
// has no production reader yet -- Task 9 adds the __Host-session cookie
// helpers that use it -- so this seam is what keeps it a real, exercised
// symbol rather than dead code in the meantime).
const SessionCookieName = sessionCookieName

// unroutableProviderSentinel is NewServiceForTest's default substitution
// for ANY of its three provider-override parameters left empty: a
// syntactically valid but genuinely unroutable address (loopback, port
// 1 -- nothing ever listens there, so a connection attempt fails
// immediately with connection-refused, no DNS lookup and no real network
// egress). Originally introduced (fix round 1 item 1) as a LinkedIn-only
// default living in handlers_test.go's own resolvedLinkedInIssuer helper;
// fix round 2 item 3 generalized it to every provider and moved it here,
// into NewServiceForTest itself, specifically because two existing call
// sites (google_test.go's newServiceWithOrigin and this file's own
// TestGoogleProvider_DiscoveryDoesNotHoldCacheMutex-adjacent callers, plus
// any future hand-built Service) passed "" for an override they didn't
// care about directly to NewServiceForTest, bypassing the test-helper-
// level default entirely -- a structural gap a local helper in a
// different file could never close.
const unroutableProviderSentinel = "http://127.0.0.1:1"

// defaultTestOverride returns v unchanged if non-empty, else
// unroutableProviderSentinel. The one place NewServiceForTest applies
// this substitution, factored out as its own pure function so it can be
// tested directly and deterministically (see DefaultTestOverrideForTest)
// without constructing a whole Service.
func defaultTestOverride(v string) string {
	if v == "" {
		return unroutableProviderSentinel
	}
	return v
}

// NewServiceForTest builds a Service exactly like NewService, but also
// sets the unexported googleIssuerOverride/githubEndpointOverride/
// linkedinIssuerOverride fields to googleIssuer/githubEndpoint/
// linkedinIssuer -- task-4-brief.md Step 2's issuer-override seam: "the
// Service needs a way to use a non-https://accounts.google.com issuer in
// tests ... add an unexported googleIssuerOverride field", extended here
// to GitHub's OAuth2/API endpoints and LinkedIn's own discovery issuer.
//
// Every one of the three arguments is passed through defaultTestOverride
// first (fix round 2 item 3): an empty string is never stored verbatim --
// it becomes unroutableProviderSentinel instead. This makes every
// *Service this constructor returns safe by construction, independent of
// which (if any) test-helper-level guard a particular call site also
// has -- handlers_test.go's newTestService still hard t.Fatals when EVERY
// override it collected is empty (belt-and-suspenders, kept for its
// existing, more specific error message), but that guard is no longer the
// only thing standing between a forgotten override and a real network
// call: this constructor itself cannot produce a Service pointed at a
// real provider endpoint by accident, regardless of caller.
//
// This lives in export_test.go rather than as an exported production
// constructor/option -- the same seam idiom NewSessionManagerForTest above
// already established for this package -- so production code has no way
// to point itself at an arbitrary issuer/endpoint by accident.
func NewServiceForTest(logger *slog.Logger, cfg config.Config, q *store.Queries, googleIssuer, githubEndpoint, linkedinIssuer string) (*Service, error) {
	svc, err := NewService(logger, cfg, q)
	if err != nil {
		return nil, err
	}
	svc.googleIssuerOverride = defaultTestOverride(googleIssuer)
	svc.githubEndpointOverride = defaultTestOverride(githubEndpoint)
	svc.linkedinIssuerOverride = defaultTestOverride(linkedinIssuer)
	return svc, nil
}

// UnroutableTestSentinel exposes unroutableProviderSentinel to package
// auth_test's pure-function tests (see DefaultTestOverrideForTest).
const UnroutableTestSentinel = unroutableProviderSentinel

// MaxProviderResponseBytesForTest exposes maxProviderResponseBytes
// (provider_http.go) to package auth_test, so a test proving githubAPIGet
// truncates an oversized provider response (github_test.go) can size its
// fixture relative to the real production cap, rather than hard-coding a
// second, independently-maintained copy of that number.
const MaxProviderResponseBytesForTest = maxProviderResponseBytes

// DefaultTestOverrideForTest exposes defaultTestOverride to package
// auth_test, so the substitution NewServiceForTest applies to each
// provider override can be tested directly and deterministically, without
// constructing a whole Service or driving a real route.
func DefaultTestOverrideForTest(v string) string {
	return defaultTestOverride(v)
}

// GoogleProviderCacheTryLockForTest attempts to acquire svc's Google OIDC
// provider-discovery cache mutex (see provider_cache.go's
// oidcProviderCache) without blocking. It exists so a test can prove
// googleProvider/discover does NOT hold this mutex for the duration of
// the discovery network call: while a concurrent discovery is
// deliberately kept in flight (oidctest.Provider.BlockDiscoveryForTest),
// this must still succeed immediately. ok is false only if the mutex is
// currently held by another goroutine, in which case unlock is nil.
func GoogleProviderCacheTryLockForTest(svc *Service) (unlock func(), ok bool) {
	if !svc.google.cache.mu.TryLock() {
		return nil, false
	}
	return svc.google.cache.mu.Unlock, true
}

// ResolveLoginIdentityForTest exposes resolveLoginIdentity (handlers.go,
// Task 10) to package auth_test, so a test can exercise the shared
// login-resolution algorithm directly, once per provider, without driving
// three separate providers' worth of OIDC/REST mechanics through the full
// HTTP surface -- see handlers_test.go's
// TestResolveLoginIdentity_NewThenExisting_AcrossProviders (task-10-brief.md
// Step 1: "exercised once per provider to confirm wiring, not re-proving
// provider mechanics").
func ResolveLoginIdentityForTest(ctx context.Context, svc *Service, provider Provider, providerUserID, email, name string) (LoginResultForTest, error) {
	result, err := svc.resolveLoginIdentity(ctx, provider, providerUserID, email, name)
	return LoginResultForTest{Kind: int(result.Kind), User: result.User}, err
}

// LoginResultForTest mirrors handlers.go's unexported loginResult for
// package auth_test's use via ResolveLoginIdentityForTest -- Kind is one
// of the LoginResultXxxForTest constants below (loginResultKind's own
// values are unexported, so this package cannot compare against them by
// name).
type LoginResultForTest struct {
	Kind int
	User store.User
}

// The LoginResultForTest.Kind values ResolveLoginIdentityForTest can
// return, mirroring handlers.go's unexported loginResultKind constants
// (loginResultNewUser, loginResultExistingIdentity,
// loginResultEmailCollision) in the same iota order, so a test can
// compare LoginResultForTest.Kind against these without importing an
// unexported type.
const (
	LoginResultNewUserForTest int = iota
	LoginResultExistingIdentityForTest
	LoginResultEmailCollisionForTest
)

// resolveLinkedInUser and its DD-C12 interim link/reauth safety net
// (errLinkedInLinkRejected) were removed by Task 10 -- replaced by
// link.go's shared resolveLinkOrReauth, applied uniformly to all three
// providers -- so the ResolveLinkedInUserForTest/IsLinkedInLinkRejectedForTest
// seams that exposed them are gone too. linkedin_test.go's own DD-C12
// tests were removed in the same change; Task 10's link algorithm gets
// its own independent adversarial coverage instead (task-10-brief.md Step
// 3).

// SetSessionIssuerForTest replaces svc's session-issuance seam
// (sessionIssuer, handlers.go) with si -- fix-round Important 1's seam,
// so a test can inject a deterministic failure at the one point
// (SessionManager.Issue) that has no other realistic way to fail without
// corrupting a live database mid-request, and prove writeInternalError's
// obligations (cookie cleared, generic body, no session cookie) against a
// genuine 500 path. si only needs to structurally satisfy the unexported
// sessionIssuer interface's single Issue method -- package auth_test can
// define such a type without ever naming the interface itself (Go
// interface satisfaction is structural), so this parameter type is not
// itself exported.
func SetSessionIssuerForTest(svc *Service, si sessionIssuer) {
	svc.sessions = si
}

// SetSessionManagerForTest replaces svc's session-management seam
// (RequireSession, and every Task 9 handler that revokes/lists sessions
// or checks recent reauth) with m, instead of the *SessionManager
// NewService built internally with the real wall clock -- the same seam
// idiom as SetSessionIssuerForTest above, but for sessionMgr rather than
// the narrower sessionIssuer field. This lets a test inject a
// SessionManager built via NewSessionManagerForTest (a fake, advanceable
// clock) so it can drive Task 9's rotation-forwarding behavior, or a
// recent-reauth boundary, deterministically through the REAL HTTP handler
// chain (RequireSession -> RequireCSRF -> handler), instead of either
// racing the real wall clock or reaching into the database directly.
func SetSessionManagerForTest(svc *Service, m *SessionManager) {
	svc.sessionMgr = m
}
