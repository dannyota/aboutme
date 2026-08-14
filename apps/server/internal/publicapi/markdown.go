package publicapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/publicformat"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
)

var ErrUnavailableDependencies = errors.New("public dependencies unavailable")

func publicGetOrHead(w http.ResponseWriter, request *http.Request) bool {
	if request.Method == http.MethodGet || request.Method == http.MethodHead {
		return true
	}
	w.Header().Set("Allow", "GET, HEAD")
	serveTextError(w, request, http.StatusMethodNotAllowed)
	return false
}

func serveTextError(w http.ResponseWriter, request *http.Request, status int) {
	body := map[int]string{http.StatusBadRequest: "Bad request.\n", http.StatusNotFound: "Not found.\n", http.StatusMethodNotAllowed: "Method not allowed.\n", http.StatusServiceUnavailable: "Service temporarily unavailable.\n"}[status]
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if status == http.StatusServiceUnavailable {
		w.Header().Set("Retry-After", "1")
	}
	w.WriteHeader(status)
	if request.Method != http.MethodHead {
		_, _ = w.Write([]byte(body))
	}
}

type MarkdownDependencies struct {
	Reader    *publicresume.Reader
	Cache     *publiccache.Cache
	AppDigest string
}

func NewMarkdownHandler(dependencies MarkdownDependencies) (http.Handler, error) {
	if dependencies.Reader == nil || dependencies.Cache == nil || dependencies.AppDigest == "" {
		return nil, ErrUnavailableDependencies
	}
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if !publicGetOrHead(w, request) {
			return
		}
		slug := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/"), ".md")
		snapshot, lease, err := dependencies.Reader.ReadResume(request.Context(), slug, publicstate.RepresentationMarkdown)
		if err != nil {
			if lease != nil {
				lease.Release()
			}
			if errors.Is(err, publicresume.ErrNotFound) {
				serveTextError(w, request, http.StatusNotFound)
				return
			}
			serveTextError(w, request, http.StatusServiceUnavailable)
			return
		}
		if !snapshot.DiscoveryEnabled {
			lease.Release()
			serveTextError(w, request, http.StatusNotFound)
			return
		}
		defer lease.Release()
		key := publiccache.Key{RouteClass: "resume", Representation: publicstate.RepresentationMarkdown, Variant: "discoverable", ResumeID: snapshot.ResumeID, Generation: snapshot.Revision, FormatVersion: publicformat.MarkdownFormatVersion, AppDigest: dependencies.AppDigest}
		if cached, ok := dependencies.Cache.Get(key); ok {
			SelectedResponse{Status: cached.Status, Header: cached.Header, Body: cached.Body}.ServeHTTP(w, request)
			return
		}
		body, err := publicformat.Markdown(snapshot.Public)
		if err != nil {
			serveTextError(w, request, http.StatusServiceUnavailable)
			return
		}
		response, err := NewSelectedResponse(http.StatusOK, "text/markdown; charset=utf-8", "no-cache, must-revalidate", body, nil)
		if err != nil {
			serveTextError(w, request, http.StatusServiceUnavailable)
			return
		}
		dependencies.Cache.Put(key, publiccache.Value{Status: response.Status, Header: response.Header, Body: response.Body})
		response.ServeHTTP(w, request)
	}), nil
}
