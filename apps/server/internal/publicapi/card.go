package publicapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/previewcard"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/renderjob"
)

type PreviewCards interface {
	Version(publicresume.Snapshot) (string, error)
	Stored(context.Context, uuid.UUID) (previewcard.StoredCard, bool, error)
	Build(context.Context, publicresume.Snapshot, renderjob.Priority) ([]byte, error)
}

var _ PreviewCards = (*previewcard.Service)(nil)

const (
	cardPathPrefix = "/api/v1/public/resumes/"
	cardPathInfix  = "/og/"
	cardPathSuffix = ".png"
)

func cardPathParts(path string) (slug, version string, ok bool) {
	rest, found := strings.CutPrefix(path, cardPathPrefix)
	if !found {
		return "", "", false
	}
	slug, file, found := strings.Cut(rest, cardPathInfix)
	if !found || !validPublicSlug(slug) {
		return "", "", false
	}
	version, found = strings.CutSuffix(file, cardPathSuffix)
	if !found || !previewcard.ValidVersion(version) {
		return "", "", false
	}
	return slug, version, true
}

func cardRequestParts(path string, alias bool) (slug, version string, ok bool) {
	if alias {
		return artifactSlug(path, "/og.png"), "", true
	}
	return cardPathParts(path)
}

func (s *artifactService) cardHandler(cards PreviewCards, alias bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if !validateArtifactRequest(w, request) {
			return
		}
		slug, requested, ok := cardRequestParts(request.URL.EscapedPath(), alias)
		if !ok {
			servePublicJSONError(w, request, http.StatusNotFound)
			return
		}
		clientIP, admitted := api.ClientIP(request, s.dependencies.TrustedProxies)
		if !admitted {
			servePublicJSONError(w, request, http.StatusBadRequest)
			return
		}
		if allowed, retry := s.requests.Admit(s.dependencies.Clock(), clientIP); !allowed {
			servePublicRateError(w, request, retry)
			return
		}

		snapshot, lease, err := s.dependencies.Reader.ReadResume(request.Context(), slug, publicstate.RepresentationPNG)
		if err != nil {
			if lease != nil {
				lease.Release()
			}
			serveArtifactReadError(w, request, err)
			return
		}
		defer releaseAfterFlush(w, lease)
		if _, _, valid := parseIfNoneMatch(request.Header); !valid {
			serveConditionalError(w, request)
			return
		}
		version, err := cards.Version(snapshot)
		if err != nil {
			servePublicJSONError(w, request, http.StatusServiceUnavailable)
			return
		}
		if !alias && requested != version {
			servePublicJSONError(w, request, http.StatusNotFound)
			return
		}

		//nolint:contextcheck // The lease context derives from request.Context and adds revocation cancellation.
		stored, found, err := cards.Stored(lease.Context(), snapshot.ResumeID)
		if serveCanceledLease(w, request, lease) {
			return
		}
		if err != nil {
			servePublicJSONError(w, request, http.StatusServiceUnavailable)
			return
		}
		png := stored.PNG
		if !found || stored.Version != version {
			if allowed, retry := s.renders.Admit(s.dependencies.Clock(), clientIP); !allowed {
				servePublicRateError(w, request, retry)
				return
			}
			//nolint:contextcheck // The lease context derives from request.Context and adds revocation cancellation.
			png, err = buildCard(lease.Context(), cards, snapshot)
			if serveCanceledLease(w, request, lease) {
				return
			}
			if err != nil {
				servePublicJSONError(w, request, http.StatusServiceUnavailable)
				return
			}
		}
		response, err := newSelectedResponseWithLimit(
			http.StatusOK, "image/png", "no-cache, must-revalidate", png, make(http.Header), previewcard.MaxPNGBytes,
		)
		if err != nil {
			servePublicJSONError(w, request, http.StatusServiceUnavailable)
			return
		}
		serveLeasedArtifact(w, request, lease, withDiscoveryRobots(response, snapshot.DiscoveryEnabled))
	})
}

func buildCard(ctx context.Context, cards PreviewCards, snapshot publicresume.Snapshot) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, renderjob.MaxJobTimeout)
	defer cancel()
	png, err := cards.Build(ctx, snapshot, renderjob.PriorityNormal)
	if err != nil && !errors.Is(err, previewcard.ErrStale) {
		return nil, err
	}
	if len(png) == 0 || len(png) > previewcard.MaxPNGBytes {
		return nil, previewcard.ErrRender
	}
	return png, nil
}
