package projectapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
)

type clientError struct {
	status        int
	code, message string
	details       any
	retryAfter    int64
}

func (e *clientError) Error() string { return e.code }
func failure(status int, code, message string) *clientError {
	return &clientError{status: status, code: code, message: message}
}
func requestError() *clientError {
	return failure(400, "request_invalid", "request does not match the operation")
}

// decodeError answers request_invalid. When one value is at fault the message
// names its path; request_invalid carries no details.
func decodeError(err error) *clientError {
	var pathErr *api.JSONPathError
	if errors.As(err, &pathErr) {
		return failure(400, "request_invalid", pathErr.Error())
	}
	return requestError()
}
func internalError() *clientError {
	return failure(500, "internal_error", "an internal error occurred")
}
func sessionError() *clientError {
	return failure(401, "session_required", "a valid session is required")
}
func rateError() *clientError {
	return &clientError{status: 429, code: "rate_limited", message: "too many slug attempts", retryAfter: 1}
}

func mapError(err error) *clientError {
	var client *clientError
	if errors.As(err, &client) {
		return client
	}
	var bare *project.RevisionMismatch
	if errors.As(err, &bare) {
		err = &linkedMismatch{current: resourceOf(bare.Current, nil)}
	}
	var mismatch *linkedMismatch
	if errors.As(err, &mismatch) {
		e := failure(412, "revision_mismatch", "project revision has changed")
		e.details = struct {
			// Revision is the current revision as decimal text.
			Revision string `json:"revision"`
			// Project is the canonical current owner project.
			Project resource `json:"project"`
		}{Revision: strconv.FormatInt(mismatch.current.Revision, 10), Project: mismatch.current}
		return e
	}
	var drain *publicstate.DrainTimeoutError
	if errors.Is(err, resumeapi.ErrPublicStateBusy) || errors.As(err, &drain) {
		return &clientError{status: 503, code: "public_state_busy", message: "public state is busy", retryAfter: 1}
	}
	var capacity *resume.IdempotencyCapacityError
	if errors.As(err, &capacity) {
		return &clientError{status: 429, code: "rate_limited", message: "idempotency capacity exceeded", retryAfter: capacity.RetryAfterSeconds}
	}
	switch {
	case errors.Is(err, project.ErrNotFound), errors.Is(err, resume.ErrProjectNotFound):
		return failure(404, "project_not_found", "project not found")
	case errors.Is(err, project.ErrLimit):
		return failure(409, "project_limit", "account project limit reached")
	case errors.Is(err, project.ErrSlugTaken):
		return failure(409, "project_slug_taken", "project slug is unavailable")
	case errors.Is(err, project.ErrSlugHeld):
		return failure(409, "project_slug_held", "project slug is held from reuse")
	case errors.Is(err, project.ErrSlugRateLimited):
		return rateError()
	case errors.Is(err, project.ErrInvalid):
		return failure(422, "document_invalid", "project document is invalid")
	case errors.Is(err, resume.ErrIdempotencyKeyReuse):
		return failure(409, "idempotency_key_reuse", "Idempotency-Key was already used with a different request")
	case errors.Is(err, auth.ErrSessionInvalid):
		return sessionError()
	case errors.Is(err, auth.ErrReauthRequired):
		return failure(403, "reauth_required", "recent reauthentication is required")
	default:
		return internalError()
	}
}

// linkedMismatch is a failed project precondition with the current project
// and its linked resume ids.
type linkedMismatch struct {
	current resource
}

// Error returns a closed mismatch description.
func (e *linkedMismatch) Error() string { return "project revision mismatch" }

type errorBody struct {
	// Code is the closed machine-readable failure code.
	Code string `json:"code"`
	// Message is a fixed description without submitted project content.
	Message string `json:"message"`
	// Details carries the current owned project for revision mismatches.
	Details any `json:"details,omitempty"`
}

type errorEnvelope struct {
	// Error is the problem returned to the owner.
	Error errorBody `json:"error"`
}

func writeError(w http.ResponseWriter, e *clientError) {
	if e.retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.FormatInt(e.retryAfter, 10))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(e.status)
	if err := json.NewEncoder(w).Encode(errorEnvelope{Error: errorBody{Code: e.code, Message: e.message, Details: e.details}}); err != nil {
		return
	}
}
