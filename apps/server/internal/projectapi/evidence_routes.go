package projectapi

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/store"
)

type EvidenceSource interface {
	Enabled() bool
	Candidates(context.Context, store.Session, uuid.UUID, project.EvidenceQuery) (project.EvidenceCandidates, error)
	Attach(context.Context, store.Session, project.EvidenceRequest) (project.EvidenceCheck, error)
	Recheck(context.Context, store.Session, store.ProjectEvidence) (project.EvidenceCheck, error)
}

type evidenceRoute int

const (
	evidenceCandidates evidenceRoute = iota
	evidenceAttach
	evidenceOrder
	evidenceRemove
	evidenceRecheck
)

func (s *Service) evidenceEnabled() bool {
	return s.evidence != nil && s.evidence.Enabled()
}

func (s *Service) registerEvidence(mux *http.ServeMux) {
	base := projectsPath + "/{id}/evidence"
	mux.Handle(base, s.evidenceHandler(http.MethodPost, evidenceAttach))
	mux.Handle(base+"/candidates", s.evidenceHandler(http.MethodGet, evidenceCandidates))
	mux.Handle(base+"/order", s.evidenceHandler(http.MethodPut, evidenceOrder))
	mux.Handle(base+"/{evidenceId}", s.evidenceHandler(http.MethodDelete, evidenceRemove))
	mux.Handle(base+"/{evidenceId}/recheck", s.evidenceHandler(http.MethodPost, evidenceRecheck))
}

func (s *Service) evidenceHandler(method string, route evidenceRoute) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", api.CacheControlNoStore)
		if !s.evidenceEnabled() {
			writeError(w, mapError(project.ErrEvidenceUnavailable))
			return
		}
		if s.readLimit == nil || s.writeLimit == nil || s.links == nil {
			writeError(w, internalError())
			return
		}
		if r.Method != method {
			w.Header().Set("Allow", method)
			api.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed on this route")
			return
		}
		var handler http.Handler
		if route == evidenceCandidates {
			handler = s.readLimit(http.HandlerFunc(s.candidates))
		} else {
			handler = s.writeLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.mutateEvidence(w, r, route) }))
		}
		auth.RequireSession(s.sessions)(auth.RequireCSRF(s.origin)(handler)).ServeHTTP(w, r)
	})
}

func (s *Service) candidates(w http.ResponseWriter, r *http.Request) {
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		writeError(w, sessionError())
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	query, valid := candidateQuery(r.URL)
	if err != nil || !valid || r.ContentLength != 0 || len(r.TransferEncoding) != 0 || len(r.Header.Values("Authorization")) > 0 {
		writeError(w, requestError())
		return
	}
	page, err := s.evidence.Candidates(r.Context(), sess, id, query)
	if err != nil {
		writeError(w, mapError(err))
		return
	}
	api.WriteData(w, http.StatusOK, page)
}

func candidateQuery(u *url.URL) (project.EvidenceQuery, bool) {
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil || u.ForceQuery {
		return project.EvidenceQuery{}, false
	}
	for name, given := range values {
		if len(given) != 1 || (name != "kind" && name != "page" && name != "repository") {
			return project.EvidenceQuery{}, false
		}
	}
	query := project.EvidenceQuery{Kind: values.Get("kind")}
	page, pageOK := canonicalNumber(values.Get("page"))
	if !project.ValidEvidenceKind(query.Kind) || !pageOK || page > 10 {
		return project.EvidenceQuery{}, false
	}
	query.Page = int(page)
	if !values.Has("repository") {
		return query, query.Kind != project.EvidenceRelease
	}
	repository, repositoryOK := canonicalNumber(values.Get("repository"))
	query.Repository = repository
	return query, repositoryOK && query.Kind == project.EvidenceRelease
}

func canonicalNumber(text string) (int64, bool) {
	n, err := strconv.ParseInt(text, 10, 64)
	return n, err == nil && n > 0 && strconv.FormatInt(n, 10) == text
}
