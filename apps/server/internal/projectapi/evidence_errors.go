package projectapi

import (
	"errors"
	"math"

	"danny.vn/aboutme/apps/server/internal/project"
)

func evidenceError(err error) *clientError {
	var limited *project.EvidenceRateLimitError
	if errors.As(err, &limited) {
		seconds := int64(math.Ceil(limited.Retry.Seconds()))
		return &clientError{status: 429, code: "rate_limited", message: "too many requests; retry later", retryAfter: max(seconds, 1)}
	}
	switch {
	case errors.Is(err, project.ErrEvidenceUnavailable):
		return failure(404, "evidence_unavailable", "GitHub evidence is not available")
	case errors.Is(err, project.ErrEvidenceSessionRequired):
		return failure(409, "evidence_session_required", "no GitHub evidence session is live")
	case errors.Is(err, project.ErrEvidenceNotConfirmed):
		return failure(422, "evidence_not_confirmed", "GitHub does not confirm this item")
	case errors.Is(err, project.ErrEvidenceDuplicate):
		return failure(409, "evidence_duplicate", "this item is already on the project")
	case errors.Is(err, project.ErrEvidenceLimit):
		return failure(422, "evidence_limit", "a project holds at most 10 evidence items")
	case errors.Is(err, project.ErrEvidenceUnsupportedLink):
		return failure(422, "evidence_unsupported_link", "the link is not a supported GitHub link")
	case errors.Is(err, project.ErrEvidenceUpstream):
		return failure(502, "evidence_upstream", "GitHub did not respond as expected")
	case errors.Is(err, project.ErrEvidenceNotFound):
		return failure(404, "evidence_not_found", "evidence item not found")
	case errors.Is(err, project.ErrEvidenceRequest):
		return requestError()
	}
	return nil
}
