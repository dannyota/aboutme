package accountemail

import (
	"errors"
	"strings"
)

const MaxBytes = 254

var ErrEmailInvalid = errors.New("account email invalid")

func Canonicalize(raw string) (string, error) {
	if len(raw) < 5 || len(raw) > MaxBytes {
		return "", ErrEmailInvalid
	}
	for i := 0; i < len(raw); i++ {
		b := raw[i]
		if b >= 0x80 || b < 0x21 || b == 0x7f {
			return "", ErrEmailInvalid
		}
	}

	lower := strings.ToLower(raw)

	at := strings.IndexByte(lower, '@')
	if at < 0 || strings.IndexByte(lower[at+1:], '@') >= 0 {
		return "", ErrEmailInvalid
	}
	local := lower[:at]
	domain := lower[at+1:]

	if !validLocal(local) {
		return "", ErrEmailInvalid
	}
	if !validDomain(domain) {
		return "", ErrEmailInvalid
	}
	return lower, nil
}

func validLocal(local string) bool {
	if len(local) < 1 || len(local) > 64 {
		return false
	}
	if local[0] == '.' || local[len(local)-1] == '.' {
		return false
	}
	prevDot := false
	for i := 0; i < len(local); i++ {
		c := local[i]
		if c == '.' {
			if prevDot {
				return false
			}
			prevDot = true
			continue
		}
		prevDot = false
		if !isLocalAtom(c) {
			return false
		}
	}
	return true
}

func isLocalAtom(c byte) bool {
	if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
		return true
	}
	switch c {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '/', '=', '?', '^', '_', '`', '{', '|', '}', '~':
		return true
	}
	return false
}

func validDomain(domain string) bool {
	if len(domain) < 1 || len(domain) > 253 {
		return false
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) < 1 || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			if !isDomainLabelChar(label[i]) {
				return false
			}
		}
	}
	return true
}

func isDomainLabelChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-'
}
