package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// Session lifecycle values are defined in docs/design/security.md. Rotation
// delivery and lineage are defined in docs/adr/0015-session-rotation-delivery.md.
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
	// user-row lock fence in D4 is enforced. See Issue and tryRotate.
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
	RawToken string
	Session  store.Session
}

// IssueTx is the only primitive that constructs a fresh login session. Its
// caller must already hold the exact user row lock via GetUserForUpdate in the
// same transaction; IssueTx never opens or commits a transaction of its own.
// It inserts the existing opaque session format with rotated_from = NULL.
func (m *SessionManager) IssueTx(ctx context.Context, qtx *store.Queries, user store.User, ua, ip string) (SessionIssue, error) {
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
		UserID:            user.ID,
		TokenHash:         hashSessionToken(raw),
		CSRFSecret:        csrfSecret,
		CreatedAt:         now,
		LastSeenAt:        now,
		ReauthenticatedAt: now,
		AbsoluteExpiresAt: now.Add(absoluteTimeout),
		UA:                stringParam(ua),
		IP:                ipParam,
		// RotatedFrom is deliberately omitted (nil): a fresh login is
		// never a rotation successor -- see tryRotate's own successor
		// insert (below) for the one call site that sets it.
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
		issued, issueErr := m.IssueTx(ctx, m.q, store.User{ID: userID}, ua, ip)
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
// absolute expiry, reauth time, user agent, and IP; rotation extends none of
// them. See docs/adr/0015-session-rotation-delivery.md.
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

	if err := m.touchLastSeenAt(ctx, &sess, now); err != nil {
		return store.Session{}, "", err
	}
	return sess, "", nil
}

// errRotationPredecessorRevoked marks a successor insert aborted because the
// predecessor was revoked after its admission update committed -- the reset
// fence in D4. It is a closed, non-error outcome for the rotation loser.
var errRotationPredecessorRevoked = errors.New("auth: rotation predecessor revoked")

// tryRotate admits at most one rotation winner. The admission update and
// successor insert are separate statements: a lost insert leaves the
// predecessor usable only to its parked deadline, while a lost response leaves
// an unreachable successor. See docs/adr/0015-session-rotation-delivery.md.
//
// A pool-backed manager inserts the successor in a short transaction that locks
// the user row and re-reads the predecessor as live, so a reset that already
// revoked the predecessor (RevokeAllSessions under the same user lock) cannot
// mint a successor past that fence. A query-only manager (nil pool) keeps the
// historic direct insert.
func (m *SessionManager) tryRotate(ctx context.Context, predecessor store.Session, now time.Time) (successor store.Session, raw string, won bool, err error) {
	graceUntil := now.Add(rotationAge)
	if graceUntil.After(predecessor.AbsoluteExpiresAt) {
		graceUntil = predecessor.AbsoluteExpiresAt
	}
	_, err = m.q.BeginSessionRotation(ctx, store.BeginSessionRotationParams{
		ID:                 predecessor.ID,
		RotationGraceUntil: &graceUntil,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Session{}, "", false, nil
		}
		return store.Session{}, "", false, fmt.Errorf("auth: authenticate: begin rotation: %w", err)
	}

	if m.pool == nil {
		successor, raw, err = m.createRotationSuccessor(ctx, m.q, predecessor, now)
		if err != nil {
			return store.Session{}, "", false, err
		}
		return successor, raw, true, nil
	}

	if m.rotationProbe != nil {
		m.rotationProbe()
	}

	err = pgx.BeginFunc(ctx, m.pool, func(tx pgx.Tx) error {
		qtx := m.q.WithTx(tx)
		if _, lockErr := qtx.GetUserForUpdate(ctx, predecessor.UserID); lockErr != nil {
			return fmt.Errorf("auth: rotate session: lock user: %w", lockErr)
		}
		live, readErr := qtx.GetSessionByIDForUpdate(ctx, predecessor.ID)
		if readErr != nil {
			return fmt.Errorf("auth: rotate session: re-read predecessor: %w", readErr)
		}
		if live.RevokedAt != nil {
			return errRotationPredecessorRevoked
		}
		successor, raw, err = m.createRotationSuccessor(ctx, qtx, predecessor, now)
		return err
	})
	if err != nil {
		if errors.Is(err, errRotationPredecessorRevoked) {
			return store.Session{}, "", false, nil
		}
		return store.Session{}, "", false, err
	}
	return successor, raw, true, nil
}

// createRotationSuccessor mints the successor row, inheriting identity,
// absolute expiry, reauth time, user agent, and IP from the predecessor while
// setting the database-enforced lineage link. Rotation extends none of them.
func (m *SessionManager) createRotationSuccessor(ctx context.Context, qtx *store.Queries, predecessor store.Session, now time.Time) (successor store.Session, raw string, err error) {
	raw, err = randomSessionToken()
	if err != nil {
		return store.Session{}, "", fmt.Errorf("auth: rotate session: %w", err)
	}
	csrfSecret, err := randomCSRFSecret()
	if err != nil {
		return store.Session{}, "", fmt.Errorf("auth: rotate session: %w", err)
	}

	successor, err = qtx.CreateSession(ctx, store.CreateSessionParams{
		UserID:            predecessor.UserID,
		TokenHash:         hashSessionToken(raw),
		CSRFSecret:        csrfSecret,
		CreatedAt:         now,
		LastSeenAt:        now,
		ReauthenticatedAt: predecessor.ReauthenticatedAt,
		AbsoluteExpiresAt: predecessor.AbsoluteExpiresAt,
		UA:                predecessor.UA,
		IP:                predecessor.IP,
		// RotatedFrom is the database-enforced lineage link used when revoking
		// either half of a rotation pair.
		RotatedFrom: &predecessor.ID,
	})
	if err != nil {
		return store.Session{}, "", fmt.Errorf("auth: rotate session: create successor: %w", err)
	}
	return successor, raw, nil
}

// startPredecessorGrace shortens the parked deadline on a successor's first
// use. A successor with last_seen_at equal to created_at is unused; the forced
// last-seen write changes that marker despite the normal activity throttle.
// Both updates are safe to repeat and failures reject authentication.
func (m *SessionManager) startPredecessorGrace(ctx context.Context, sess *store.Session, now time.Time) error {
	if sess.RotatedFrom == nil || !sess.LastSeenAt.Equal(sess.CreatedAt) {
		return nil
	}

	if err := m.q.StartSessionRotationGrace(ctx, store.StartSessionRotationGraceParams{
		ID:         *sess.RotatedFrom,
		GraceUntil: now.Add(rotationGrace),
	}); err != nil {
		return fmt.Errorf("auth: authenticate: start predecessor rotation grace: %w", err)
	}

	if now.After(sess.LastSeenAt) {
		if err := m.q.TouchLastSeenAt(ctx, store.TouchLastSeenAtParams{ID: sess.ID, LastSeenAt: now}); err != nil {
			return fmt.Errorf("auth: authenticate: mark successor first use: %w", err)
		}
		sess.LastSeenAt = now
	}
	return nil
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

// Revoke immediately and idempotently revokes sessionID without checking
// ownership. Callers must prove it belongs to the authenticated user; use
// RevokeForUser for caller-supplied IDs.
func (m *SessionManager) Revoke(ctx context.Context, sessionID uuid.UUID) error {
	now := m.now()
	if err := m.q.RevokeSession(ctx, store.RevokeSessionParams{ID: sessionID, RevokedAt: &now}); err != nil {
		return fmt.Errorf("auth: revoke session: %w", err)
	}
	return nil
}

// RevokeForUser revokes only a live session owned by userID. Callers must treat
// zero rows as one no-oracle outcome covering absent, foreign, and dead IDs.
func (m *SessionManager) RevokeForUser(ctx context.Context, sessionID, userID uuid.UUID) (int64, error) {
	now := m.now()
	n, err := m.q.RevokeSessionForUser(ctx, store.RevokeSessionForUserParams{
		ID:         sessionID,
		UserID:     userID,
		RevokedAt:  &now,
		IdleCutoff: now.Add(-idleTimeout),
		Now:        now,
	})
	if err != nil {
		return 0, fmt.Errorf("auth: revoke session for user: %w", err)
	}
	return n, nil
}

// RevokeAll revokes every not-already-revoked session belonging to
// userID -- "log out everywhere" -- and returns how many rows that
// affected.
func (m *SessionManager) RevokeAll(ctx context.Context, userID uuid.UUID) (int64, error) {
	now := m.now()
	n, err := m.q.RevokeAllSessions(ctx, store.RevokeAllSessionsParams{UserID: userID, RevokedAt: &now})
	if err != nil {
		return 0, fmt.Errorf("auth: revoke all sessions: %w", err)
	}
	return n, nil
}

// TouchReauthenticated is valid only after a full reauth OAuth round trip.
// Rotation must never call it.
func (m *SessionManager) TouchReauthenticated(ctx context.Context, sessionID uuid.UUID) error {
	now := m.now()
	if err := m.q.TouchReauthenticatedAt(ctx, store.TouchReauthenticatedAtParams{ID: sessionID, ReauthenticatedAt: now}); err != nil {
		return fmt.Errorf("auth: touch reauthenticated: %w", err)
	}
	return nil
}

// RequireRecentReauth enforces the window in docs/design/security.md.
func RequireRecentReauth(sess store.Session, now time.Time) error {
	if now.Sub(sess.ReauthenticatedAt) > reauthWindow {
		return ErrReauthRequired
	}
	return nil
}

// RequireLiveSession applies the opaque terminal-session checks at a caller's
// authoritative clock. Transactional mutation paths use it after their final
// session-row read so a session revoked while a public-state fence is closing
// cannot commit a write.
func RequireLiveSession(sess store.Session, now time.Time) error {
	if sessionDead(sess, now) {
		return ErrSessionInvalid
	}
	return nil
}

// randomSessionToken returns an unpadded base64url bearer token.
func randomSessionToken() (string, error) {
	b := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// randomCSRFSecret returns csrfSecretBytes of cryptographically random
// bytes for a new session's csrf_secret column.
func randomCSRFSecret() ([]byte, error) {
	b := make([]byte, csrfSecretBytes)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generate csrf secret: %w", err)
	}
	return b, nil
}

// hashSessionToken returns the SHA-256 hash of raw, the form stored in
// sessions.token_hash, so a database read or leak never discloses a usable
// bearer token.
func hashSessionToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// stringParam returns nil for an empty string, and a pointer to s
// otherwise -- the shape sessions.ua (nullable text) wants.
func stringParam(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// parseSessionIP parses ip into the *netip.Addr sessions.ip (nullable
// inet) wants, returning nil for an empty string (no address recorded).
func parseSessionIP(ip string) (*netip.Addr, error) {
	if ip == "" {
		return nil, nil
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return nil, fmt.Errorf("parse session ip %q: %w", ip, err)
	}
	return &addr, nil
}
