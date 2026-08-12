package resumeapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
)

var productionErrorVocabulary = map[string]struct{}{
	"session_required": {}, "csrf_rejected": {},
	"resume_not_found": {}, "resume_cap_exceeded": {},
	"idempotency_key_required": {}, "idempotency_key_invalid": {},
	"idempotency_key_reuse": {}, "precondition_required": {},
	"precondition_malformed": {}, "precondition_not_supported": {},
	"revision_mismatch": {}, "document_invalid": {}, "request_invalid": {},
	"unsupported_schema_version": {}, "customization_path_denied": {},
	"media_type_unsupported": {}, "media_too_large": {}, "media_invalid": {},
	"media_busy": {}, "media_not_found": {},
}

var genericErrorVocabulary = map[string]struct{}{
	"bad_request": {}, "body_too_large": {}, "internal_error": {},
	"invalid_client_ip": {}, "method_not_allowed": {}, "not_found": {},
	"rate_limited": {},
}

var constructionErrorVocabulary = map[string]struct{}{
	"not_implemented": {},
}

var detailsErrorVocabulary = map[string]struct{}{
	"document_invalid": {}, "revision_mismatch": {}, "unsupported_schema_version": {},
}

type clientError struct {
	Status  int
	Code    string
	Message string
	Details any
	Headers map[string]string
}

func (e *clientError) Error() string { return e.Code + ": " + e.Message }

func internalClientError() *clientError {
	return &clientError{Status: http.StatusInternalServerError, Code: "internal_error", Message: "an internal error occurred"}
}

type errorEnvelope struct {
	Error errorObject `json:"error"`
}

type errorObject struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func writeResumeError(w http.ResponseWriter, err *clientError) {
	if _, production := productionErrorVocabulary[err.Code]; !production {
		if _, generic := genericErrorVocabulary[err.Code]; !generic {
			err = internalClientError()
		}
	}
	if err.Details != nil {
		if _, allowed := detailsErrorVocabulary[err.Code]; !allowed {
			err = internalClientError()
		}
	}
	for name, value := range err.Headers {
		w.Header().Set(name, value)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(err.Status)
	if encodeErr := json.NewEncoder(w).Encode(errorEnvelope{Error: errorObject{
		Code: err.Code, Message: err.Message, Details: err.Details,
	}}); encodeErr != nil {
		return
	}
}

func writeStoredResponse(w http.ResponseWriter, response resume.StoredResponse) {
	for name, value := range response.Headers {
		w.Header().Set(name, value)
	}
	if response.Status == http.StatusNoContent {
		w.Header().Del("Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(response.Status)
	if _, err := w.Write(response.Body); err != nil {
		return
	}
}

func mapMutationError(err error) *clientError {
	if err == nil {
		return nil
	}
	var client *clientError
	if errors.As(err, &client) {
		return client
	}
	if errors.Is(err, resume.ErrNotFound) {
		return &clientError{Status: http.StatusNotFound, Code: "resume_not_found", Message: "resume not found"}
	}
	if errors.Is(err, resume.ErrCapExceeded) {
		return &clientError{Status: http.StatusConflict, Code: "resume_cap_exceeded", Message: "resume limit reached"}
	}
	if errors.Is(err, resume.ErrIdempotencyKeyReuse) {
		return &clientError{Status: http.StatusConflict, Code: "idempotency_key_reuse", Message: "idempotency key was already used for a different request"}
	}
	if errors.Is(err, docmigrate.ErrInvalidDocument) {
		return &clientError{
			Status: http.StatusUnprocessableEntity, Code: "document_invalid",
			Message: "resume document is invalid",
		}
	}
	var capacity *resume.IdempotencyCapacityError
	if errors.As(err, &capacity) {
		return &clientError{
			Status: http.StatusTooManyRequests, Code: "rate_limited",
			Message: "too many retained mutation results; retry later",
			Headers: map[string]string{"Retry-After": strconv.FormatInt(capacity.RetryAfterSeconds, 10)},
		}
	}
	var validation *resume.ValidationError
	if errors.As(err, &validation) {
		issues := make([]map[string]string, len(validation.Issues))
		for i, issue := range validation.Issues {
			issues[i] = map[string]string{"path": "", "code": "invalid", "message": issue}
		}
		return &clientError{
			Status: http.StatusUnprocessableEntity, Code: "document_invalid",
			Message: "resume document is invalid", Details: map[string]any{"issues": issues},
		}
	}
	return internalClientError()
}

func (s *Service) mapMutationErrorAtWire(err error, wireVersion int32) *clientError {
	var mismatch *resume.RevisionMismatchError
	if !errors.As(err, &mismatch) {
		return mapMutationError(err)
	}
	if s.projector == nil {
		return internalClientError()
	}
	canonical, assembleErr := resume.AssembleCanonical(mismatch.Current.Doc)
	if assembleErr != nil {
		return internalClientError()
	}
	document, emitErr := s.projector.EmitWire(canonical, wireVersion)
	if emitErr != nil {
		return internalClientError()
	}
	return &clientError{
		Status: http.StatusPreconditionFailed, Code: "revision_mismatch",
		Message: "the resume was modified since you last loaded it",
		Details: map[string]any{
			"revision": strconv.FormatInt(mismatch.CurrentRevision, 10),
			"document": document,
		},
		Headers: map[string]string{wireVersionHeader: wireVersionString(wireVersion)},
	}
}
