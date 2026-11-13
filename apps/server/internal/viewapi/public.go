package viewapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/netip"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/publicroots"
	"danny.vn/aboutme/apps/server/internal/viewcount"
	"danny.vn/aboutme/apps/server/internal/viewpass"
)

const maxBodyBytes = 4 * 1024

const (
	botLabelHeader        = "X-Amzn-Waf-Aboutme-Bot"
	datacenterLabelHeader = "X-Amzn-Waf-Aboutme-Dc"
)

var hexPattern = regexp.MustCompile(`^[0-9a-f]{2,128}$`)

func (s *Service) preflight(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if strings.Contains(r.URL.EscapedPath(), "%") {
		api.WriteError(w, http.StatusNotFound, "not_found", "no route for this path")
		return nil, false
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		api.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed on this route")
		return nil, false
	}
	origins := r.Header.Values("Origin")
	if len(origins) != 1 || origins[0] != s.publicOrigin {
		api.WriteError(w, http.StatusForbidden, "origin_rejected", "request origin is not allowed")
		return nil, false
	}
	if !jsonMediaType(r.Header) {
		api.WriteError(w, http.StatusUnsupportedMediaType, "media_type_unsupported", "Content-Type must be application/json")
		return nil, false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			api.WriteError(w, http.StatusRequestEntityTooLarge, "body_too_large", "request body exceeds the 4096 byte limit")
			return nil, false
		}
		writeInvalid(w, invalidRequestMessage)
		return nil, false
	}
	return body, true
}

func jsonMediaType(header http.Header) bool {
	values := header.Values("Content-Type")
	if len(values) != 1 {
		return false
	}
	mediaType, params, err := mime.ParseMediaType(values[0])
	if err != nil || mediaType != "application/json" {
		return false
	}
	delete(params, "charset")
	return len(params) == 0
}

const invalidRequestMessage = "request is invalid"

func writeInvalid(w http.ResponseWriter, message string) {
	api.WriteError(w, http.StatusBadRequest, "request_invalid", message)
}

func decodeStrict(body []byte, shape *api.JSONShape, into any) (string, bool) {
	if err := api.CheckJSONShape(body, shape); err != nil {
		var pathErr *api.JSONPathError
		if errors.As(err, &pathErr) {
			return pathErr.Error(), false
		}
		return invalidRequestMessage, false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return invalidRequestMessage, false
	}
	if !errors.Is(decoder.Decode(new(json.RawMessage)), io.EOF) {
		return invalidRequestMessage, false
	}
	return "", true
}

type startResponse struct {
	Owner     bool                 `json:"owner"`
	Token     string               `json:"token,omitempty"`
	Challenge *viewcount.Challenge `json:"challenge,omitempty"`
	SignedIn  bool                 `json:"signedIn"`
}

func (s *Service) handleStart(w http.ResponseWriter, r *http.Request, body []byte) {
	var request struct{}
	if !bytes.HasPrefix(bytes.TrimSpace(body), []byte("{")) {
		writeInvalid(w, (&api.JSONPathError{Problem: "must be an object"}).Error())
		return
	}
	if message, ok := decodeStrict(body, api.JSONShapeOf(reflect.TypeOf(request)), &request); !ok {
		writeInvalid(w, message)
		return
	}
	slug := r.PathValue("slug")
	if !publicroots.ValidSlug(slug) {
		writeNotFound(w)
		return
	}
	resume, err := s.queries.GetLiveViewResumeBySlug(r.Context(), slug)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		writeNotFound(w)
		return
	case err != nil:
		s.writeInternal(w, r)
		return
	}
	if resume.SignInToView && !hasValidPass(r, s.viewPassKey, resume.ID, resume.ViewPassEpoch, s.now) {
		writeNotFound(w)
		return
	}

	result, err := s.counter.Start(viewcount.LiveResume{ID: resume.ID, Owner: resume.UserID}, s.viewer(r))
	if err != nil {
		w.Header().Set("Retry-After", "60")
		api.WriteError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "service temporarily unavailable")
		return
	}
	_, signedIn := auth.SessionFromContext(r.Context())
	response := startResponse{Owner: result.Owner, SignedIn: signedIn}
	if !result.Owner {
		response.Token = result.Token
		response.Challenge = &result.Challenge
	}
	api.WriteData(w, http.StatusOK, response)
}

func hasValidPass(r *http.Request, key []byte, resumeID uuid.UUID, passEpoch int32, clock func() time.Time) bool {
	cookie, err := r.Cookie(viewpass.CookieName)
	if err != nil {
		return false
	}
	now := time.Now
	if clock != nil {
		now = clock
	}
	return viewpass.Valid(cookie.Value, key, resumeID, passEpoch, now())
}

func writeNotFound(w http.ResponseWriter) {
	api.WriteError(w, http.StatusNotFound, "public_not_found", "public resume not found")
}

type collectRequest struct {
	Token     *string              `json:"token"`
	Challenge *viewcount.Challenge `json:"challenge"`
	Solution  *struct {
		Counter    *int    `json:"counter"`
		DerivedKey *string `json:"derivedKey"`
	} `json:"solution"`
}

func (c collectRequest) valid() bool {
	if c.Token == nil || c.Challenge == nil || c.Solution == nil ||
		c.Solution.Counter == nil || c.Solution.DerivedKey == nil {
		return false
	}
	token := *c.Token
	if len(token) < 16 || len(token) > 200 || strings.Trim(token, tokenAlphabet) != "" {
		return false
	}
	counter := *c.Solution.Counter
	return counter >= 0 && counter <= 10_000_000 && hexPattern.MatchString(*c.Solution.DerivedKey)
}

const tokenAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

func (s *Service) handleCollect(w http.ResponseWriter, r *http.Request, body []byte) {
	var request collectRequest
	message, ok := decodeStrict(body, collectShape(), &request)
	if ok && !request.valid() {
		message, ok = invalidRequestMessage, false
	}
	if !ok {
		writeInvalid(w, message)
		return
	}
	outcome := s.counter.Collect(r.Context(), viewcount.CollectInput{
		Token:     *request.Token,
		Challenge: *request.Challenge,
		Solution:  viewcount.Solution{Counter: *request.Solution.Counter, DerivedKey: *request.Solution.DerivedKey},
	}, s.viewer(r), s.edgeLabels(r))
	if s.logger != nil && outcome != viewcount.OutcomeNone {
		s.logger.DebugContext(r.Context(), "viewapi: collect", "outcome", string(outcome))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) viewer(r *http.Request) viewcount.Viewer {
	var viewer viewcount.Viewer
	if raw, ok := api.ClientIP(r, s.trustedProxies); ok {
		if addr, err := netip.ParseAddr(raw); err == nil {
			viewer.Addr = addr
		}
	}
	if sess, ok := auth.SessionFromContext(r.Context()); ok {
		account := sess.UserID
		viewer.Account = &account
	}
	return viewer
}

func (s *Service) edgeLabels(r *http.Request) viewcount.EdgeLabels {
	if !s.trustedProxies.TrustsPeer(r) {
		return viewcount.EdgeLabels{}
	}
	return viewcount.EdgeLabels{
		Bot:        labelSet(r.Header, botLabelHeader),
		Datacenter: labelSet(r.Header, datacenterLabelHeader),
	}
}

func labelSet(header http.Header, name string) bool {
	values := header.Values(name)
	return len(values) == 1 && values[0] == "1"
}
