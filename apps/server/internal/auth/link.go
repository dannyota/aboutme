package auth

// Link and reauthentication callbacks must authenticate as the transaction's
// user. Provider identity, not email, determines the target. See
// docs/design/security.md and docs/adr/0014-oauth-start-methods.md.

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// errIdentityAlreadyLinked means the provider identity belongs to another user.
var errIdentityAlreadyLinked = errors.New("auth: identity already linked to a different user")

// errLinkOrReauthRejected collapses unclaimed reauthentication and callback
// session failures into one no-oracle response.
var errLinkOrReauthRejected = errors.New("auth: link/reauth transaction rejected")

// resolveLinkOrReauth resolves a consumed link or reauthentication transaction.
// It uses only provider identity, never email. Linking is idempotent for an
// identity already owned by the transaction user. Reauthentication requires an
// existing link and updates only the authenticated callback session. This path
// never creates a user or issues a session. See docs/design/security.md.
func (s *Service) resolveLinkOrReauth(ctx context.Context, r *http.Request, w http.ResponseWriter, tx Transaction, provider Provider, providerUserID string) error {
	sess, err := s.authenticateLinkOrReauthSession(r, w, tx.LinkingUserID)
	if err != nil {
		return err
	}

	identity, err := s.q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
		Provider:       string(provider),
		ProviderUserID: providerUserID,
	})
	switch {
	case err == nil:
		if identity.UserID != tx.LinkingUserID {
			return errIdentityAlreadyLinked
		}
		if tx.Purpose == PurposeReauth {
			if touchErr := s.sessionMgr.TouchReauthenticated(ctx, sess.ID); touchErr != nil {
				return fmt.Errorf("auth: resolve link or reauth: touch reauthenticated: %w", touchErr)
			}
			return nil
		}
		// Linking an identity already owned by this user is idempotent.
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		if tx.Purpose == PurposeReauth {
			// Reauthentication cannot create a provider link.
			return errLinkOrReauthRejected
		}
		if _, createErr := s.q.CreateIdentity(ctx, store.CreateIdentityParams{
			UserID:         tx.LinkingUserID,
			Provider:       string(provider),
			ProviderUserID: providerUserID,
		}); createErr != nil {
			if isUniqueViolation(createErr) {
				// A concurrent link may win after the lookup. Re-read to apply
				// the same ownership rule instead of exposing a constraint error.
				reread, getErr := s.q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
					Provider:       string(provider),
					ProviderUserID: providerUserID,
				})
				if getErr != nil {
					return fmt.Errorf("auth: resolve link or reauth: get identity after race: %w", getErr)
				}
				if reread.UserID != tx.LinkingUserID {
					return errIdentityAlreadyLinked
				}
				return nil // The concurrent winner linked the identity to this user.
			}
			return fmt.Errorf("auth: resolve link or reauth: create identity: %w", createErr)
		}
		return nil
	default:
		return fmt.Errorf("auth: resolve link or reauth: get identity: %w", err)
	}
}

// authenticateLinkOrReauthSession authenticates the public callback as
// linkingUserID. The callback cookie supplies the concrete session ID needed
// for reauthentication. All invalid, expired, revoked, or wrong-user sessions
// collapse to errLinkOrReauthRejected. A rotated successor cookie is delivered
// before the user comparison because rotation is already durable.
func (s *Service) authenticateLinkOrReauthSession(r *http.Request, w http.ResponseWriter, linkingUserID uuid.UUID) (store.Session, error) {
	sess, rotated, err := readAndAuthenticateSession(r, s.sessionMgr)
	if rotated != "" {
		SetSessionCookie(w, rotated)
	}
	if err != nil {
		if errors.Is(err, ErrSessionInvalid) {
			return store.Session{}, errLinkOrReauthRejected
		}
		return store.Session{}, fmt.Errorf("auth: authenticate link or reauth: %w", err)
	}
	if sess.UserID != linkingUserID {
		return store.Session{}, errLinkOrReauthRejected
	}
	return sess, nil
}

// redirectLinkOrReauthError exposes expected authorization failures and keeps
// unexpected failures opaque.
func (s *Service) redirectLinkOrReauthError(w http.ResponseWriter, r *http.Request, provider Provider, purpose Purpose, err error) {
	switch {
	case errors.Is(err, errIdentityAlreadyLinked):
		s.redirectWithError(w, r, provider, purpose, identityAlreadyLinkedErrorCode,
			reasonLinkIdentityAlreadyClaimed)
	case errors.Is(err, errLinkOrReauthRejected):
		s.redirectAuthFailed(w, r, provider, purpose,
			reasonLinkOrReauthRejected)
	default:
		s.writeInternalError(w, r, provider, "resolve_link_or_reauth", err)
	}
}
