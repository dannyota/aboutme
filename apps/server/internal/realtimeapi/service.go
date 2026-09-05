// Package realtimeapi serves authenticated and anonymous revision streams.
package realtimeapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/publicroots"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/realtime"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const (
	heartbeatInterval = 25 * time.Second
	writeTimeout      = 2 * time.Second
	lookupTimeout     = 2 * time.Second
)

type streamQueries interface {
	GetPublicRealtimeResume(context.Context, string) (store.GetPublicRealtimeResumeRow, error)
	GetSessionByID(context.Context, uuid.UUID) (store.Session, error)
}

type streamTicker interface {
	Chan() <-chan time.Time
	Stop()
}

type wallTicker struct {
	*time.Ticker
}

// Chan returns the ticker's delivery channel.
func (t wallTicker) Chan() <-chan time.Time { return t.C }

// Dependencies connects stream admission to the existing session and public fences.
type Dependencies struct {
	Hub            *realtime.Hub
	Store          streamQueries
	Sessions       *auth.SessionManager
	Coordinator    *publicstate.Coordinator
	TrustedProxies api.TrustedProxies
	Clock          func() time.Time
}

// Service serves metadata streams. Writes continue through the resume API.
type Service struct {
	dependencies Dependencies
	newTicker    func(time.Duration) streamTicker
	public       http.Handler
}

// New requires the same coordinator and session store used by ordinary routes.
func New(dependencies Dependencies) (*Service, error) {
	if dependencies.Hub == nil || dependencies.Store == nil || dependencies.Sessions == nil || dependencies.Coordinator == nil {
		return nil, errors.New("realtimeapi: missing dependencies")
	}
	if dependencies.Clock == nil {
		dependencies.Clock = time.Now
	}
	service := &Service{
		dependencies: dependencies,
		newTicker: func(interval time.Duration) streamTicker {
			return wallTicker{Ticker: time.NewTicker(interval)}
		},
	}
	limited := api.RateLimit(api.RateLimiterConfig{
		Requests:       api.DefaultRateLimitRequests,
		Window:         api.DefaultRateLimitWindow,
		TrustedProxies: dependencies.TrustedProxies,
		Clock:          dependencies.Clock,
	})(http.HandlerFunc(service.servePublic))
	service.public = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-transform")
		limited.ServeHTTP(w, r)
	})
	return service, nil
}

// RegisterRoutes installs the authenticated account stream.
func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	owner := auth.RequireSession(s.dependencies.Sessions)(http.HandlerFunc(s.owner))
	mux.Handle("/api/v1/events", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/v1/events" {
			streamError(w, http.StatusNotFound)
			return
		}
		if !streamGet(w, r) {
			return
		}
		owner.ServeHTTP(w, r)
	}))
}

// PublicHandler ignores cookies and admits the current live slug only.
func (s *Service) PublicHandler() http.Handler { return s.public }

func (s *Service) owner(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		streamError(w, http.StatusUnauthorized)
		return
	}
	ip, ok := api.ClientIP(r, s.dependencies.TrustedProxies)
	if !ok {
		streamError(w, http.StatusBadRequest)
		return
	}
	subscription, err := s.dependencies.Hub.Subscribe(realtime.Scope{AccountID: session.UserID, IP: ip})
	if err != nil {
		admissionError(w, err)
		return
	}
	defer subscription.Close()
	validSession := func(ctx context.Context) bool {
		checkCtx, cancel := context.WithTimeout(ctx, lookupTimeout)
		defer cancel()
		current, err := s.dependencies.Store.GetSessionByID(checkCtx, session.ID)
		return err == nil && current.UserID == session.UserID && auth.RequireLiveSession(current, s.dependencies.Clock()) == nil
	}
	s.stream(r.Context(), w, r, subscription, validSession, true)
}

func (s *Service) servePublic(w http.ResponseWriter, r *http.Request) {
	if !streamGet(w, r) {
		return
	}
	const prefix = "/api/v1/live/"
	path := r.URL.EscapedPath()
	if !strings.HasPrefix(path, prefix) || !publicroots.ValidSlug(strings.TrimPrefix(path, prefix)) {
		streamError(w, http.StatusNotFound)
		return
	}
	ip, ok := api.ClientIP(r, s.dependencies.TrustedProxies)
	if !ok {
		streamError(w, http.StatusBadRequest)
		return
	}
	row, lease, err := s.admitPublic(r.Context(), strings.TrimPrefix(path, prefix))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			streamError(w, http.StatusNotFound)
		} else {
			streamError(w, http.StatusServiceUnavailable)
		}
		return
	}
	defer lease.Release()
	subscription, err := s.dependencies.Hub.Subscribe(realtime.Scope{ResumeID: row.ID, IP: ip})
	if err != nil {
		admissionError(w, err)
		return
	}
	defer subscription.Close()
	// The synchronous writer returns before releasing its lease. Its deadline
	// bounds cancellation even if a peer stops reading during a flush.
	//nolint:contextcheck // The lease context derives from r.Context and adds revocation cancellation.
	s.stream(lease.Context(), w, r, subscription, nil, false)
}

func (s *Service) admitPublic(ctx context.Context, slug string) (store.GetPublicRealtimeResumeRow, *publicstate.Lease, error) {
	lookupCtx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	for attempt := 0; attempt < 2; attempt++ {
		row, err := s.dependencies.Store.GetPublicRealtimeResume(lookupCtx, slug)
		if err != nil {
			return row, nil, err
		}
		if row.ID == uuid.Nil || row.Revision <= 0 {
			return row, nil, errors.New("realtimeapi: invalid public state")
		}
		lease, err := s.dependencies.Coordinator.AcquireResume(ctx, row.ID, row.Revision, publicstate.RepresentationSSE)
		if err == nil {
			return row, lease, nil
		}
		var mismatch *publicstate.GenerationMismatchError
		if !errors.As(err, &mismatch) || attempt == 1 {
			return row, nil, err
		}
	}
	return store.GetPublicRealtimeResumeRow{}, nil, publicstate.ErrAdmissionClosed
}

func (s *Service) stream(ctx context.Context, w http.ResponseWriter, r *http.Request, subscription *realtime.Subscription, sessionLive func(context.Context) bool, owner bool) {
	if ctx.Err() != nil {
		streamError(w, http.StatusServiceUnavailable)
		return
	}
	if sessionLive != nil && !sessionLive(ctx) {
		streamError(w, http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	writeFrame := func(frame string) bool {
		if ctx.Err() != nil {
			return false
		}
		if err := controller.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
			return false
		}
		if _, err := fmt.Fprint(w, frame); err != nil {
			return false
		}
		if err := controller.Flush(); err != nil {
			return false
		}
		return controller.SetWriteDeadline(time.Time{}) == nil
	}
	const heartbeat = "event: heartbeat\ndata: {\"version\":1}\n\n"
	if !writeFrame(heartbeat) {
		return
	}
	ticker := s.newTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.Context().Done():
			return
		case <-subscription.Done:
			return
		case change, ok := <-subscription.Events:
			if !ok || ctx.Err() != nil || (sessionLive != nil && !sessionLive(ctx)) {
				return
			}
			// A disconnect wins over queued events; no stale queue is drained
			// after listener loss or eviction.
			select {
			case <-subscription.Done:
				return
			default:
			}
			frame, err := revisionFrame(change, owner)
			if err != nil || !writeFrame(frame) {
				return
			}
		case <-ticker.Chan():
			if sessionLive != nil && !sessionLive(ctx) {
				return
			}
			if !writeFrame(heartbeat) {
				return
			}
		}
	}
}

func revisionFrame(change realtime.Change, owner bool) (string, error) {
	revision := strconv.FormatInt(change.Revision, 10)
	id := revision
	var payload any = struct {
		Version  int    `json:"version"`
		Revision string `json:"revision"`
	}{1, revision}
	if owner {
		id = change.ResumeID.String() + ":" + revision
		payload = struct {
			Version  int       `json:"version"`
			ResumeID uuid.UUID `json:"resume_id"`
			Revision string    `json:"revision"`
			Deleted  bool      `json:"deleted"`
		}{1, change.ResumeID, revision, change.Deleted}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("realtimeapi: encode revision frame: %w", err)
	}
	return "event: revision\nid: " + id + "\ndata: " + string(encoded) + "\n\n", nil
}

func streamGet(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Cache-Control", "no-store, no-transform")
	if r.Method == http.MethodGet {
		return true
	}
	w.Header().Set("Allow", "GET")
	streamError(w, http.StatusMethodNotAllowed)
	return false
}

func admissionError(w http.ResponseWriter, err error) {
	if errors.Is(err, realtime.ErrLimited) {
		streamError(w, http.StatusTooManyRequests)
	} else {
		streamError(w, http.StatusServiceUnavailable)
	}
}

func streamError(w http.ResponseWriter, status int) {
	w.Header().Set("Cache-Control", "no-store, no-transform")
	code, message := "temporarily_unavailable", "service temporarily unavailable"
	switch status {
	case http.StatusNotFound:
		code, message = "public_not_found", "public resume not found"
	case http.StatusUnauthorized:
		code, message = "session_required", "a valid session is required"
	case http.StatusBadRequest:
		code, message = "request_invalid", "request is invalid"
	case http.StatusMethodNotAllowed:
		code, message = "method_not_allowed", "method is not allowed"
	case http.StatusTooManyRequests:
		code, message = "rate_limited", "too many connections"
	}
	if status == http.StatusServiceUnavailable || status == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", "5")
	}
	api.WriteError(w, status, code, message)
}
