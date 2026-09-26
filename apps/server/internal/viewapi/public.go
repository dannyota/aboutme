package viewapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/netip"
	"regexp"
	"strings"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/publicroots"
	"github.com/dannyota/aboutme/apps/server/internal/viewcount"
)

// maxBodyBytes bounds start and collect bodies. A collect body with its
// echoed challenge is under 1 KiB.
const maxBodyBytes = 4 * 1024

// Edge label headers. The CloudFront edge's WAF inserts them on the collect
// path, Caddy passes only these two from its CloudFront listener, and Go
// reads them only from a trusted proxy (docs/design/viewer-analytics/
// counting.md, "Layers 2 and 3"). A viewer who sends them itself can only
// move its own report out of the count.
const (
	botLabelHeader        = "X-Amzn-Waf-Aboutme-Bot"
	datacenterLabelHeader = "X-Amzn-Waf-Aboutme-Dc"
)

var hexPattern = regexp.MustCompile(`^[0-9a-f]{2,128}$`)

// preflight rejects a request that is not a same-origin JSON POST with a
// bounded body, before any session or database work, and returns the body.
// The public routes carry no CSRF token because they hold no session
// authority; the exact Origin and JSON media type keep cross-site pages out,
// since the origin never answers a CORS preflight.
func (s *Service) preflight(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	// The edge matches the collect path as sent, and the mux matches it
	// decoded, so an escaped spelling could skip the edge labels. Only the
	// plain spelling is served.
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
		writeInvalid(w)
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

func writeInvalid(w http.ResponseWriter) {
	api.WriteError(w, http.StatusBadRequest, "request_invalid", "request is invalid")
}

// decodeStrict decodes exactly one JSON value with no unknown fields.
func decodeStrict(body []byte, into any) bool {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return false
	}
	return errors.Is(decoder.Decode(new(json.RawMessage)), io.EOF)
}

type startResponse struct {
	Owner     bool                 `json:"owner"`
	Token     string               `json:"token,omitempty"`
	Challenge *viewcount.Challenge `json:"challenge,omitempty"`
}

func (s *Service) handleStart(w http.ResponseWriter, r *http.Request, body []byte) {
	var request struct{}
	if !bytes.HasPrefix(bytes.TrimSpace(body), []byte("{")) || !decodeStrict(body, &request) {
		writeInvalid(w)
		return
	}
	slug := r.PathValue("slug")
	if !publicroots.ValidSlug(slug) {
		writeNotFound(w)
		return
	}
	result, err := s.counter.Start(r.Context(), slug, s.viewer(r))
	switch {
	case errors.Is(err, viewcount.ErrNotFound):
		writeNotFound(w)
		return
	case err != nil:
		w.Header().Set("Retry-After", "60")
		api.WriteError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "service temporarily unavailable")
		return
	}
	response := startResponse{Owner: result.Owner}
	if !result.Owner {
		response.Token = result.Token
		response.Challenge = &result.Challenge
	}
	api.WriteData(w, http.StatusOK, response)
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

// valid checks the shape the OpenAPI ViewCollectRequest schema states.
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

// handleCollect answers 204 for every outcome after the body is valid, so a
// client cannot learn which check rejected it.
func (s *Service) handleCollect(w http.ResponseWriter, r *http.Request, body []byte) {
	var request collectRequest
	if !decodeStrict(body, &request) || !request.valid() {
		writeInvalid(w)
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

// viewer returns the canonical client address and, when OptionalSession
// authenticated one, the account. The account only recognizes the owner.
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

// edgeLabels reads the WAF label headers only when the socket peer is a
// trusted proxy.
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
