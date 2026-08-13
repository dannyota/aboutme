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

var detailsErrorVocabulary = map[string]struct{}{
	"document_invalid": {}, "revision_mismatch": {}, "unsupported_schema_version": {},
	"media_invalid": {},
}

var mediaInvalidReasons = map[string]struct{}{
	"malformed": {}, "animated": {}, "dimensions": {}, "orientation": {},
	"trailing_data": {}, "normalization_failed": {},
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
		} else if err.Code == "media_invalid" && !validMediaInvalidDetails(err.Details) {
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

func mediaInvalidError(reason string) *clientError {
	return &clientError{
		Status: http.StatusUnprocessableEntity, Code: "media_invalid",
		Message: "image is invalid", Details: map[string]string{"reason": reason},
	}
}

func validMediaInvalidDetails(details any) bool {
	var reason string
	switch values := details.(type) {
	case map[string]string:
		if len(values) != 1 {
			return false
		}
		reason = values["reason"]
	case map[string]any:
		if len(values) != 1 {
			return false
		}
		var ok bool
		reason, ok = values["reason"].(string)
		if !ok {
			return false
		}
	default:
		return false
	}
	_, ok := mediaInvalidReasons[reason]
	return ok
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
			Message: "resume document is invalid", Details: validationDetails(resume.DescribeValidationError(err)),
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
		return &clientError{
			Status: http.StatusUnprocessableEntity, Code: "document_invalid",
			Message: "resume document is invalid", Details: validationDetails(resume.DescribeValidationError(validation)),
		}
	}
	return internalClientError()
}

func validationDetails(source []resume.ValidationIssue) map[string]any {
	issues := make([]map[string]string, len(source))
	for index, issue := range source {
		issues[index] = map[string]string{"path": issue.Path, "code": issue.Code, "message": issue.Message}
	}
	return map[string]any{"issues": issues}
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
