package oauthsrv

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

var (
	// oauthEntropyMu protects injected entropy readers. io.Reader does not
	// promise concurrent safety, while two approvals may issue codes together.
	oauthEntropyMu sync.Mutex

	// ErrConsentInvalid is the closed failure for a malformed, forged, or stale
	// stateless consent request. It never contains request material.
	ErrConsentInvalid = errors.New("oauth consent invalid")
	// ErrConsentNotFound is the session API's no-oracle client or redirect
	// miss. It remains an ErrConsentInvalid for protocol callers that need one
	// closed authorization failure.
	ErrConsentNotFound = fmt.Errorf("%w: authorization client not found", ErrConsentInvalid)
	// ErrGrantLimit is the closed M5 failure when a user would receive an
	// eleventh live agent grant.
	ErrGrantLimit = errors.New("oauth live grant limit")
)

// ConsentQuery is the validated browser representation of an authorization
// request. Scope remains its canonical OAuth parameter spelling at this edge.
type ConsentQuery struct {
	ClientID            uuid.UUID
	RedirectURI         string
	ResponseType        string
	Scope               string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	Scopes              Scopes
}

// ConsentView is safe for the session-authenticated consent page: it exposes
// display metadata but never state, verifier material, or credentials.
type ConsentView struct {
	ClientName string
	Scopes     Scopes
}

// ConsentDecision carries the exact authorization request together with the
// user's stateless approval or denial.
type ConsentDecision struct {
	ConsentQuery
	Decision string
}

// ConsentContext revalidates a stateless request and returns only the client
// name and canonical requested scopes for rendering.
func (s *Service) ConsentContext(ctx context.Context, _ uuid.UUID, q ConsentQuery) (ConsentView, error) {
	client, err := s.queries.GetOAuthClient(ctx, q.ClientID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ConsentView{}, ErrConsentNotFound
	}
	if err != nil {
		return ConsentView{}, fmt.Errorf("get OAuth consent client: %w", err)
	}
	if !registeredRedirect(client, q.RedirectURI) {
		return ConsentView{}, ErrConsentNotFound
	}
	if validateConsentQuery(q) != nil {
		return ConsentView{}, ErrConsentInvalid
	}
	scopes, scopeErr := q.parsedScopes()
	if scopeErr != nil {
		return ConsentView{}, ErrConsentInvalid
	}
	return ConsentView{ClientName: client.ClientName, Scopes: scopes}, nil
}

// ConsentDecision revalidates the complete request. Approval writes the grant
// and code in one transaction; denial only builds a result URL for the exact
// registered redirect URI.
func (s *Service) ConsentDecision(ctx context.Context, userID uuid.UUID, d ConsentDecision) (string, error) {
	if d.Decision != "approve" && d.Decision != "deny" {
		return "", ErrConsentInvalid
	}
	if _, err := s.ConsentContext(ctx, userID, d.ConsentQuery); err != nil {
		return "", err
	}
	if d.Decision == "deny" {
		return s.denyConsent(ctx, d.ConsentQuery)
	}
	return s.approveConsent(ctx, userID, d.ConsentQuery)
}

func (s *Service) denyConsent(ctx context.Context, request ConsentQuery) (redirectTo string, err error) {
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
	if !registeredRedirect(client, request.RedirectURI) {
		return "", ErrConsentNotFound
	}
	if validateConsentQuery(request) != nil {
		return "", ErrConsentInvalid
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit consent denial transaction: %w", err)
	}
	return oauthResultURL(request.RedirectURI, "", "access_denied", request.State), nil
}

func validateConsentQuery(q ConsentQuery) error {
	if q.ClientID == uuid.Nil || q.ResponseType != "code" || len(q.State) > stateMaxBytes || q.CodeChallengeMethod != "S256" || !isS256Challenge(q.CodeChallenge) {
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
		"client_id":             {q.ClientID.String()},
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

func (s *Service) approveConsent(ctx context.Context, userID uuid.UUID, request ConsentQuery) (redirectTo string, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin consent transaction: %w", err)
	}
	defer rollbackTransaction(context.WithoutCancel(ctx), tx, &err, "rollback consent transaction")
	qtx := store.New(tx)
	client, err := qtx.GetOAuthClientForUpdate(ctx, request.ClientID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrConsentNotFound
	}
	if err != nil {
		return "", fmt.Errorf("lock consent client: %w", err)
	}
	if !registeredRedirect(client, request.RedirectURI) {
		return "", ErrConsentNotFound
	}
	if validateConsentQuery(request) != nil {
		return "", ErrConsentInvalid
	}
	if _, userErr := qtx.GetUserForUpdate(ctx, userID); userErr != nil {
		return "", ErrConsentInvalid
	}
	grant, hasGrant, err := lockedLiveGrant(ctx, qtx, userID, request.ClientID)
	if err != nil {
		return "", err
	}
	if !hasGrant {
		count, countErr := qtx.CountLiveOAuthGrantsForUser(ctx, userID)
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
	now := s.clock()
	if _, err := qtx.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: userID, ClientID: request.ClientID, Scopes: request.Scope, CreatedAt: now}); err != nil {
		return "", fmt.Errorf("upsert OAuth grant: %w", err)
	}
	if _, err := qtx.CreateOAuthAuthorizationCode(ctx, store.CreateOAuthAuthorizationCodeParams{
		CodeDigest: digest[:], ClientID: request.ClientID, UserID: userID, Scopes: request.Scope, CodeChallenge: request.CodeChallenge, RedirectURI: request.RedirectURI, CreatedAt: now,
	}); err != nil {
		return "", fmt.Errorf("create authorization code: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit consent transaction: %w", err)
	}
	return oauthResultURL(request.RedirectURI, rawCode, "", request.State), nil
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

func (s *Service) issueCode(ctx context.Context, userID uuid.UUID, request ConsentQuery) (string, error) {
	return s.approveExistingGrant(ctx, userID, request)
}

func (s *Service) approveExistingGrant(ctx context.Context, userID uuid.UUID, request ConsentQuery) (redirectTo string, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin code transaction: %w", err)
	}
	defer rollbackTransaction(context.WithoutCancel(ctx), tx, &err, "rollback authorization code transaction")
	qtx := store.New(tx)
	client, err := qtx.GetOAuthClientForUpdate(ctx, request.ClientID)
	if err != nil || !registeredRedirect(client, request.RedirectURI) || validateConsentQuery(request) != nil {
		return "", ErrConsentInvalid
	}
	if _, userErr := qtx.GetUserForUpdate(ctx, userID); userErr != nil {
		return "", ErrConsentInvalid
	}
	grant, live, err := lockedLiveGrant(ctx, qtx, userID, request.ClientID)
	if err != nil {
		return "", err
	}
	requestedScopes, scopeErr := request.parsedScopes()
	if scopeErr != nil || !live || !grantAllows(grant.Scopes, requestedScopes) {
		return "", ErrConsentInvalid
	}
	rawCode, digest, err := s.newCode()
	if err != nil {
		return "", fmt.Errorf("new authorization code: %w", err)
	}
	now := s.clock()
	// A single live grant is the token authority. Narrow it atomically with
	// code issue so the code's requested scope and later token authority agree.
	if _, err := qtx.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: userID, ClientID: request.ClientID, Scopes: request.Scope, CreatedAt: now}); err != nil {
		return "", fmt.Errorf("narrow OAuth grant for authorization code: %w", err)
	}
	if _, err := qtx.CreateOAuthAuthorizationCode(ctx, store.CreateOAuthAuthorizationCodeParams{CodeDigest: digest[:], ClientID: request.ClientID, UserID: userID, Scopes: request.Scope, CodeChallenge: request.CodeChallenge, RedirectURI: request.RedirectURI, CreatedAt: now}); err != nil {
		return "", fmt.Errorf("create authorization code: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit code transaction: %w", err)
	}
	return oauthResultURL(request.RedirectURI, rawCode, "", request.State), nil
}

func (s *Service) newCode() (string, [32]byte, error) {
	oauthEntropyMu.Lock()
	defer oauthEntropyMu.Unlock()
	return NewCode(s.entropy)
}

func rollbackTransaction(ctx context.Context, tx pgx.Tx, primary *error, action string) {
	rollbackErr := tx.Rollback(ctx)
	if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) && *primary == nil {
		*primary = fmt.Errorf("%s: %w", action, rollbackErr)
	}
}
