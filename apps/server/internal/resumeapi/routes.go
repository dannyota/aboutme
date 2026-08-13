package resumeapi

import (
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const apiResumePath = "/api/v1/resumes"

var defaultPhotoKeyInvariantFailures atomic.Uint64
var defaultPhotoNormalizationNanoseconds atomic.Uint64

// Options configures the resume API route service.
type Options struct {
	Logger                     *slog.Logger
	SessionManager             *auth.SessionManager
	PublicOrigin               string
	TrustedProxies             api.TrustedProxies
	Clock                      func() time.Time
	AcceptedVersions           []int32
	PhotoKeyInvariant          func()
	PhotoRandom                io.Reader
	PhotoNormalizationDuration func(time.Duration)
}

// Service owns the authenticated resume HTTP surface and its write-safety
// dependencies.
type Service struct {
	resumes                    resumeBoundary
	idempotency                idempotencyBoundary
	projector                  *docmigrate.Projector
	blobs                      media.Backend
	logger                     *slog.Logger
	sessions                   *auth.SessionManager
	publicOrigin               string
	trustedProxies             api.TrustedProxies
	clock                      func() time.Time
	acceptedVersions           []int32
	writeResponse              func(http.ResponseWriter, resume.StoredResponse)
	sanitizeDocument           func(schema.Resume) schema.Resume
	photoKeyInvariant          func()
	photoRandom                io.Reader
	photoNormalizationDuration func(time.Duration)
}

type resumeBoundary interface {
	CreateTx(context.Context, *store.Queries, uuid.UUID, string, *string, schema.Resume) (resume.Resume, error)
	GetTx(context.Context, *store.Queries, uuid.UUID, uuid.UUID) (resume.Resume, error)
	SaveDocumentTx(context.Context, *store.Queries, uuid.UUID, uuid.UUID, schema.Resume, int64) (int64, error)
	SaveMetadataAndDocumentTx(context.Context, *store.Queries, uuid.UUID, uuid.UUID, string, *string, schema.Resume, int64) (int64, error)
	DeleteTx(context.Context, *store.Queries, uuid.UUID, uuid.UUID, int64) (resume.Resume, error)
}

type idempotencyBoundary interface {
	Inspect(context.Context, uuid.UUID, string, uuid.UUID, [32]byte) (resume.StoredResponse, bool, error)
	Execute(context.Context, uuid.UUID, string, uuid.UUID, [32]byte,
		func(*store.Queries) (resume.StoredResponse, error)) (resume.ExecuteResult, error)
}

// New constructs the resume API service. RegisterRoutes installs its routes.
func New(store *resume.Store, idem *resume.IdempotencyStore, proj *docmigrate.Projector,
	blobs media.Backend, opts Options,
) *Service {
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.AcceptedVersions == nil {
		opts.AcceptedVersions = docmigrate.AcceptedVersions()
	}
	if opts.PhotoRandom == nil {
		opts.PhotoRandom = rand.Reader
	}
	if opts.PhotoKeyInvariant == nil {
		opts.PhotoKeyInvariant = func() { defaultPhotoKeyInvariantFailures.Add(1) }
	}
	if opts.PhotoNormalizationDuration == nil {
		opts.PhotoNormalizationDuration = func(elapsed time.Duration) {
			defaultPhotoNormalizationNanoseconds.Add(uint64(max(elapsed.Nanoseconds(), 0)))
		}
	}
	return &Service{
		resumes: store, idempotency: idem, projector: proj, blobs: blobs,
		logger: opts.Logger, sessions: opts.SessionManager,
		publicOrigin: opts.PublicOrigin, trustedProxies: opts.TrustedProxies,
		clock: opts.Clock, acceptedVersions: append([]int32(nil), opts.AcceptedVersions...),
		writeResponse: writeStoredResponse, sanitizeDocument: sanitizeDocument,
		photoKeyInvariant:          opts.PhotoKeyInvariant,
		photoRandom:                opts.PhotoRandom,
		photoNormalizationDuration: opts.PhotoNormalizationDuration,
	}
}

func (s *Service) recordPhotoNormalizationDuration(elapsed time.Duration) {
	if s.photoNormalizationDuration != nil {
		s.photoNormalizationDuration(elapsed)
	}
}

func (s *Service) recordPhotoKeyInvariant(ctx context.Context) {
	if s.photoKeyInvariant != nil {
		s.photoKeyInvariant()
	}
	if s.logger != nil {
		s.logger.ErrorContext(ctx, "resume photo key invariant failed",
			"request_id", api.RequestIDFromContext(ctx))
	}
}

type routeSpec struct {
	Method             string
	Pattern            string
	Operation          string
	Mutation           bool
	Upload             bool
	OperationKind      operationKind
	AcceptsWireVersion bool
	EmitsWireVersion   bool
	Handler            func(*Service, http.ResponseWriter, *http.Request)
}

func registeredRoutes() []routeSpec {
	var routes []routeSpec
	routes = append(routes, resumeRoutes()...)
	routes = append(routes, entryRoutes()...)
	routes = append(routes, sectionRoutes()...)
	routes = append(routes, structureRoutes()...)
	routes = append(routes, personalDetailsRoutes()...)
	routes = append(routes, customizationRoutes()...)
	routes = append(routes, photoRoutes()...)
	return routes
}

// RegisterRoutes attaches the whole P2B route inventory in one step.
func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	chains := s.newRouteChains()
	byPattern := make(map[string][]routeSpec)
	for _, route := range registeredRoutes() {
		byPattern[route.Pattern] = append(byPattern[route.Pattern], route)
	}
	patterns := make([]string, 0, len(byPattern))
	for pattern := range byPattern {
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)
	for _, pattern := range patterns {
		routes := byPattern[pattern]
		mux.Handle(pattern, s.dispatch(routes, chains))
	}
}

func (s *Service) dispatch(routes []routeSpec, chains routeChains) http.Handler {
	handlers := make(map[string]http.Handler, len(routes))
	allowed := make([]string, 0, len(routes))
	for _, route := range routes {
		route := route
		base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route.Handler(s, w, r)
		})
		handlers[route.Method] = chains.wrap(route, base)
		allowed = append(allowed, route.Method)
	}
	sort.Strings(allowed)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.Method
		if method == http.MethodHead {
			method = http.MethodGet
		}
		if handler, ok := handlers[method]; ok {
			handler.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		api.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed on this route")
	})
}
