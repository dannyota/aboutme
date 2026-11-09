package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// Session lifecycle values are defined in docs/design/security.md. Rotation
// delivery and lineage are defined in docs/adr/0015-accounts-passwords-and-sessions.md.
const (
	sessionCookieName = "__Host-session"
	sessionTokenBytes = 32 // 256-bit CSPRNG.
	idleTimeout       = 30 * 24 * time.Hour
	absoluteTimeout   = 90 * 24 * time.Hour
	rotationAge       = 24 * time.Hour
	rotationGrace     = 60 * time.Second
	lastSeenThrottle  = time.Hour
	reauthWindow      = 15 * time.Minute
)

// csrfSecretBytes gives CSRF secrets 256 bits of entropy. RequireCSRF compares
// the raw bytes in constant time.
const csrfSecretBytes = 32

// ErrSessionInvalid collapses unknown, revoked, expired, and grace-dead
// credentials so authentication exposes no validity oracle.
var ErrSessionInvalid = errors.New("auth: session invalid")

// ErrReauthRequired marks a failed recent-reauth gate. See
// docs/design/security.md.
var ErrReauthRequired = errors.New("auth: recent reauthentication required")

// SessionManager issues and authenticates database-backed sessions, including
// expiry, activity throttling, and compare-and-swap rotation.
type SessionManager struct {
	q *store.Queries
	// pool is the transaction beginner every session issuer and rotation
	// successor insert serializes on. It is nil for query-only managers
	// (authentication, revocation, resumeapi middleware, and existing
	// non-fence tests); production issuance and rotation always set it so the
	// user-row lock fence is enforced. See Issue and tryRotate.
	pool *store.Pool
	now  func() time.Time

	// lockProbe and rotationProbe are nil in production. Deterministic fence
	// tests set them to observe (and pause) the exact moments Issue acquires
	// the user lock and rotation commits its admission update, so the reset
	// interleavings below are proven without a timing race.
	lockProbe     func()
	rotationProbe func()
}

// NewSessionManager builds a query-only SessionManager backed by q, using the
// real wall clock. It can authenticate, revoke, and (for compatibility) issue
// sessions directly, but it holds no transaction beginner, so it never applies
// the user-row lock fence. Production callers that issue sessions use
// NewSessionManagerWithPool.
func NewSessionManager(q *store.Queries) *SessionManager {
	return &SessionManager{q: q, now: time.Now}
}

// NewSessionManagerWithPool builds a SessionManager backed by pool, deriving
// its queries from the same pool. This is the production constructor: Issue and
// rotation serialize on the user row lock.
func NewSessionManagerWithPool(pool *store.Pool) *SessionManager {
	return &SessionManager{q: store.New(pool), pool: pool, now: time.Now}
}

// SessionIssue carries a newly minted session and the raw bearer token that
// must reach the client. Only the token's SHA-256 hash is stored.
type SessionIssue struct {
	// RawToken is the bearer token for the session cookie: 32 random bytes
	// as unpadded base64url. It is never stored.
	RawToken string
	// Session is the inserted session row; its TokenHash is the SHA-256 hash
	// of RawToken.
	Session store.Session
}

// IssueTx is the only primitive that constructs a fresh login session. It runs
// in the caller's transaction and never opens or commits one of its own. It
// takes the user row lock itself (a no-op when the caller already holds it) and
// copies the locked row's authentication epoch, so a stale user snapshot cannot
// mint a stale-epoch session. It inserts a session with rotated_from = NULL.
func (m *SessionManager) IssueTx(ctx context.Context, qtx *store.Queries, user store.User, ua, ip string) (SessionIssue, error) {
	lockedUser, err := qtx.GetUserForUpdate(ctx, user.ID)
	if err != nil {
		return SessionIssue{}, fmt.Errorf("auth: issue session: lock user: %w", err)
	}
	raw, err := randomSessionToken()
	if err != nil {
		return SessionIssue{}, fmt.Errorf("auth: issue session: %w", err)
	}
	csrfSecret, err := randomCSRFSecret()
	if err != nil {
		return SessionIssue{}, fmt.Errorf("auth: issue session: %w", err)
	}
	ipParam, err := parseSessionIP(ip)
	if err != nil {
		return SessionIssue{}, fmt.Errorf("auth: issue session: %w", err)
	}

	now := m.now()
	sess, err := qtx.CreateSession(ctx, store.CreateSessionParams{
		UserID:            lockedUser.ID,
		TokenHash:         hashSessionToken(raw),
		CSRFSecret:        csrfSecret,
		CreatedAt:         now,
		LastSeenAt:        now,
		ReauthenticatedAt: now,
		AbsoluteExpiresAt: now.Add(absoluteTimeout),
		UA:                stringParam(ua),
		IP:                ipParam,
		AuthEpoch:         lockedUser.AuthEpoch,
		// RotatedFrom is deliberately omitted (nil): a fresh login is
		// never a rotation successor. tryRotate's own successor insert in
		// session_rotation.go is the one call site that sets it.
	})
	if err != nil {
		return SessionIssue{}, fmt.Errorf("auth: issue session: %w", err)
	}
	return SessionIssue{RawToken: raw, Session: sess}, nil
}

// Issue is the compatibility wrapper around IssueTx. When the manager holds a
// transaction beginner it opens a transaction, locks the user row, calls
// IssueTx, and commits. A query-only manager (nil pool) issues directly, which
// preserves the historic calling convention for tests and middleware that never
// mint sessions in production. It always creates a new session to prevent
// fixation; it returns the raw token but stores only its SHA-256 hash.
func (m *SessionManager) Issue(ctx context.Context, userID uuid.UUID, ua, ip string) (rawToken string, sess store.Session, err error) {
	if m.pool == nil {
		user, readErr := m.q.GetUserByID(ctx, userID)
		if readErr != nil {
			return "", store.Session{}, fmt.Errorf("auth: issue session: read user: %w", readErr)
		}
		issued, issueErr := m.IssueTx(ctx, m.q, user, ua, ip)
		if issueErr != nil {
			return "", store.Session{}, issueErr
		}
		return issued.RawToken, issued.Session, nil
	}

	var issued SessionIssue
	err = pgx.BeginFunc(ctx, m.pool, func(tx pgx.Tx) error {
		qtx := m.q.WithTx(tx)
		user, lockErr := qtx.GetUserForUpdate(ctx, userID)
		if lockErr != nil {
			return fmt.Errorf("auth: issue session: lock user: %w", lockErr)
		}
		if m.lockProbe != nil {
			m.lockProbe()
		}
		var issueErr error
		issued, issueErr = m.IssueTx(ctx, qtx, user, ua, ip)
		return issueErr
	})
	if err != nil {
		return "", store.Session{}, err
	}
	return issued.RawToken, issued.Session, nil
}

// Authenticate returns the governing session and a raw successor token only to
// the rotation winner. Dead sessions never rotate. Successors inherit identity,
// absolute expiry, reauth time, user agent, IP, authentication epoch, and
// factor-proof time; rotation extends none of them. See
// docs/adr/0015-accounts-passwords-and-sessions.md.
//
// The token lookup compares the session's copied authentication epoch with the
// account's current epoch in the same statement, so a stale-epoch session
// authenticates nothing without a user-row lock on the request path. See
// docs/design/second-factor-authentication.md.
func (m *SessionManager) Authenticate(ctx context.Context, rawToken string) (sess store.Session, rotatedToken string, err error) {
	now := m.now()

	sess, err = m.q.GetSessionByTokenHash(ctx, hashSessionToken(rawToken))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Session{}, "", ErrSessionInvalid
		}
		return store.Session{}, "", fmt.Errorf("auth: authenticate: %w", err)
	}

	if sessionDead(sess, now) {
		return store.Session{}, "", ErrSessionInvalid
	}

	// First successor use proves delivery. Start predecessor grace before this
	// session can rotate again.
	if graceErr := m.startPredecessorGrace(ctx, &sess, now); graceErr != nil {
		return store.Session{}, "", graceErr
	}

	if now.Sub(sess.CreatedAt) > rotationAge && sess.RotationGraceUntil == nil {
		var successor store.Session
		var raw string
		var won bool
		successor, raw, won, err = m.tryRotate(ctx, sess, now)
		if err != nil {
			return store.Session{}, "", err
		}
		if won {
			return successor, raw, nil
		}
		// A rotation loser continues with the still-live predecessor.
	}

	if err = m.touchLastSeenAt(ctx, &sess, now); err != nil {
		return store.Session{}, "", err
	}
	return sess, "", nil
}

// touchLastSeenAt throttles writes and keeps the returned session in sync.
func (m *SessionManager) touchLastSeenAt(ctx context.Context, sess *store.Session, now time.Time) error {
	if now.Sub(sess.LastSeenAt) < lastSeenThrottle {
		return nil
	}
	if err := m.q.TouchLastSeenAt(ctx, store.TouchLastSeenAtParams{ID: sess.ID, LastSeenAt: now}); err != nil {
		return fmt.Errorf("auth: touch last seen at: %w", err)
	}
	sess.LastSeenAt = now
	return nil
}

// sessionDead applies every terminal session condition before rotation.
func sessionDead(sess store.Session, now time.Time) bool {
	if sess.RevokedAt != nil {
		return true
	}
	if now.Sub(sess.LastSeenAt) > idleTimeout {
		return true
	}
	if now.After(sess.AbsoluteExpiresAt) {
		return true
	}
	if sess.RotationGraceUntil != nil && now.After(*sess.RotationGraceUntil) {
		return true
	}
	return false
}
