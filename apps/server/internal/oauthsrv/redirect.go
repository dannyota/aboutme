package oauthsrv

import (
	"errors"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

var (
	ErrRedirectInvalid   = errors.New("oauth redirect uri invalid")
	ErrClientNameInvalid = errors.New("oauth client name invalid")
)

const (
	redirectURIMaxBytes     = 512
	clientNameMaxCodePoints = 64
	clientNameMaxRawBytes   = 4096
)

func loopbackRedirectHost(host string) bool {
	switch host {
	case "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
}

func ValidateRedirectURI(raw string) error {
	if len(raw) == 0 || len(raw) > redirectURIMaxBytes {
		return ErrRedirectInvalid
	}
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
		if loopbackRedirectHost(strings.ToLower(u.Hostname())) {
			return nil
		}
		return ErrRedirectInvalid
	default:
		return ErrRedirectInvalid
	}
}

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
