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

	"danny.vn/aboutme/apps/server/internal/store"
)

func (m *SessionManager) Revoke(ctx context.Context, sessionID uuid.UUID) error {
	now := m.now()
	if err := m.q.RevokeSession(ctx, store.RevokeSessionParams{ID: sessionID, RevokedAt: &now}); err != nil {
		return fmt.Errorf("auth: revoke session: %w", err)
	}
	return nil
}

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

func (m *SessionManager) RevokeAll(ctx context.Context, userID uuid.UUID) (int64, error) {
	now := m.now()
	n, err := m.q.RevokeAllSessions(ctx, store.RevokeAllSessionsParams{UserID: userID, RevokedAt: &now})
	if err != nil {
		return 0, fmt.Errorf("auth: revoke all sessions: %w", err)
	}
	return n, nil
}

func (m *SessionManager) TouchReauthenticated(ctx context.Context, sessionID uuid.UUID) error {
	now := m.now()
	if err := m.q.TouchReauthenticatedAt(ctx, store.TouchReauthenticatedAtParams{ID: sessionID, ReauthenticatedAt: now}); err != nil {
		return fmt.Errorf("auth: touch reauthenticated: %w", err)
	}
	return nil
}

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

func RequireRecentReauth(sess store.Session, now time.Time) error {
	if now.Sub(sess.ReauthenticatedAt) > reauthWindow {
		return ErrReauthRequired
	}
	return nil
}

func RequireLiveSession(sess store.Session, now time.Time) error {
	if sessionDead(sess, now) {
		return ErrSessionInvalid
	}
	return nil
}

func randomSessionToken() (string, error) {
	b := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func randomCSRFSecret() ([]byte, error) {
	b := make([]byte, csrfSecretBytes)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generate csrf secret: %w", err)
	}
	return b, nil
}

func hashSessionToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

func stringParam(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

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
