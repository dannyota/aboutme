package projectapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
	"danny.vn/aboutme/apps/server/internal/store"
)

const maxEvidenceBodyBytes = 16 << 10

type evidenceCall struct {
	owner    uuid.UUID
	project  uuid.UUID
	item     uuid.UUID
	revision int64
	route    evidenceRoute
	attach   project.EvidenceRequest
	order    []uuid.UUID
	check    project.EvidenceCheck
	binding  int64
}

func (s *Service) mutateEvidence(w http.ResponseWriter, r *http.Request, route evidenceRoute) {
	headers, headerErr := parseEvidenceHeaders(r)
	if headerErr != nil {
		writeError(w, headerErr)
		return
	}
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		writeError(w, sessionError())
		return
	}
	call, body, callErr := readEvidenceCall(r, route)
	if callErr != nil {
		writeError(w, callErr)
		return
	}
	call.owner, call.revision = sess.UserID, headers.revision
	actor := s.sessionActor(sess)
	operation, fingerprint := evidenceIdentity(r.Method, call.project, evidencePath(route, call.item), call.revision, actor.caller, body)
	cached, replayed, err := s.idempotency.Inspect(r.Context(), call.owner, operation, headers.key, fingerprint)
	if err != nil {
		writeError(w, mapError(err))
		return
	}
	if replayed {
		writeStored(w, cached)
		return
	}
	if err = s.readEvidence(r.Context(), sess, &call); err != nil {
		writeError(w, mapError(err))
		return
	}
	result, err := s.links.EditProjectEvidence(r.Context(), resumeapi.ProjectEvidenceEdit{
		UserID: call.owner, ProjectID: call.project, Operation: operation, Key: headers.key, RequestHash: fingerprint,
		Authorize: func(ctx context.Context, q *store.Queries) error { return s.authorizeEvidence(ctx, q, actor, &call) },
		Apply: func(ctx context.Context, q *store.Queries, changed []resumeapi.ChangedResume) (resume.StoredResponse, error) {
			return s.applyEvidence(ctx, q, &call, changed)
		},
	})
	if err != nil {
		writeError(w, mapError(err))
		return
	}
	writeStored(w, result.Response)
}

func readEvidenceCall(r *http.Request, route evidenceRoute) (evidenceCall, []byte, *clientError) {
	var call evidenceCall
	var err error
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("Authorization")) > 0 {
		return call, nil, requestError()
	}
	if call.project, err = uuid.Parse(r.PathValue("id")); err != nil {
		return call, nil, requestError()
	}
	if route == evidenceRemove || route == evidenceRecheck {
		if call.item, err = uuid.Parse(r.PathValue("evidenceId")); err != nil {
			return call, nil, requestError()
		}
	}
	if r.Body == nil {
		r.Body = http.NoBody
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxEvidenceBodyBytes+1))
	if err != nil {
		return call, nil, requestError()
	}
	if len(body) > maxEvidenceBodyBytes {
		return call, nil, failure(413, "body_too_large", "request body exceeds the 16384 byte limit")
	}
	call.route = route
	switch route {
	case evidenceAttach:
		call.attach, err = decodeAttach(body)
	case evidenceOrder:
		call.order, err = decodeOrder(body)
	default:
		if len(body) != 0 {
			err = project.ErrEvidenceRequest
		}
	}
	if err != nil {
		return call, nil, mapError(err)
	}
	return call, body, nil
}

func (s *Service) readEvidence(ctx context.Context, sess store.Session, call *evidenceCall) error {
	if _, err := s.match(ctx, s.q, call.owner, call.project, call.revision); err != nil {
		return err
	}
	var err error
	switch call.route {
	case evidenceAttach:
		call.check, err = s.evidence.Attach(ctx, sess, call.attach)
	case evidenceRecheck:
		stored, getErr := s.q.GetProjectEvidence(ctx, store.GetProjectEvidenceParams{ID: call.item, ProjectID: call.project, UserID: call.owner})
		if errors.Is(getErr, pgx.ErrNoRows) {
			return project.ErrEvidenceNotFound
		}
		if getErr != nil {
			return getErr
		}
		call.check, err = s.evidence.Recheck(ctx, sess, stored)
	}
	return err
}

func (s *Service) authorizeEvidence(ctx context.Context, q *store.Queries, actor mutationActor, call *evidenceCall) error {
	if err := actor.authorize(ctx, q); err != nil {
		return err
	}
	if _, err := s.match(ctx, q, call.owner, call.project, call.revision); err != nil {
		return err
	}
	if call.check.Confirm == nil {
		return nil
	}
	binding, err := call.check.Confirm(ctx, q)
	call.binding = binding
	return err
}

func parseEvidenceHeaders(r *http.Request) (mutationHeaders, *clientError) {
	key, err := parseKey(r)
	if err != nil {
		return mutationHeaders{}, err
	}
	revision, err := parseMatch(r)
	if err != nil {
		return mutationHeaders{}, err
	}
	return mutationHeaders{key: key, revision: revision}, nil
}

func evidencePath(route evidenceRoute, item uuid.UUID) string {
	switch route {
	case evidenceOrder:
		return "evidence/order"
	case evidenceRemove:
		return "evidence/" + item.String()
	case evidenceRecheck:
		return "evidence/" + item.String() + "/recheck"
	default:
		return "evidence"
	}
}

func evidenceIdentity(method string, id uuid.UUID, path string, revision int64, caller string, body []byte) (string, [32]byte) {
	operation := sha256.Sum256([]byte("aboutme.project.evidence.operation.v1\x00" + method + "\x00" + id.String() + "\x00" + path))
	request := append([]byte(path), 0)
	request = append(strconv.AppendInt(request, revision, 10), 0)
	request = append(append(request, caller...), 0)
	return hex.EncodeToString(operation[:]), sha256.Sum256(append(request, body...))
}
