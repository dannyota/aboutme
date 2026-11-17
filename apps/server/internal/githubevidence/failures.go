package githubevidence

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"danny.vn/aboutme/apps/server/internal/api"
)

const (
	notConfirmedCode    = "evidence_not_confirmed"
	unsupportedLinkCode = "evidence_unsupported_link"
	upstreamCode        = "evidence_upstream"
	rateLimitedCode     = "rate_limited"
	defaultGitHubRetry  = 60 * time.Second
)

var (
	errRateLimited     = errors.New("githubevidence: rate limited")
	errUnsupportedLink = errors.New("githubevidence: unsupported link")
)

type rateLimitError struct {
	retry time.Duration
}

func (e *rateLimitError) Error() string {
	return errRateLimited.Error()
}

func (e *rateLimitError) Unwrap() error {
	return errRateLimited
}

func retryAfter(err error) (time.Duration, bool) {
	var limited *rateLimitError
	if errors.As(err, &limited) {
		return limited.retry, true
	}
	return 0, false
}

type readFailure struct {
	status  int
	code    string
	message string
	retry   time.Duration
}

func evidenceFailure(err error) readFailure {
	if retry, limited := retryAfter(err); limited {
		return readFailure{status: http.StatusTooManyRequests, code: rateLimitedCode, message: "too many requests; retry later", retry: retry}
	}
	switch {
	case errors.Is(err, errSessionRejected), errors.Is(err, errTokenGone):
		return readFailure{status: http.StatusConflict, code: sessionRequiredCode, message: "no GitHub evidence session is live"}
	case errors.Is(err, errGone):
		return readFailure{status: http.StatusUnprocessableEntity, code: notConfirmedCode, message: "GitHub does not confirm this item"}
	case errors.Is(err, errUnsupportedLink):
		return readFailure{status: http.StatusUnprocessableEntity, code: unsupportedLinkCode, message: "the link is not a supported GitHub link"}
	default:
		return readFailure{status: http.StatusBadGateway, code: upstreamCode, message: "GitHub did not respond as expected"}
	}
}

func writeEvidenceFailure(w http.ResponseWriter, err error) {
	failure := evidenceFailure(err)
	if failure.code == rateLimitedCode {
		writeLimited(w, failure.retry)
		return
	}
	api.WriteError(w, failure.status, failure.code, failure.message)
}

func rateLimitedResponse(header http.Header, body []byte) bool {
	if header.Get("Retry-After") != "" || header.Get("X-RateLimit-Remaining") == "0" {
		return true
	}
	fields, err := strictObject(body)
	if err != nil {
		return false
	}
	message, isString := jsonString(fields["message"])
	return isString && strings.Contains(strings.ToLower(message), "rate limit")
}

func githubRetry(header http.Header, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(strings.TrimSpace(header.Get("Retry-After")), 10, 32); err == nil && seconds >= 0 {
		return wholeSeconds(time.Duration(seconds) * time.Second)
	}
	if header.Get("X-RateLimit-Remaining") == "0" {
		if reset, err := strconv.ParseInt(strings.TrimSpace(header.Get("X-RateLimit-Reset")), 10, 64); err == nil {
			return wholeSeconds(time.Unix(reset, 0).Sub(now))
		}
	}
	return defaultGitHubRetry
}

func wholeSeconds(d time.Duration) time.Duration {
	if d < time.Second {
		return time.Second
	}
	return (d + time.Second - 1).Truncate(time.Second)
}
