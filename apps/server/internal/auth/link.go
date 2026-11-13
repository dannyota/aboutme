package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/store"
)

var errIdentityAlreadyLinked = errors.New("auth: identity already linked to a different user")

var errLinkOrReauthRejected = errors.New("auth: link/reauth transaction rejected")

var errLinkIdentityRaced = errors.New("auth: link identity insert raced")

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
		reread, getErr := s.q.GetIdentityByProviderSubject(ctx, subject)
		if getErr != nil {
			return "", fmt.Errorf("auth: resolve link or reauth: get identity after race: %w", getErr)
		}
		if reread.UserID != tx.LinkingUserID {
			return "", errIdentityAlreadyLinked
		}
		return "", nil
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
		s.logInternalError(r, provider, "create_pending_authentication", err)
		return "", errLinkOrReauthRejected
	}
	return issued.RawToken, nil
}

func reauthenticateProviderTx(ctx context.Context, qtx *store.Queries, user store.User, sessionID uuid.UUID, subject store.GetIdentityByProviderSubjectParams, now time.Time) (bool, error) {
	if _, err := lockCallerSession(ctx, qtx, user, sessionID, now); err != nil {
		return false, err
	}
	identity, err := qtx.GetIdentityByProviderSubject(ctx, subject)
	if errors.Is(err, pgx.ErrNoRows) {
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

func previousPendingToken(r *http.Request) string {
	raw, _ := readPendingCookie(r)
	return raw
}

func (s *Service) finishLinkOrReauth(w http.ResponseWriter, r *http.Request, tx Transaction, pendingRaw string) {
	ClearOAuthTxCookie(w)
	if pendingRaw != "" {
		SetPendingAuthenticationCookie(w, pendingRaw)
		http.Redirect(w, r, s.publicOrigin+secondFactorPagePath, http.StatusFound)
		return
	}
	http.Redirect(w, r, s.callbackSuccessRedirect(tx), http.StatusFound)
}

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

func (s *Service) redirectLinkOrReauthError(w http.ResponseWriter, r *http.Request, provider Provider, tx Transaction, err error) {
	switch {
	case errors.Is(err, errIdentityAlreadyLinked):
		s.redirectWithError(w, r, provider, tx, identityAlreadyLinkedErrorCode,
			reasonLinkIdentityAlreadyClaimed)
	case errors.Is(err, errLinkOrReauthRejected):
		s.redirectAuthFailed(w, r, provider, tx,
			reasonLinkOrReauthRejected)
	default:
		s.writeInternalError(w, r, provider, "resolve_link_or_reauth", err)
	}
}
