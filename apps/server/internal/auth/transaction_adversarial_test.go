// Adversarial, spec-derived tests for TransactionStore.Begin/Consume,
// covering the failure modes task-2-brief.md Step 4 requires (replay,
// expiry, provider mix-up, unknown handle) plus the concurrency and
// no-oracle properties the spec implies but Step 4's table doesn't spell
// out row-by-row. Originally authored independently, from the brief and
// docs/specs/aboutme-design.md §3 alone, without reading transaction.go;
// reconciled against the landed implementation (commit ccbb334) only for
// its two seams (clock injection, handle hashing) and to reuse this
// package's existing live-DB harness (newTestQueries/
// requireTestDatabaseURL, both defined in transaction_test.go) instead of
// duplicating it -- see notes.md's integration report for exactly what
// changed and why.
//
// Scope: this file does not test cookie.go, or Begin's own output shape
// (handle length, PKCE/nonce presence, PurposeLink/PurposeReauth
// round-tripping) -- those are covered by transaction_test.go's
// happy-path tests, authored separately.
package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// txTTL mirrors auth's unexported oauthTxTTL (design spec §3: the
// __Host-oauth-tx cookie is Max-Age=600 seconds = 10 minutes, matching
// transaction.go's `oauthTxTTL = 10 * time.Minute`). Redeclared here
// rather than referenced because oauthTxTTL is unexported and this file
// is black-box (package auth_test).
const txTTL = 10 * time.Minute

// ---- row-state assertion helpers --------------------------------------
//
// store.Queries (Task 1's committed querier.go) exposes no
// oauth_transactions accessors, so the replay test's "no second-row
// mutation" assertion goes straight at the table with a dedicated,
// unshared connection pool rather than inventing a store method.

// rowQuerier is satisfied by *store.Pool (via its embedded
// *pgxpool.Pool): the minimal surface rowState needs.
type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// handleHash reproduces oauth_transactions.handle_hash from a raw handle
// string. Confirmed against the landed implementation's own hashHandle
// (transaction.go): sha256.Sum256(handle) as a raw digest, matching
// design spec §3's description of the handle as "hashed at rest (sha256)
// exactly like the session token".
func handleHash(handle string) [sha256.Size]byte {
	return sha256.Sum256([]byte(handle))
}

// rowState reports how many oauth_transactions rows exist for handle and
// the latest consumed_at among them, letting tests verify Consume's
// "atomically marks consumed" promise. Using count(*)/max(consumed_at)
// rather than a plain SELECT means the query always returns exactly one
// row (count=0, consumed_at=NULL when the handle doesn't exist), so
// callers never have to special-case pgx.ErrNoRows.
func rowState(ctx context.Context, t *testing.T, db rowQuerier, handle string) (consumedAt *time.Time, count int) {
	t.Helper()

	sum := handleHash(handle)
	err := db.QueryRow(ctx,
		`SELECT count(*), max(consumed_at) FROM oauth_transactions WHERE handle_hash = $1`,
		sum[:],
	).Scan(&count, &consumedAt)
	if err != nil {
		t.Fatalf("rowState query error: %v", err)
	}
	return consumedAt, count
}

// newRowInspectorPool opens a small, dedicated connection pool against
// TEST_DATABASE_URL for rowState's direct table reads. It reuses
// requireTestDatabaseURL (defined in transaction_test.go) rather than
// re-reading the environment variable itself. Callers must call
// newTestQueries (which applies migrations idempotently) before this, in
// the same test, so the schema is guaranteed to exist by the time this
// pool is used.
func newRowInspectorPool(t *testing.T) *store.Pool {
	t.Helper()

	dsn := requireTestDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() error: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	return pool
}

// ---- misc test helpers -------------------------------------------------

// randomHandle returns a handle shaped exactly like a real one (32 raw
// bytes, base64url, no padding) that was never passed to Begin, for
// TestConsume_UnknownHandle.
func randomHandle(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("rand.Read() error: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

// assertTxEqual compares two auth.Transaction values field-by-field via
// ==: the struct's fields (two named strings, a uuid.UUID, and four
// plain strings) are all comparable, so a whole-struct != is sufficient
// and avoids a fragile field-by-field listing that would drift if
// Transaction gains fields.
func assertTxEqual(t *testing.T, got, want auth.Transaction) {
	t.Helper()
	if got != want {
		t.Errorf("Transaction mismatch:\n got  = %+v\n want = %+v", got, want)
	}
}

// ---- tests ---------------------------------------------------------

// TestOAuthTxCookieName_MatchesHostPrefixContract pins the literal value
// task-2-brief.md's "Produces" section specifies. This matters beyond
// naming taste: browsers only honor a __Host- prefixed cookie when it is
// also Secure, Path=/, and carries no Domain attribute (design spec §3),
// so a typo here would silently defeat that browser-enforced guarantee
// rather than fail loudly.
func TestOAuthTxCookieName_MatchesHostPrefixContract(t *testing.T) {
	const want = "__Host-oauth-tx"
	if auth.OAuthTxCookieName != want {
		t.Errorf("OAuthTxCookieName = %q, want %q", auth.OAuthTxCookieName, want)
	}
}

// TestConsume_RejectsReplay is task-2-brief.md Step 4's replay test:
// Begin, Consume once (must succeed), Consume the same handle again
// (must fail with ErrTransactionInvalid and must not mutate the row a
// second time). The row-state checks are the "no second-row mutation"
// half of the brief's assertion column, which a bare error check can't
// observe on its own.
func TestConsume_RejectsReplay(t *testing.T) {
	q := newTestQueries(t)
	ts := auth.NewTransactionStore(q)
	ctx := context.Background()
	inspector := newRowInspectorPool(t)

	handle, began, err := ts.Begin(ctx, auth.ProviderGoogle, auth.PurposeLogin, uuid.Nil,
		"https://aboutme.vn/api/v1/auth/google/callback")
	if err != nil {
		t.Fatalf("Begin() error: %v", err)
	}

	got1, err := ts.Consume(ctx, handle, auth.ProviderGoogle)
	if err != nil {
		t.Fatalf("first Consume() error: %v, want nil", err)
	}
	assertTxEqual(t, got1, began)

	consumedAt1, count1 := rowState(ctx, t, inspector, handle)
	if consumedAt1 == nil {
		t.Fatal("after first Consume(): consumed_at is NULL, want non-NULL")
	}
	if count1 != 1 {
		t.Fatalf("after first Consume(): %d rows for handle, want 1", count1)
	}

	if _, err := ts.Consume(ctx, handle, auth.ProviderGoogle); !errors.Is(err, auth.ErrTransactionInvalid) {
		t.Fatalf("second (replayed) Consume() error = %v, want ErrTransactionInvalid", err)
	}

	consumedAt2, count2 := rowState(ctx, t, inspector, handle)
	if count2 != 1 {
		t.Errorf("after replayed Consume(): %d rows for handle, want 1 (no new row created)", count2)
	}
	if consumedAt2 == nil || !consumedAt2.Equal(*consumedAt1) {
		t.Errorf("after replayed Consume(): consumed_at = %v, want unchanged %v (no second-row mutation)", consumedAt2, consumedAt1)
	}
}

// TestConsume_RejectsExpired is task-2-brief.md Step 4's expiry test,
// driven by an injected clock (auth.NewTransactionStoreForTest) instead
// of a sleep. It also covers the brief's suggested "replay-after-expiry"
// strengthening: an expired transaction must stay rejected on every
// subsequent attempt, not just the first.
//
// This file deliberately does NOT assert whether an expired Consume
// leaves consumed_at NULL or sets it: DD-C2 (integration-owner ruling)
// records that as unpinned, so a test asserting either reading would be
// testing an implementation detail the spec doesn't fix, not a
// contract.
func TestConsume_RejectsExpired(t *testing.T) {
	q := newTestQueries(t)
	clk := testutil.NewClockAtEpoch()
	ts := auth.NewTransactionStoreForTest(q, clk.Now)
	ctx := context.Background()

	handle, _, err := ts.Begin(ctx, auth.ProviderGoogle, auth.PurposeLogin, uuid.Nil,
		"https://aboutme.vn/api/v1/auth/google/callback")
	if err != nil {
		t.Fatalf("Begin() error: %v", err)
	}

	clk.Advance(txTTL + time.Second)

	if _, err := ts.Consume(ctx, handle, auth.ProviderGoogle); !errors.Is(err, auth.ErrTransactionInvalid) {
		t.Fatalf("Consume() after TTL error = %v, want ErrTransactionInvalid", err)
	}

	// Replay-after-expiry: still rejected, not a different outcome.
	if _, err := ts.Consume(ctx, handle, auth.ProviderGoogle); !errors.Is(err, auth.ErrTransactionInvalid) {
		t.Errorf("second Consume() after TTL error = %v, want ErrTransactionInvalid", err)
	}
}

// TestConsume_RejectsProviderMismatch is task-2-brief.md Step 4's mix-up
// regression test, generalized from the single GitHub-begin/
// Google-consume example to every ordered pair of the three providers:
// a defense that only special-cases one pair would be a plausible bug
// (e.g. `if expectedProvider == ProviderGoogle && tx.Provider ==
// ProviderGitHub`) that a narrower test would miss.
//
// It also asserts DD-C1 (integration-owner ruling, binding): Consume is
// single-attempt fail-closed, so a mismatched-provider attempt burns the
// transaction even though it fails -- a subsequent Consume with the
// *correct* provider must also return ErrTransactionInvalid, not
// succeed. transaction.go's own Consume doc comment confirms this is the
// intended design ("checked here in Go ... after the row has already
// been atomically claimed ... so a mismatched-provider attempt still
// burns the transaction"); this test holds that behavior to its
// contract rather than just trusting the comment.
func TestConsume_RejectsProviderMismatch(t *testing.T) {
	tests := []struct {
		name      string
		begin     auth.Provider
		consumeAs auth.Provider
	}{
		{name: "github begin, google consume", begin: auth.ProviderGitHub, consumeAs: auth.ProviderGoogle},
		{name: "google begin, github consume", begin: auth.ProviderGoogle, consumeAs: auth.ProviderGitHub},
		{name: "google begin, linkedin consume", begin: auth.ProviderGoogle, consumeAs: auth.ProviderLinkedIn},
		{name: "linkedin begin, google consume", begin: auth.ProviderLinkedIn, consumeAs: auth.ProviderGoogle},
		{name: "github begin, linkedin consume", begin: auth.ProviderGitHub, consumeAs: auth.ProviderLinkedIn},
		{name: "linkedin begin, github consume", begin: auth.ProviderLinkedIn, consumeAs: auth.ProviderGitHub},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := newTestQueries(t)
			ts := auth.NewTransactionStore(q)
			ctx := context.Background()

			handle, _, err := ts.Begin(ctx, tt.begin, auth.PurposeLogin, uuid.Nil,
				fmt.Sprintf("https://aboutme.vn/api/v1/auth/%s/callback", tt.begin))
			if err != nil {
				t.Fatalf("Begin() error: %v", err)
			}

			// The handle itself is valid and unexpired -- Consume must
			// still reject it under the wrong provider (RFC 9700 §4.4
			// mix-up defense). This matters concretely because
			// __Host-oauth-tx is Path=/ and Lax, so the browser will
			// attach it to any /api/v1/auth/*/callback request,
			// including a different provider's.
			if _, err := ts.Consume(ctx, handle, tt.consumeAs); !errors.Is(err, auth.ErrTransactionInvalid) {
				t.Fatalf("Consume(as %s) error = %v, want ErrTransactionInvalid", tt.consumeAs, err)
			}

			// DD-C1: the mismatched attempt above already burned the
			// transaction. A retry with the *correct* provider must not
			// be allowed to succeed just because it names the right
			// provider this time.
			if _, err := ts.Consume(ctx, handle, tt.begin); !errors.Is(err, auth.ErrTransactionInvalid) {
				t.Errorf("Consume(as %s, the correct provider) after a mismatched attempt error = %v, want ErrTransactionInvalid (DD-C1: single-attempt fail-closed)", tt.begin, err)
			}
		})
	}
}

// TestConsume_UnknownHandle is task-2-brief.md Step 4's last required
// test: a handle shaped exactly like a real one but never begun must
// fail the same way as every other invalid case.
func TestConsume_UnknownHandle(t *testing.T) {
	q := newTestQueries(t)
	ts := auth.NewTransactionStore(q)
	ctx := context.Background()

	handle := randomHandle(t)

	if _, err := ts.Consume(ctx, handle, auth.ProviderGoogle); !errors.Is(err, auth.ErrTransactionInvalid) {
		t.Fatalf("Consume(unknown handle) error = %v, want ErrTransactionInvalid", err)
	}
}

// TestConsume_NoOracleAcrossFailureModes strengthens all four required
// tests together: it isn't enough that each failure independently
// satisfies errors.Is(err, ErrTransactionInvalid); the error's *text*
// must also be indistinguishable across replay, expiry, mismatch, and
// unknown-handle. task-2-brief.md's comment on ErrTransactionInvalid is
// explicit that this is deliberate ("not found / expired / already
// consumed / provider mismatch — one sentinel deliberately ... not
// giving an attacker an oracle"); a fmt.Errorf("...: %w", ...) wrap that
// added scenario-specific context in only some branches would satisfy
// errors.Is while still reopening exactly that oracle, so this is a
// distinct failure mode from what the four individual tests already
// catch.
func TestConsume_NoOracleAcrossFailureModes(t *testing.T) {
	errs := make(map[string]error, 4)

	// replay
	{
		q := newTestQueries(t)
		ts := auth.NewTransactionStore(q)
		ctx := context.Background()
		handle, _, err := ts.Begin(ctx, auth.ProviderGoogle, auth.PurposeLogin, uuid.Nil,
			"https://aboutme.vn/api/v1/auth/google/callback")
		if err != nil {
			t.Fatalf("replay setup: Begin() error: %v", err)
		}
		if _, firstErr := ts.Consume(ctx, handle, auth.ProviderGoogle); firstErr != nil {
			t.Fatalf("replay setup: first Consume() error: %v, want nil", firstErr)
		}
		_, err = ts.Consume(ctx, handle, auth.ProviderGoogle)
		errs["replay"] = err
	}

	// expired
	{
		q := newTestQueries(t)
		clk := testutil.NewClockAtEpoch()
		ts := auth.NewTransactionStoreForTest(q, clk.Now)
		ctx := context.Background()
		handle, _, err := ts.Begin(ctx, auth.ProviderGoogle, auth.PurposeLogin, uuid.Nil,
			"https://aboutme.vn/api/v1/auth/google/callback")
		if err != nil {
			t.Fatalf("expired setup: Begin() error: %v", err)
		}
		clk.Advance(txTTL + time.Second)
		_, err = ts.Consume(ctx, handle, auth.ProviderGoogle)
		errs["expired"] = err
	}

	// provider mismatch
	{
		q := newTestQueries(t)
		ts := auth.NewTransactionStore(q)
		ctx := context.Background()
		handle, _, err := ts.Begin(ctx, auth.ProviderGitHub, auth.PurposeLogin, uuid.Nil,
			"https://aboutme.vn/api/v1/auth/github/callback")
		if err != nil {
			t.Fatalf("mismatch setup: Begin() error: %v", err)
		}
		_, err = ts.Consume(ctx, handle, auth.ProviderGoogle)
		errs["mismatch"] = err
	}

	// unknown handle
	{
		q := newTestQueries(t)
		ts := auth.NewTransactionStore(q)
		ctx := context.Background()
		_, err := ts.Consume(ctx, randomHandle(t), auth.ProviderGoogle)
		errs["unknown"] = err
	}

	for name, err := range errs {
		if !errors.Is(err, auth.ErrTransactionInvalid) {
			t.Errorf("%s: error = %v, want ErrTransactionInvalid", name, err)
		}
	}

	want := auth.ErrTransactionInvalid.Error()
	for name, err := range errs {
		if err == nil {
			continue
		}
		if got := err.Error(); got != want {
			t.Errorf("%s: error text = %q, want %q (identical text across failure modes -- no oracle)", name, got, want)
		}
	}
}

// TestConsume_ConcurrentDoubleConsume_ExactlyOneSucceeds derives from
// the Consume doc comment's "atomically marks the transaction consumed"
// promise: atomicity is only meaningfully tested under real concurrency,
// not two sequential calls on one goroutine (that's TestConsume_
// RejectsReplay). It races many goroutines at the same handle through
// newTestQueries' pool-backed store (safe for concurrent use, unlike a
// single pgx.Tx) and requires exactly one winner -- a Consume implemented
// as a SELECT-then-UPDATE instead of a single conditional UPDATE (e.g.
// `WHERE consumed_at IS NULL`) would let more than one goroutine win the
// race under real concurrent load, which is the classic TOCTOU bug this
// test exists to catch.
func TestConsume_ConcurrentDoubleConsume_ExactlyOneSucceeds(t *testing.T) {
	q := newTestQueries(t)
	ts := auth.NewTransactionStore(q)
	ctx := context.Background()

	handle, began, err := ts.Begin(ctx, auth.ProviderGoogle, auth.PurposeLogin, uuid.Nil,
		"https://aboutme.vn/api/v1/auth/google/callback")
	if err != nil {
		t.Fatalf("Begin() error: %v", err)
	}

	const workers = 10
	results := make([]struct {
		tx  auth.Transaction
		err error
	}, workers)

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := range results {
		go func(i int) {
			defer wg.Done()
			results[i].tx, results[i].err = ts.Consume(ctx, handle, auth.ProviderGoogle)
		}(i)
	}
	wg.Wait()

	var successes, invalidFailures, otherFailures int
	var winner auth.Transaction
	for _, r := range results {
		switch {
		case r.err == nil:
			successes++
			winner = r.tx
		case errors.Is(r.err, auth.ErrTransactionInvalid):
			invalidFailures++
		default:
			otherFailures++
			t.Errorf("unexpected error from concurrent Consume(): %v", r.err)
		}
	}

	if successes != 1 {
		t.Errorf("successes = %d, want exactly 1 (atomic single-consume guarantee)", successes)
	}
	if invalidFailures != workers-1 {
		t.Errorf("invalidFailures = %d, want %d", invalidFailures, workers-1)
	}
	if otherFailures != 0 {
		t.Errorf("otherFailures = %d, want 0", otherFailures)
	}
	if successes == 1 {
		assertTxEqual(t, winner, began)
	}
}
