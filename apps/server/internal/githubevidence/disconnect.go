package githubevidence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
	"danny.vn/aboutme/apps/server/internal/store"
)

const disconnectQueryMessage = "keepLinks must be true or false and generation a whole number, each given once"

var errConnectionChanged = errors.New("githubevidence: the connection generation changed")

type revisionEntry struct {
	ID       uuid.UUID `json:"id"`
	Revision int64     `json:"revision,string"`
}

type disconnectBody struct {
	Generation       int64           `json:"generation"`
	ProjectRevisions []revisionEntry `json:"projectRevisions"`
	ResumeRevisions  []revisionEntry `json:"resumeRevisions"`
}

type disconnectCall struct {
	session    store.Session
	keepLinks  bool
	generation int64
	account    account
}

func (s *Service) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		writeSessionRequired(w)
		return
	}
	key, code, message := idempotencyKey(r)
	if code != "" {
		api.WriteError(w, http.StatusBadRequest, code, message)
		return
	}
	call := &disconnectCall{session: sess}
	var valid bool
	if call.keepLinks, call.generation, valid = disconnectParams(r); !valid || !emptyBody(r) {
		writeRequestInvalid(w, disconnectQueryMessage)
		return
	}
	if s.library == nil {
		s.writeInternal(w, r, "disconnect", errors.New("githubevidence: no project library"))
		return
	}
	operation, fingerprint := disconnectIdentity(call.keepLinks, call.generation)
	stored, replayed, err := s.idempotency.Inspect(r.Context(), sess.UserID, operation, key, fingerprint)
	if err == nil && !replayed {
		var result resume.ExecuteResult
		result, err = s.library.DisconnectEvidence(r.Context(), resumeapi.EvidenceDisconnect{
			UserID: sess.UserID, Operation: operation, Key: key, RequestHash: fingerprint,
			Projects: func(ctx context.Context, q *store.Queries) ([]uuid.UUID, error) {
				return q.ListProjectsWithEvidence(ctx, sess.UserID)
			},
			Authorize: func(ctx context.Context, q *store.Queries) error { return s.authorizeDisconnect(ctx, q, call) },
			Apply: func(ctx context.Context, q *store.Queries, changed []resumeapi.ChangedResume) (resume.StoredResponse, error) {
				return s.applyDisconnect(ctx, q, call, changed)
			},
		})
		stored = result.Response
	}
	if err != nil {
		s.writeDisconnectError(w, r, err)
		return
	}
	writeStored(w, stored)
}

func (s *Service) authorizeDisconnect(ctx context.Context, q *store.Queries, call *disconnectCall) error {
	a, err := s.lockAccount(ctx, nil, q, call.session)
	if err != nil {
		return err
	}
	var current int64
	if a.found {
		current = a.connection.Generation
	}
	if current != call.generation {
		return errConnectionChanged
	}
	call.account = a
	return nil
}

func (s *Service) applyDisconnect(ctx context.Context, q *store.Queries, call *disconnectCall, changed []resumeapi.ChangedResume) (resume.StoredResponse, error) {
	userID := call.account.user.ID
	generation, err := q.ClearGitHubConnection(ctx, userID)
	if err != nil {
		return resume.StoredResponse{}, fmt.Errorf("githubevidence: clear connection: %w", err)
	}
	body := disconnectBody{Generation: generation, ProjectRevisions: []revisionEntry{}, ResumeRevisions: make([]revisionEntry, len(changed))}
	if !call.keepLinks {
		projects, deleteErr := q.DeleteAccountEvidence(ctx, store.DeleteAccountEvidenceParams{UserID: userID, UpdatedAt: call.account.now})
		if deleteErr != nil {
			return resume.StoredResponse{}, fmt.Errorf("githubevidence: delete evidence: %w", deleteErr)
		}
		for _, project := range projects {
			body.ProjectRevisions = append(body.ProjectRevisions, revisionEntry{ID: project.ID, Revision: project.Revision})
		}
	}
	for i, item := range changed {
		body.ResumeRevisions[i] = revisionEntry{ID: item.ID, Revision: item.Revision}
	}
	encoded, err := json.Marshal(struct {
		Data disconnectBody `json:"data"`
	}{Data: body})
	if err != nil {
		return resume.StoredResponse{}, fmt.Errorf("githubevidence: encode disconnect: %w", err)
	}
	return resume.StoredResponse{
		Status: http.StatusOK, Body: encoded,
		BeforeCommit: func() { s.registry.end(userID) },
	}, nil
}

func (s *Service) writeDisconnectError(w http.ResponseWriter, r *http.Request, err error) {
	var capacity *resume.IdempotencyCapacityError
	var drain *publicstate.DrainTimeoutError
	switch {
	case errors.Is(err, auth.ErrSessionInvalid):
		writeSessionRequired(w)
	case errors.Is(err, errConnectionChanged):
		api.WriteError(w, http.StatusConflict, "evidence_connection_changed", "the GitHub connection has changed")
	case errors.Is(err, resume.ErrIdempotencyKeyReuse):
		api.WriteError(w, http.StatusConflict, "idempotency_key_reuse", "Idempotency-Key was already used with a different request")
	case errors.As(err, &capacity):
		w.Header().Set("Retry-After", strconv.FormatInt(max(capacity.RetryAfterSeconds, 1), 10))
		api.WriteError(w, http.StatusTooManyRequests, "rate_limited", "idempotency capacity exceeded")
	case errors.Is(err, resumeapi.ErrPublicStateBusy), errors.As(err, &drain):
		w.Header().Set("Retry-After", "1")
		api.WriteError(w, http.StatusServiceUnavailable, "public_state_busy", "public state is busy")
	default:
		s.writeInternal(w, r, "disconnect", err)
	}
}

func writeStored(w http.ResponseWriter, response resume.StoredResponse) {
	for name, value := range response.Headers {
		w.Header().Set(name, value)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(response.Status)
	if _, err := w.Write(response.Body); err != nil {
		return
	}
}

func idempotencyKey(r *http.Request) (uuid.UUID, string, string) {
	values := r.Header.Values("Idempotency-Key")
	if len(values) == 0 {
		return uuid.Nil, "idempotency_key_required", "Idempotency-Key is required"
	}
	if len(values) != 1 || values[0] == "" || strings.Contains(values[0], ",") {
		return uuid.Nil, "idempotency_key_invalid", "Idempotency-Key must be one UUID"
	}
	key, err := uuid.Parse(values[0])
	if err != nil {
		return uuid.Nil, "idempotency_key_invalid", "Idempotency-Key must be one UUID"
	}
	return key, "", ""
}

func disconnectParams(r *http.Request) (bool, int64, bool) {
	if r.URL.ForceQuery || r.URL.RawQuery == "" {
		return false, 0, false
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(query) != 2 || len(query["keepLinks"]) != 1 || len(query["generation"]) != 1 {
		return false, 0, false
	}
	var keepLinks bool
	switch query.Get("keepLinks") {
	case "true":
		keepLinks = true
	case "false":
	default:
		return false, 0, false
	}
	text := query.Get("generation")
	generation, err := strconv.ParseInt(text, 10, 64)
	if err != nil || generation < 0 || strconv.FormatInt(generation, 10) != text {
		return false, 0, false
	}
	return keepLinks, generation, true
}

func disconnectIdentity(keepLinks bool, generation int64) (string, [32]byte) {
	payload := "keepLinks=" + strconv.FormatBool(keepLinks) + "&generation=" + strconv.FormatInt(generation, 10)
	return resumeapi.AccountWriteIdentity(http.MethodDelete, "deleteGitHubEvidence", []byte(payload))
}
