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
	"danny.vn/aboutme/apps/server/internal/resumeapi"
	"danny.vn/aboutme/apps/server/internal/store"
)

type projectInput struct {
	Slug *string `json:"slug,omitempty"`
	project.Document
}

func (i projectInput) slug() string {
	if i.Slug == nil {
		return ""
	}
	return *i.Slug
}

type changedResume struct {
	ID       uuid.UUID `json:"id"`
	Revision int64     `json:"revision,string"`
}

type replaceResult struct {
	Project        resource        `json:"project"`
	ChangedResumes []changedResume `json:"changedResumes"`
}

type deleteResult struct {
	ChangedResumes []changedResume `json:"changedResumes"`
}

type mutationActor struct {
	owner     uuid.UUID
	authorize func(context.Context, *store.Queries) error
	recent    func(context.Context, *store.Queries) error
}

func (s *Service) sessionActor(sess store.Session) mutationActor {
	return mutationActor{
		owner:     sess.UserID,
		authorize: func(ctx context.Context, q *store.Queries) error { return s.reauthenticate(ctx, q, sess) },
		recent:    func(ctx context.Context, q *store.Queries) error { return s.requireRecent(ctx, q, sess, s.now()) },
	}
}

func (s *Service) mutate(w http.ResponseWriter, r *http.Request) {
	s.mutateAs(w, r, func(ctx context.Context) (mutationActor, bool) {
		sess, ok := auth.SessionFromContext(ctx)
		if !ok {
			return mutationActor{}, false
		}
		return s.sessionActor(sess), true
	})
}

func (s *Service) mutateAs(w http.ResponseWriter, r *http.Request, actorOf func(context.Context) (mutationActor, bool)) {
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
	actor, ok := actorOf(r.Context())
	if !ok {
		writeError(w, sessionError())
		return
	}
	operation, fingerprint := identity(r.Method, id, headers.revision, body)
	cached, replayed, err := s.idempotency.Inspect(r.Context(), actor.owner, operation, headers.key, fingerprint)
	if err != nil {
		writeError(w, mapError(err))
		return
	}
	if replayed {
		writeStored(w, cached)
		return
	}
	call := boundApply{service: s, ctx: r.Context(), actor: actor, id: id, revision: headers.revision, input: input}
	var result resume.ExecuteResult
	switch r.Method {
	case http.MethodDelete:
		result, err = s.links.RemoveProject(r.Context(), resumeapi.ProjectRemoval{
			UserID: actor.owner, ProjectID: id, Operation: operation, Key: headers.key, RequestHash: fingerprint,
			Authorize: call.authorizeDelete, Remove: call.remove,
		})
	case http.MethodPut:
		result, err = s.links.ReplaceProject(r.Context(), resumeapi.ProjectReplacement{
			UserID: actor.owner, ProjectID: id, Operation: operation, Key: headers.key, RequestHash: fingerprint,
			Change: call.change, Authorize: call.authorizeReplace, Replace: call.replace,
		})
	default:
		result, err = s.idempotency.Execute(r.Context(), actor.owner, operation, headers.key, fingerprint, call.run)
	}
	if err != nil {
		writeError(w, mapError(err))
		return
	}
	writeStored(w, result.Response)
}

type boundApply struct {
	service  *Service
	ctx      context.Context
	actor    mutationActor
	id       uuid.UUID
	revision int64
	input    projectInput
}

func (b boundApply) run(q *store.Queries) (resume.StoredResponse, error) {
	if err := b.actor.authorize(b.ctx, q); err != nil {
		return resume.StoredResponse{}, err
	}
	return b.service.create(b.ctx, q, b.actor.owner, b.input)
}

func (b boundApply) requireRecent(ctx context.Context, q *store.Queries) error {
	if b.actor.recent == nil {
		return nil
	}
	return b.actor.recent(ctx, q)
}

func (b boundApply) authorizeDelete(ctx context.Context, q *store.Queries) error {
	if err := b.actor.authorize(ctx, q); err != nil {
		return err
	}
	if _, err := b.service.match(ctx, q, b.actor.owner, b.id, b.revision); err != nil {
		return err
	}
	return b.requireRecent(ctx, q)
}

func (b boundApply) remove(ctx context.Context, q *store.Queries, changed []resumeapi.ChangedResume) (resume.StoredResponse, error) {
	if err := b.service.projects.DeleteTx(ctx, q, b.actor.owner, b.id, b.revision, b.service.now()); err != nil {
		return resume.StoredResponse{}, err
	}
	return stored(200, deleteResult{ChangedResumes: changedResumes(changed)}, nil)
}

func changedResumes(changed []resumeapi.ChangedResume) []changedResume {
	out := make([]changedResume, len(changed))
	for i, item := range changed {
		out[i] = changedResume{ID: item.ID, Revision: item.Revision}
	}
	return out
}

func (s *Service) match(ctx context.Context, q *store.Queries, owner, id uuid.UUID, revision int64) (project.Project, error) {
	current, err := s.projects.MatchTx(ctx, q, owner, id, revision)
	var mismatch *project.RevisionMismatch
	if errors.As(err, &mismatch) {
		links, linkErr := s.links.LinkedResumeIDs(ctx, q, owner)
		if linkErr != nil {
			return project.Project{}, linkErr
		}
		return project.Project{}, &linkedMismatch{current: resourceOf(mismatch.Current, links)}
	}
	return current, err
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
		return nil, projectInput{}, decodeError(err)
	}
	if input.Slug != nil && !project.ValidSlug(*input.Slug) {
		return nil, projectInput{}, mapError(project.ErrInvalid)
	}
	if r.Method == http.MethodPut && input.Slug == nil {
		return nil, projectInput{}, mapError(project.ErrInvalid)
	}
	return body, input, nil
}

func (s *Service) create(ctx context.Context, q *store.Queries, owner uuid.UUID, input projectInput) (resume.StoredResponse, error) {
	now := s.now()
	admit := project.SlugAdmission(func() bool { return s.allowSlug(owner, now) })
	p, err := s.projects.CreateTx(ctx, q, owner, input.slug(), input.Document, now, admit)
	if err != nil {
		return resume.StoredResponse{}, err
	}
	return stored(201, resourceOf(p, nil), map[string]string{"Location": projectsPath + "/" + p.ID.String(), "ETag": revisionTag(p.Revision)})
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
