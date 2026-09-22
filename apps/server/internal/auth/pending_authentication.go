package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const (
	pendingAuthenticationLifetime           = 5 * time.Minute
	pendingAuthenticationCleanupLimit int32 = 200
	pendingAuthenticationMaxAttempts  int32 = 5
)

// Pending authentication purposes. A login row binds no session; a reauth row
// binds the concrete live session whose proofs it will refresh.
const (
	// PendingAuthenticationPurposeLogin completes a primary login.
	PendingAuthenticationPurposeLogin = "login"
	// PendingAuthenticationPurposeReauth completes a recent reauthentication
	// of the bound session.
	PendingAuthenticationPurposeReauth = "reauth"
)

// ErrPendingAuthenticationRequired collapses absent, expired, consumed,
// wrong-epoch, wrong-session, and foreign pending credentials into one
// outcome, so pending routes expose no validity oracle.
var ErrPendingAuthenticationRequired = errors.New("auth: pending authentication required")

// PendingAuthenticationIssue carries a new pending row and the raw cookie token.
// Only the token's SHA-256 digest is stored.
type PendingAuthenticationIssue struct {
	RawToken string
	Pending  store.PendingAuthentication
}

// PendingAuthenticationRequest describes a pending authentication created after
// a valid primary credential. SessionID is required for reauth and must be nil
// for login. PreviousRawToken is the browser's current pending cookie, if any.
type PendingAuthenticationRequest struct {
	UserID            uuid.UUID
	Purpose           string
	PrimaryVerifiedAt time.Time
	ReturnPath        string
	SessionID         *uuid.UUID
	PreviousRawToken  string
}

// PendingAuthenticationLocked is valid only for its callback transaction.
// Session is set only for a reauth row.
type PendingAuthenticationLocked struct {
	User    store.User
	Pending store.PendingAuthentication
	Session *store.Session
}

// PendingExhaustedHook runs in the completion transaction after the fifth
// failed attempt consumed the pending row, for example to insert the
// exhaustion notification. Its writes commit together with the attempt; an
// error rolls back the callback's writes and the attempt.
type PendingExhaustedHook func(ctx context.Context, qtx *store.Queries, pending store.PendingAuthentication) error

// PendingVerificationFailure is the error a WithLivePending completion callback
// returns, through FailPendingVerification, when the presented factor or
// recovery code did not verify. WithLivePending then records one failed
// attempt in the same transaction and commits it together with the callback's
// writes, such as a consumed ceremony or a counter security event. Exhausted
// reports that the attempt was the fifth and consumed the row; the caller then
// clears the pending cookie.
type PendingVerificationFailure struct {
	Err       error
	Exhausted bool

	onExhausted PendingExhaustedHook
}

// FailPendingVerification wraps a verification error so WithLivePending records
// one failed attempt and commits it with the callback's writes. onExhausted,
// when not nil, runs in the same transaction only when that attempt is the
// fifth.
func FailPendingVerification(err error, onExhausted PendingExhaustedHook) error {
	return &PendingVerificationFailure{Err: err, onExhausted: onExhausted}
}

// Error describes the failed verification without naming the method.
func (f *PendingVerificationFailure) Error() string {
	return fmt.Sprintf("auth: pending verification failed: %v", f.Err)
}

// Unwrap returns the callback's verification error.
func (f *PendingVerificationFailure) Unwrap() error {
	return f.Err
}

// PendingAuthenticationManager creates and completes pending authentications
// under the account lock order in docs/design/second-factor-authentication.md.
type PendingAuthenticationManager struct {
	pool   *store.Pool
	q      *store.Queries
	now    func() time.Time
	logger *slog.Logger
}

// NewPendingAuthenticationManager builds a manager over pool using the wall
// clock. A nil logger drops cleanup warnings.
func NewPendingAuthenticationManager(pool *store.Pool, logger *slog.Logger) *PendingAuthenticationManager {
	return &PendingAuthenticationManager{pool: pool, q: store.New(pool), now: time.Now, logger: logger}
}

// Create inserts a pending authentication after a valid primary credential.
// It first runs bounded best-effort cleanup, then, under the user lock, checks
// a reauth binding, consumes the browser's previous live pending row when it
// belongs to the same account whatever its purpose or binding, and expires the
// oldest live row when five are already live. A previous row that belongs to
// another account stays live: consuming it would write another account's
// authority outside that account's lock, and it expires within five minutes.
func (m *PendingAuthenticationManager) Create(ctx context.Context, req PendingAuthenticationRequest) (PendingAuthenticationIssue, error) {
	if err := validatePendingRequest(req); err != nil {
		return PendingAuthenticationIssue{}, err
	}
	m.cleanupExpired(ctx)
	var previous store.PendingAuthentication
	if req.PreviousRawToken != "" {
		row, readErr := m.q.GetPendingAuthenticationByTokenDigest(ctx, hashSessionToken(req.PreviousRawToken))
		if readErr == nil {
			previous = row
		} else if !errors.Is(readErr, pgx.ErrNoRows) {
			return PendingAuthenticationIssue{}, fmt.Errorf("auth: create pending authentication: read previous pending: %w", readErr)
		}
	}
	raw, err := randomSessionToken()
	if err != nil {
		return PendingAuthenticationIssue{}, fmt.Errorf("auth: create pending authentication: %w", err)
	}
	csrf, err := randomCSRFSecret()
	if err != nil {
		return PendingAuthenticationIssue{}, fmt.Errorf("auth: create pending authentication: %w", err)
	}
	var pending store.PendingAuthentication
	err = pgx.BeginFunc(ctx, m.pool, func(tx pgx.Tx) error {
		qtx := m.q.WithTx(tx)
		user, lockErr := qtx.GetUserForUpdate(ctx, req.UserID)
		if lockErr != nil {
			return fmt.Errorf("lock user: %w", lockErr)
		}
		now := m.now()
		if bindErr := lockBoundSession(ctx, qtx, user, req.SessionID, now); bindErr != nil {
			return bindErr
		}
		if previous.ID != uuid.Nil && previous.UserID == user.ID {
			if consumeErr := consumePreviousPending(ctx, qtx, user, previous.TokenDigest, now); consumeErr != nil {
				return consumeErr
			}
		}
		if _, expireErr := qtx.ConsumeOldestLivePendingAuthentication(ctx, store.ConsumeOldestLivePendingAuthenticationParams{ConsumedAt: now, UserID: user.ID}); expireErr != nil && !errors.Is(expireErr, pgx.ErrNoRows) {
			return fmt.Errorf("expire oldest pending authentication: %w", expireErr)
		}
		var createErr error
		pending, createErr = qtx.CreatePendingAuthentication(ctx, store.CreatePendingAuthenticationParams{
			TokenDigest: hashSessionToken(raw), CSRFSecret: csrf, UserID: user.ID,
			Purpose: req.Purpose, AuthEpoch: user.AuthEpoch, SessionID: req.SessionID,
			PrimaryVerifiedAt: req.PrimaryVerifiedAt, ReturnPath: req.ReturnPath,
			CreatedAt: now, ExpiresAt: now.Add(pendingAuthenticationLifetime),
		})
		return createErr
	})
	if err != nil {
		return PendingAuthenticationIssue{}, fmt.Errorf("auth: create pending authentication: %w", err)
	}
	return PendingAuthenticationIssue{RawToken: raw, Pending: pending}, nil
}

// consumePreviousPending consumes the browser's previous pending row when it is
// still live for the locked user. The caller holds the user lock.
func consumePreviousPending(ctx context.Context, qtx *store.Queries, user store.User, digest []byte, now time.Time) error {
	locked, err := qtx.GetPendingAuthenticationByTokenDigestForUpdate(ctx, digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock previous pending: %w", err)
	}
	if !livePendingForUser(locked, user, now) {
		return nil
	}
	if _, err = qtx.ClaimPendingAuthentication(ctx, store.ClaimPendingAuthenticationParams{TokenDigest: locked.TokenDigest, ConsumedAt: now}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("consume previous pending: %w", err)
	}
	return nil
}

// WithLivePending runs every completion step in one transaction. It locks the
// user, then for a reauth row the bound session, then calls beforePending to
// lock factor policy or credentials, then locks and rechecks the pending row.
// sessionID is the browser's current session, if any. A login row ignores it
// and binds nothing; a reauth row requires it to equal the bound session.
//
// complete runs in the same transaction. When it returns nil, its writes
// commit. When it returns a PendingVerificationFailure (see
// FailPendingVerification), one failed attempt is recorded, the failure's
// exhaustion hook runs if that attempt was the fifth, and the callback's
// writes, the attempt, and the hook's writes commit together; the failure is
// returned with Exhausted set when the attempt consumed the row. A hook error,
// a recording error, or any other callback error is an internal error: the
// whole transaction rolls back and no attempt is counted.
func (m *PendingAuthenticationManager) WithLivePending(ctx context.Context, rawToken string, sessionID *uuid.UUID, beforePending func(context.Context, *store.Queries, store.User, *store.Session) error, complete func(context.Context, *store.Queries, PendingAuthenticationLocked) error) (PendingAuthenticationLocked, error) {
	candidate, err := m.q.GetPendingAuthenticationByTokenDigest(ctx, hashSessionToken(rawToken))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PendingAuthenticationLocked{}, ErrPendingAuthenticationRequired
		}
		return PendingAuthenticationLocked{}, fmt.Errorf("auth: read pending authentication: %w", err)
	}
	var (
		locked  PendingAuthenticationLocked
		failure *PendingVerificationFailure
	)
	err = pgx.BeginFunc(ctx, m.pool, func(tx pgx.Tx) error {
		qtx := m.q.WithTx(tx)
		user, lockErr := qtx.GetUserForUpdate(ctx, candidate.UserID)
		if lockErr != nil {
			if errors.Is(lockErr, pgx.ErrNoRows) {
				return ErrPendingAuthenticationRequired
			}
			return fmt.Errorf("lock user: %w", lockErr)
		}
		now := m.now()
		var session *store.Session
		if candidate.SessionID != nil {
			if sessionID == nil || *sessionID != *candidate.SessionID {
				return ErrPendingAuthenticationRequired
			}
			sess, sessErr := lockLiveSession(ctx, qtx, user, *sessionID, now)
			if sessErr != nil {
				return sessErr
			}
			session = &sess
		}
		if beforePending != nil {
			if beforeErr := beforePending(ctx, qtx, user, session); beforeErr != nil {
				return beforeErr
			}
		}
		pending, pendingErr := qtx.GetPendingAuthenticationByTokenDigestForUpdate(ctx, candidate.TokenDigest)
		if pendingErr != nil {
			if errors.Is(pendingErr, pgx.ErrNoRows) {
				return ErrPendingAuthenticationRequired
			}
			return fmt.Errorf("lock pending authentication: %w", pendingErr)
		}
		if !livePendingForUser(pending, user, now) || !pendingBindingMatches(pending, candidate.SessionID) {
			return ErrPendingAuthenticationRequired
		}
		locked = PendingAuthenticationLocked{User: user, Pending: pending, Session: session}
		if complete == nil {
			return nil
		}
		completeErr := complete(ctx, qtx, locked)
		if !errors.As(completeErr, &failure) {
			return completeErr
		}
		updated, exhausted, recordErr := m.RecordFailureTx(ctx, qtx, pending)
		if recordErr != nil {
			return recordErr
		}
		if exhausted && failure.onExhausted != nil {
			if hookErr := failure.onExhausted(ctx, qtx, updated); hookErr != nil {
				return fmt.Errorf("auth: pending exhaustion hook: %w", hookErr)
			}
		}
		locked.Pending = updated
		failure.Exhausted = exhausted
		return nil
	})
	if err != nil {
		return PendingAuthenticationLocked{}, err
	}
	if failure != nil {
		return locked, failure
	}
	return locked, nil
}

// Authenticate returns the live pending row named by rawToken without changing
// it. sessionID follows the WithLivePending binding rules.
func (m *PendingAuthenticationManager) Authenticate(ctx context.Context, rawToken string, sessionID *uuid.UUID) (store.PendingAuthentication, error) {
	locked, err := m.WithLivePending(ctx, rawToken, sessionID, nil, nil)
	if err != nil {
		return store.PendingAuthentication{}, err
	}
	return locked.Pending, nil
}

// ClaimTx consumes the locked pending row on successful completion. It returns
// ErrPendingAuthenticationRequired when the row is no longer live.
func (m *PendingAuthenticationManager) ClaimTx(ctx context.Context, qtx *store.Queries, pending store.PendingAuthentication) error {
	_, err := qtx.ClaimPendingAuthentication(ctx, store.ClaimPendingAuthenticationParams{TokenDigest: pending.TokenDigest, ConsumedAt: m.now()})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPendingAuthenticationRequired
	}
	if err != nil {
		return fmt.Errorf("auth: consume pending authentication: %w", err)
	}
	return nil
}

// RecordFailureTx records one failed completion on the locked pending row and
// reports whether it was the fifth, which consumes the row. The record commits
// only with the caller's transaction. A completion callback normally returns
// FailPendingVerification instead, which also runs the exhaustion hook.
func (m *PendingAuthenticationManager) RecordFailureTx(ctx context.Context, qtx *store.Queries, pending store.PendingAuthentication) (store.PendingAuthentication, bool, error) {
	updated, err := qtx.RecordPendingAuthenticationFailure(ctx, store.RecordPendingAuthenticationFailureParams{ID: pending.ID, AttemptedAt: m.now()})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.PendingAuthentication{}, false, ErrPendingAuthenticationRequired
	}
	if err != nil {
		return store.PendingAuthentication{}, false, fmt.Errorf("auth: record pending authentication failure: %w", err)
	}
	return updated, updated.FailedAttempts == pendingAuthenticationMaxAttempts && updated.ConsumedAt != nil, nil
}

func (m *PendingAuthenticationManager) cleanupExpired(ctx context.Context) {
	if _, err := m.q.DeleteExpiredPendingAuthentications(ctx, pendingAuthenticationCleanupLimit); err != nil && m.logger != nil {
		m.logger.Warn("pending authentication cleanup failed")
	}
	if _, err := m.q.DeleteExpiredWebAuthnCeremonies(ctx, pendingAuthenticationCleanupLimit); err != nil && m.logger != nil {
		m.logger.Warn("webauthn ceremony cleanup failed")
	}
}

func validatePendingRequest(req PendingAuthenticationRequest) error {
	if req.Purpose != PendingAuthenticationPurposeLogin && req.Purpose != PendingAuthenticationPurposeReauth {
		return fmt.Errorf("auth: create pending authentication: invalid purpose")
	}
	if req.Purpose == PendingAuthenticationPurposeReauth && req.SessionID == nil {
		return fmt.Errorf("auth: create pending authentication: missing session binding")
	}
	if req.Purpose == PendingAuthenticationPurposeLogin && req.SessionID != nil {
		return fmt.Errorf("auth: create pending authentication: login has session binding")
	}
	return nil
}

func lockBoundSession(ctx context.Context, qtx *store.Queries, user store.User, sessionID *uuid.UUID, now time.Time) error {
	if sessionID == nil {
		return nil
	}
	_, err := lockLiveSession(ctx, qtx, user, *sessionID, now)
	return err
}

// lockLiveSession locks sessionID and requires a live session owned by the
// locked user at its current epoch.
func lockLiveSession(ctx context.Context, qtx *store.Queries, user store.User, sessionID uuid.UUID, now time.Time) (store.Session, error) {
	sess, err := qtx.GetSessionByIDForUpdate(ctx, sessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Session{}, ErrPendingAuthenticationRequired
		}
		return store.Session{}, fmt.Errorf("lock bound session: %w", err)
	}
	if sess.UserID != user.ID || sess.AuthEpoch != user.AuthEpoch || RequireLiveSession(sess, now) != nil {
		return store.Session{}, ErrPendingAuthenticationRequired
	}
	return sess, nil
}

func livePendingForUser(pending store.PendingAuthentication, user store.User, now time.Time) bool {
	return pending.UserID == user.ID && pending.AuthEpoch == user.AuthEpoch && pending.ConsumedAt == nil && pending.ExpiresAt.After(now)
}

// pendingBindingMatches requires the locked row to keep the binding read before
// the lock. Bindings never change, so this guards only against a digest reuse.
func pendingBindingMatches(pending store.PendingAuthentication, bound *uuid.UUID) bool {
	if pending.SessionID == nil || bound == nil {
		return pending.SessionID == nil && bound == nil
	}
	return *pending.SessionID == *bound
}

// PendingCSRFValid compares a presented pending CSRF token with the row's
// separate secret in constant time.
func PendingCSRFValid(pending store.PendingAuthentication, token []byte) bool {
	return len(token) == len(pending.CSRFSecret) && subtle.ConstantTimeCompare(token, pending.CSRFSecret) == 1
}
