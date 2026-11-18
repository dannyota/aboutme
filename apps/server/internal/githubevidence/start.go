package githubevidence

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/store"
)

const (
	projectPathPrefix = "/app/projects/"
	maxStartBodyBytes = 4 << 10
)

type startBody struct {
	AuthorizeURL string `json:"authorizeUrl"`
}

func startRequestShape() *api.JSONShape {
	return &api.JSONShape{Fields: map[string]*api.JSONShape{"returnTo": {Kind: api.JSONString}}}
}

func (s *Service) handleStart(w http.ResponseWriter, r *http.Request) {
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		writeSessionRequired(w)
		return
	}
	returnTo, valid := startReturnTo(r)
	if !valid {
		writeRequestInvalid(w, "this request takes no query and an optional object with a string returnTo")
		return
	}
	if s.registry.isClosed() {
		api.WriteError(w, http.StatusServiceUnavailable, unavailableCode, "GitHub evidence is not available")
		return
	}
	if allowed, retry := s.starts.admit(sess.UserID, s.now()); !allowed {
		writeLimited(w, retry)
		return
	}
	if err := s.transactions.ReapExpired(r.Context()); err != nil {
		s.logInternal(r, "reap_transactions", err)
	}
	row, found, err := s.connection(r, sess.UserID)
	if err != nil {
		s.writeInternal(w, r, "read_connection", err)
		return
	}
	var generation int64
	if found {
		generation = row.Generation
	}
	project, err := s.ownedProject(r.Context(), sess.UserID, returnTo)
	if err != nil {
		s.writeInternal(w, r, "read_return_project", err)
		return
	}
	handle, tx, err := s.transactions.BeginEvidence(r.Context(), sess.UserID, generation, project, s.callbackURL)
	if err != nil {
		s.writeInternal(w, r, "begin_transaction", err)
		return
	}
	auth.SetOAuthTxCookie(w, handle)
	api.WriteData(w, http.StatusOK, startBody{AuthorizeURL: s.client.authorizeLink(tx.State, tx.PKCEVerifier, s.callbackURL)})
}

func startReturnTo(r *http.Request) (string, bool) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return "", false
	}
	if r.Body == nil || r.Body == http.NoBody {
		return "", true
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxStartBodyBytes+1))
	if err != nil || len(raw) > maxStartBodyBytes {
		return "", false
	}
	if len(raw) == 0 {
		return "", true
	}
	if err = api.CheckJSONShape(raw, startRequestShape()); err != nil {
		return "", false
	}
	fields, err := strictObject(raw)
	if err != nil {
		return "", false
	}
	value, present := fields["returnTo"]
	if !present {
		return "", true
	}
	returnTo, isString := jsonString(value)
	return returnTo, isString
}

func projectFromReturnPath(returnTo string) (uuid.UUID, bool) {
	rest, ok := strings.CutPrefix(returnTo, projectPathPrefix)
	if !ok {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(rest)
	if err != nil || id == uuid.Nil || id.String() != rest {
		return uuid.Nil, false
	}
	return id, true
}

func (s *Service) ownedProject(ctx context.Context, userID uuid.UUID, returnTo string) (uuid.UUID, error) {
	project, ok := projectFromReturnPath(returnTo)
	if !ok {
		return uuid.Nil, nil
	}
	owned, err := s.q.ProjectBelongsToUser(ctx, store.ProjectBelongsToUserParams{ProjectID: project, UserID: userID})
	if err != nil || !owned {
		return uuid.Nil, err
	}
	return project, nil
}
