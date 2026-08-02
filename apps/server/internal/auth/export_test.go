package auth

import (
	"time"

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
