package mailcapture

import (
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
	"strings"
	"time"

	"danny.vn/aboutme/apps/server/internal/authmail"
)

// Fixed loopback-only addresses.
const (
	// NativeAddr is the default loopback listen address for native development.
	NativeAddr = "127.0.0.1:20091"
	// HTTPSAddr is the loopback listen address for the HTTPS development stack.
	HTTPSAddr = "127.0.0.1:20444"
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
// fresh in-memory store (a restart resets capture state).
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
