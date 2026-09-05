package publicapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/printsnapshot"
	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
)

const (
	publicArtifactRequestsPerMinute = 300
	publicRenderRequestsPerMinute   = 20
	publicPDFFormatVersion          = 1
	publicPNGFormatVersion          = 1
)

// RenderQueue is the public artifact boundary to the shared print queue.
type RenderQueue interface {
	Render(context.Context, renderjob.Request) (renderjob.Result, error)
}

// ArtifactDependencies contains the dependencies shared by PDF and PNG.
type ArtifactDependencies struct {
	Reader         *publicresume.Reader
	Cache          *publiccache.Cache
	Queue          RenderQueue
	AppDigest      string
	RendererDigest string
	TrustedProxies api.TrustedProxies
	Clock          func() time.Time
}

type artifactHandlers struct {
	pdf http.Handler
	png http.Handler
}

type artifactService struct {
	dependencies ArtifactDependencies
	requests     *api.BoundedRateLimiter
	renders      *api.BoundedRateLimiter
}

type artifactContract struct {
	format         renderjob.Format
	representation publicstate.Representation
	suffix         string
	variant        publiccache.Variant
	formatVersion  int
	contentType    string
	disposition    string
	maxBytes       int
}

func newArtifactHandlers(dependencies ArtifactDependencies) (*artifactHandlers, error) {
	if dependencies.Reader == nil || dependencies.Cache == nil ||
		dependencies.AppDigest == "" || dependencies.RendererDigest == "" {
		return nil, ErrUnavailableDependencies
	}
	if dependencies.Clock == nil {
		dependencies.Clock = time.Now
	}
	service := &artifactService{
		dependencies: dependencies,
		requests: api.NewBoundedRateLimiter(api.RateLimiterConfig{
			Requests: publicArtifactRequestsPerMinute, Window: time.Minute,
		}),
		renders: api.NewBoundedRateLimiter(api.RateLimiterConfig{
			Requests: publicRenderRequestsPerMinute, Window: time.Minute,
		}),
	}
	return &artifactHandlers{
		pdf: service.handler(artifactContract{
			format: renderjob.PDF, representation: publicstate.RepresentationPDF,
			suffix: "/pdf", variant: "default", formatVersion: publicPDFFormatVersion,
			contentType: "application/pdf", disposition: "attachment; filename=\"resume.pdf\"",
			maxBytes: renderjob.PDFMaxBytes,
		}),
		png: service.handler(artifactContract{
			format: renderjob.PNG, representation: publicstate.RepresentationPNG,
			suffix: "/og.png", variant: "1200x630", formatVersion: publicPNGFormatVersion,
			contentType: "image/png", maxBytes: renderjob.PNGMaxBytes,
		}),
	}, nil
}

func (s *artifactService) handler(contract artifactContract) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if !validateArtifactRequest(w, request) {
			return
		}
		clientIP, ok := api.ClientIP(request, s.dependencies.TrustedProxies)
		if !ok {
			servePublicJSONError(w, request, http.StatusBadRequest)
			return
		}
		if allowed, retry := s.requests.Admit(s.dependencies.Clock(), clientIP); !allowed {
			servePublicRateError(w, request, retry)
			return
		}

		slug := artifactSlug(request.URL.EscapedPath(), contract.suffix)
		snapshot, lease, err := s.dependencies.Reader.ReadResume(request.Context(), slug, contract.representation)
		if err != nil {
			if lease != nil {
				lease.Release()
			}
			serveArtifactReadError(w, request, err)
			return
		}
		defer lease.Release()
		if _, _, valid := parseIfNoneMatch(request.Header); !valid {
			serveConditionalError(w, request)
			return
		}

		key := publiccache.Key{
			RouteClass: "resume", Representation: contract.representation, Variant: contract.variant,
			ResumeID: snapshot.ResumeID, Generation: snapshot.Revision, FormatVersion: contract.formatVersion,
			AppDigest: s.dependencies.AppDigest, RendererDigest: s.dependencies.RendererDigest,
		}
		if cached, found := s.dependencies.Cache.Get(key); found {
			if serveCanceledLease(w, request, lease) {
				return
			}
			serveLeasedArtifact(w, request, lease, SelectedResponse{Status: cached.Status, Header: cached.Header, Body: cached.Body})
			return
		}
		if allowed, retry := s.renders.Admit(s.dependencies.Clock(), clientIP); !allowed {
			servePublicRateError(w, request, retry)
			return
		}
		if s.dependencies.Queue == nil {
			servePublicJSONError(w, request, http.StatusServiceUnavailable)
			return
		}

		//nolint:contextcheck // The lease context derives from request.Context and adds revocation cancellation.
		result, err := s.dependencies.Queue.Render(lease.Context(), renderjob.Request{
			Format: contract.format,
			Prepare: func(ctx context.Context) (renderjob.Snapshot, error) {
				var photo []byte
				var contentType string
				if snapshot.Public.Document.PersonalDetails.Photo != nil {
					var readErr error
					photo, contentType, readErr = s.dependencies.Reader.ReadPhoto(ctx, snapshot)
					if readErr != nil {
						return renderjob.Snapshot{}, readErr
					}
				}
				envelope, snapshotErr := printsnapshot.FromPublic(snapshot, photo, contentType)
				if snapshotErr != nil {
					return renderjob.Snapshot{}, snapshotErr
				}
				payload, marshalErr := printsnapshot.Marshal(envelope)
				if marshalErr != nil {
					return renderjob.Snapshot{}, marshalErr
				}
				return renderjob.Snapshot{
					ResumeID: snapshot.ResumeID, Revision: snapshot.Revision,
					SchemaVersion:    int(snapshot.Public.Document.SchemaVersion),
					PublicGeneration: snapshot.Revision, Payload: payload,
				}, nil
			},
			ValidateGeneration: func(ctx context.Context, frozen renderjob.Snapshot) error {
				current, validationLease, readErr := s.dependencies.Reader.ReadResume(ctx, slug, contract.representation)
				if validationLease != nil {
					defer validationLease.Release()
				}
				if readErr != nil || current.ResumeID != frozen.ResumeID || current.Revision != frozen.PublicGeneration {
					return renderjob.ErrGenerationChanged
				}
				return nil
			},
		})
		if serveCanceledLease(w, request, lease) {
			return
		}
		if err != nil || result.Revision != snapshot.Revision ||
			len(result.Bytes) == 0 || len(result.Bytes) > contract.maxBytes {
			servePublicJSONError(w, request, http.StatusServiceUnavailable)
			return
		}
		extra := make(http.Header)
		if contract.disposition != "" {
			extra.Set("Content-Disposition", contract.disposition)
		}
		response, err := newSelectedResponseWithLimit(
			http.StatusOK, contract.contentType, "no-cache, must-revalidate", result.Bytes, extra, contract.maxBytes,
		)
		if serveCanceledLease(w, request, lease) {
			return
		}
		if err != nil {
			servePublicJSONError(w, request, http.StatusServiceUnavailable)
			return
		}
		s.dependencies.Cache.Put(key, publiccache.Value{Status: response.Status, Header: response.Header, Body: response.Body})
		if serveCanceledLease(w, request, lease) {
			return
		}
		serveLeasedArtifact(w, request, lease, response)
	})
}

func validateArtifactRequest(w http.ResponseWriter, request *http.Request) bool {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		servePublicJSONError(w, request, http.StatusMethodNotAllowed)
		return false
	}
	if request.URL.RawQuery != "" || request.URL.ForceQuery ||
		len(request.Header.Values("Content-Encoding")) != 0 ||
		request.ContentLength != 0 || len(request.TransferEncoding) != 0 {
		servePublicJSONError(w, request, http.StatusBadRequest)
		return false
	}
	return true
}

func artifactSlug(path, suffix string) string {
	const prefix = "/api/v1/public/resumes/"
	return path[len(prefix) : len(path)-len(suffix)]
}

func serveArtifactReadError(w http.ResponseWriter, request *http.Request, err error) {
	if errors.Is(err, publicresume.ErrNotFound) {
		servePublicJSONError(w, request, http.StatusNotFound)
		return
	}
	servePublicJSONError(w, request, http.StatusServiceUnavailable)
}

func servePublicRateError(w http.ResponseWriter, request *http.Request, retry int) {
	w.Header().Set("Retry-After", strconv.Itoa(retry))
	body := []byte("{\"error\":{\"code\":\"rate_limited\",\"message\":\"too many requests; retry later\"}}\n")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusTooManyRequests)
	if request.Method != http.MethodHead {
		_, _ = w.Write(body) //nolint:errcheck // The response is committed; a replacement status or retry cannot recover it.
	}
}

func serveLeasedArtifact(w http.ResponseWriter, request *http.Request, lease *publicstate.Lease, response SelectedResponse) {
	if serveCanceledLease(w, request, lease) {
		return
	}
	tag, present, valid := parseIfNoneMatch(request.Header)
	if !valid {
		serveConditionalError(w, request)
		return
	}
	header := response.Header.Clone()
	responseHeader := w.Header()
	if serveCanceledLease(w, request, lease) {
		return
	}
	if present && tag == header.Get("ETag") {
		header.Del("Content-Length")
		writeArtifactHeader(responseHeader, header)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if request.Method == http.MethodHead {
		writeArtifactHeader(responseHeader, header)
		w.WriteHeader(response.Status)
		return
	}

	controller := http.NewResponseController(w)
	stop := make(chan struct{})
	armed := make(chan struct{})
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		close(armed)
		select {
		case <-lease.Context().Done():
			// Unsupported deadline cancellation leaves the handler and lease active so the mutation drain times out.
			_ = controller.SetWriteDeadline(time.Now()) //nolint:errcheck // There is no fallback that can interrupt this committed write.
		case <-stop:
		}
	}()
	<-armed
	writeArtifactHeader(responseHeader, header)
	w.WriteHeader(response.Status)
	_, _ = w.Write(response.Body) //nolint:errcheck // The response is committed; cancellation and cleanup still require the join below.
	close(stop)
	<-joined
}

func writeArtifactHeader(destination, source http.Header) {
	for key, values := range source {
		for _, value := range values {
			destination.Set(key, value)
		}
	}
}

func serveCanceledLease(w http.ResponseWriter, request *http.Request, lease *publicstate.Lease) bool {
	if lease.Context().Err() == nil {
		return false
	}
	if request.Context().Err() == nil {
		servePublicJSONError(w, request, http.StatusServiceUnavailable)
	}
	return true
}
