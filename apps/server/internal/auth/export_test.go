package auth

import (
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

// NewServiceForTest builds a Service exactly like NewService, but also
// sets the unexported googleIssuerOverride field to googleIssuer --
// task-4-brief.md Step 2's issuer-override seam: "the Service needs a way
// to use a non-https://accounts.google.com issuer in tests ... add an
// unexported googleIssuerOverride field". googleIssuer is NOT optional in
// practice: every caller in this package's own test helper
// (handlers_test.go's newTestService) is required to supply a non-empty
// one and fails the test immediately otherwise -- a fix-round Critical
// finding was exactly a test that omitted it and performed live OIDC
// discovery against the real https://accounts.google.com. This
// constructor itself stays permissive (an empty override still leaves
// production's real issuer in place) so it also doubles as a direct,
// deliberate way to test NewService's own no-override default if that's
// ever needed.
//
// This lives in export_test.go rather than as an exported production
// constructor/option -- the same seam idiom NewSessionManagerForTest above
// already established for this package -- so production code has no way
// to point itself at an arbitrary issuer by accident.
func NewServiceForTest(logger *slog.Logger, cfg config.Config, q *store.Queries, googleIssuer string) (*Service, error) {
	svc, err := NewService(logger, cfg, q)
	if err != nil {
		return nil, err
	}
	svc.googleIssuerOverride = googleIssuer
	return svc, nil
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
