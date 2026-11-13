package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/store"
)

const (
	sessionCookieName = "__Host-session"
	sessionTokenBytes = 32
	idleTimeout       = 30 * 24 * time.Hour
	absoluteTimeout   = 90 * 24 * time.Hour
	rotationAge       = 24 * time.Hour
	rotationGrace     = 60 * time.Second
	lastSeenThrottle  = time.Hour
	reauthWindow      = 15 * time.Minute
)

const csrfSecretBytes = 32

var ErrSessionInvalid = errors.New("auth: session invalid")

var ErrReauthRequired = errors.New("auth: recent reauthentication required")

type SessionManager struct {
	q    *store.Queries
	pool *store.Pool
	now  func() time.Time

	lockProbe     func()
	rotationProbe func()
}

func NewSessionManager(q *store.Queries) *SessionManager {
	return &SessionManager{q: q, now: time.Now}
}

func NewSessionManagerWithPool(pool *store.Pool) *SessionManager {
	return &SessionManager{q: store.New(pool), pool: pool, now: time.Now}
}

type SessionIssue struct {
	RawToken string
	Session  store.Session
}

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
	})
	if err != nil {
		return SessionIssue{}, fmt.Errorf("auth: issue session: %w", err)
	}
	return SessionIssue{RawToken: raw, Session: sess}, nil
}

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
	}

	if err = m.touchLastSeenAt(ctx, &sess, now); err != nil {
		return store.Session{}, "", err
	}
	return sess, "", nil
}

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
