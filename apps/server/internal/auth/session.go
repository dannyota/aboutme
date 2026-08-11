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

// Session lifecycle constants (design spec §3's session table, docs/adr for
// design decisions 1 and 2). sessionCookieName is not read by this file --
// it names the __Host-session cookie that Task 9's RequireSession
// middleware and cookie helpers set and read; it lives here because it's
// the session package's own contract, not something later tasks should be
// free to redefine.
const (
	sessionCookieName = "__Host-session"
	sessionTokenBytes = 32 // 256-bit CSPRNG, matching transaction.go's randomTokenBytes convention.
	idleTimeout       = 30 * 24 * time.Hour
	absoluteTimeout   = 90 * 24 * time.Hour
	rotationAge       = 24 * time.Hour
	rotationGrace     = 60 * time.Second
	lastSeenThrottle  = time.Hour
	reauthWindow      = 15 * time.Minute // design decision 2.
)

// csrfSecretBytes is the size, in raw bytes, of a session's csrf_secret:
// 32 bytes (256 bits), the same CSPRNG strength as sessionTokenBytes and
// transaction.go's randomTokenBytes. Unlike those two, the secret is never
// base64-encoded before storage -- sessions.csrf_secret is bytea, and
// Task 8's CSRF middleware compares it directly via
// crypto/subtle.ConstantTimeCompare on raw bytes.
const csrfSecretBytes = 32

// ErrSessionInvalid is returned by Authenticate for every way a session
// can fail to authenticate: unknown token hash, revoked, idle-expired,
// absolute-expired, or an old token presented after its rotation grace
// window has passed. These are deliberately collapsed into one sentinel,
// the same no-oracle reasoning as ErrTransactionInvalid: a caller (and, in
// turn, an attacker probing a token) never gets to learn which of the five
// actually happened.
var ErrSessionInvalid = errors.New("auth: session invalid")

// ErrReauthRequired is returned by RequireRecentReauth when a session's
// last full OAuth login (reauthenticated_at) is older than reauthWindow --
// the compensating control the design spec requires before a sensitive
// operation (provider link/unlink, account deletion, email change, slug
// release, log-out-everywhere) given the session's long idle/absolute
// timeouts.
var ErrReauthRequired = errors.New("auth: recent reauthentication required")

// SessionManager creates and authenticates sessions backed by the sessions
// table: issuance, hashing, idle/absolute expiry, and atomic >24h rotation
// with a grace interval (design spec §3, AC-AUTH-004).
type SessionManager struct {
	q   *store.Queries
	now func() time.Time
}

// NewSessionManager builds a SessionManager backed by q, using the real
// wall clock.
func NewSessionManager(q *store.Queries) *SessionManager {
	return &SessionManager{q: q, now: time.Now}
}

// Issue creates a brand-new session row -- fixation defense: it is always
// used at login and never reuses an existing row, even for a user who
// already has other active sessions -- and returns the raw token for the
// caller to Set-Cookie. The raw token is never stored; only its SHA-256
// hash (token_hash) is.
func (m *SessionManager) Issue(ctx context.Context, userID uuid.UUID, ua, ip string) (rawToken string, sess store.Session, err error) {
	raw, err := randomSessionToken()
	if err != nil {
		return "", store.Session{}, fmt.Errorf("auth: issue session: %w", err)
	}
	csrfSecret, err := randomCSRFSecret()
	if err != nil {
		return "", store.Session{}, fmt.Errorf("auth: issue session: %w", err)
	}
	ipParam, err := parseSessionIP(ip)
	if err != nil {
		return "", store.Session{}, fmt.Errorf("auth: issue session: %w", err)
	}

	now := m.now()
	sess, err = m.q.CreateSession(ctx, store.CreateSessionParams{
		UserID:            userID,
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
		return "", store.Session{}, fmt.Errorf("auth: issue session: %w", err)
	}
	return raw, sess, nil
}

// Authenticate looks up rawToken, enforces idle/absolute/revoked expiry,
// performs >24h rotation if due, and throttles the last_seen_at write to
// at most once per lastSeenThrottle. It returns the *governing* session --
// the successor, if this call triggered a rotation -- and, only when this
// call itself minted a new successor, that successor's raw token for the
// caller to Set-Cookie.
//
// Rotation algorithm (task-7-brief.md, AC-AUTH-004): a session already
// dead (revoked, idle-expired, absolute-expired, or an old token past its
// rotation_grace_until) never enters rotation logic -- reject it fast with
// ErrSessionInvalid first. Otherwise, if the session is older than
// rotationAge and has not already started rotating
// (rotation_grace_until IS NULL), attempt BeginSessionRotation: a
// single-row conditional UPDATE. Exactly one concurrent caller's UPDATE
// affects a row (wins); every other caller's affects zero rows (loses).
// The winner inserts one successor row, copying user_id,
// reauthenticated_at, absolute_expires_at, ua, and ip unchanged from the
// predecessor, and recording the predecessor's own id in the successor's
// rotated_from column (fix round 3, DD-C14c: the exact, database-enforced
// lineage link sessions_handlers.go's revokeLineagePartners depends on) --
// rotation never extends absolute expiry, and never resets
// reauthenticated_at (it is not itself a fresh OAuth login: design
// decision 2 tracks the session's whole *lineage*, and resetting it here
// would silently satisfy the recent-reauth gate without a real
// reauthentication ever happening) -- and returns the successor plus its
// raw token. A loser (or a request presenting an already-in-grace old
// token later in the same window) just authenticates against the existing
// row: still valid, since rotation_grace_until being non-NULL doesn't
// itself invalidate a row, only now > rotation_grace_until does.
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

	// P1.1 item 4: this is the first request ever made with a successor's
	// token, so the one-and-only delivery of that token provably reached a
	// client -- start its predecessor's real grace countdown. Runs BEFORE
	// the rotation block below so a successor that is itself already past
	// rotationAge on its first use still retires its OWN predecessor,
	// rather than silently leaving that row parked at its
	// absolute_expires_at forever.
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
		// Lost the race: another concurrent request already won. Fall
		// through and authenticate against the still-valid predecessor
		// row, exactly like a loser (or an already-in-grace old token)
		// below -- no new row, no new cookie.
	}

	if err := m.touchLastSeenAt(ctx, &sess, now); err != nil {
		return store.Session{}, "", err
	}
	return sess, "", nil
}

// tryRotate attempts to win the >24h rotation for sess via
// BeginSessionRotation's single-row conditional UPDATE. won reports
// whether this call won: if it did, successor and raw are the newly
// minted successor row and its raw token; if it lost the race, both are
// zero-valued and the caller must authenticate against the existing row
// instead.
//
// BeginSessionRotation and the successor CreateSession below are two
// separate statements, not one transaction: SessionManager holds only a
// *store.Queries (task-7-brief.md's exact, binding struct shape), which
// has no pool/transaction access of its own. AC-AUTH-004's "atomic" is the
// exactly-one-winner CAS itself (BeginSessionRotation's single-statement
// conditional UPDATE) -- accepted at the 2026-08-02 Task 7 review, with the
// matching entry on the phase ledger -- not statement-level atomicity
// across the winning path's two writes. Two crash outcomes follow from
// that, both bounded and neither a stuck or double-rotated row:
//
//   - The UPDATE lands but the INSERT is lost (process/connection dies
//     between the two, or CreateSession itself errors below): the
//     predecessor is left with rotation_grace_until set but no successor.
//     It stays valid -- and un-rotatable again, since rotation_grace_until
//     is no longer NULL -- for the remainder of the grace window, then
//     dies with no successor to take over. The user simply has to log in
//     again; availability impact only, no session is ever left reachable
//     in a broken state.
//   - The INSERT lands but the response never reaches the caller (e.g. the
//     server crashes, or the connection drops, after CreateSession returns
//     but before Authenticate's caller receives it): the successor row is
//     a real, valid session, but its raw token existed only in the
//     crashed process's memory and was never Set-Cookie'd anywhere. It is
//     an orphan -- unreachable by any client -- until it dies on its own
//     at its inherited absolute_expires_at (never later, since rotation
//     never extends it). P1.1 item 4 is what keeps that orphan from
//     taking the whole session down with it: see graceUntil below.
//
// graceUntil (P1.1 item 4, docs/plans/phase-1-deferred.md; bound added
// for phase gate finding B4): the predecessor's rotation_grace_until is
// parked at min(now+rotationAge, predecessor.AbsoluteExpiresAt) here, not
// at now+rotationGrace. The column still goes non-NULL --
// BeginSessionRotation's CAS above depends on that to admit exactly one
// winner, and it is what stops this lineage from ever minting a second
// successor (which sessions_rotated_from_key's partial UNIQUE index
// independently forbids too) -- but the predecessor is no longer
// scheduled to die 60 seconds from THIS instant. It dies 60 seconds from
// the successor's FIRST USE instead (startPredecessorGrace), or at the
// parked deadline if that first use never comes.
//
// Why defer at all: the successor's raw token is delivered on exactly one
// response and is never stored (only its sha256 is). Killing the
// predecessor on a timer started by the rotation means a single lost
// response orphans the whole session -- the predecessor dies while the
// client still holds it, and the live successor is unreachable by anyone.
// Over ~90 rotations in a 90-day lifetime, a 0.1% per-response loss rate
// strands ~9% of sessions.
//
// Why the bound: the deferred window exists to survive a LOST RESPONSE,
// and a client that has not used its successor within one full rotation
// interval is not mid-request, it is gone. Extending past that buys no
// availability and only extends theft exposure -- a STOLEN predecessor
// token stays usable for the whole parked window, so an unbounded park
// (the original absolute_expires_at, up to 90 days) is exactly the
// unbounded credential lifetime RFC 9700 §4.14 and OWASP's session
// guidance forbid: rotation's security value IS that the old credential
// stops working promptly. The min() also never exceeds the inherited
// absolute expiry -- rotation must not extend a session's life by even
// the grace window, for a session with under a rotation interval left.
//
// What this costs, stated plainly: between the rotation and the
// successor's first use, BOTH rows are live. Both authenticate, and both
// appear in the user's own device list (ListLiveSessionsForUser and
// RevokeSessionForUser, sql/queries.sql, read rotation_grace_until as a
// liveness predicate exactly as sessionDead does) -- so one physical
// device shows two entries for that window. In the ordinary case the
// window is the same ~60 seconds it always was, because the successor's
// first use is the very next request. In the lost-response case it is now
// bounded by one rotation interval, after which the predecessor dies and
// the duplicate entry disappears with no request from anyone; before the
// bound it stood until absolute expiry.
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

	raw, err = randomSessionToken()
	if err != nil {
		return store.Session{}, "", false, fmt.Errorf("auth: rotate session: %w", err)
	}
	csrfSecret, err := randomCSRFSecret()
	if err != nil {
		return store.Session{}, "", false, fmt.Errorf("auth: rotate session: %w", err)
	}

	successor, err = m.q.CreateSession(ctx, store.CreateSessionParams{
		UserID:            predecessor.UserID,
		TokenHash:         hashSessionToken(raw),
		CSRFSecret:        csrfSecret,
		CreatedAt:         now,
		LastSeenAt:        now,
		ReauthenticatedAt: predecessor.ReauthenticatedAt,
		AbsoluteExpiresAt: predecessor.AbsoluteExpiresAt,
		UA:                predecessor.UA,
		IP:                predecessor.IP,
		// RotatedFrom (fix round 3, DD-C14c): the exact, database-
		// enforced (sessions_rotated_from_key) lineage link back to
		// predecessor -- sessions_handlers.go's revokeLineagePartners
		// reads this straight off a row it already has in hand for the
		// predecessor direction, and queries FindLiveSuccessorSession
		// against it for the successor direction, with no timestamp
		// reconstruction either way.
		RotatedFrom: &predecessor.ID,
	})
	if err != nil {
		return store.Session{}, "", false, fmt.Errorf("auth: rotate session: create successor: %w", err)
	}
	return successor, raw, true, nil
}

// startPredecessorGrace converts a rotation predecessor's parked
// rotation_grace_until (tryRotate sets it to the predecessor's own
// absolute_expires_at) into a real, short deadline -- now + rotationGrace
// -- the first time sess, the SUCCESSOR, is used (P1.1 item 4,
// docs/plans/phase-1-deferred.md). A no-op for every other session.
//
// "First use" is read off the row itself, with no extra column and no
// migration: a successor is a row with rotated_from set (only tryRotate's
// insert sets it), and it has never served a request while its
// last_seen_at is still exactly the created_at both were written with in
// the same CreateSession call. touchLastSeenAt's ordinary 1-hour throttle
// would leave those two equal for an hour, so this function forces the
// write itself -- that is what flips the bit, and it is why the forced
// write must not be folded into the throttled path.
//
// Both writes are deliberately safe to repeat. StartSessionRotationGrace
// only ever moves a deadline INWARD (see its own doc comment,
// sql/queries.sql), so a clock so coarse that now still equals created_at
// -- a frozen test clock, never a real one -- re-enters this branch on
// the next request and changes nothing. A failure of either write fails
// the whole Authenticate: the same treatment touchLastSeenAt's own error
// already gets, and the correct one here, since silently skipping the
// grace start would leave a superseded credential alive with nothing to
// retire it.
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

// touchLastSeenAt writes sess.LastSeenAt = now, throttled to at most once
// per lastSeenThrottle: a call within the throttle window of the last
// write is a no-op. When it does write, it also updates *sess so the
// caller's returned session reflects the same value now persisted.
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

// sessionDead reports whether sess must never authenticate again as of
// now: explicitly revoked, idle-expired (no activity for over
// idleTimeout), absolute-expired (past absolute_expires_at regardless of
// activity), or -- for a row already mid-rotation -- past its
// rotation_grace_until. A session already dead never enters rotation
// logic.
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

// Revoke sets revoked_at on the session identified by sessionID,
// immediately and with no grace window -- revoked_at and
// rotation_grace_until are orthogonal (design decision 1). Revoking a
// session that does not exist, or is already revoked, is a no-op success
// rather than an error, so logout is safe to retry.
//
// Callers MUST verify sessionID belongs to the authenticated user before
// calling; this method performs no ownership check. It exists for the
// "revoke my own current session" call sites (e.g. logout), which already
// know sessionID is the caller's own because it came from the caller's own
// authenticated request. For revoking a session by an id a caller merely
// names (e.g. Task 9's DELETE /sessions/{id}, where the id could belong to
// someone else), use RevokeForUser instead, which checks ownership itself.
func (m *SessionManager) Revoke(ctx context.Context, sessionID uuid.UUID) error {
	now := m.now()
	if err := m.q.RevokeSession(ctx, store.RevokeSessionParams{ID: sessionID, RevokedAt: &now}); err != nil {
		return fmt.Errorf("auth: revoke session: %w", err)
	}
	return nil
}

// RevokeForUser revokes the session identified by sessionID only if it
// belongs to userID AND is still LIVE by sessionDead's exact predicates
// (fix round 1, findings I1/M5), and reports how many rows that affected:
// 1 if sessionID existed, belonged to userID, was not already revoked,
// and was not idle-expired, absolute-expired, or a grace-dead rotation
// predecessor; 0 otherwise. Callers must treat 0 as "no such LIVE session
// for this user" without distinguishing why, the same no-oracle reasoning
// as ErrSessionInvalid: Task 9's DELETE /sessions/{id} turns 0 into a
// 404, never a 403, so an attacker probing session ids can't learn
// whether a given id belongs to someone else, doesn't exist, or is
// merely dead -- and a caller can never "revoke" a row GET /sessions'
// own device list already refuses to show them (the self-inconsistency
// the review caught before this fix).
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

// TouchReauthenticated records that sessionID's lineage just completed a
// full OAuth login (design decision 2): it sets reauthenticated_at = now,
// resetting RequireRecentReauth's window. Callers use this only after an
// actual reauth round trip (purpose=reauth) -- never from rotation, which
// must not be able to satisfy the recent-reauth gate on its own.
func (m *SessionManager) TouchReauthenticated(ctx context.Context, sessionID uuid.UUID) error {
	now := m.now()
	if err := m.q.TouchReauthenticatedAt(ctx, store.TouchReauthenticatedAtParams{ID: sessionID, ReauthenticatedAt: now}); err != nil {
		return fmt.Errorf("auth: touch reauthenticated: %w", err)
	}
	return nil
}

// RequireRecentReauth returns ErrReauthRequired if sess's last full OAuth
// login (reauthenticated_at) is older than reauthWindow, and nil
// otherwise. Sensitive operations (provider link/unlink, account
// deletion, email change, slug release, log-out-everywhere) call this
// before proceeding.
func RequireRecentReauth(sess store.Session, now time.Time) error {
	if now.Sub(sess.ReauthenticatedAt) > reauthWindow {
		return ErrReauthRequired
	}
	return nil
}

// randomSessionToken returns a base64url (no padding) encoding of
// sessionTokenBytes cryptographically random bytes -- the raw bearer
// token returned by Issue and by a rotation's successor, and never
// itself persisted (only its hashSessionToken hash is).
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
// sessions.token_hash: the token is a bearer credential, hashed at rest
// (schema.sql's sessions comment) so a database read or leak never
// discloses a usable token.
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
