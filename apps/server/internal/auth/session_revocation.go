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
// userID ("log out everywhere") and returns how many rows that
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

// ReplaceAfterEpochChangeTx replaces the deliberate current session after the
// caller advanced the user's authentication epoch by one in qtx's transaction.
// It re-locks the user row and then locks and re-reads currentSessionID, so a
// session revoked, expired, or advanced by another commit mints nothing. It
// revokes every session of the user and inserts a fresh session with no
// rotation lineage at the new epoch. The replacement copies the prior creation
// time, absolute expiry, device metadata, and primary-proof time, so no window
// is extended. Only the factor-proof time comes from the caller: the completion
// time for first enrollment, the prior value while enforcement stays, and nil
// for final removal. See docs/design/second-factor-authentication.md.
func (m *SessionManager) ReplaceAfterEpochChangeTx(ctx context.Context, qtx *store.Queries, userID, currentSessionID uuid.UUID, secondFactorVerifiedAt *time.Time) (SessionIssue, error) {
	user, err := qtx.GetUserForUpdate(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SessionIssue{}, ErrSessionInvalid
		}
		return SessionIssue{}, fmt.Errorf("auth: replace session: lock user: %w", err)
	}
	current, err := qtx.GetSessionByIDForUpdate(ctx, currentSessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SessionIssue{}, ErrSessionInvalid
		}
		return SessionIssue{}, fmt.Errorf("auth: replace session: lock current session: %w", err)
	}
	now := m.now()
	if current.UserID != user.ID || current.AuthEpoch+1 != user.AuthEpoch || RequireLiveSession(current, now) != nil {
		return SessionIssue{}, ErrSessionInvalid
	}
	if _, err = qtx.RevokeAllSessions(ctx, store.RevokeAllSessionsParams{UserID: user.ID, RevokedAt: &now}); err != nil {
		return SessionIssue{}, fmt.Errorf("auth: replace session: revoke sessions: %w", err)
	}
	raw, err := randomSessionToken()
	if err != nil {
		return SessionIssue{}, fmt.Errorf("auth: replace session: %w", err)
	}
	csrf, err := randomCSRFSecret()
	if err != nil {
		return SessionIssue{}, fmt.Errorf("auth: replace session: %w", err)
	}
	replacement, err := qtx.CreateSession(ctx, store.CreateSessionParams{
		UserID:                 user.ID,
		TokenHash:              hashSessionToken(raw),
		CSRFSecret:             csrf,
		CreatedAt:              current.CreatedAt,
		LastSeenAt:             now,
		ReauthenticatedAt:      current.ReauthenticatedAt,
		AbsoluteExpiresAt:      current.AbsoluteExpiresAt,
		UA:                     current.UA,
		IP:                     current.IP,
		AuthEpoch:              user.AuthEpoch,
		SecondFactorVerifiedAt: secondFactorVerifiedAt,
	})
	if err != nil {
		return SessionIssue{}, fmt.Errorf("auth: replace session: create replacement: %w", err)
	}
	return SessionIssue{RawToken: raw, Session: replacement}, nil
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
// otherwise: the shape sessions.ua (nullable text) wants.
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
