// Package auth_test: independent, spec-derived adversarial tests for
// SessionManager (task-7-brief.md, AC-AUTH-004 "Session rotation >24h is
// atomic with grace interval"). Originally authored independently, from
// task-7-brief.md, docs/specs/aboutme-design.md §3's Sessions table, and
// apps/server/sql/schema.sql's sessions table alone -- at derivation time,
// `ls apps/server/internal/auth` showed only transaction.go,
// transaction_test.go, transaction_adversarial_test.go, cookie.go,
// cookie_test.go; session.go/session_test.go/export_test.go did not exist
// and were never read. Reconciled against the landed implementation
// (commit d30b618, merged as 14d37b7) only for its two ADAPT-marked
// guesses and one predicted (and confirmed) helper-name collision -- see
// notes.md's integration report for exactly what changed and why.
//
// Harness reuse: requireTestDatabaseURL, newTestQueries, createTestUser
// (transaction_test.go); rowQuerier, newRowInspectorPool, randomHandle
// (transaction_adversarial_test.go); sessionTokenHash (session_test.go,
// see its own reconciliation note below) already exist in this package
// and are reused here rather than duplicated, matching the precedent set
// by task-2-adversarial/transaction_adversarial_test.go.
package auth_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// ---- mirrored constants -------------------------------------------------
//
// These mirror auth's unexported constants (task-7-brief.md's "Produces"
// section) byte-for-byte. Redeclared here, exactly like
// transaction_adversarial_test.go's txTTL mirrors oauthTxTTL, because this
// file is black-box (package auth_test) and cannot reference an
// unexported constant of package auth directly.

const (
	idleTimeout      = 30 * 24 * time.Hour
	absoluteTimeout  = 90 * 24 * time.Hour
	rotationAge      = 24 * time.Hour
	rotationGrace    = 60 * time.Second
	lastSeenThrottle = time.Hour
	reauthWindow     = 15 * time.Minute
)

// ---- harness --------------------------------------------------------------

// newTestSessionManager matches, verbatim, the helper-call shape
// task-7-brief.md's Step 2 sample code uses (`newTestSessionManager(t,
// clock.Now)`). It opens its own store.Queries via newTestQueries
// (transaction_test.go) and wraps it with the injected clock.
//
// ADAPT reconciliation (clock seam): confirmed, not a guess anymore.
// export_test.go's own doc comment explains the seam is deliberately an
// export_test.go-only constructor rather than an exported production one
// (task-2-report.md's ledger had flagged transaction.go's
// NewTransactionStoreForTest -- an exported ForTest constructor in
// transaction.go itself -- as a minor; this is that fix applied to
// SessionManager from the start). The name and signature guessed here,
// auth.NewSessionManagerForTest(q *store.Queries, now func() time.Time)
// *SessionManager, is byte-for-byte what export_test.go defines -- no
// change needed. session_test.go itself never defines a same-named
// wrapper (every one of its tests calls auth.NewSessionManager(q) or
// auth.NewSessionManagerForTest(q, clk.Now) inline), so the
// newTestSessionManager/newTestSessionManagerWithQueries collision this
// file's derivation notes flagged as near-certain did not materialize;
// both helpers below are kept as originally written.
func newTestSessionManager(t *testing.T, now func() time.Time) *auth.SessionManager {
	t.Helper()
	return newTestSessionManagerWithQueries(t, newTestQueries(t), now)
}

// newTestSessionManagerWithQueries is the other half of
// newTestSessionManager, for tests that also need the same q for
// createTestUser or a row-level assertion (most of this file): it lets
// the SessionManager and those other calls share one store.Queries/pool
// instead of each test opening a second one via newTestSessionManager.
func newTestSessionManagerWithQueries(t *testing.T, q *store.Queries, now func() time.Time) *auth.SessionManager {
	t.Helper()
	return auth.NewSessionManagerForTest(q, now)
}

// ADAPT reconciliation (token-hash encoding): confirmed, not a guess
// anymore -- session_test.go already defines its own sessionTokenHash
// (raw string) []byte doing exactly sha256.Sum256([]byte(raw)); return
// sum[:], the same reasoning and the same byte-for-byte implementation
// this file originally guessed independently. Since both files compile
// into the same package auth_test, this file's own copy was deleted (a
// duplicate declaration, not a rename) in favor of reusing theirs; every
// call site below is unchanged, just now resolving to session_test.go's
// definition instead of a local one.

// ---- row-state assertion helpers ------------------------------------------
//
// store.Queries (Task 1's committed querier.go, confirmed via
// apps/server/internal/store/querier.go before writing this file) exposes
// only BeginSessionRotation for the sessions table -- no
// CreateSession/GetSessionByTokenHash/TouchLastSeenAt/RevokeSession
// accessors exist yet for a black-box test to call (the brief notes the
// implementer will append those). Every row-level assertion below goes
// straight at the table with plain pgx via the shared rowQuerier
// interface and newRowInspectorPool (both defined in
// transaction_adversarial_test.go), per the task's own guidance to prefer
// that over assuming store methods exist.

// sessionRowCountForUser reports how many sessions rows exist for userID
// -- the concurrent-rotation test's core row-level assertion (want
// exactly 2: predecessor + the one successor, never up to 20).
func sessionRowCountForUser(ctx context.Context, t *testing.T, db rowQuerier, userID uuid.UUID) int {
	t.Helper()
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id = $1`, userID).Scan(&count); err != nil {
		t.Fatalf("sessionRowCountForUser query error: %v", err)
	}
	return count
}

// sessionRowTokenHash reads token_hash for id, failing the test if id
// does not exist (a row this test expects to still be present has
// vanished).
func sessionRowTokenHash(ctx context.Context, t *testing.T, db rowQuerier, id uuid.UUID) []byte {
	t.Helper()
	var hash []byte
	if err := db.QueryRow(ctx, `SELECT token_hash FROM sessions WHERE id = $1`, id).Scan(&hash); err != nil {
		t.Fatalf("sessionRowTokenHash(%s) query error: %v", id, err)
	}
	return hash
}

// sessionRowLastSeenAt reads last_seen_at for id.
func sessionRowLastSeenAt(ctx context.Context, t *testing.T, db rowQuerier, id uuid.UUID) time.Time {
	t.Helper()
	var ts time.Time
	if err := db.QueryRow(ctx, `SELECT last_seen_at FROM sessions WHERE id = $1`, id).Scan(&ts); err != nil {
		t.Fatalf("sessionRowLastSeenAt(%s) query error: %v", id, err)
	}
	return ts
}

// sessionRowAbsoluteExpiresAt reads absolute_expires_at for id.
func sessionRowAbsoluteExpiresAt(ctx context.Context, t *testing.T, db rowQuerier, id uuid.UUID) time.Time {
	t.Helper()
	var ts time.Time
	if err := db.QueryRow(ctx, `SELECT absolute_expires_at FROM sessions WHERE id = $1`, id).Scan(&ts); err != nil {
		t.Fatalf("sessionRowAbsoluteExpiresAt(%s) query error: %v", id, err)
	}
	return ts
}

// setSessionRowLastSeenAt directly patches last_seen_at, bypassing
// Authenticate's own throttled write path entirely. Used to isolate the
// absolute-expiry tests from the idle-expiry check: calling Authenticate
// repeatedly to "naturally" keep last_seen_at fresh across a 90+ day
// span would also repeatedly re-enter the rotation algorithm (every 24h),
// confounding the one property under test. A direct column patch is the
// same "go straight at the table" approach as the row-state helpers
// above, applied to setup instead of assertion.
func setSessionRowLastSeenAt(ctx context.Context, t *testing.T, pool *store.Pool, id uuid.UUID, ts time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE sessions SET last_seen_at = $2 WHERE id = $1`, id, ts); err != nil {
		t.Fatalf("setSessionRowLastSeenAt(%s) exec error: %v", id, err)
	}
}

// ---- Step 2: the named concurrent-rotation adversarial test ---------------

// TestAuthenticate_ConcurrentRotation_MintsExactlyOneSuccessor is
// task-7-brief.md's Step 2 test, reproduced in the exact shape the brief
// gives: 20 goroutines race Authenticate against one already-24h+-old
// session's raw token. Every one must succeed (the old token is still
// valid within the grace window); exactly one must have won the rotation
// CAS and returned a new (rotatedToken, successor) pair; the rest must
// observe that successor and mint nothing.
//
// Failure mode this catches: a naive implementation either (a) mints up
// to 20 successor rows (no atomic CAS, or a SELECT-then-INSERT race), or
// (b) 401s the 19 losers instead of transparently authenticating them
// against the winner's row -- both are explicitly called out as the wrong
// naive outcomes in task-7-brief.md's Step 2 commentary. Must be
// deterministic under `-race -count=20`, not just "usually" -- a CAS that
// is not actually atomic is a global-constraint violation (flaky = broken),
// not a candidate for a retry loop in this test.
func TestAuthenticate_ConcurrentRotation_MintsExactlyOneSuccessor(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	userID := createTestUser(t, newTestQueries(t))
	m := newTestSessionManager(t, clock.Now) // matches task-7-brief.md's Step 2 sample call shape verbatim
	ctx := context.Background()
	pool := newRowInspectorPool(t)

	raw, origSess, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	clock.Advance(25 * time.Hour) // past rotationAge

	const n = 20
	var wg sync.WaitGroup
	results := make([]struct {
		sess         store.Session
		rotatedToken string
		err          error
	}, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i].sess, results[i].rotatedToken, results[i].err = m.Authenticate(ctx, raw)
		}(i)
	}
	wg.Wait()

	for _, r := range results {
		if r.err != nil {
			t.Errorf("Authenticate() error = %v, want nil (old token still valid within grace)", r.err)
		}
	}

	gotSuccessors := 0
	var rotatedToken string
	var winnerSessID uuid.UUID
	for _, r := range results {
		if r.rotatedToken != "" {
			gotSuccessors++
			rotatedToken = r.rotatedToken
			winnerSessID = r.sess.ID
		}
	}
	if gotSuccessors != 1 {
		t.Fatalf("exactly one goroutine should have won rotation and returned a new token, got %d", gotSuccessors)
	}

	// Reconciliation note (binding ruling from the coordinator, post-
	// integration): a stronger assertion originally lived here --  every
	// one of the 20 results' sess.ID had to be identical, reasoned from
	// the brief's Authenticate doc comment ("the successor, if this call
	// triggered *or observed* a rotation"). The landed session.go
	// deliberately does not do this: a losing/later caller presenting the
	// predecessor's own token authenticates "against the existing row"
	// (the brief's own rotation-algorithm prose, and session.go's
	// Authenticate doc comment and TestAuthenticate_
	// RotatesAfter24h_SequentialSingleRequest both confirm this -- "still
	// authenticate -- against itself, not the successor"). The
	// coordinator's ruling is that the loser's read-race mechanism
	// (including what it returns) is the implementation's choice; this
	// suite pins outcomes only: all 20 calls succeed, exactly one of them
	// mints a successor, and exactly 2 rows exist afterward. The winner's
	// own returned session id is still checked below, since *that* is
	// pinned by the algorithm text ("this call won: insert a successor
	// session ... return the successor").
	if winnerSessID == origSess.ID {
		t.Error("the winning call's returned session id == predecessor id, want the new successor's id (rotation did not actually happen)")
	}

	// Row-level assertion the brief calls for explicitly: query sessions
	// WHERE user_id = userID -- expect exactly 2 rows total (original +
	// the one successor), not up to 20.
	if got := sessionRowCountForUser(ctx, t, pool, userID); got != 2 {
		t.Errorf("sessions row count for user = %d, want exactly 2 (predecessor + exactly one successor)", got)
	}

	// The predecessor row must still exist (rotation never deletes it --
	// it just becomes unreachable after the grace window) ...
	sessionRowTokenHash(ctx, t, pool, origSess.ID)
	// ... and the successor row's token_hash must be exactly
	// sha256(rotatedToken): the token handed back to the caller for
	// Set-Cookie must be the one that actually authenticates the row the
	// winning call minted.
	gotHash := sessionRowTokenHash(ctx, t, pool, winnerSessID)
	wantHash := sessionTokenHash(rotatedToken)
	if string(gotHash) != string(wantHash) {
		t.Errorf("successor row token_hash = %x, want sha256(rotatedToken) = %x", gotHash, wantHash)
	}
}

// ---- Step 3: the six-row adversarial table, verbatim in intent -----------

// TestAuthenticate_RejectsIdleExpired is Step 3 row 1: a session that
// hasn't been touched in 31 days (idleTimeout is 30d) must be rejected,
// with no intervening Authenticate calls to reset the idle clock.
func TestAuthenticate_RejectsIdleExpired(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	m := newTestSessionManagerWithQueries(t, q, clock.Now)
	ctx := context.Background()

	raw, _, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	clock.Advance(idleTimeout + 24*time.Hour) // 31d, no intervening requests

	if _, _, err := m.Authenticate(ctx, raw); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Fatalf("Authenticate() after idle timeout error = %v, want ErrSessionInvalid", err)
	}
}

// TestAuthenticate_RejectsAbsoluteExpired is Step 3 row 2: 91 days past
// created_at must reject even with a *recent* last_seen_at, proving
// absolute expiry is a hard ceiling independent of activity -- not just
// "idle expiry with a longer window." last_seen_at is patched directly
// (setSessionRowLastSeenAt) to isolate that specific claim: without the
// patch, 91 days of simulated elapsed time would also trip idle expiry
// (30d), and the test wouldn't be able to tell which check actually fired.
func TestAuthenticate_RejectsAbsoluteExpired(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	m := newTestSessionManagerWithQueries(t, q, clock.Now)
	ctx := context.Background()
	pool := newRowInspectorPool(t)

	raw, sess, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	clock.Advance(absoluteTimeout + 24*time.Hour) // 91d past created_at

	// Prove the rejection below can't be idle expiry: last_seen_at is
	// "now", well inside the 30d idle window.
	setSessionRowLastSeenAt(ctx, t, pool, sess.ID, clock.Now())

	if _, _, err := m.Authenticate(ctx, raw); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Fatalf("Authenticate() 91d past created_at (recent last_seen_at) error = %v, want ErrSessionInvalid", err)
	}
}

// TestAuthenticate_RotationDoesNotExtendAbsoluteExpiry is Step 3 row 3:
// rotate once at 25h, then advance to just past the *original*
// absolute_expires_at -- the whole lineage (predecessor and successor
// alike) must die together, proving rotation never pushes the absolute
// ceiling out. This is the core "anchored to the original login" claim in
// the rotation algorithm's prose ("absolute expiry is anchored to the
// original login, rotation must never extend it").
func TestAuthenticate_RotationDoesNotExtendAbsoluteExpiry(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	m := newTestSessionManagerWithQueries(t, q, clock.Now)
	ctx := context.Background()
	pool := newRowInspectorPool(t)

	raw, origSess, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	clock.Advance(rotationAge + time.Hour) // 25h, past rotationAge

	succSess, rotatedToken, err := m.Authenticate(ctx, raw)
	if err != nil {
		t.Fatalf("Authenticate() at 25h (should rotate) error = %v", err)
	}
	if rotatedToken == "" {
		t.Fatal("Authenticate() at 25h returned no rotatedToken, want rotation to have occurred")
	}
	if !succSess.AbsoluteExpiresAt.Equal(origSess.AbsoluteExpiresAt) {
		t.Fatalf("successor AbsoluteExpiresAt = %v, want unchanged from predecessor %v (anchored to original login)", succSess.AbsoluteExpiresAt, origSess.AbsoluteExpiresAt)
	}
	// Cross-check against the row directly, not just the returned struct.
	if got := sessionRowAbsoluteExpiresAt(ctx, t, pool, succSess.ID); !got.Equal(origSess.AbsoluteExpiresAt) {
		t.Errorf("successor row absolute_expires_at = %v, want %v", got, origSess.AbsoluteExpiresAt)
	}

	// Advance to just past the (unchanged) absolute ceiling. Patch
	// last_seen_at on the successor to "now" first, for the same reason
	// as TestAuthenticate_RejectsAbsoluteExpired: isolate that the
	// rejection below is the absolute ceiling, not idle expiry (the
	// successor's real last_seen_at would otherwise be ~89 days stale by
	// this point).
	remaining := origSess.AbsoluteExpiresAt.Sub(clock.Now())
	clock.Advance(remaining + time.Hour)
	setSessionRowLastSeenAt(ctx, t, pool, succSess.ID, clock.Now())

	if _, _, err := m.Authenticate(ctx, rotatedToken); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("Authenticate(successor token) past the original absolute ceiling error = %v, want ErrSessionInvalid (lineage dies together)", err)
	}
}

// TestAuthenticate_OldTokenRejectedAfterGraceWindow is Step 3 row 4: once
// rotationGrace has elapsed since a rotation, the predecessor's raw token
// must stop authenticating -- the successor's token must still work
// (sanity: rejection isn't just "everything is broken").
func TestAuthenticate_OldTokenRejectedAfterGraceWindow(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	m := newTestSessionManagerWithQueries(t, q, clock.Now)
	ctx := context.Background()

	raw, _, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	clock.Advance(rotationAge + time.Hour)

	_, rotatedToken, err := m.Authenticate(ctx, raw)
	if err != nil {
		t.Fatalf("Authenticate() at 25h (should rotate) error = %v", err)
	}
	if rotatedToken == "" {
		t.Fatal("Authenticate() at 25h returned no rotatedToken, want rotation to have occurred")
	}

	clock.Advance(rotationGrace + time.Second) // past grace

	if _, _, err := m.Authenticate(ctx, raw); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("Authenticate(old token) after grace window error = %v, want ErrSessionInvalid", err)
	}
	if _, _, err := m.Authenticate(ctx, rotatedToken); err != nil {
		t.Errorf("Authenticate(successor token) after grace window error = %v, want nil (successor unaffected by predecessor's grace expiry)", err)
	}
}

// TestAuthenticate_LastSeenAtThrottled is Step 3 row 5: two calls close
// together must not both write last_seen_at (throttle window is 1h). The
// first call is placed 2h after Issue specifically so it *does* trigger a
// write (otherwise "unchanged after the second call" would trivially hold
// even with no throttle logic at all, since neither call would ever have
// written anything) -- the second call, 1 minute after the first, must
// then observe no further write.
func TestAuthenticate_LastSeenAtThrottled(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	m := newTestSessionManagerWithQueries(t, q, clock.Now)
	ctx := context.Background()
	pool := newRowInspectorPool(t)

	raw, origSess, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	clock.Advance(2 * time.Hour) // past lastSeenThrottle (1h) -- forces a write
	sess1, _, err := m.Authenticate(ctx, raw)
	if err != nil {
		t.Fatalf("first Authenticate() error = %v", err)
	}
	if sess1.LastSeenAt.Equal(origSess.LastSeenAt) {
		t.Fatal("last_seen_at unchanged after 2h, want a write (throttle setup did not force one -- test would be vacuous)")
	}
	lastSeenAfterFirst := sessionRowLastSeenAt(ctx, t, pool, origSess.ID)

	clock.Advance(time.Minute) // well under the 1h throttle
	sess2, _, err := m.Authenticate(ctx, raw)
	if err != nil {
		t.Fatalf("second Authenticate() error = %v", err)
	}

	if !sess2.LastSeenAt.Equal(sess1.LastSeenAt) {
		t.Errorf("returned LastSeenAt after second call = %v, want unchanged %v (throttle window is 1h, only 1m elapsed)", sess2.LastSeenAt, sess1.LastSeenAt)
	}
	if got := sessionRowLastSeenAt(ctx, t, pool, origSess.ID); !got.Equal(lastSeenAfterFirst) {
		t.Errorf("row last_seen_at after second call = %v, want unchanged %v", got, lastSeenAfterFirst)
	}
}

// TestRevoke_IsImmediate_NoGraceWindow is Step 3 row 6: Revoke then
// Authenticate immediately (no clock advance at all) must already reject.
// This is design decision 1's orthogonality claim: revoked_at and
// rotation_grace_until must never be conflated, or a revoked session's
// token would keep working for up to rotationGrace after the user thought
// they'd logged out.
func TestRevoke_IsImmediate_NoGraceWindow(t *testing.T) {
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	m := newTestSessionManagerWithQueries(t, q, time.Now)
	ctx := context.Background()

	raw, sess, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	if err := m.Revoke(ctx, sess.ID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}

	if _, _, err := m.Authenticate(ctx, raw); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("Authenticate() immediately after Revoke() error = %v, want ErrSessionInvalid", err)
	}
}

// ---- strengthening: no-oracle, revoke-mid-grace, reauth boundary, fixation

// TestAuthenticate_NoOracleAcrossFailureModes strengthens every failure
// scenario above together, the same way
// TestConsume_NoOracleAcrossFailureModes strengthens transaction_
// adversarial_test.go's four required transaction tests: it is not
// enough that each failure independently satisfies errors.Is(err,
// ErrSessionInvalid); task-7-brief.md's own comment on ErrSessionInvalid
// ("not found / revoked / idle-expired / absolute-expired -- one
// sentinel, same no-oracle reasoning as ErrTransactionInvalid") is
// explicit that the *text* must also be indistinguishable, or a caller
// (or an attacker with error-message visibility) gets an oracle to learn
// which of the four actually happened. A fmt.Errorf("...: %w", ...) wrap
// that added scenario-specific context in only some branches would pass
// every individual errors.Is check while still reopening exactly that
// oracle.
func TestAuthenticate_NoOracleAcrossFailureModes(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	ctx := context.Background()
	errs := make(map[string]error, 5)

	// unknown token: shaped like a real one (32 raw bytes, base64url, no
	// padding -- sessionTokenBytes matches oauth's randomTokenBytes) but
	// never issued. randomHandle (transaction_adversarial_test.go) is
	// reused rather than duplicated: identical encoding shape.
	{
		q := newTestQueries(t)
		m := newTestSessionManagerWithQueries(t, q, clock.Now)
		_, _, err := m.Authenticate(ctx, randomHandle(t))
		errs["unknown"] = err
	}

	// idle-expired
	{
		clk := testutil.NewClockAtEpoch()
		q := newTestQueries(t)
		userID := createTestUser(t, q)
		m := newTestSessionManagerWithQueries(t, q, clk.Now)
		raw, _, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
		if err != nil {
			t.Fatalf("idle-expired setup: Issue() error: %v", err)
		}
		clk.Advance(idleTimeout + 24*time.Hour)
		_, _, err = m.Authenticate(ctx, raw)
		errs["idle-expired"] = err
	}

	// absolute-expired (recent last_seen_at, per TestAuthenticate_RejectsAbsoluteExpired)
	{
		clk := testutil.NewClockAtEpoch()
		q := newTestQueries(t)
		userID := createTestUser(t, q)
		m := newTestSessionManagerWithQueries(t, q, clk.Now)
		pool := newRowInspectorPool(t)
		raw, sess, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
		if err != nil {
			t.Fatalf("absolute-expired setup: Issue() error: %v", err)
		}
		clk.Advance(absoluteTimeout + 24*time.Hour)
		setSessionRowLastSeenAt(ctx, t, pool, sess.ID, clk.Now())
		_, _, err = m.Authenticate(ctx, raw)
		errs["absolute-expired"] = err
	}

	// revoked
	{
		clk := testutil.NewClockAtEpoch()
		q := newTestQueries(t)
		userID := createTestUser(t, q)
		m := newTestSessionManagerWithQueries(t, q, clk.Now)
		raw, sess, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
		if err != nil {
			t.Fatalf("revoked setup: Issue() error: %v", err)
		}
		if revokeErr := m.Revoke(ctx, sess.ID); revokeErr != nil {
			t.Fatalf("revoked setup: Revoke() error: %v", revokeErr)
		}
		_, _, err = m.Authenticate(ctx, raw)
		errs["revoked"] = err
	}

	// old token, past the rotation grace window
	{
		clk := testutil.NewClockAtEpoch()
		q := newTestQueries(t)
		userID := createTestUser(t, q)
		m := newTestSessionManagerWithQueries(t, q, clk.Now)
		raw, _, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
		if err != nil {
			t.Fatalf("old-token-post-grace setup: Issue() error: %v", err)
		}
		clk.Advance(rotationAge + time.Hour)
		if _, _, rotateErr := m.Authenticate(ctx, raw); rotateErr != nil {
			t.Fatalf("old-token-post-grace setup: rotating Authenticate() error: %v", rotateErr)
		}
		clk.Advance(rotationGrace + time.Second)
		_, _, err = m.Authenticate(ctx, raw)
		errs["old-token-post-grace"] = err
	}

	for name, err := range errs {
		if !errors.Is(err, auth.ErrSessionInvalid) {
			t.Errorf("%s: error = %v, want ErrSessionInvalid", name, err)
		}
	}

	want := auth.ErrSessionInvalid.Error()
	for name, err := range errs {
		if err == nil {
			continue
		}
		if got := err.Error(); got != want {
			t.Errorf("%s: error text = %q, want %q (identical text across every failure mode -- no oracle)", name, got, want)
		}
	}
}

// TestAuthenticate_RevokedMidGrace_PredecessorCannotAuthenticate
// strengthens design decision 1's orthogonality claim
// (rotation_grace_until and revoked_at are independent) into the one
// combination the Step 3 table doesn't directly cover: a predecessor
// that is *explicitly* revoked while still inside its own rotation grace
// window must reject immediately, not stay valid until
// rotation_grace_until passes. A bug that (incorrectly) treated "still in
// grace" as an independent source of validity -- rather than checking
// revoked_at unconditionally, before or in addition to the grace check --
// would let a session the user explicitly revoked keep authenticating for
// up to rotationGrace. The successor is asserted unaffected: revoking one
// row in a lineage must not cascade to a sibling row.
func TestAuthenticate_RevokedMidGrace_PredecessorCannotAuthenticate(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	m := newTestSessionManagerWithQueries(t, q, clock.Now)
	ctx := context.Background()

	raw, predSess, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	clock.Advance(rotationAge + time.Hour) // past rotationAge

	_, rotatedToken, err := m.Authenticate(ctx, raw)
	if err != nil {
		t.Fatalf("Authenticate() at 25h (should rotate) error = %v", err)
	}
	if rotatedToken == "" {
		t.Fatal("Authenticate() at 25h returned no rotatedToken, want rotation to have occurred")
	}

	// Still well inside the 60s grace window.
	clock.Advance(10 * time.Second)
	if err := m.Revoke(ctx, predSess.ID); err != nil {
		t.Fatalf("Revoke(predecessor) error = %v", err)
	}

	// Still inside grace by the clock alone (10s + a few more < 60s) --
	// only the explicit revoke should be what kills it.
	clock.Advance(5 * time.Second)
	if _, _, err := m.Authenticate(ctx, raw); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("Authenticate(predecessor token) after revoking the predecessor mid-grace error = %v, want ErrSessionInvalid", err)
	}

	// Orthogonality, the other direction: the successor is a different
	// row and must be untouched by revoking its predecessor.
	if _, _, err := m.Authenticate(ctx, rotatedToken); err != nil {
		t.Errorf("Authenticate(successor token) after revoking only the predecessor error = %v, want nil (revoke must not cascade to a sibling row)", err)
	}
}

// TestRequireRecentReauth_BoundaryAtExactly15Minutes pins the boundary
// task-7-brief.md's doc comment states explicitly: "returns
// ErrReauthRequired if now.Sub(sess.ReauthenticatedAt) > reauthWindow" --
// strict greater-than, so exactly 15m since reauthenticated_at is still
// within the window (inclusive), and only the instant after it is not.
// RequireRecentReauth is a pure function (no DB, no SessionManager), so
// this test needs neither TEST_DATABASE_URL nor a live session -- a bare
// store.Session literal is enough.
func TestRequireRecentReauth_BoundaryAtExactly15Minutes(t *testing.T) {
	base := testutil.Epoch

	tests := []struct {
		name    string
		elapsed time.Duration
		wantErr bool
	}{
		{"just under the window", reauthWindow - time.Nanosecond, false},
		{"exactly at the window (inclusive per strict >)", reauthWindow, false},
		{"one nanosecond past the window", reauthWindow + time.Nanosecond, true},
		{"well past the window", reauthWindow + time.Hour, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := store.Session{ReauthenticatedAt: base}
			now := base.Add(tt.elapsed)

			err := auth.RequireRecentReauth(sess, now)
			if tt.wantErr {
				if !errors.Is(err, auth.ErrReauthRequired) {
					t.Errorf("RequireRecentReauth() error = %v, want ErrReauthRequired", err)
				}
			} else if err != nil {
				t.Errorf("RequireRecentReauth() error = %v, want nil", err)
			}
		})
	}
}

// TestIssue_AlwaysNewRow_FixationProperty pins Issue's doc comment
// verbatim: "fixation defense: always used at login, never reuses an
// existing row." Two Issue calls for the same user must produce two
// distinct rows and two distinct raw tokens -- an implementation that
// reused or updated an existing row in place (e.g. keyed by user_id, "one
// active session per user") would defeat the fixation defense the
// comment claims and would also break "log in from two devices," which
// the sessions schema (no unique constraint on user_id) clearly allows
// for.
func TestIssue_AlwaysNewRow_FixationProperty(t *testing.T) {
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	m := newTestSessionManagerWithQueries(t, q, time.Now)
	ctx := context.Background()
	pool := newRowInspectorPool(t)

	raw1, sess1, err := m.Issue(ctx, userID, "ua-1", "1.2.3.4")
	if err != nil {
		t.Fatalf("first Issue() error = %v", err)
	}
	raw2, sess2, err := m.Issue(ctx, userID, "ua-2", "5.6.7.8")
	if err != nil {
		t.Fatalf("second Issue() error = %v", err)
	}

	if raw1 == raw2 {
		t.Error("two Issue() calls returned the same raw token, want distinct tokens")
	}
	if sess1.ID == sess2.ID {
		t.Error("two Issue() calls returned the same session id, want distinct rows")
	}

	if got := sessionRowCountForUser(ctx, t, pool, userID); got != 2 {
		t.Errorf("sessions row count for user after two Issue() calls = %d, want 2", got)
	}
	// Both must independently authenticate -- confirms they're really two
	// live, distinct rows, not e.g. one row whose token_hash got
	// overwritten by the second Issue (which would make raw1 stop
	// working).
	if _, _, err := m.Authenticate(ctx, raw1); err != nil {
		t.Errorf("Authenticate(raw1) after second Issue() error = %v, want nil", err)
	}
	if _, _, err := m.Authenticate(ctx, raw2); err != nil {
		t.Errorf("Authenticate(raw2) error = %v, want nil", err)
	}
}
