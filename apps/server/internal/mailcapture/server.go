package mailcapture

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/authmail"
)

// Fixed loopback-only addresses (D7).
const (
	NativeAddr = "127.0.0.1:20091"
	HTTPSAddr  = "127.0.0.1:20444"
)

const (
	// bearerPrefix is the only accepted Authorization scheme.
	bearerPrefix = "Bearer "
	// viewerLimit caps the rendered HTML page so a full 256 KiB store stays
	// bounded and readable.
	viewerLimit = MaxMessages
	// bodyReadLimit is one byte above the per-message cap so a 413 can be
	// returned without buffering unbounded input.
	bodyReadLimit = MaxMessageBytes + 1
)

// Server is the authenticated loopback capture server. It binds loopback only
// (callers listen on NativeAddr or HTTPSAddr), requires a 32-byte bearer secret
// on every route, escapes every value in its human viewer, and never logs the
// secret.
type Server struct {
	store  *Store
	secret [32]byte
	logger *slog.Logger
}

// NewServer validates the 32-byte secret and logger and returns a Server with a
// fresh in-memory store (a restart resets capture state, D7).
func NewServer(secret []byte, logger *slog.Logger) (*Server, error) {
	if len(secret) != 32 {
		return nil, ErrSecret
	}
	if logger == nil {
		return nil, ErrConfig
	}
	s := &Server{store: NewStore(), logger: logger}
	copy(s.secret[:], secret)
	return s, nil
}

// Handler returns the fully-authenticated route mux.
//
//	POST   /capture        accept one message
//	GET    /api/messages   closed JSON snapshot (newest first)
//	DELETE /api/messages   reset the store
//	GET    /               human HTML viewer (all values escaped)
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/capture", s.handleCapture)
	mux.HandleFunc("/api/messages", s.handleMessages)
	mux.HandleFunc("/", s.handleViewer)
	return s.requireSecretAndLoopback(mux)
}

// Client is an authmail.Sender that POSTs messages to a local capture server
// over authenticated loopback HTTP. It is used by development wiring, never in
// production config, which rejects the capture sender outright (D7).
type Client struct {
	baseURL string
	secret  [32]byte
	http    *http.Client
	logger  *slog.Logger
}

// NewClient validates the loopback base URL, the 32-byte secret, and the
// logger, returning a Sender for the capture server.
func NewClient(baseURL string, secret []byte, logger *slog.Logger) (*Client, error) {
	if len(secret) != 32 {
		return nil, ErrSecret
	}
	if logger == nil {
		return nil, ErrConfig
	}
	u, err := parseLoopbackBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	c := &Client{baseURL: u, http: &http.Client{}, logger: logger}
	copy(c.secret[:], secret)
	return c, nil
}

// Send POSTs one message to POST /capture and classifies the HTTP result.
// Transport failures are ambiguous and therefore temporary; a 413/400 is a
// permanent rejection; anything else is temporary. No message field or secret
// ever appears in a returned error or log line.
func (c *Client) Send(ctx context.Context, m authmail.Message) (authmail.SendResult, error) {
	if messageSize(m) > MaxMessageBytes {
		return authmail.SendResult{Outcome: authmail.SendPermanentFailure}, nil
	}
	body, err := json.Marshal(m)
	if err != nil {
		return authmail.SendResult{Outcome: authmail.SendPermanentFailure}, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/capture", bytes.NewReader(body))
	if err != nil {
		return authmail.SendResult{Outcome: authmail.SendTemporaryFailure}, nil
	}
	req.Header.Set("Authorization", bearerPrefix+base64.RawURLEncoding.EncodeToString(c.secret[:]))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		c.logger.Warn("mailcapture: client transport failure")
		return authmail.SendResult{Outcome: authmail.SendTemporaryFailure}, nil
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			c.logger.Warn("mailcapture: client close response body failure")
		}
	}()
	if _, copyErr := io.Copy(io.Discard, io.LimitReader(resp.Body, 4096)); copyErr != nil {
		c.logger.Warn("mailcapture: client drain response body failure")
	}

	switch resp.StatusCode {
	case http.StatusAccepted:
		return authmail.SendResult{Outcome: authmail.SendAccepted}, nil
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge:
		return authmail.SendResult{Outcome: authmail.SendPermanentFailure}, nil
	default:
		return authmail.SendResult{Outcome: authmail.SendTemporaryFailure}, nil
	}
}

func (s *Server) handleCapture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, bodyReadLimit))
	if err != nil {
		http.Error(w, "read failed", http.StatusBadRequest)
		return
	}
	if len(body) > MaxMessageBytes {
		http.Error(w, "message too large", http.StatusRequestEntityTooLarge)
		return
	}
	var m authmail.Message
	if err := decodeStrictJSON(body, &m); err != nil {
		http.Error(w, "invalid message", http.StatusBadRequest)
		return
	}
	if err := validateMessage(m); err != nil {
		http.Error(w, "invalid message", http.StatusBadRequest)
		return
	}
	if _, err := s.store.Add(m); err != nil {
		if errors.Is(err, ErrOversize) {
			http.Error(w, "message too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "store failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	if _, err := w.Write([]byte(`{"accepted":true}`)); err != nil {
		return
	}
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		msgs := s.store.Messages()
		if msgs == nil {
			msgs = []StoredMessage{}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string][]StoredMessage{"messages": msgs}); err != nil {
			s.logger.Error("mailcapture: encode messages response", "error", err)
		}
	case http.MethodDelete:
		s.store.Reset()
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleViewer(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	msgs := s.store.Messages()
	if len(msgs) > viewerLimit {
		msgs = msgs[:viewerLimit]
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := io.WriteString(w, "<!doctype html><meta charset=\"utf-8\"><title>Aboutme auth email capture</title><style>body{font:14px/1.4 system-ui,sans-serif;margin:2rem;max-width:64rem}pre{white-space:pre-wrap;background:#f6f6f6;padding:.5rem}li{margin-bottom:1rem}</style>"); err != nil {
		return
	}
	if _, err := io.WriteString(w, "<h1>Auth email capture</h1>"); err != nil {
		return
	}
	if len(msgs) == 0 {
		if _, err := io.WriteString(w, "<p>No messages captured.</p>"); err != nil {
			return
		}
		return
	}
	if _, err := io.WriteString(w, "<ol>"); err != nil {
		return
	}
	for _, m := range msgs {
		if _, err := fmt.Fprintf(w, "<li><strong>%s</strong> — %s — to %s<br>",
			html.EscapeString(m.ReceivedAt.UTC().Format(time.RFC3339)),
			html.EscapeString(string(m.Kind)),
			html.EscapeString(m.To),
		); err != nil {
			return
		}
		if _, err := fmt.Fprintf(w, "<h2>%s</h2>", html.EscapeString(m.Subject)); err != nil {
			return
		}
		if m.TextBody != "" {
			if _, err := fmt.Fprintf(w, "<pre>%s</pre>", html.EscapeString(m.TextBody)); err != nil {
				return
			}
		}
		if m.HTMLBody != "" {
			if _, err := fmt.Fprintf(w, "<pre>%s</pre>", html.EscapeString(m.HTMLBody)); err != nil {
				return
			}
		}
		if _, err := io.WriteString(w, "</li>"); err != nil {
			return
		}
	}
	if _, err := io.WriteString(w, "</ol>"); err != nil {
		return
	}
}

// requireSecretAndLoopback rejects every request that is not loopback-origin or
// does not carry the exact 32-byte bearer secret (constant-time comparison).
func (s *Server) requireSecretAndLoopback(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopback(r.RemoteAddr) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if !s.authorized(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="capture"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authorized(r *http.Request) bool {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), bearerPrefix)
	if !ok || token == "" {
		return false
	}
	secret, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(secret) != 32 {
		return false
	}
	return subtle.ConstantTimeCompare(secret, s.secret[:]) == 1
}

func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// decodeStrictJSON parses exactly one JSON object with no duplicate or unknown
// fields, no trailing content, and exact string scalar types.
func decodeStrictJSON(data []byte, out *authmail.Message) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("mailcapture: not an object")
	}

	seen := make(map[string]bool, 5)
	for dec.More() {
		keyTok, keyErr := dec.Token()
		if keyErr != nil {
			return keyErr
		}
		key, ok := keyTok.(string)
		if !ok {
			return fmt.Errorf("mailcapture: invalid field name")
		}
		if seen[key] {
			return fmt.Errorf("mailcapture: duplicate field %q", key)
		}
		seen[key] = true

		var value string
		if decodeErr := dec.Decode(&value); decodeErr != nil {
			return fmt.Errorf("mailcapture: invalid %q value", key)
		}
		switch key {
		case "kind":
			out.Kind = authmail.Kind(value)
		case "to":
			out.To = value
		case "subject":
			out.Subject = value
		case "text_body":
			out.TextBody = value
		case "html_body":
			out.HTMLBody = value
		default:
			return fmt.Errorf("mailcapture: unknown field %q", key)
		}
	}

	tok, err = dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '}' {
		return fmt.Errorf("mailcapture: invalid object")
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("mailcapture: trailing content")
	}
	if !seen["kind"] || !seen["to"] {
		return fmt.Errorf("mailcapture: missing required fields")
	}
	return nil
}

// validateMessage enforces the closed field set: a known kind and a non-empty,
// structurally valid recipient. Subjects and bodies may be empty; the size cap
// is enforced separately by the Store.
func validateMessage(m authmail.Message) error {
	switch m.Kind {
	case authmail.KindVerify, authmail.KindReset, authmail.KindPasswordChanged:
	default:
		return fmt.Errorf("mailcapture: invalid kind")
	}
	if m.To == "" {
		return fmt.Errorf("mailcapture: invalid recipient")
	}
	return nil
}

// parseLoopbackBaseURL validates that baseURL is an http/https URL whose host
// is loopback, returning it normalized without a trailing slash.
func parseLoopbackBaseURL(baseURL string) (string, error) {
	if baseURL == "" {
		return "", ErrConfig
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", ErrConfig
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", ErrConfig
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return "", ErrConfig
	}
	return strings.TrimRight(baseURL, "/"), nil
}
