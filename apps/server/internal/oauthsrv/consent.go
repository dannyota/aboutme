package oauthsrv

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/store"
)

var (
	ErrConsentInvalid  = errors.New("oauth consent invalid")
	ErrConsentNotFound = fmt.Errorf("%w: authorization client not found", ErrConsentInvalid)
	ErrGrantLimit      = errors.New("oauth live grant limit")
)

type ConsentQuery struct {
	PublicClientID      string
	metadata            *CIMDMetadata
	address             netip.Addr
	ClientID            uuid.UUID
	RedirectURI         string
	ResponseType        string
	Scope               string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	Scopes              Scopes
}

type ConsentView struct {
	ClientKind string
	ClientHost *string
	ClientName string
	Scopes     Scopes
}

type ConsentDecision struct {
	ConsentQuery
	Decision string
}

func (s *Service) ConsentContext(ctx context.Context, _ uuid.UUID, q ConsentQuery) (ConsentView, error) {
	resolved, client, err := s.resolveConsent(ctx, q)
	if err != nil {
		return ConsentView{}, fmt.Errorf("get OAuth consent client: %w", err)
	}
	if validateConsentQuery(resolved) != nil {
		return ConsentView{}, ErrConsentInvalid
	}
	scopes, scopeErr := q.parsedScopes()
	if scopeErr != nil {
		return ConsentView{}, ErrConsentInvalid
	}
	kind, host := clientDisplay(client.MetadataUrl)
	if resolved.metadata != nil {
		kind = "cimd"
		value := resolved.metadata.host
		host = &value
	}
	return ConsentView{ClientName: client.ClientName, Scopes: scopes, ClientKind: kind, ClientHost: host}, nil
}

func (s *Service) ConsentDecision(ctx context.Context, sess store.Session, d ConsentDecision) (string, error) {
	if d.Decision != "approve" && d.Decision != "deny" {
		return "", ErrConsentInvalid
	}
	resolved, _, err := s.resolveConsent(ctx, d.ConsentQuery)
	if err != nil {
		return "", err
	}
	if validateConsentQuery(resolved) != nil {
		return "", ErrConsentInvalid
	}
	d.ConsentQuery = resolved
	if d.Decision == "deny" {
		return s.denyConsent(ctx, d.ConsentQuery)
	}
	return s.approveConsent(ctx, sess, d.ConsentQuery)
}

func (s *Service) denyConsent(ctx context.Context, request ConsentQuery) (redirectTo string, err error) {
	if request.metadata != nil {
		if !request.metadata.matchesRedirect(request.RedirectURI) {
			return "", ErrConsentInvalid
		}
		return oauthResultURL(request.RedirectURI, s.publicOrigin, "", "access_denied", request.State), nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin consent denial transaction: %w", err)
	}
	defer rollbackTransaction(context.WithoutCancel(ctx), tx, &err, "rollback consent denial transaction")
	client, err := store.New(tx).GetOAuthClientForUpdate(ctx, request.ClientID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrConsentNotFound
	}
	if err != nil {
		return "", fmt.Errorf("lock consent client: %w", err)
	}
	if !consentRedirectMatches(client, request) {
		return "", ErrConsentNotFound
	}
	if validateConsentQuery(request) != nil {
		return "", ErrConsentInvalid
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit consent denial transaction: %w", err)
	}
	return oauthResultURL(request.RedirectURI, s.publicOrigin, "", "access_denied", request.State), nil
}

func validateConsentQuery(q ConsentQuery) error {
	if (q.ClientID == uuid.Nil && !strings.HasPrefix(q.PublicClientID, "https://")) || q.ResponseType != "code" || len(q.State) > stateMaxBytes || q.CodeChallengeMethod != "S256" || !isS256Challenge(q.CodeChallenge) {
		return ErrConsentInvalid
	}
	scopes, err := ParseScopes(q.Scope)
	if err != nil {
		return err
	}
	if q.Scope != scopes.String() {
		return ErrConsentInvalid
	}
	return nil
}

func (q ConsentQuery) parsedScopes() (Scopes, error) {
	if err := validateConsentQuery(q); err != nil {
		return nil, err
	}
	return ParseScopes(q.Scope)
}

func (q ConsentQuery) values() url.Values {
	values := url.Values{
		"client_id":             {q.publicID()},
		"redirect_uri":          {q.RedirectURI},
		"response_type":         {q.ResponseType},
		"scope":                 {q.Scope},
		"code_challenge":        {q.CodeChallenge},
		"code_challenge_method": {q.CodeChallengeMethod},
	}
	if q.State != "" {
		values.Set("state", q.State)
	}
	return values
}

func (s *Service) approveConsent(ctx context.Context, sess store.Session, request ConsentQuery) (redirectTo string, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin consent transaction: %w", err)
	}
	defer rollbackTransaction(context.WithoutCancel(ctx), tx, &err, "rollback consent transaction")
	qtx := store.New(tx)
	client, err := s.lockApprovalClient(ctx, qtx, &request)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrConsentNotFound
	}
	if err != nil {
		return "", fmt.Errorf("lock consent client: %w", err)
	}
	if !consentRedirectMatches(client, request) {
		return "", ErrConsentNotFound
	}
	if validateConsentQuery(request) != nil {
		return "", ErrConsentInvalid
	}
	user, userErr := qtx.GetUserForUpdate(ctx, sess.UserID)
	if userErr != nil {
		return "", ErrConsentInvalid
	}
	now := s.clock()
	if _, authErr := requireConsentAuthority(ctx, qtx, user, sess.ID, now); authErr != nil {
		return "", authErr
	}
	grant, hasGrant, err := lockedLiveGrant(ctx, qtx, user.ID, request.ClientID)
	if err != nil {
		return "", err
	}
	if !hasGrant {
		count, countErr := qtx.CountLiveOAuthGrantsForUser(ctx, user.ID)
		if countErr != nil {
			return "", fmt.Errorf("count live OAuth grants: %w", countErr)
		}
		if count >= int64(s.liveGrantLimit) {
			return "", ErrGrantLimit
		}
	}
	_ = grant
	rawCode, digest, err := s.newCode()
	if err != nil {
		return "", fmt.Errorf("new authorization code: %w", err)
	}
	epoch := user.AuthEpoch
	updatedGrant, err := qtx.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: user.ID, ClientID: request.ClientID, Scopes: request.Scope, CreatedAt: now, AuthEpoch: epoch})
	if err != nil {
		return "", fmt.Errorf("upsert OAuth grant: %w", err)
	}
	if _, err := qtx.CreateOAuthAuthorizationCode(ctx, store.CreateOAuthAuthorizationCodeParams{
		CodeDigest: digest[:], ClientID: request.ClientID, UserID: user.ID, Scopes: request.Scope, CodeChallenge: request.CodeChallenge, RedirectURI: request.RedirectURI, CreatedAt: now,
		GrantID: updatedGrant.ID, AuthEpoch: epoch,
	}); err != nil {
		return "", fmt.Errorf("create authorization code: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit consent transaction: %w", err)
	}
	return oauthResultURL(request.RedirectURI, s.publicOrigin, rawCode, "", request.State), nil
}

func requireConsentAuthority(ctx context.Context, qtx *store.Queries, user store.User, sessionID uuid.UUID, now time.Time) (store.Session, error) {
	sess, err := qtx.GetSessionByIDForUpdate(ctx, sessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Session{}, auth.ErrSessionInvalid
		}
		return store.Session{}, fmt.Errorf("lock consent session: %w", err)
	}
	policy, err := qtx.GetSecondFactorPolicyForUpdate(ctx, user.ID)
	var policyPtr *store.SecondFactorPolicy
	switch {
	case err == nil:
		policyPtr = &policy
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return store.Session{}, fmt.Errorf("lock consent factor policy: %w", err)
	}
	if reauthErr := auth.RequireRecentSecondFactorReauth(user, policyPtr, sess, now); reauthErr != nil {
		return store.Session{}, reauthErr
	}
	return sess, nil
}

func lockedLiveGrant(ctx context.Context, q *store.Queries, userID, clientID uuid.UUID) (store.OAuthGrant, bool, error) {
	grant, err := q.GetLiveOAuthGrant(ctx, store.GetLiveOAuthGrantParams{UserID: userID, ClientID: clientID})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.OAuthGrant{}, false, nil
	}
	if err != nil {
		return store.OAuthGrant{}, false, fmt.Errorf("get live OAuth grant: %w", err)
	}
	locked, err := q.GetOAuthGrantForUpdate(ctx, grant.ID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && locked.RevokedAt != nil) {
		return store.OAuthGrant{}, false, nil
	}
	if err != nil {
		return store.OAuthGrant{}, false, fmt.Errorf("lock OAuth grant: %w", err)
	}
	return locked, true, nil
}

func (s *Service) issueCode(ctx context.Context, sess store.Session, request ConsentQuery) (string, error) {
	return s.approveExistingGrant(ctx, sess, request)
}

func (s *Service) approveExistingGrant(ctx context.Context, sess store.Session, request ConsentQuery) (redirectTo string, err error) {
	if request.metadata != nil && nativeLoopbackRedirect(request.RedirectURI) {
		return "", ErrConsentInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin code transaction: %w", err)
	}
	defer rollbackTransaction(context.WithoutCancel(ctx), tx, &err, "rollback authorization code transaction")
	qtx := store.New(tx)
	client, err := qtx.GetOAuthClientForUpdate(ctx, request.ClientID)
	if err != nil || !consentRedirectMatches(client, request) || validateConsentQuery(request) != nil {
		return "", ErrConsentInvalid
	}
	user, userErr := qtx.GetUserForUpdate(ctx, sess.UserID)
	if userErr != nil {
		return "", ErrConsentInvalid
	}
	now := s.clock()
	if _, authErr := requireConsentAuthority(ctx, qtx, user, sess.ID, now); authErr != nil {
		return "", authErr
	}
	grant, live, err := lockedLiveGrant(ctx, qtx, user.ID, request.ClientID)
	if err != nil {
		return "", err
	}
	requestedScopes, scopeErr := request.parsedScopes()
	if scopeErr != nil || !live || grant.AuthEpoch != user.AuthEpoch || !grantAllows(grant.Scopes, requestedScopes) {
		return "", ErrConsentInvalid
	}
	rawCode, digest, err := s.newCode()
	if err != nil {
		return "", fmt.Errorf("new authorization code: %w", err)
	}
	epoch := user.AuthEpoch
	updatedGrant, err := qtx.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: user.ID, ClientID: request.ClientID, Scopes: request.Scope, CreatedAt: now, AuthEpoch: epoch})
	if err != nil {
		return "", fmt.Errorf("narrow OAuth grant for authorization code: %w", err)
	}
	if _, err := qtx.CreateOAuthAuthorizationCode(ctx, store.CreateOAuthAuthorizationCodeParams{
		CodeDigest: digest[:], ClientID: request.ClientID, UserID: user.ID, Scopes: request.Scope, CodeChallenge: request.CodeChallenge, RedirectURI: request.RedirectURI, CreatedAt: now,
		GrantID: updatedGrant.ID, AuthEpoch: epoch,
	}); err != nil {
		return "", fmt.Errorf("create authorization code: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit code transaction: %w", err)
	}
	return oauthResultURL(request.RedirectURI, s.publicOrigin, rawCode, "", request.State), nil
}

func rollbackTransaction(ctx context.Context, tx pgx.Tx, primary *error, action string) {
	rollbackErr := tx.Rollback(ctx)
	if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) && *primary == nil {
		*primary = fmt.Errorf("%s: %w", action, rollbackErr)
	}
}
