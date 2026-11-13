package projectapi

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
	"danny.vn/aboutme/apps/server/internal/store"
)

const projectsPath = "/api/v1/projects"

type Options struct {
	Sessions     *auth.SessionManager
	PublicOrigin string
	ReadLimit    api.Middleware
	WriteLimit   api.Middleware
	AllowSlug    func(uuid.UUID, time.Time) bool
	Clock        func() time.Time
	Links        LinkedResumes
}

type LinkedResumes interface {
	LinkedResumeIDs(context.Context, *store.Queries, uuid.UUID) (map[uuid.UUID][]uuid.UUID, error)
	ReplaceProject(context.Context, resumeapi.ProjectReplacement) (resume.ExecuteResult, error)
	RemoveProject(context.Context, resumeapi.ProjectRemoval) (resume.ExecuteResult, error)
}

type idempotencyStore interface {
	Inspect(context.Context, uuid.UUID, string, uuid.UUID, [32]byte) (resume.StoredResponse, bool, error)
	Execute(context.Context, uuid.UUID, string, uuid.UUID, [32]byte, func(*store.Queries) (resume.StoredResponse, error)) (resume.ExecuteResult, error)
}

type Service struct {
	q           *store.Queries
	projects    *project.Store
	idempotency idempotencyStore
	sessions    *auth.SessionManager
	origin      string
	readLimit   api.Middleware
	writeLimit  api.Middleware
	allowSlug   func(uuid.UUID, time.Time) bool
	now         func() time.Time
	links       LinkedResumes
}

func New(pool *store.Pool, idem *resume.IdempotencyStore, opts Options) *Service {
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	return &Service{q: store.New(pool), projects: project.NewStore(), idempotency: idem, sessions: opts.Sessions, origin: opts.PublicOrigin, readLimit: opts.ReadLimit, writeLimit: opts.WriteLimit, allowSlug: opts.AllowSlug, now: opts.Clock, links: opts.Links}
}

func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	for _, path := range []string{projectsPath, projectsPath + "/{id}"} {
		mux.Handle(path, http.HandlerFunc(s.dispatch))
	}
}

func (s *Service) dispatch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", api.CacheControlNoStore)
	if s.readLimit == nil || s.writeLimit == nil || s.allowSlug == nil || s.links == nil {
		writeError(w, internalError())
		return
	}
	item := r.PathValue("id") != ""
	var handler http.Handler
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		handler = auth.RequireSession(s.sessions)(s.readLimit(http.HandlerFunc(s.read)))
	case http.MethodPost:
		if !item {
			handler = auth.RequireSession(s.sessions)(auth.RequireCSRF(s.origin)(s.writeLimit(http.HandlerFunc(s.mutate))))
		}
	case http.MethodPut, http.MethodDelete:
		if item {
			handler = auth.RequireSession(s.sessions)(auth.RequireCSRF(s.origin)(s.writeLimit(http.HandlerFunc(s.mutate))))
		}
	}
	if handler == nil {
		allow := "GET, POST"
		if item {
			allow = "DELETE, GET, PUT"
		}
		w.Header().Set("Allow", allow)
		api.WriteError(w, 405, "method_not_allowed", "method not allowed on this route")
		return
	}
	handler.ServeHTTP(w, r)
}

type resource struct {
	project.Project
	LinkedResumeIDs []uuid.UUID `json:"linkedResumeIds"`
}

func resourceOf(p project.Project, links map[uuid.UUID][]uuid.UUID) resource {
	linked := append([]uuid.UUID{}, links[p.ID]...)
	return resource{Project: p, LinkedResumeIDs: linked}
}

func (s *Service) read(w http.ResponseWriter, r *http.Request) {
	if !plainRequest(r) {
		writeError(w, requestError())
		return
	}
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		writeError(w, sessionError())
		return
	}
	links, err := s.links.LinkedResumeIDs(r.Context(), s.q, sess.UserID)
	if err != nil {
		writeError(w, mapError(err))
		return
	}
	if r.PathValue("id") == "" {
		projects, listErr := s.projects.ListTx(r.Context(), s.q, sess.UserID)
		if listErr != nil {
			writeError(w, mapError(listErr))
			return
		}
		out := make([]resource, len(projects))
		for i, p := range projects {
			out[i] = resourceOf(p, links)
		}
		api.WriteData(w, 200, struct {
			Projects []resource `json:"projects"`
		}{Projects: out})
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, requestError())
		return
	}
	p, err := s.projects.GetTx(r.Context(), s.q, sess.UserID, id)
	if err != nil {
		writeError(w, mapError(err))
		return
	}
	w.Header().Set("ETag", revisionTag(p.Revision))
	api.WriteData(w, 200, resourceOf(p, links))
}
