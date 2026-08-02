package auth

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

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

// ResolveLinkedInUserForTest exposes resolveLinkedInUser (linkedin.go) to
// package auth_test. It exists specifically so a test can exercise DD-C12's
// defense-in-depth linkingUserID==uuid.Nil branch directly: the
// oauth_transactions table's own CHECK constraint
// (oauth_transactions_link_needs_user) already makes that state
// impossible to reach through a real Begin-backed Transaction, so the
// only way to prove the Go-level check exists and works is to call this
// function directly with a hand-built purpose/linkingUserID pair, rather
// than through the full /start-/callback HTTP round trip every other
// test in this package drives.
func ResolveLinkedInUserForTest(ctx context.Context, svc *Service, purpose Purpose, linkingUserID uuid.UUID, providerUserID, email string, emailVerified *bool, name string) (store.User, error) {
	return svc.resolveLinkedInUser(ctx, purpose, linkingUserID, providerUserID, email, emailVerified, name)
}

// IsLinkedInLinkRejectedForTest reports whether err is
// errLinkedInLinkRejected (linkedin.go) -- DD-C12's interim link/reauth
// safety-net sentinel -- so a test using ResolveLinkedInUserForTest can
// assert a rejection happened for EXACTLY this reason, not some unrelated
// database error.
func IsLinkedInLinkRejectedForTest(err error) bool {
	return errors.Is(err, errLinkedInLinkRejected)
}

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
