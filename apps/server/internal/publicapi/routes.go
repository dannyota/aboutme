package publicapi

import (
	"net/http"
	"strings"

	"github.com/dannyota/aboutme/apps/server/internal/publicroots"
)

type publicRoute uint8

const (
	publicRouteNone publicRoute = iota
	publicRouteJSON
	publicRoutePhoto
	publicRouteHTML
	publicRouteMarkdown
	publicRouteSitemap
	publicRouteRobots
	publicRouteLLMS
)

// Recognizes reports whether escapedPath belongs to a public route.
func (s *Service) Recognizes(escapedPath string) bool {
	return classifyPublicRoute(escapedPath) != publicRouteNone
}

func (s *Service) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	switch classifyPublicRoute(request.URL.EscapedPath()) {
	case publicRouteJSON:
		s.json.ServeHTTP(w, request)
	case publicRoutePhoto:
		s.photo.ServeHTTP(w, request)
	case publicRouteHTML:
		s.html.ServeHTTP(w, request)
	case publicRouteMarkdown:
		s.markdown.ServeHTTP(w, request)
	case publicRouteSitemap:
		s.sitemap.ServeHTTP(w, request)
	case publicRouteRobots:
		s.robots.ServeHTTP(w, request)
	case publicRouteLLMS:
		s.llms.ServeHTTP(w, request)
	default:
		http.NotFound(w, request)
	}
}

func classifyPublicRoute(path string) publicRoute {
	switch path {
	case "/sitemap.xml":
		return publicRouteSitemap
	case "/robots.txt":
		return publicRouteRobots
	case "/llms.txt":
		return publicRouteLLMS
	}
	if strings.Contains(path, "%") {
		return publicRouteNone
	}
	const prefix = "/api/v1/public/resumes/"
	if strings.HasPrefix(path, prefix) {
		rest := strings.TrimPrefix(path, prefix)
		if strings.HasSuffix(rest, "/photo") {
			if validPublicSlug(strings.TrimSuffix(rest, "/photo")) {
				return publicRoutePhoto
			}
			return publicRouteNone
		}
		if validPublicSlug(rest) {
			return publicRouteJSON
		}
		return publicRouteNone
	}
	if !strings.HasPrefix(path, "/") || strings.Count(path, "/") != 1 {
		return publicRouteNone
	}
	segment := strings.TrimPrefix(path, "/")
	if strings.HasSuffix(segment, ".md") && validPublicSlug(strings.TrimSuffix(segment, ".md")) {
		return publicRouteMarkdown
	}
	if validPublicSlug(segment) {
		return publicRouteHTML
	}
	return publicRouteNone
}

func publicSlug(path string) string {
	const prefix = "/api/v1/public/resumes/"
	if strings.HasPrefix(path, prefix) {
		return strings.TrimSuffix(strings.TrimPrefix(path, prefix), "/photo")
	}
	return strings.TrimSuffix(strings.TrimPrefix(path, "/"), ".md")
}

func validPublicSlug(slug string) bool {
	if len(slug) < 4 || len(slug) > 30 || publicroots.Reserved(slug) || slug[0] == '-' || slug[len(slug)-1] == '-' {
		return false
	}
	previousHyphen := false
	for i := range slug {
		letter := slug[i] >= 'a' && slug[i] <= 'z'
		digit := slug[i] >= '0' && slug[i] <= '9'
		if slug[i] == '-' {
			if previousHyphen {
				return false
			}
			previousHyphen = true
			continue
		}
		if !letter && !digit {
			return false
		}
		previousHyphen = false
	}
	return true
}
