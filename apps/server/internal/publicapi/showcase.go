package publicapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/showcase"
)

const (
	showcasePath              = "/api/v1/public/showcase"
	showcaseRequestsPerMinute = 60
	showcaseRobots            = "noindex, nofollow"
)

var showcasePagePattern = regexp.MustCompile(`^[1-9][0-9]{0,2}$`)

type ShowcaseListing interface {
	List(context.Context, showcase.Filter) (showcase.Page, error)
}

var _ ShowcaseListing = (*showcase.Service)(nil)

func newShowcaseHandler(listing ShowcaseListing, trustedProxies api.TrustedProxies, clock func() time.Time) http.Handler {
	if clock == nil {
		clock = time.Now
	}
	requests := api.NewBoundedRateLimiter(api.RateLimiterConfig{
		Requests: showcaseRequestsPerMinute, Window: time.Minute,
	})
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			serveShowcaseError(w, request, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed", 0)
			return
		}
		if request.URL.ForceQuery || len(request.Header.Values("Content-Encoding")) != 0 ||
			request.ContentLength != 0 || len(request.TransferEncoding) != 0 {
			serveShowcaseError(w, request, http.StatusBadRequest, "request_invalid", "request is invalid", 0)
			return
		}
		clientIP, admitted := api.ClientIP(request, trustedProxies)
		if !admitted {
			serveShowcaseError(w, request, http.StatusBadRequest, "request_invalid", "request is invalid", 0)
			return
		}
		if allowed, retry := requests.Admit(clock(), clientIP); !allowed {
			serveShowcaseError(w, request, http.StatusTooManyRequests, "rate_limited", "too many requests; retry later", retry)
			return
		}
		filter, valid := parseShowcaseQuery(request.URL.RawQuery)
		if !valid {
			serveShowcaseError(w, request, http.StatusBadRequest, "request_invalid", "request is invalid", 0)
			return
		}
		page, err := listing.List(request.Context(), filter)
		if err != nil {
			serveShowcaseError(w, request, http.StatusServiceUnavailable, "temporarily_unavailable", "service temporarily unavailable", 1)
			return
		}
		body, err := json.Marshal(page)
		if err != nil {
			serveShowcaseError(w, request, http.StatusServiceUnavailable, "temporarily_unavailable", "service temporarily unavailable", 1)
			return
		}
		body = append(body, '\n')
		setShowcaseHeaders(w)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		if request.Method != http.MethodHead {
			if _, writeErr := w.Write(body); writeErr != nil {
				return
			}
		}
	})
}

func parseShowcaseQuery(raw string) (showcase.Filter, bool) {
	if strings.HasPrefix(raw, "&") || strings.HasSuffix(raw, "&") || strings.Contains(raw, "&&") {
		return showcase.Filter{}, false
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return showcase.Filter{}, false
	}
	filter := showcase.Filter{Page: 1}
	for name, list := range values {
		if len(list) != 1 || list[0] == "" {
			return showcase.Filter{}, false
		}
		value := list[0]
		switch name {
		case "role":
			if !showcase.ValidRole(value) {
				return showcase.Filter{}, false
			}
			filter.Role = value
		case "lang":
			if !showcase.ValidLanguageFilter(value) {
				return showcase.Filter{}, false
			}
			filter.Lang = value
		case "template":
			if !showcase.ValidTemplateFilter(value) {
				return showcase.Filter{}, false
			}
			filter.Template = value
		case "page":
			if !showcasePagePattern.MatchString(value) {
				return showcase.Filter{}, false
			}
			number, convErr := strconv.Atoi(value)
			if convErr != nil || number < 1 || number > showcase.MaxPage {
				return showcase.Filter{}, false
			}
			filter.Page = number
		default:
			return showcase.Filter{}, false
		}
	}
	return filter, true
}

func setShowcaseHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", api.CacheControlNoStore)
	w.Header().Set("X-Robots-Tag", showcaseRobots)
}

func serveShowcaseError(w http.ResponseWriter, request *http.Request, status int, code, message string, retry int) {
	body := []byte(`{"error":{"code":"` + code + `","message":"` + message + `"}}` + "\n")
	setShowcaseHeaders(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if retry > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
	}
	w.WriteHeader(status)
	if request.Method != http.MethodHead {
		_, _ = w.Write(body) //nolint:errcheck // The response is committed; a replacement status cannot recover it.
	}
}
