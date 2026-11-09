package mailcapture

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/dannyota/aboutme/apps/server/internal/authmail"
)

// Client is an authmail.Sender that POSTs messages to a local capture server
// over authenticated loopback HTTP. It is used by development wiring, never in
// production config, which rejects the capture sender outright.
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
