package config

// Password-authentication email configuration (Phase PA T09). Every value is
// validated at load time; secrets are base64url-decoded exactly once and never
// echoed in an error. Capture and SES are exclusive modes, and capture is
// permitted only in development so production can never route mail to a local
// loopback sink.

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/dannyota/aboutme/apps/server/internal/accountemail"
)

const (
	authEmailModeSES     = "ses"
	authEmailModeCapture = "capture"
	// requiredSESRegion is the exact region SES mode requires (D7).
	requiredSESRegion = "ap-southeast-1"
)

// AuthEmailConfig holds the validated password-mail configuration. The two key
// fields are 32-byte arrays so a decoded secret can never be a string that
// accidentally reaches a log or error.
type AuthEmailConfig struct {
	RateHMACKey   [32]byte
	ActiveKeyID   string
	ActiveKey     [32]byte
	PreviousKeyID string
	PreviousKey   [32]byte
	HasPrevious   bool
	Mode          string
	CaptureURL    string
	CaptureBearer [32]byte
	SESFrom       string
	SESConfigSet  string
	SESRegion     string
}

// loadAuthEmailConfig reads and validates the password-mail configuration. It
// rejects a missing or malformed rate key, active key, a previous key that is
// only half-set or duplicates the active ID, an unknown mode, a capture mode in
// a non-dev environment, a non-loopback capture URL, a malformed capture
// bearer, a noncanonical SES From address, a non-AWS-safe configuration set,
// and any SES/capture field set in the wrong mode.
func loadAuthEmailConfig(getenv func(string) string, environment string) (AuthEmailConfig, error) {
	var cfg AuthEmailConfig

	rateKey, err := decodeBase64URL32(getenv("PASSWORD_RATE_HMAC_KEY"))
	if err != nil {
		return cfg, errors.New("config: PASSWORD_RATE_HMAC_KEY must be base64url encoding exactly 32 bytes")
	}
	cfg.RateHMACKey = rateKey

	activeID := strings.TrimSpace(getenv("AUTH_EMAIL_ACTIVE_KEY_ID"))
	if !isPrintableASCII(activeID) {
		return cfg, errors.New("config: AUTH_EMAIL_ACTIVE_KEY_ID must be 1-64 printable ASCII")
	}
	activeKey, err := decodeBase64URL32(getenv("AUTH_EMAIL_ACTIVE_KEY"))
	if err != nil {
		return cfg, errors.New("config: AUTH_EMAIL_ACTIVE_KEY must be base64url encoding exactly 32 bytes")
	}
	cfg.ActiveKeyID = activeID
	cfg.ActiveKey = activeKey

	prevID := strings.TrimSpace(getenv("AUTH_EMAIL_PREVIOUS_KEY_ID"))
	prevKeyRaw := strings.TrimSpace(getenv("AUTH_EMAIL_PREVIOUS_KEY"))
	if (prevID == "") != (prevKeyRaw == "") {
		return cfg, errors.New("config: AUTH_EMAIL_PREVIOUS_KEY_ID and AUTH_EMAIL_PREVIOUS_KEY must be set together")
	}
	if prevID != "" {
		if !isPrintableASCII(prevID) {
			return cfg, errors.New("config: AUTH_EMAIL_PREVIOUS_KEY_ID must be 1-64 printable ASCII")
		}
		if prevID == activeID {
			return cfg, errors.New("config: AUTH_EMAIL_PREVIOUS_KEY_ID must differ from AUTH_EMAIL_ACTIVE_KEY_ID")
		}
		prevKey, decErr := decodeBase64URL32(prevKeyRaw)
		if decErr != nil {
			return cfg, errors.New("config: AUTH_EMAIL_PREVIOUS_KEY must be base64url encoding exactly 32 bytes")
		}
		cfg.PreviousKeyID = prevID
		cfg.PreviousKey = prevKey
		cfg.HasPrevious = true
	}

	mode := strings.ToLower(strings.TrimSpace(getenv("AUTH_EMAIL_MODE")))
	switch mode {
	case authEmailModeSES, authEmailModeCapture:
		cfg.Mode = mode
	default:
		return cfg, errors.New("config: AUTH_EMAIL_MODE must be ses or capture")
	}

	captureURL := strings.TrimSpace(getenv("AUTH_EMAIL_CAPTURE_URL"))
	captureBearerRaw := strings.TrimSpace(getenv("AUTH_EMAIL_CAPTURE_BEARER"))
	sesFrom := strings.TrimSpace(getenv("SES_FROM_ADDRESS"))
	sesConfigSet := strings.TrimSpace(getenv("SES_CONFIGURATION_SET"))
	sesRegion := strings.TrimSpace(getenv("AWS_REGION"))

	if mode == authEmailModeCapture {
		if environment != "dev" {
			return cfg, fmt.Errorf("config: AUTH_EMAIL_MODE=capture is permitted only when ENV=dev, not %s", environment)
		}
		if captureURL == "" {
			return cfg, errors.New("config: AUTH_EMAIL_CAPTURE_URL is required when AUTH_EMAIL_MODE=capture")
		}
		if err := validateCaptureURL(captureURL); err != nil {
			return cfg, err
		}
		bearer, decErr := decodeBase64URL32(captureBearerRaw)
		if decErr != nil {
			return cfg, errors.New("config: AUTH_EMAIL_CAPTURE_BEARER must be base64url encoding exactly 32 bytes")
		}
		cfg.CaptureURL = captureURL
		cfg.CaptureBearer = bearer
		for _, field := range []struct{ name, value string }{
			{"SES_FROM_ADDRESS", sesFrom},
			{"SES_CONFIGURATION_SET", sesConfigSet},
			{"AWS_REGION", sesRegion},
		} {
			if field.value != "" {
				return cfg, fmt.Errorf("config: %s must be absent when AUTH_EMAIL_MODE=capture", field.name)
			}
		}
		return cfg, nil
	}

	// SES mode.
	for _, field := range []struct{ name, value string }{
		{"AUTH_EMAIL_CAPTURE_URL", captureURL},
		{"AUTH_EMAIL_CAPTURE_BEARER", captureBearerRaw},
	} {
		if field.value != "" {
			return cfg, fmt.Errorf("config: %s must be absent when AUTH_EMAIL_MODE=ses", field.name)
		}
	}
	if sesRegion != requiredSESRegion {
		return cfg, errors.New("config: AWS_REGION must be ap-southeast-1 when AUTH_EMAIL_MODE=ses")
	}
	if _, err := accountemail.Canonicalize(sesFrom); err != nil {
		return cfg, errors.New("config: SES_FROM_ADDRESS must be a canonical email address")
	}
	if !isAWSSafeASCII(sesConfigSet) {
		return cfg, errors.New("config: SES_CONFIGURATION_SET must be 1-64 ASCII letters, digits, hyphens, or underscores")
	}
	cfg.SESFrom = sesFrom
	cfg.SESConfigSet = sesConfigSet
	cfg.SESRegion = sesRegion
	return cfg, nil
}

// decodeBase64URL32 decodes an unpadded base64url string and requires exactly 32
// decoded bytes. It never returns a decoded value as a string.
func decodeBase64URL32(raw string) ([32]byte, error) {
	var zero [32]byte
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return zero, errors.New("empty")
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil || len(decoded) != 32 {
		return zero, errors.New("not 32 base64url bytes")
	}
	var out [32]byte
	copy(out[:], decoded)
	return out, nil
}

// isPrintableASCII reports whether s is 1-64 bytes of printable ASCII
// (0x20-0x7e).
func isPrintableASCII(s string) bool {
	if len(s) < 1 || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// isAWSSafeASCII reports whether s is 1-64 bytes of ASCII letters, digits,
// hyphens, or underscores — the SES configuration-set name alphabet.
func isAWSSafeASCII(s string) bool {
	if len(s) < 1 || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// validateCaptureURL requires an absolute loopback http URL with an explicit
// port and no userinfo, query, or fragment. The mailcapture client only ever
// POSTs there, so this is the D7 loopback-only boundary.
func validateCaptureURL(raw string) error {
	u, err := parseLoopbackURL(raw)
	if err != nil {
		return fmt.Errorf("config: AUTH_EMAIL_CAPTURE_URL: %w", err)
	}
	if u.Scheme != "http" {
		return errors.New("config: AUTH_EMAIL_CAPTURE_URL must use http")
	}
	if u.Port() == "" {
		return errors.New("config: AUTH_EMAIL_CAPTURE_URL must include an explicit port")
	}
	return nil
}

// parseLoopbackURL returns the parsed absolute URL if raw is an http/https URL
// whose host is loopback (or localhost), with no userinfo, path, query, or
// fragment.
func parseLoopbackURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("must be an absolute http URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("must use http or https")
	}
	if u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("must be scheme://host[:port] with no userinfo, path, query, or fragment")
	}
	host := strings.ToLower(u.Hostname())
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, errors.New("host must be loopback")
	}
	return u, nil
}
