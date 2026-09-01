package oauthsrv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const (
	consentBodyLimit = 4096
	agentGrantLimit  = 10
)

var errAgentGrantNotFound = errors.New("oauth agent grant not found")

type consentViewData struct {
	ClientName string `json:"clientName"`
	Scopes     Scopes `json:"scopes"`
}

type consentDecisionData struct {
	RedirectTo string `json:"redirectTo"`
}

type agentGrantData struct {
	ID         string     `json:"id"`
	ClientName string     `json:"clientName"`
	Scopes     Scopes     `json:"scopes"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

type agentGrantsData struct {
	Grants []agentGrantData `json:"grants"`
}

// ConsentHTTPHandler returns the method-dispatching, session-authenticated
// consent adapter described by the OpenAPI contract.
func (s *Service) ConsentHTTPHandler(sessions *auth.SessionManager) http.Handler {
	get := auth.RequireSession(sessions)(http.HandlerFunc(s.handleConsentContext))
	post := auth.RequireSession(sessions)(auth.RequireExactJSONCSRF(s.publicOrigin)(http.HandlerFunc(s.handleConsentDecision)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			get.ServeHTTP(w, r)
		case http.MethodPost:
			post.ServeHTTP(w, r)
		default:
			w.Header().Set("Allow", "GET, POST")
			api.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed on "+r.URL.Path+"; use GET or POST")
		}
	})
}

// AgentGrantsHTTPHandler returns the session-authenticated connected-agent
// collection adapter.
func (s *Service) AgentGrantsHTTPHandler(sessions *auth.SessionManager) http.Handler {
	get := auth.RequireSession(sessions)(http.HandlerFunc(s.handleAgentGrants))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			get.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Allow", http.MethodGet)
		api.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed on "+r.URL.Path+"; use GET")
	})
}

// AgentGrantHTTPHandler returns the session, CSRF, and exact-Origin protected
// owner-scoped grant revocation adapter.
func (s *Service) AgentGrantHTTPHandler(sessions *auth.SessionManager) http.Handler {
	remove := auth.RequireSession(sessions)(auth.RequireExactJSONCSRF(s.publicOrigin)(http.HandlerFunc(s.handleRevokeAgentGrant)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			remove.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Allow", http.MethodDelete)
		api.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed on "+r.URL.Path+"; use DELETE")
	})
}

func (s *Service) handleConsentContext(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		writeSessionRequired(w)
		return
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeConsentInvalid(w)
		return
	}
	query, err := consentQueryFromValues(values)
	if err != nil {
		writeConsentInvalid(w)
		return
	}
	view, err := s.ConsentContext(r.Context(), session.UserID, query)
	if err != nil {
		writeConsentSessionError(w, err)
		return
	}
	api.WriteData(w, http.StatusOK, consentViewData{ClientName: view.ClientName, Scopes: view.Scopes})
}

func (s *Service) handleConsentDecision(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		writeSessionRequired(w)
		return
	}
	decision, err := decodeConsentDecision(w, r)
	if err != nil {
		return
	}
	redirectTo, err := s.ConsentDecision(r.Context(), session.UserID, decision)
	if err != nil {
		writeConsentSessionError(w, err)
		return
	}
	api.WriteData(w, http.StatusOK, consentDecisionData{RedirectTo: redirectTo})
}

func decodeConsentDecision(w http.ResponseWriter, r *http.Request) (ConsentDecision, error) {
	limited := http.MaxBytesReader(w, r.Body, consentBodyLimit)
	raw, err := io.ReadAll(limited)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			api.WriteError(w, http.StatusRequestEntityTooLarge, "body_too_large", "request body is too large")
		} else {
			writeConsentInvalid(w)
		}
		return ConsentDecision{}, err
	}
	fields, err := decodeConsentFields(raw)
	if err != nil {
		writeConsentInvalid(w)
		return ConsentDecision{}, err
	}
	clientID, err := uuid.Parse(fields["client_id"])
	if err != nil || clientID == uuid.Nil {
		writeConsentInvalid(w)
		return ConsentDecision{}, ErrConsentInvalid
	}
	return ConsentDecision{ConsentQuery: ConsentQuery{
		ClientID: clientID, RedirectURI: fields["redirect_uri"], ResponseType: fields["response_type"],
		Scope: fields["scope"], State: fields["state"], CodeChallenge: fields["code_challenge"],
		CodeChallengeMethod: fields["code_challenge_method"],
	}, Decision: fields["decision"]}, nil
}

func decodeConsentFields(raw []byte) (map[string]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrConsentInvalid
	}
	allowed := map[string]bool{
		"client_id": true, "redirect_uri": true, "response_type": true, "scope": true,
		"state": true, "code_challenge": true, "code_challenge_method": true, "decision": true,
	}
	fields := make(map[string]string, len(allowed))
	for decoder.More() {
		nameToken, tokenErr := decoder.Token()
		name, ok := nameToken.(string)
		if tokenErr != nil || !ok || !allowed[name] {
			return nil, ErrConsentInvalid
		}
		if _, duplicate := fields[name]; duplicate {
			return nil, ErrConsentInvalid
		}
		var value string
		if err := decoder.Decode(&value); err != nil {
			return nil, ErrConsentInvalid
		}
		fields[name] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, ErrConsentInvalid
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, ErrConsentInvalid
	}
	for _, required := range []string{"client_id", "redirect_uri", "response_type", "scope", "code_challenge", "code_challenge_method", "decision"} {
		if fields[required] == "" {
			return nil, ErrConsentInvalid
		}
	}
	return fields, nil
}

func (s *Service) handleAgentGrants(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		writeSessionRequired(w)
		return
	}
	rows, err := s.queries.ListLiveOAuthGrantsForUser(r.Context(), store.ListLiveOAuthGrantsForUserParams{
		UserID: session.UserID, LimitRows: agentGrantLimit,
	})
	if err != nil {
		writeSessionInternalError(w)
		return
	}
	grants := make([]agentGrantData, 0, len(rows))
	for _, row := range rows {
		scopes, parseErr := ParseScopes(row.Scopes)
		if parseErr != nil {
			writeSessionInternalError(w)
			return
		}
		var lastUsedAt *time.Time
		if row.LastUsedAt != nil {
			value := row.LastUsedAt.UTC()
			lastUsedAt = &value
		}
		grants = append(grants, agentGrantData{
			ID: row.ID.String(), ClientName: row.ClientName, Scopes: scopes,
			CreatedAt: row.CreatedAt.UTC(), LastUsedAt: lastUsedAt,
		})
	}
	api.WriteData(w, http.StatusOK, agentGrantsData{Grants: grants})
}

func (s *Service) handleRevokeAgentGrant(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		writeSessionRequired(w)
		return
	}
	grantID, err := uuid.Parse(r.PathValue("grantId"))
	if err != nil || grantID == uuid.Nil {
		writeAgentGrantNotFound(w)
		return
	}
	if err := s.revokeAgentGrant(r.Context(), session.UserID, grantID); err != nil {
		if errors.Is(err, errAgentGrantNotFound) {
			writeAgentGrantNotFound(w)
		} else {
			writeSessionInternalError(w)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) revokeAgentGrant(ctx context.Context, userID, grantID uuid.UUID) error {
	rows, err := s.queries.ListLiveOAuthGrantsForUser(ctx, store.ListLiveOAuthGrantsForUserParams{
		UserID: userID, LimitRows: agentGrantLimit,
	})
	if err != nil {
		return err
	}
	var clientID uuid.UUID
	for _, row := range rows {
		if row.ID == grantID {
			clientID = row.ClientID
			break
		}
	}
	if clientID == uuid.Nil {
		return errAgentGrantNotFound
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := store.New(tx)
	if _, err := q.GetOAuthClientForUpdate(ctx, clientID); err != nil {
		return err
	}
	if _, err := q.GetUserForUpdate(ctx, userID); err != nil {
		return err
	}
	grant, err := q.GetOAuthGrantForUpdate(ctx, grantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errAgentGrantNotFound
	}
	if err != nil {
		return err
	}
	if grant.UserID != userID || grant.ClientID != clientID || grant.RevokedAt != nil {
		return errAgentGrantNotFound
	}
	now := s.clock()
	if _, err := q.RevokeOAuthGrantForUser(ctx, store.RevokeOAuthGrantForUserParams{
		ID: grantID, UserID: userID, RevokedAt: now,
	}); errors.Is(err, pgx.ErrNoRows) {
		return errAgentGrantNotFound
	} else if err != nil {
		return err
	}
	if _, err := q.RevokeOAuthTokensForGrant(ctx, store.RevokeOAuthTokensForGrantParams{
		GrantID: grantID, RevokedAt: now,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func writeConsentSessionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrConsentNotFound):
		api.WriteError(w, http.StatusNotFound, "not_found", "no such authorization client")
	case errors.Is(err, ErrConsentInvalid), errors.Is(err, ErrScopeInvalid), errors.Is(err, ErrGrantLimit):
		writeConsentInvalid(w)
	default:
		writeSessionInternalError(w)
	}
}

func writeConsentInvalid(w http.ResponseWriter) {
	api.WriteError(w, http.StatusBadRequest, "request_invalid", "authorization request is invalid")
}

func writeSessionRequired(w http.ResponseWriter) {
	api.WriteError(w, http.StatusUnauthorized, "session_required", "a valid session is required")
}

func writeAgentGrantNotFound(w http.ResponseWriter) {
	api.WriteError(w, http.StatusNotFound, "not_found", "no such agent grant")
}

func writeSessionInternalError(w http.ResponseWriter) {
	api.WriteError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
}
