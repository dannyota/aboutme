package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// MeIdentitiesPath is the collection of the caller's linked provider
// identities; DELETE MeIdentitiesPath/{id} unlinks one.
const MeIdentitiesPath = "/api/v1/me/identities"

// lastSignInMethodCode refuses an unlink that would leave the account with no
// password and no identity of an enabled provider.
const lastSignInMethodCode = "last_sign_in_method"

// Unlink admission matches account deletion: per (account, client IP).
const (
	unlinkRateLimitRequests = 5
	unlinkRateLimitWindow   = time.Minute
)

var (
	errIdentityNotFound = errors.New("auth: identity not found")
	errLastSignInMethod = errors.New("auth: identity is the last sign-in method")
)

// unlinkRoute is cookie-authenticated: session, then the full CSRF rule set,
// then the per-account-and-IP limiter. It never reads a bearer token, so an
// agent cannot call it.
func (s *Service) unlinkRoute() http.HandlerFunc {
	limited := api.RateLimit(api.RateLimiterConfig{
		Requests:       unlinkRateLimitRequests,
		Window:         unlinkRateLimitWindow,
		TrustedProxies: s.trustedProxies,
		Clock:          s.sessionMgr.now,
		Key:            api.CompositeKeyFunc(api.AccountKeyFunc, api.IPKeyFunc),
		Logger:         s.logger,
	})(http.HandlerFunc(s.handleUnlinkIdentity))
	return route(http.MethodDelete, s.sessionChain(limited.ServeHTTP))
}

// handleUnlinkIdentity implements DELETE /api/v1/me/identities/{id}. Malformed,
// absent, and foreign IDs share one not-found response that never echoes the
// path. Other sessions stay live: sessions do not record a provider.
func (s *Service) handleUnlinkIdentity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess, ok := SessionFromContext(ctx)
	if !ok {
		rejectSession(w)
		return
	}
	if err := RequireRecentReauth(sess, s.sessionMgr.now()); err != nil {
		api.WriteError(w, http.StatusForbidden, reauthRequiredCode, "recent reauthentication is required")
		return
	}
	identityID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		api.WriteError(w, http.StatusNotFound, notFoundCode, "no such identity")
		return
	}

	provider, err := s.unlinkIdentity(ctx, sess, identityID)
	if err != nil && writeSensitiveGateError(w, err) {
		return
	}
	switch {
	case errors.Is(err, errIdentityNotFound):
		api.WriteError(w, http.StatusNotFound, notFoundCode, "no such identity")
		return
	case errors.Is(err, errLastSignInMethod):
		api.WriteError(w, http.StatusConflict, lastSignInMethodCode,
			"add a password or link another provider before removing this one")
		return
	case err != nil:
		s.writeSessionAPIInternalError(w, r, "unlink_identity", err)
		return
	}
	if s.logger != nil {
		s.logger.InfoContext(ctx, "auth: identity unlinked",
			"request_id", api.RequestIDFromContext(ctx), "provider", provider)
	}
	writeNoContent(w)
}

// unlinkIdentity deletes identityID for the caller's account under the
// user-row lock that password mutations and session issuers also take, so two
// concurrent unlinks cannot both pass the last-sign-in-method check. Under that
// lock it rechecks the caller's session, epoch, and recent proofs as
// docs/design/second-factor-authentication.md requires. The same transaction
// writes one identity_unlinked lifecycle audit event. It returns the provider
// of the removed identity.
func (s *Service) unlinkIdentity(ctx context.Context, sess store.Session, identityID uuid.UUID) (string, error) {
	userID := sess.UserID
	now := s.sessionMgr.now()
	var provider string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		user, lockErr := lockAccountUser(ctx, qtx, userID)
		if lockErr != nil {
			return lockErr
		}
		if _, gateErr := lockSensitiveSession(ctx, qtx, user, sess.ID, now); gateErr != nil {
			return gateErr
		}
		identities, err := qtx.ListIdentitiesByUserID(ctx, userID)
		if err != nil {
			return fmt.Errorf("list identities: %w", err)
		}
		found, otherSignIn := false, false
		for _, identity := range identities {
			if identity.ID == identityID {
				found, provider = true, identity.Provider
				continue
			}
			if s.providerEnabled(identity.Provider) {
				otherSignIn = true
			}
		}
		if !found {
			return errIdentityNotFound
		}
		if !otherSignIn {
			if _, credErr := qtx.GetPasswordCredential(ctx, userID); errors.Is(credErr, pgx.ErrNoRows) {
				return errLastSignInMethod
			} else if credErr != nil {
				return fmt.Errorf("get password credential: %w", credErr)
			}
		}
		n, err := qtx.DeleteIdentityForUser(ctx, store.DeleteIdentityForUserParams{ID: identityID, UserID: userID})
		if err != nil {
			return fmt.Errorf("delete identity: %w", err)
		}
		if n != 1 {
			return errIdentityNotFound
		}
		if err := qtx.InsertIdentityUnlinkedAuditEvent(ctx, now); err != nil {
			return fmt.Errorf("insert audit event: %w", err)
		}
		return nil
	})
	return provider, err
}

// providerEnabled reports whether an identity of provider can still sign in.
func (s *Service) providerEnabled(provider string) bool {
	switch Provider(provider) {
	case ProviderGoogle:
		return s.providerLogin.Google
	case ProviderGitHub:
		return s.providerLogin.GitHub
	case ProviderLinkedIn:
		return s.providerLogin.LinkedIn
	default:
		return false
	}
}
