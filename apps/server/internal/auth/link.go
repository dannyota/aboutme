package auth

// Link and reauthentication callbacks must authenticate as the transaction's
// user. Provider identity, not email, determines the target. See
// docs/design/security.md and docs/adr/0014-oauth-start-methods.md.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// errIdentityAlreadyLinked means the provider identity belongs to another user.
var errIdentityAlreadyLinked = errors.New("auth: identity already linked to a different user")

// errLinkOrReauthRejected collapses unclaimed reauthentication and callback
// session failures into one no-oracle response.
var errLinkOrReauthRejected = errors.New("auth: link/reauth transaction rejected")

// errLinkIdentityRaced marks a link insert that lost a unique-constraint race.
// The transaction rolls back and the caller re-reads the identity.
var errLinkIdentityRaced = errors.New("auth: link identity insert raced")

// resolveLinkOrReauth resolves a consumed link or reauthentication transaction.
// It uses only provider identity, never email, and never creates a user or
// issues a session. See docs/design/security.md.
//
// Both purposes lock the user and then the concrete callback session and
// recheck its owner, liveness, and epoch before any write. Linking also
// requires recent primary and, for an enrolled account, factor proofs, and is
// idempotent for an identity the user already owns. Reauthentication requires
// an existing link. For an unenrolled account it refreshes the callback
// session's primary proof. For an enrolled account it changes no session and
// returns a pending reauthentication token bound to that session instead. See
// docs/design/second-factor-authentication.md.
func (s *Service) resolveLinkOrReauth(ctx context.Context, r *http.Request, w http.ResponseWriter, tx Transaction, provider Provider, providerUserID string) (pendingRaw string, err error) {
	sess, err := s.authenticateLinkOrReauthSession(r, w, tx.LinkingUserID)
	if err != nil {
		return "", err
	}
	subject := store.GetIdentityByProviderSubjectParams{Provider: string(provider), ProviderUserID: providerUserID}
	now := s.sessionMgr.now()

	var enrolled bool
	err = pgx.BeginFunc(ctx, s.pool, func(dbTx pgx.Tx) error {
		qtx := s.q.WithTx(dbTx)
		user, lockErr := lockAccountUser(ctx, qtx, tx.LinkingUserID)
		if lockErr != nil {
			return lockErr
		}
		if tx.Purpose == PurposeReauth {
			var reauthErr error
			enrolled, reauthErr = reauthenticateProviderTx(ctx, qtx, user, sess.ID, subject, now)
			return reauthErr
		}
		return linkProviderIdentityTx(ctx, qtx, user, sess.ID, subject, now)
	})
	switch {
	case errors.Is(err, errLinkIdentityRaced):
		// A concurrent link may win after the lookup. Re-read to apply the
		// same ownership rule instead of exposing a constraint error.
		reread, getErr := s.q.GetIdentityByProviderSubject(ctx, subject)
		if getErr != nil {
			return "", fmt.Errorf("auth: resolve link or reauth: get identity after race: %w", getErr)
		}
		if reread.UserID != tx.LinkingUserID {
			return "", errIdentityAlreadyLinked
		}
		return "", nil // The concurrent winner linked the identity to this user.
	case errors.Is(err, ErrSessionInvalid), errors.Is(err, ErrReauthRequired):
		return "", errLinkOrReauthRejected
	case err != nil:
		return "", err
	}
	if !enrolled {
		return "", nil
	}

	issued, err := s.pending.Create(ctx, PendingAuthenticationRequest{
		UserID:            tx.LinkingUserID,
		Purpose:           PendingAuthenticationPurposeReauth,
		PrimaryVerifiedAt: now,
		ReturnPath:        settingsSessionsPath,
		SessionID:         &sess.ID,
		PreviousRawToken:  previousPendingToken(r),
	})
	if err != nil {
		// A callback that cannot create the pending row grants nothing and
		// uses the closed auth_failed outcome.
		s.logInternalError(r, provider, "create_pending_authentication", err)
		return "", errLinkOrReauthRejected
	}
	return issued.RawToken, nil
}

// reauthenticateProviderTx runs under the user lock. It locks the callback
// session, requires the identity to belong to user, and then locks the factor
// policy. It refreshes the primary proof only for an unenrolled account and
// reports whether the account is enrolled.
func reauthenticateProviderTx(ctx context.Context, qtx *store.Queries, user store.User, sessionID uuid.UUID, subject store.GetIdentityByProviderSubjectParams, now time.Time) (bool, error) {
	if _, err := lockCallerSession(ctx, qtx, user, sessionID, now); err != nil {
		return false, err
	}
	identity, err := qtx.GetIdentityByProviderSubject(ctx, subject)
	if errors.Is(err, pgx.ErrNoRows) {
		// Reauthentication cannot create a provider link.
		return false, errLinkOrReauthRejected
	}
	if err != nil {
		return false, fmt.Errorf("auth: resolve link or reauth: get identity: %w", err)
	}
	if identity.UserID != user.ID {
		return false, errIdentityAlreadyLinked
	}
	policy, err := lockSecondFactorPolicy(ctx, qtx, user.ID)
	if err != nil {
		return false, err
	}
	if policy != nil {
		return true, nil
	}
	if err = qtx.TouchReauthenticatedAt(ctx, store.TouchReauthenticatedAtParams{ID: sessionID, ReauthenticatedAt: now}); err != nil {
		return false, fmt.Errorf("auth: resolve link or reauth: touch reauthenticated: %w", err)
	}
	return false, nil
}

// linkProviderIdentityTx runs under the user lock. It rechecks the callback
// session and both recent proofs, then creates the identity for user unless
// user already owns it.
func linkProviderIdentityTx(ctx context.Context, qtx *store.Queries, user store.User, sessionID uuid.UUID, subject store.GetIdentityByProviderSubjectParams, now time.Time) error {
	if _, err := lockSensitiveSession(ctx, qtx, user, sessionID, now); err != nil {
		return err
	}
	identity, err := qtx.GetIdentityByProviderSubject(ctx, subject)
	switch {
	case err == nil:
		if identity.UserID != user.ID {
			return errIdentityAlreadyLinked
		}
		// Linking an identity already owned by this user is idempotent.
		return nil
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("auth: resolve link or reauth: get identity: %w", err)
	}
	if _, err = qtx.CreateIdentity(ctx, store.CreateIdentityParams{
		UserID:         user.ID,
		Provider:       subject.Provider,
		ProviderUserID: subject.ProviderUserID,
	}); err != nil {
		if isUniqueViolation(err) {
			return errLinkIdentityRaced
		}
		return fmt.Errorf("auth: resolve link or reauth: create identity: %w", err)
	}
	return nil
}

// previousPendingToken returns the browser's current pending cookie value, or
// "" when none is present.
func previousPendingToken(r *http.Request) string {
	raw, _ := readPendingCookie(r)
	return raw
}

// finishLinkOrReauth ends a successful link or reauthentication callback. An
// enrolled reauthentication sets the pending cookie and continues at the
// factor page; every other success returns to settings.
func (s *Service) finishLinkOrReauth(w http.ResponseWriter, r *http.Request, tx Transaction, pendingRaw string) {
	ClearOAuthTxCookie(w)
	if pendingRaw != "" {
		SetPendingAuthenticationCookie(w, pendingRaw)
		http.Redirect(w, r, s.publicOrigin+secondFactorPagePath, http.StatusFound)
		return
	}
	http.Redirect(w, r, s.callbackSuccessRedirect(tx), http.StatusFound)
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
