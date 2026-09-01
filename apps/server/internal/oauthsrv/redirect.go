package oauthsrv

import (
	"errors"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// The closed errors for the M1 registration grammar. Neither carries the
// presented value.
var (
	// ErrRedirectInvalid rejects a redirect URI outside the M1 grammar.
	ErrRedirectInvalid = errors.New("oauth redirect uri invalid")
	// ErrClientNameInvalid rejects a client name outside the M1 bounds.
	ErrClientNameInvalid = errors.New("oauth client name invalid")
)

const (
	// redirectURIMaxBytes is the M1 per-URI byte ceiling.
	redirectURIMaxBytes = 512
	// clientNameMaxCodePoints is the M1 name ceiling, counted after NFC.
	clientNameMaxCodePoints = 64
	// clientNameMaxRawBytes bounds the input before normalization runs, at
	// four bytes per allowed code point.
	clientNameMaxRawBytes = 4 * clientNameMaxCodePoints
)

// loopbackRedirectHosts is the closed set of hosts M1 allows over plain http.
// url.URL.Hostname strips the brackets from an IPv6 literal, so the M1 host
// [::1] appears here as ::1; no other spelling of the loopback address is
// admitted.
var loopbackRedirectHosts = map[string]bool{
	"127.0.0.1": true,
	"localhost": true,
	"::1":       true,
}

// ValidateRedirectURI enforces the M1 grammar for one redirect URI: 1 to 512
// printable ASCII bytes, absolute, no fragment, no userinfo, and either https
// with any host or http with a loopback host on any port and path. Scheme and
// host are matched case-insensitively, as RFC 3986 section 3.1 requires.
//
// The M1 cap of five URIs per client is the registration caller's rule, not
// this function's.
func ValidateRedirectURI(raw string) error {
	if len(raw) == 0 || len(raw) > redirectURIMaxBytes {
		return ErrRedirectInvalid
	}
	// One byte pass before any parsing: printable ASCII only, and never the
	// fragment delimiter. Hostile input costs no allocation.
	for i := 0; i < len(raw); i++ {
		if c := raw[i]; c < 0x21 || c > 0x7e || c == '#' {
			return ErrRedirectInvalid
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ErrRedirectInvalid
	}
	if u.Opaque != "" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return ErrRedirectInvalid
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if loopbackRedirectHosts[strings.ToLower(u.Hostname())] {
			return nil
		}
		return ErrRedirectInvalid
	default:
		return ErrRedirectInvalid
	}
}

// ValidateClientName enforces the M1 client-name bounds and returns the NFC
// canonical form: valid UTF-8, 1 to 64 Unicode code points after NFC, and no
// control characters. The name is not trimmed, collapsed, or case-folded, and
// input longer than four bytes per allowed code point is rejected before
// normalization runs.
func ValidateClientName(raw string) (canonical string, err error) {
	if len(raw) == 0 || len(raw) > clientNameMaxRawBytes {
		return "", ErrClientNameInvalid
	}
	if !utf8.ValidString(raw) {
		return "", ErrClientNameInvalid
	}
	normalized := norm.NFC.String(raw)
	if n := utf8.RuneCountInString(normalized); n < 1 || n > clientNameMaxCodePoints {
		return "", ErrClientNameInvalid
	}
	for _, r := range normalized {
		if unicode.IsControl(r) {
			return "", ErrClientNameInvalid
		}
	}
	return normalized, nil
}
