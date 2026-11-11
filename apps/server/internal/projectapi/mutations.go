package projectapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/store"
)

type projectInput struct {
	// Slug is proposed on create and required on complete replacement.
	Slug *string `json:"slug,omitempty"`
	// Document is every replaceable project field.
	project.Document
}

// changedResume is the revision response shared by replace and delete.
type changedResume struct {
	// ID identifies a changed owned resume.
	ID uuid.UUID `json:"id"`
	// Revision is the advanced revision as decimal text.
	Revision int64 `json:"revision,string"`
}

type replaceResult struct {
	// Project is the canonical sanitized stored project.
	Project resource `json:"project"`
	// ChangedResumes lists the resume revisions the mutation advanced.
	ChangedResumes []changedResume `json:"changedResumes"`
}

type deleteResult struct {
	// ChangedResumes lists the resume revisions the deletion advanced.
	ChangedResumes []changedResume `json:"changedResumes"`
}

func (s *Service) mutate(w http.ResponseWriter, r *http.Request) {
	headers, headerErr := parseHeaders(r)
	if headerErr != nil {
		writeError(w, headerErr)
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("Authorization")) > 0 {
		writeError(w, requestError())
		return
	}
	var id uuid.UUID
	if r.Method != http.MethodPost {
		var err error
		id, err = uuid.Parse(r.PathValue("id"))
		if err != nil {
			writeError(w, requestError())
			return
		}
	}
	body, input, decodeErr := decodeInput(r)
	if decodeErr != nil {
		writeError(w, decodeErr)
		return
	}
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		writeError(w, sessionError())
		return
	}
	operation, fingerprint := identity(r.Method, id, headers.revision, body)
	cached, replayed, err := s.idempotency.Inspect(r.Context(), sess.UserID, operation, headers.key, fingerprint)
	if err != nil {
		writeError(w, mapError(err))
		return
	}
	if replayed {
		writeStored(w, cached)
		return
	}
	call := boundApply{service: s, ctx: r.Context(), sess: sess, method: r.Method, id: id, revision: headers.revision, input: input}
	result, err := s.idempotency.Execute(r.Context(), sess.UserID, operation, headers.key, fingerprint, call.run)
	if err != nil {
		writeError(w, mapError(err))
		return
	}
	writeStored(w, result.Response)
}

// boundApply carries the request context into the transaction callback.
type boundApply struct {
	service  *Service
	ctx      context.Context
	sess     store.Session
	method   string
	id       uuid.UUID
	revision int64
	input    projectInput
}

func (b boundApply) run(q *store.Queries) (resume.StoredResponse, error) {
	if err := b.service.reauthenticate(b.ctx, q, b.sess); err != nil {
		return resume.StoredResponse{}, err
	}
	return b.service.apply(b.ctx, q, b.sess, b.method, b.id, b.revision, b.input)
}

func decodeInput(r *http.Request) ([]byte, projectInput, *clientError) {
	if r.Body == nil {
		r.Body = http.NoBody
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 256*1024+1))
	if err != nil {
		return nil, projectInput{}, requestError()
	}
	if len(body) > 256*1024 {
		return nil, projectInput{}, failure(413, "body_too_large", "request body exceeds the 262144 byte limit")
	}
	if r.Method == http.MethodDelete {
		if len(body) > 0 {
			return nil, projectInput{}, requestError()
		}
		return body, projectInput{}, nil
	}
	var input projectInput
	if err = project.Decode(body, &input); err != nil {
		return nil, projectInput{}, requestError()
	}
	if input.Slug != nil && !project.ValidSlug(*input.Slug) {
		return nil, projectInput{}, mapError(project.ErrInvalid)
	}
	if r.Method == http.MethodPut && input.Slug == nil {
		return nil, projectInput{}, mapError(project.ErrInvalid)
	}
	return body, input, nil
}

func (s *Service) apply(ctx context.Context, q *store.Queries, sess store.Session, method string, id uuid.UUID, revision int64, input projectInput) (resume.StoredResponse, error) {
	now := s.now()
	slug := ""
	if input.Slug != nil {
		slug = *input.Slug
	}
	if method == http.MethodPost {
		if !s.allowSlug(sess.UserID, now) {
			return resume.StoredResponse{}, rateError()
		}
		p, err := s.projects.CreateTx(ctx, q, sess.UserID, slug, input.Document, now)
		if err != nil {
			return resume.StoredResponse{}, err
		}
		return stored(201, resourceOf(p), map[string]string{"Location": projectsPath + "/" + p.ID.String(), "ETag": revisionTag(p.Revision)})
	}
	current, err := s.projects.MatchTx(ctx, q, sess.UserID, id, revision)
	if err != nil {
		return resume.StoredResponse{}, err
	}
	if method == http.MethodDelete || current.Slug != slug {
		if err = s.requireRecent(ctx, q, sess, now); err != nil {
			return resume.StoredResponse{}, err
		}
	}
	if method == http.MethodDelete {
		if err = s.projects.DeleteTx(ctx, q, sess.UserID, id, revision, now); err != nil {
			return resume.StoredResponse{}, err
		}
		return stored(200, deleteResult{ChangedResumes: []changedResume{}}, nil)
	}
	if current.Slug != slug && !s.allowSlug(sess.UserID, now) {
		return resume.StoredResponse{}, rateError()
	}
	p, err := s.projects.ReplaceTx(ctx, q, sess.UserID, id, revision, slug, input.Document, now)
	if err != nil {
		return resume.StoredResponse{}, err
	}
	return stored(200, replaceResult{Project: resourceOf(p), ChangedResumes: []changedResume{}}, map[string]string{"ETag": revisionTag(p.Revision)})
}

func (s *Service) reauthenticate(ctx context.Context, q *store.Queries, sess store.Session) error {
	user, err := q.GetUserForUpdate(ctx, sess.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrSessionInvalid
	}
	if err != nil {
		return err
	}
	current, err := q.GetSessionByIDForUpdate(ctx, sess.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrSessionInvalid
	}
	if err != nil {
		return err
	}
	if current.UserID != user.ID || current.AuthEpoch != user.AuthEpoch || subtle.ConstantTimeCompare(current.CSRFSecret, sess.CSRFSecret) != 1 {
		return auth.ErrSessionInvalid
	}
	return auth.RequireLiveSession(current, s.now())
}
func (s *Service) requireRecent(ctx context.Context, q *store.Queries, sess store.Session, now time.Time) error {
	user, err := q.GetUserForUpdate(ctx, sess.UserID)
	if err != nil {
		return err
	}
	current, err := q.GetSessionByIDForUpdate(ctx, sess.ID)
	if err != nil {
		return err
	}
	policy, err := q.GetSecondFactorPolicyForUpdate(ctx, user.ID)
	var enrolled *store.SecondFactorPolicy
	if err == nil {
		enrolled = &policy
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return auth.RequireRecentSecondFactorReauth(user, enrolled, current, now)
}
func stored(status int, data any, headers map[string]string) (resume.StoredResponse, error) {
	body, err := json.Marshal(struct {
		Data any `json:"data"`
	}{Data: data})
	if err != nil {
		return resume.StoredResponse{}, err
	}
	return resume.StoredResponse{Status: status, Body: body, Headers: headers}, nil
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
func plainRequest(r *http.Request) bool {
	return r.URL.RawQuery == "" && !r.URL.ForceQuery && r.ContentLength == 0 && len(r.TransferEncoding) == 0 && len(r.Header.Values("Authorization")) == 0
}
