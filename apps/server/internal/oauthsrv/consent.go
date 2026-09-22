package oauthsrv

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
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
// and code in one transaction, after locking the client, the user, and the
// caller's concrete session in that order and requiring recent primary and
// second-factor proof for an enrolled account. Denial only builds a result
// URL for the exact registered redirect URI and needs no recent proof. See
// docs/design/second-factor-authentication.md.
func (s *Service) ConsentDecision(ctx context.Context, sess store.Session, d ConsentDecision) (string, error) {
	if d.Decision != "approve" && d.Decision != "deny" {
		return "", ErrConsentInvalid
	}
	if _, err := s.ConsentContext(ctx, sess.UserID, d.ConsentQuery); err != nil {
		return "", err
	}
	if d.Decision == "deny" {
		return s.denyConsent(ctx, d.ConsentQuery)
	}
	return s.approveConsent(ctx, sess, d.ConsentQuery)
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

func (s *Service) approveConsent(ctx context.Context, sess store.Session, request ConsentQuery) (redirectTo string, err error) {
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
		GrantID: &updatedGrant.ID, AuthEpoch: &epoch,
	}); err != nil {
		return "", fmt.Errorf("create authorization code: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit consent transaction: %w", err)
	}
	return oauthResultURL(request.RedirectURI, rawCode, "", request.State), nil
}

// requireConsentAuthority locks the caller's concrete session and second-factor
// policy inside qtx, after the client and user locks the lock order requires
// (see docs/design/second-factor-authentication.md), and enforces the recent
// primary and second-factor proof an enrolled account needs before approval or
// silent grant reuse mints a grant or authorization code. A nil policy
// preserves the primary-only requirement for an unenrolled account.
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
		// Unenrolled account: RequireRecentSecondFactorReauth checks primary
		// proof only.
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

// approveExistingGrant is the silent-reuse path HandleAuthorize takes when a
// live grant already covers the request. It requires the same current session
// authority and recent proof as an explicit approval, so a browser session
// left over from before an authentication epoch change cannot mint a fresh
// code without reproving itself.
func (s *Service) approveExistingGrant(ctx context.Context, sess store.Session, request ConsentQuery) (redirectTo string, err error) {
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
	if scopeErr != nil || !live || !grantAllows(grant.Scopes, requestedScopes) {
		return "", ErrConsentInvalid
	}
	rawCode, digest, err := s.newCode()
	if err != nil {
		return "", fmt.Errorf("new authorization code: %w", err)
	}
	epoch := user.AuthEpoch
	// A single live grant is the token authority. Narrow it atomically with
	// code issue so the code's requested scope and later token authority agree.
	updatedGrant, err := qtx.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: user.ID, ClientID: request.ClientID, Scopes: request.Scope, CreatedAt: now, AuthEpoch: epoch})
	if err != nil {
		return "", fmt.Errorf("narrow OAuth grant for authorization code: %w", err)
	}
	if _, err := qtx.CreateOAuthAuthorizationCode(ctx, store.CreateOAuthAuthorizationCodeParams{
		CodeDigest: digest[:], ClientID: request.ClientID, UserID: user.ID, Scopes: request.Scope, CodeChallenge: request.CodeChallenge, RedirectURI: request.RedirectURI, CreatedAt: now,
		GrantID: &updatedGrant.ID, AuthEpoch: &epoch,
	}); err != nil {
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
