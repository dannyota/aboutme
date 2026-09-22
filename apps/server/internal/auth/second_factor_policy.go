package auth

import (
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// RequireRecentSecondFactorReauth checks data that the caller already locked
// and revalidated in its mutation transaction. A nil policy preserves the
// existing primary-only behavior for unenrolled accounts.
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
