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
)

func TestPendingCSRFValid_RequiresExactIndependentSecret(t *testing.T) {
	pending := store.PendingAuthentication{CSRFSecret: []byte("independent-secret")}
	if !auth.PendingCSRFValid(pending, []byte("independent-secret")) {
		t.Fatal("PendingCSRFValid rejected the stored pending secret")
	}
	if auth.PendingCSRFValid(pending, []byte("independent-secreu")) {
		t.Fatal("PendingCSRFValid accepted a different pending secret")
	}
}

func TestPendingAuthentication_SixthCreationConsumesOldest(t *testing.T) {
	ctx := context.Background()
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	m := auth.NewPendingAuthenticationManager(newTestPool(t), nil)
	request := auth.PendingAuthenticationRequest{UserID: userID, Purpose: auth.PendingAuthenticationPurposeLogin, ReturnPath: "/app/resumes"}
	var oldest uuid.UUID
	for range 5 {
		issued, err := m.Create(ctx, request)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if oldest == uuid.Nil {
			oldest = issued.Pending.ID
		}
	}
	if _, err := m.Create(ctx, request); err != nil {
		t.Fatalf("sixth Create() error = %v", err)
	}
	var consumed bool
	if err := newTestPool(t).QueryRow(ctx, "SELECT consumed_at IS NOT NULL FROM pending_authentications WHERE id = $1", oldest).Scan(&consumed); err != nil {
		t.Fatalf("read oldest pending row: %v", err)
	}
	if !consumed {
		t.Fatal("sixth pending creation left the oldest live row unconsumed")
	}
}

func TestPendingAuthentication_RejectsStaleEpoch(t *testing.T) {
	ctx := context.Background()
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	m := auth.NewPendingAuthenticationManager(newTestPool(t), nil)
	issued, err := m.Create(ctx, auth.PendingAuthenticationRequest{UserID: userID, Purpose: auth.PendingAuthenticationPurposeLogin, ReturnPath: "/app/resumes"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, execErr := newTestPool(t).Exec(ctx, "UPDATE users SET auth_epoch = auth_epoch + 1 WHERE id = $1", userID); execErr != nil {
		t.Fatalf("advance auth epoch: %v", execErr)
	}
	if _, authErr := m.Authenticate(ctx, issued.RawToken, nil); !errors.Is(authErr, auth.ErrPendingAuthenticationRequired) {
		t.Fatalf("Authenticate(stale epoch) error = %v, want ErrPendingAuthenticationRequired", authErr)
	}
}

func TestPendingAuthentication_CreateConsumesOnlyPreviousBrowserPending(t *testing.T) {
	ctx := context.Background()
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	m := auth.NewPendingAuthenticationManager(newTestPool(t), nil)
	req := auth.PendingAuthenticationRequest{UserID: userID, Purpose: auth.PendingAuthenticationPurposeLogin, ReturnPath: "/app/resumes"}
	first, err := m.Create(ctx, req)
	if err != nil {
		t.Fatalf("Create(first) error = %v", err)
	}
	other, err := m.Create(ctx, req)
	if err != nil {
		t.Fatalf("Create(other browser) error = %v", err)
	}
	req.PreviousRawToken = first.RawToken
	if _, err := m.Create(ctx, req); err != nil {
		t.Fatalf("Create(replacement) error = %v", err)
	}
	pool := newTestPool(t)
	var firstConsumed, otherConsumed bool
	if err := pool.QueryRow(ctx, "SELECT consumed_at IS NOT NULL FROM pending_authentications WHERE id = $1", first.Pending.ID).Scan(&firstConsumed); err != nil {
		t.Fatalf("read first pending: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT consumed_at IS NOT NULL FROM pending_authentications WHERE id = $1", other.Pending.ID).Scan(&otherConsumed); err != nil {
		t.Fatalf("read other pending: %v", err)
	}
	if !firstConsumed || otherConsumed {
		t.Fatalf("prior replacement state = first consumed %t, other consumed %t", firstConsumed, otherConsumed)
	}
}

// pendingConsumed reports whether the pending row id has been consumed.
func pendingConsumed(ctx context.Context, t *testing.T, pool *store.Pool, id uuid.UUID) bool {
	t.Helper()
	var consumed bool
	if err := pool.QueryRow(ctx, "SELECT consumed_at IS NOT NULL FROM pending_authentications WHERE id = $1", id).Scan(&consumed); err != nil {
		t.Fatalf("read pending %v: %v", id, err)
	}
	return consumed
}

// TestPendingAuthentication_PreviousTokenCannotCrossUser keeps another
// account's pending row live: consuming it would write that account's authority
// without its user lock.
func TestPendingAuthentication_PreviousTokenCannotCrossUser(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	q := store.New(pool)
	firstUser := createTestUser(t, q)
	secondUser := createTestUser(t, q)
	m := auth.NewPendingAuthenticationManager(pool, nil)
	login := auth.PendingAuthenticationRequest{UserID: firstUser, Purpose: auth.PendingAuthenticationPurposeLogin, ReturnPath: "/app/resumes"}
	issued, err := m.Create(ctx, login)
	if err != nil {
		t.Fatalf("Create(first user) error = %v", err)
	}
	login.UserID = secondUser
	login.PreviousRawToken = issued.RawToken
	if _, err = m.Create(ctx, login); err != nil {
		t.Fatalf("Create(cross-user replacement) error = %v", err)
	}
	if pendingConsumed(ctx, t, pool, issued.Pending.ID) {
		t.Fatal("cross-user previous token was consumed")
	}
}

// TestPendingAuthentication_PreviousTokenConsumedAcrossPurposeAndBinding
// consumes the browser's previous pending row for the same account whatever its
// purpose or bound session.
func TestPendingAuthentication_PreviousTokenConsumedAcrossPurposeAndBinding(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	q := store.New(pool)
	userID := createTestUser(t, q)
	sm := auth.NewSessionManagerWithPoolForTest(pool, time.Now)
	_, firstSession, err := sm.Issue(ctx, userID, "ua", "203.0.113.30")
	if err != nil {
		t.Fatalf("Issue(first session) error = %v", err)
	}
	_, secondSession, err := sm.Issue(ctx, userID, "ua", "203.0.113.31")
	if err != nil {
		t.Fatalf("Issue(second session) error = %v", err)
	}
	m := auth.NewPendingAuthenticationManager(pool, nil)
	login, err := m.Create(ctx, auth.PendingAuthenticationRequest{UserID: userID, Purpose: auth.PendingAuthenticationPurposeLogin, ReturnPath: "/app/resumes"})
	if err != nil {
		t.Fatalf("Create(login) error = %v", err)
	}
	reauth := auth.PendingAuthenticationRequest{UserID: userID, Purpose: auth.PendingAuthenticationPurposeReauth, ReturnPath: "/app/settings/sessions", SessionID: &firstSession.ID, PreviousRawToken: login.RawToken}
	bound, err := m.Create(ctx, reauth)
	if err != nil {
		t.Fatalf("Create(reauth after login) error = %v", err)
	}
	if !pendingConsumed(ctx, t, pool, login.Pending.ID) {
		t.Fatal("reauth creation left the previous login pending row live")
	}
	reauth.SessionID = &secondSession.ID
	reauth.PreviousRawToken = bound.RawToken
	if _, err = m.Create(ctx, reauth); err != nil {
		t.Fatalf("Create(reauth for another session) error = %v", err)
	}
	if !pendingConsumed(ctx, t, pool, bound.Pending.ID) {
		t.Fatal("new pending creation left the previous differently bound row live")
	}
}

func TestPendingAuthentication_CallbackRollbackPreservesAuthority(t *testing.T) {
	ctx := context.Background()
	q := newTestQueries(t)
	m := auth.NewPendingAuthenticationManager(newTestPool(t), nil)
	issued, err := m.Create(ctx, auth.PendingAuthenticationRequest{UserID: createTestUser(t, q), Purpose: auth.PendingAuthenticationPurposeLogin, ReturnPath: "/app/resumes"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	rollback := errors.New("deliberate callback rollback")
	if _, completeErr := m.WithLivePending(ctx, issued.RawToken, nil, nil, func(ctx context.Context, qtx *store.Queries, locked auth.PendingAuthenticationLocked) error {
		if claimErr := m.ClaimTx(ctx, qtx, locked.Pending); claimErr != nil {
			return claimErr
		}
		return rollback
	}); !errors.Is(completeErr, rollback) {
		t.Fatalf("WithLivePending() error = %v, want callback error", completeErr)
	}
	if _, authErr := m.Authenticate(ctx, issued.RawToken, nil); authErr != nil {
		t.Fatalf("pending authority after callback rollback = %v", authErr)
	}
}

// TestPendingAuthentication_CreateSerializesWithCompletionAndFailure races a
// replacing Create against a completion or a recorded failure on the same
// previous row. Exactly one of them acts on that row, and the end state
// matches the winner: the previous row is consumed once, a recorded failure
// appears only when the completion side won, and one live row remains.
func TestPendingAuthentication_CreateSerializesWithCompletionAndFailure(t *testing.T) {
	const rounds = 4
	verifyErr := errors.New("verification failed")
	for _, recordFailure := range []bool{false, true} {
		name := "claim"
		if recordFailure {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			pool := newTestPool(t)
			q := store.New(pool)
			m := auth.NewPendingAuthenticationManager(pool, nil)
			for round := range rounds {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				userID := createTestUser(t, q)
				req := auth.PendingAuthenticationRequest{UserID: userID, Purpose: auth.PendingAuthenticationPurposeLogin, ReturnPath: "/app/resumes"}
				issued, err := m.Create(ctx, req)
				if err != nil {
					cancel()
					t.Fatalf("round %d: Create(initial) error = %v", round, err)
				}
				req.PreviousRawToken = issued.RawToken
				start := make(chan struct{})
				var (
					group       sync.WaitGroup
					createErr   error
					replaced    auth.PendingAuthenticationIssue
					completeErr error
				)
				group.Add(2)
				go func() {
					defer group.Done()
					<-start
					replaced, createErr = m.Create(ctx, req)
				}()
				go func() {
					defer group.Done()
					<-start
					_, completeErr = m.WithLivePending(ctx, issued.RawToken, nil, nil, func(ctx context.Context, qtx *store.Queries, locked auth.PendingAuthenticationLocked) error {
						if recordFailure {
							return auth.FailPendingVerification(verifyErr, nil)
						}
						return m.ClaimTx(ctx, qtx, locked.Pending)
					})
				}()
				close(start)
				group.Wait()
				cancel()

				if createErr != nil {
					t.Fatalf("round %d: replacing Create() error = %v", round, createErr)
				}
				completionWon := completeErr == nil
				if recordFailure {
					var failure *auth.PendingVerificationFailure
					completionWon = errors.As(completeErr, &failure)
					if completionWon && (failure.Exhausted || !errors.Is(completeErr, verifyErr)) {
						t.Fatalf("round %d: failure = %+v, want one unexhausted recorded failure", round, failure)
					}
				}
				if !completionWon && !errors.Is(completeErr, auth.ErrPendingAuthenticationRequired) {
					t.Fatalf("round %d: completion error = %v, want a win or ErrPendingAuthenticationRequired", round, completeErr)
				}
				var consumed bool
				var attempts int32
				if scanErr := pool.QueryRow(context.Background(), "SELECT consumed_at IS NOT NULL, failed_attempts FROM pending_authentications WHERE id = $1", issued.Pending.ID).Scan(&consumed, &attempts); scanErr != nil {
					t.Fatalf("round %d: read previous pending: %v", round, scanErr)
				}
				if !consumed {
					t.Fatalf("round %d: previous pending row still live", round)
				}
				wantAttempts := int32(0)
				if recordFailure && completionWon {
					wantAttempts = 1
				}
				if attempts != wantAttempts {
					t.Fatalf("round %d: previous failed attempts = %d, want %d (completion won = %t)", round, attempts, wantAttempts, completionWon)
				}
				var live int
				if scanErr := pool.QueryRow(context.Background(), "SELECT count(*) FROM pending_authentications WHERE user_id = $1 AND consumed_at IS NULL", userID).Scan(&live); scanErr != nil {
					t.Fatalf("round %d: count live pending: %v", round, scanErr)
				}
				if live != 1 {
					t.Fatalf("round %d: live pending rows = %d, want only the replacement", round, live)
				}
				if _, authErr := m.Authenticate(context.Background(), replaced.RawToken, nil); authErr != nil {
					t.Fatalf("round %d: Authenticate(replacement) error = %v", round, authErr)
				}
			}
		})
	}
}

func TestPendingAuthentication_WrongReauthBindingAuthenticatesNothing(t *testing.T) {
	ctx := context.Background()
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	sm := auth.NewSessionManager(q)
	_, first, err := sm.Issue(ctx, userID, "ua", "203.0.113.20")
	if err != nil {
		t.Fatalf("Issue(first) error = %v", err)
	}
	_, second, err := sm.Issue(ctx, userID, "ua", "203.0.113.21")
	if err != nil {
		t.Fatalf("Issue(second) error = %v", err)
	}
	m := auth.NewPendingAuthenticationManager(newTestPool(t), nil)
	issued, err := m.Create(ctx, auth.PendingAuthenticationRequest{UserID: userID, Purpose: auth.PendingAuthenticationPurposeReauth, ReturnPath: "/app/settings/sessions", SessionID: &first.ID})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, authErr := m.Authenticate(ctx, issued.RawToken, &second.ID); !errors.Is(authErr, auth.ErrPendingAuthenticationRequired) {
		t.Fatalf("Authenticate(wrong binding) error = %v, want ErrPendingAuthenticationRequired", authErr)
	}
	if _, authErr := m.Authenticate(ctx, issued.RawToken, nil); !errors.Is(authErr, auth.ErrPendingAuthenticationRequired) {
		t.Fatalf("Authenticate(missing binding) error = %v, want ErrPendingAuthenticationRequired", authErr)
	}
	if _, authErr := m.Authenticate(ctx, issued.RawToken, &first.ID); authErr != nil {
		t.Fatalf("Authenticate(correct binding) error = %v", authErr)
	}
}

// TestPendingAuthentication_LoginRowIgnoresPresentedSession lets a browser that
// still holds a session cookie complete a login row. The login row binds
// nothing and exposes no session to the completion callback.
func TestPendingAuthentication_LoginRowIgnoresPresentedSession(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	q := store.New(pool)
	userID := createTestUser(t, q)
	_, sess, err := auth.NewSessionManagerWithPoolForTest(pool, time.Now).Issue(ctx, userID, "ua", "203.0.113.22")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	m := auth.NewPendingAuthenticationManager(pool, nil)
	issued, err := m.Create(ctx, auth.PendingAuthenticationRequest{UserID: userID, Purpose: auth.PendingAuthenticationPurposeLogin, ReturnPath: "/app/resumes"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	foreign := uuid.New()
	for _, presented := range []*uuid.UUID{&sess.ID, &foreign} {
		locked, lockErr := m.WithLivePending(ctx, issued.RawToken, presented, nil, nil)
		if lockErr != nil {
			t.Fatalf("WithLivePending(login, session %v) error = %v", *presented, lockErr)
		}
		if locked.Session != nil || locked.Pending.SessionID != nil {
			t.Fatalf("login row bound a session: locked session = %v, row session = %v", locked.Session, locked.Pending.SessionID)
		}
	}
}

// TestPendingAuthentication_CreateRejectsLoginSessionBinding keeps login rows
// unbound: a caller that supplies a session ID for login gets an error and no
// row is written.
func TestPendingAuthentication_CreateRejectsLoginSessionBinding(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	q := store.New(pool)
	userID := createTestUser(t, q)
	_, sess, err := auth.NewSessionManagerWithPoolForTest(pool, time.Now).Issue(ctx, userID, "ua", "203.0.113.23")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	m := auth.NewPendingAuthenticationManager(pool, nil)
	if _, createErr := m.Create(ctx, auth.PendingAuthenticationRequest{UserID: userID, Purpose: auth.PendingAuthenticationPurposeLogin, ReturnPath: "/app/resumes", SessionID: &sess.ID}); createErr == nil {
		t.Fatal("Create(login with session ID) error = nil, want rejection")
	}
	var rows int
	if scanErr := pool.QueryRow(ctx, "SELECT count(*) FROM pending_authentications WHERE user_id = $1", userID).Scan(&rows); scanErr != nil {
		t.Fatalf("count pending rows: %v", scanErr)
	}
	if rows != 0 {
		t.Fatalf("pending rows after rejected login binding = %d, want 0", rows)
	}
}

// TestPendingAuthentication_ExpiredRowAuthenticatesNothing ages a row past its
// five-minute lifetime and proves neither status nor completion accepts it.
func TestPendingAuthentication_ExpiredRowAuthenticatesNothing(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	q := store.New(pool)
	m := auth.NewPendingAuthenticationManager(pool, nil)
	issued, err := m.Create(ctx, auth.PendingAuthenticationRequest{UserID: createTestUser(t, q), Purpose: auth.PendingAuthenticationPurposeLogin, ReturnPath: "/app/resumes"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, execErr := pool.Exec(ctx, "UPDATE pending_authentications SET created_at = created_at - interval '6 minutes', expires_at = expires_at - interval '6 minutes' WHERE id = $1", issued.Pending.ID); execErr != nil {
		t.Fatalf("age pending row: %v", execErr)
	}
	if _, authErr := m.Authenticate(ctx, issued.RawToken, nil); !errors.Is(authErr, auth.ErrPendingAuthenticationRequired) {
		t.Fatalf("Authenticate(expired) error = %v, want ErrPendingAuthenticationRequired", authErr)
	}
	called := false
	_, completeErr := m.WithLivePending(ctx, issued.RawToken, nil, nil, func(ctx context.Context, qtx *store.Queries, locked auth.PendingAuthenticationLocked) error {
		called = true
		return m.ClaimTx(ctx, qtx, locked.Pending)
	})
	if !errors.Is(completeErr, auth.ErrPendingAuthenticationRequired) || called {
		t.Fatalf("WithLivePending(expired) error = %v, callback called = %t", completeErr, called)
	}
}

func TestPendingAuthentication_ConcurrentClaimHasOneWinner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := newTestQueries(t)
	m := auth.NewPendingAuthenticationManager(newTestPool(t), nil)
	issued, err := m.Create(ctx, auth.PendingAuthenticationRequest{UserID: createTestUser(t, q), Purpose: auth.PendingAuthenticationPurposeLogin, ReturnPath: "/app/resumes"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = m.WithLivePending(ctx, issued.RawToken, nil, nil, func(ctx context.Context, qtx *store.Queries, locked auth.PendingAuthenticationLocked) error {
				return m.ClaimTx(ctx, qtx, locked.Pending)
			})
		}(i)
	}
	wg.Wait()
	winners := 0
	for _, claimErr := range errs {
		if claimErr == nil {
			winners++
		} else if !errors.Is(claimErr, auth.ErrPendingAuthenticationRequired) {
			t.Fatalf("Claim() error = %v", claimErr)
		}
	}
	if winners != 1 {
		t.Fatalf("successful concurrent claims = %d, want 1", winners)
	}
}

func TestPendingAuthentication_FifthFailureConsumesPending(t *testing.T) {
	ctx := context.Background()
	q := newTestQueries(t)
	m := auth.NewPendingAuthenticationManager(newTestPool(t), nil)
	issued, err := m.Create(ctx, auth.PendingAuthenticationRequest{UserID: createTestUser(t, q), Purpose: auth.PendingAuthenticationPurposeLogin, ReturnPath: "/app/resumes"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	for attempt := 1; attempt <= 5; attempt++ {
		var (
			pending   store.PendingAuthentication
			exhausted bool
		)
		_, failureErr := m.WithLivePending(ctx, issued.RawToken, nil, nil, func(ctx context.Context, qtx *store.Queries, locked auth.PendingAuthenticationLocked) error {
			var err error
			pending, exhausted, err = m.RecordFailureTx(ctx, qtx, locked.Pending)
			return err
		})
		if failureErr != nil {
			t.Fatalf("RecordFailure(%d) error = %v", attempt, failureErr)
		}
		if got, want := pending.FailedAttempts, int32(attempt); got != want {
			t.Fatalf("RecordFailure(%d) attempts = %d, want %d", attempt, got, want)
		}
		if exhausted != (attempt == 5) {
			t.Fatalf("RecordFailure(%d) exhausted = %t", attempt, exhausted)
		}
	}
	if _, err := m.Authenticate(ctx, issued.RawToken, nil); !errors.Is(err, auth.ErrPendingAuthenticationRequired) {
		t.Fatalf("Authenticate(after exhaustion) error = %v, want ErrPendingAuthenticationRequired", err)
	}
}

// pendingFailureFixture creates a login pending row and returns what the
// verification-failure tests need.
func pendingFailureFixture(t *testing.T) (context.Context, *store.Pool, *auth.PendingAuthenticationManager, uuid.UUID, auth.PendingAuthenticationIssue) {
	t.Helper()
	ctx := context.Background()
	pool := newTestPool(t)
	userID := createTestUser(t, store.New(pool))
	m := auth.NewPendingAuthenticationManager(pool, nil)
	issued, err := m.Create(ctx, auth.PendingAuthenticationRequest{UserID: userID, Purpose: auth.PendingAuthenticationPurposeLogin, ReturnPath: "/app/resumes"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return ctx, pool, m, userID, issued
}

// insertSecurityEvent stands in for a completion write such as a counter
// security event or an exhaustion outbox insert.
func insertSecurityEvent(ctx context.Context, qtx *store.Queries, userID uuid.UUID) error {
	_, err := qtx.InsertPasskeyCounterSecurityEvent(ctx, store.InsertPasskeyCounterSecurityEventParams{
		UserID: userID, PasskeyID: uuid.New(), StoredCounter: 7, ReceivedCounter: 7, OccurredAt: time.Now().UTC(),
	})
	return err
}

// pendingFailureState reads the row's attempt count and consumption and the
// user's security event count.
func pendingFailureState(ctx context.Context, t *testing.T, pool *store.Pool, pendingID, userID uuid.UUID) (attempts int32, consumed bool, events int) {
	t.Helper()
	if err := pool.QueryRow(ctx, "SELECT failed_attempts, consumed_at IS NOT NULL FROM pending_authentications WHERE id = $1", pendingID).Scan(&attempts, &consumed); err != nil {
		t.Fatalf("read pending row: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM authentication_security_events WHERE user_id = $1", userID).Scan(&events); err != nil {
		t.Fatalf("count security events: %v", err)
	}
	return attempts, consumed, events
}

// failPending runs one completion whose callback writes an event and then
// reports a verification failure with the given exhaustion hook.
func failPending(ctx context.Context, m *auth.PendingAuthenticationManager, raw string, userID uuid.UUID, verifyErr error, hook auth.PendingExhaustedHook) (auth.PendingAuthenticationLocked, error) {
	return m.WithLivePending(ctx, raw, nil, nil, func(ctx context.Context, qtx *store.Queries, _ auth.PendingAuthenticationLocked) error {
		if insertErr := insertSecurityEvent(ctx, qtx, userID); insertErr != nil {
			return insertErr
		}
		return auth.FailPendingVerification(verifyErr, hook)
	})
}

// TestPendingAuthentication_FailureCommitsCallbackWrites proves a verification
// failure commits the callback's writes together with the counted attempt.
func TestPendingAuthentication_FailureCommitsCallbackWrites(t *testing.T) {
	ctx, pool, m, userID, issued := pendingFailureFixture(t)
	verifyErr := errors.New("counter did not increase")
	locked, err := failPending(ctx, m, issued.RawToken, userID, verifyErr, nil)
	var failure *auth.PendingVerificationFailure
	if !errors.As(err, &failure) || !errors.Is(err, verifyErr) || failure.Exhausted {
		t.Fatalf("WithLivePending() error = %v, want one unexhausted verification failure", err)
	}
	if locked.Pending.FailedAttempts != 1 {
		t.Fatalf("returned attempts = %d, want 1", locked.Pending.FailedAttempts)
	}
	attempts, consumed, events := pendingFailureState(ctx, t, pool, issued.Pending.ID, userID)
	if attempts != 1 || consumed || events != 1 {
		t.Fatalf("state = attempts %d, consumed %t, events %d, want 1, false, 1", attempts, consumed, events)
	}
}

// TestPendingAuthentication_FifthFailureRunsExhaustionHookAtomically proves the
// fifth failure, its consumption, and the exhaustion hook's insert commit in one
// transaction, and the hook runs only on that attempt.
func TestPendingAuthentication_FifthFailureRunsExhaustionHookAtomically(t *testing.T) {
	ctx, pool, m, userID, issued := pendingFailureFixture(t)
	verifyErr := errors.New("assertion did not verify")
	hookRuns := 0
	hook := func(ctx context.Context, qtx *store.Queries, pending store.PendingAuthentication) error {
		hookRuns++
		if pending.FailedAttempts != 5 || pending.ConsumedAt == nil {
			return errors.New("hook saw a live pending row")
		}
		return insertSecurityEvent(ctx, qtx, userID)
	}
	for attempt := int32(1); attempt <= 5; attempt++ {
		_, err := failPending(ctx, m, issued.RawToken, userID, verifyErr, hook)
		var failure *auth.PendingVerificationFailure
		if !errors.As(err, &failure) || failure.Exhausted != (attempt == 5) {
			t.Fatalf("attempt %d: error = %v, want a failure with exhausted = %t", attempt, err, attempt == 5)
		}
	}
	if hookRuns != 1 {
		t.Fatalf("exhaustion hook runs = %d, want 1", hookRuns)
	}
	attempts, consumed, events := pendingFailureState(ctx, t, pool, issued.Pending.ID, userID)
	if attempts != 5 || !consumed || events != 6 {
		t.Fatalf("state = attempts %d, consumed %t, events %d, want 5, true, 6 (five callback writes and one hook insert)", attempts, consumed, events)
	}
	if _, authErr := m.Authenticate(ctx, issued.RawToken, nil); !errors.Is(authErr, auth.ErrPendingAuthenticationRequired) {
		t.Fatalf("Authenticate(after exhaustion) error = %v, want ErrPendingAuthenticationRequired", authErr)
	}
}

// TestPendingAuthentication_ExhaustionHookFailureRollsBackAttempt proves a
// failed exhaustion insert aborts the terminal attempt and the callback's
// writes, leaving the row live at four attempts.
func TestPendingAuthentication_ExhaustionHookFailureRollsBackAttempt(t *testing.T) {
	ctx, pool, m, userID, issued := pendingFailureFixture(t)
	verifyErr := errors.New("assertion did not verify")
	for attempt := 1; attempt <= 4; attempt++ {
		if _, err := failPending(ctx, m, issued.RawToken, userID, verifyErr, nil); !errors.Is(err, verifyErr) {
			t.Fatalf("attempt %d: error = %v, want verification failure", attempt, err)
		}
	}
	outboxErr := errors.New("outbox insert failed")
	hook := func(ctx context.Context, qtx *store.Queries, _ store.PendingAuthentication) error {
		if insertErr := insertSecurityEvent(ctx, qtx, userID); insertErr != nil {
			return insertErr
		}
		return outboxErr
	}
	_, err := failPending(ctx, m, issued.RawToken, userID, verifyErr, hook)
	var failure *auth.PendingVerificationFailure
	if !errors.Is(err, outboxErr) || errors.As(err, &failure) {
		t.Fatalf("WithLivePending(hook failure) error = %v, want the hook error and no recorded failure", err)
	}
	attempts, consumed, events := pendingFailureState(ctx, t, pool, issued.Pending.ID, userID)
	if attempts != 4 || consumed || events != 4 {
		t.Fatalf("state = attempts %d, consumed %t, events %d, want 4, false, 4", attempts, consumed, events)
	}
	if _, authErr := m.Authenticate(ctx, issued.RawToken, nil); authErr != nil {
		t.Fatalf("Authenticate(after hook failure) error = %v, want a live row", authErr)
	}
}

// TestPendingAuthentication_InternalErrorRollsBackEverything proves a callback
// error that is not a verification failure counts no attempt and keeps none of
// the callback's writes.
func TestPendingAuthentication_InternalErrorRollsBackEverything(t *testing.T) {
	ctx, pool, m, userID, issued := pendingFailureFixture(t)
	internalErr := errors.New("credential store unavailable")
	_, err := m.WithLivePending(ctx, issued.RawToken, nil, nil, func(ctx context.Context, qtx *store.Queries, locked auth.PendingAuthenticationLocked) error {
		if insertErr := insertSecurityEvent(ctx, qtx, userID); insertErr != nil {
			return insertErr
		}
		if claimErr := m.ClaimTx(ctx, qtx, locked.Pending); claimErr != nil {
			return claimErr
		}
		return internalErr
	})
	var failure *auth.PendingVerificationFailure
	if !errors.Is(err, internalErr) || errors.As(err, &failure) {
		t.Fatalf("WithLivePending(internal error) error = %v, want the internal error", err)
	}
	attempts, consumed, events := pendingFailureState(ctx, t, pool, issued.Pending.ID, userID)
	if attempts != 0 || consumed || events != 0 {
		t.Fatalf("state = attempts %d, consumed %t, events %d, want 0, false, 0", attempts, consumed, events)
	}
}
