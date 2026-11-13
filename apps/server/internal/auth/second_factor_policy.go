package auth

import (
	"time"

	"danny.vn/aboutme/apps/server/internal/store"
)

func RequireRecentSecondFactorReauth(user store.User, policy *store.SecondFactorPolicy, sess store.Session, now time.Time) error {
	if sess.UserID != user.ID || sess.AuthEpoch != user.AuthEpoch || RequireLiveSession(sess, now) != nil {
		return ErrSessionInvalid
	}
	if err := RequireRecentReauth(sess, now); err != nil {
		return err
	}
	if policy == nil {
		return nil
	}
	if policy.UserID != user.ID || sess.SecondFactorVerifiedAt == nil || now.Sub(*sess.SecondFactorVerifiedAt) > reauthWindow {
		return ErrReauthRequired
	}
	return nil
}
