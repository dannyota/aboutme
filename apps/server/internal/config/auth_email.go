package config

// Password-authentication email configuration. Every value is validated at
// load time; secrets are base64url-decoded exactly once and never echoed in an
// error. Capture, SES, and SMTP are exclusive modes, and capture is permitted
// only in development so production can never route mail to a local loopback
// sink. SMTP follows docs/design/vietnam-production.md, "DNS and mail".

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dannyota/aboutme/apps/server/internal/accountemail"
)

const (
	authEmailModeSES     = "ses"
	authEmailModeCapture = "capture"
	authEmailModeSMTP    = "smtp"
	// smtpTLSImplicit and smtpTLSStartTLS are the SMTP_TLS values; each has
	// exactly one port.
	smtpTLSImplicit  = "implicit"
	smtpTLSStartTLS  = "starttls"
	smtpImplicitPort = 465
	smtpStartTLSPort = 587
	// maxSMTPValueBytes bounds SMTP_USERNAME and SMTP_PASSWORD.
	maxSMTPValueBytes = 256
	// requiredSESRegion is the exact region SES mode requires (D7).
	requiredSESRegion = "ap-southeast-1"
)

// AuthEmailConfig holds the validated password-mail configuration. The two key
// fields are 32-byte arrays so a decoded secret can never be a string that
// accidentally reaches a log or error; the SMTP password is a Secret for the
// same reason. SESFrom and SESFromName are the From address and display name
// in both SES and SMTP mode.
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
	SESFromName   string
	SESConfigSet  string
	SESRegion     string
	SMTPHost      string
	SMTPPort      int
	SMTPTLS       string
	SMTPUsername  string
	SMTPPassword  Secret
}

// Secret is a string value that formats and logs as "[redacted]". Read it with
// Reveal only where the value is used.
type Secret string

// Reveal returns the secret value.
func (s Secret) Reveal() string { return string(s) }

// String implements fmt.Stringer without the value.
func (Secret) String() string { return "[redacted]" }

// GoString implements fmt.GoStringer without the value.
func (Secret) GoString() string { return "[redacted]" }

// LogValue implements slog.LogValuer without the value.
func (Secret) LogValue() slog.Value { return slog.StringValue("[redacted]") }

// loadAuthEmailConfig reads and validates the password-mail configuration. It
// rejects a missing or malformed rate key, active key, a previous key that is
// only half-set or duplicates the active ID, an unknown mode, a capture mode in
// a non-dev environment, a non-loopback capture URL, a malformed capture
// bearer, a noncanonical From address, a non-AWS-safe configuration set, an
// incomplete or inconsistent SMTP setting, and any mode-specific field set in
// the wrong mode.
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
	case authEmailModeSES, authEmailModeCapture, authEmailModeSMTP:
		cfg.Mode = mode
	default:
		return cfg, errors.New("config: AUTH_EMAIL_MODE must be ses, smtp, or capture")
	}

	captureURL := strings.TrimSpace(getenv("AUTH_EMAIL_CAPTURE_URL"))
	captureBearerRaw := strings.TrimSpace(getenv("AUTH_EMAIL_CAPTURE_BEARER"))
	sesFrom := strings.TrimSpace(getenv("SES_FROM_ADDRESS"))
	sesFromName := strings.TrimSpace(getenv("SES_FROM_NAME"))
	sesConfigSet := strings.TrimSpace(getenv("SES_CONFIGURATION_SET"))
	sesRegion := strings.TrimSpace(getenv("AWS_REGION"))
	smtpFields := []struct{ name, value string }{
		{"SMTP_HOST", strings.TrimSpace(getenv("SMTP_HOST"))},
		{"SMTP_PORT", strings.TrimSpace(getenv("SMTP_PORT"))},
		{"SMTP_TLS", strings.TrimSpace(getenv("SMTP_TLS"))},
		{"SMTP_USERNAME", strings.TrimSpace(getenv("SMTP_USERNAME"))},
		{"SMTP_PASSWORD", strings.TrimSpace(getenv("SMTP_PASSWORD"))},
	}
	if mode != authEmailModeSMTP {
		for _, field := range smtpFields {
			if field.value != "" {
				return cfg, fmt.Errorf("config: %s must be absent when AUTH_EMAIL_MODE=%s", field.name, mode)
			}
		}
	}

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
			{"SES_FROM_NAME", sesFromName},
			{"SES_CONFIGURATION_SET", sesConfigSet},
			{"AWS_REGION", sesRegion},
		} {
			if field.value != "" {
				return cfg, fmt.Errorf("config: %s must be absent when AUTH_EMAIL_MODE=capture", field.name)
			}
		}
		return cfg, nil
	}

	// SES and SMTP mode.
	for _, field := range []struct{ name, value string }{
		{"AUTH_EMAIL_CAPTURE_URL", captureURL},
		{"AUTH_EMAIL_CAPTURE_BEARER", captureBearerRaw},
	} {
		if field.value != "" {
			return cfg, fmt.Errorf("config: %s must be absent when AUTH_EMAIL_MODE=%s", field.name, mode)
		}
	}
	if _, err := accountemail.Canonicalize(sesFrom); err != nil {
		return cfg, errors.New("config: SES_FROM_ADDRESS must be a canonical email address")
	}
	if !validFromName(sesFromName) {
		return cfg, errors.New("config: SES_FROM_NAME must be at most 64 characters with no control characters")
	}
	cfg.SESFrom = sesFrom
	cfg.SESFromName = sesFromName
	if mode == authEmailModeSMTP {
		if sesConfigSet != "" {
			return cfg, errors.New("config: SES_CONFIGURATION_SET must be absent when AUTH_EMAIL_MODE=smtp")
		}
		return loadSMTPConfig(cfg, smtpFields[0].value, smtpFields[1].value, smtpFields[2].value, smtpFields[3].value, smtpFields[4].value)
	}
	if sesRegion != requiredSESRegion {
		return cfg, errors.New("config: AWS_REGION must be ap-southeast-1 when AUTH_EMAIL_MODE=ses")
	}
	if !isAWSSafeASCII(sesConfigSet) {
		return cfg, errors.New("config: SES_CONFIGURATION_SET must be 1-64 ASCII letters, digits, hyphens, or underscores")
	}
	cfg.SESConfigSet = sesConfigSet
	cfg.SESRegion = sesRegion
	return cfg, nil
}

// loadSMTPConfig validates the SMTP settings. Every error names the variable,
// never its value.
func loadSMTPConfig(cfg AuthEmailConfig, host, port, tlsMode, username, password string) (AuthEmailConfig, error) {
	if !isSMTPHostname(host) {
		return cfg, errors.New("config: SMTP_HOST must be a DNS host name when AUTH_EMAIL_MODE=smtp")
	}
	portNum, err := strconv.Atoi(port)
	if err != nil {
		return cfg, errors.New("config: SMTP_PORT must be 465 or 587 when AUTH_EMAIL_MODE=smtp")
	}
	switch {
	case tlsMode == smtpTLSImplicit && portNum == smtpImplicitPort:
	case tlsMode == smtpTLSStartTLS && portNum == smtpStartTLSPort:
	case tlsMode != smtpTLSImplicit && tlsMode != smtpTLSStartTLS:
		return cfg, errors.New("config: SMTP_TLS must be implicit or starttls when AUTH_EMAIL_MODE=smtp")
	default:
		return cfg, errors.New("config: SMTP_PORT and SMTP_TLS must be 465 with implicit or 587 with starttls")
	}
	if !validSMTPValue(username) {
		return cfg, errors.New("config: SMTP_USERNAME must be 1-256 bytes with no control characters")
	}
	if !validSMTPValue(password) {
		return cfg, errors.New("config: SMTP_PASSWORD must be 1-256 bytes with no control characters")
	}
	cfg.SMTPHost = strings.ToLower(host)
	cfg.SMTPPort = portNum
	cfg.SMTPTLS = tlsMode
	cfg.SMTPUsername = username
	cfg.SMTPPassword = Secret(password)
	return cfg, nil
}

// isSMTPHostname reports whether s is a DNS host name of at most 253 bytes
// whose labels are 1-63 ASCII letters, digits, or inner hyphens. An IP literal
// is rejected because the certificate is verified against the name.
func isSMTPHostname(s string) bool {
	if s == "" || len(s) > 253 || net.ParseIP(s) != nil {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}

// validSMTPValue reports whether v is 1-256 bytes of valid UTF-8 with no
// control characters, the bound the SMTP sender also enforces.
func validSMTPValue(v string) bool {
	if v == "" || len(v) > maxSMTPValueBytes || !utf8.ValidString(v) {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
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

// maxSESFromNameRunes matches the SES sender's display-name bound.
const maxSESFromNameRunes = 64

// validFromName repeats the SES sender's display-name rule so config fails at
// startup: empty, or at most 64 runes with no control characters, so it can
// never inject a header line. authmail is not imported here to keep the AWS
// SDK out of config.
func validFromName(name string) bool {
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > maxSESFromNameRunes {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
