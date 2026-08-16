// Package accountemail implements the canonical account-email parser for
// Phase PA (D1): a deliberate deliverable-address subset of addr-spec that is
// bounded, pure ASCII, and stored lowercase. The full grammar is authoritative
// for account creation and lookup; the migration preflight in 00008 is only a
// cheap subset of it.
package accountemail

import (
	"errors"
	"strings"
)

// MaxBytes is the maximum number of ASCII bytes in a canonical account email.
const MaxBytes = 254

// ErrEmailInvalid is the closed error returned for every malformed email.
var ErrEmailInvalid = errors.New("account email invalid")

// Canonicalize accepts a raw email address and returns its lowercase ASCII
// canonical form. It rejects surrounding space, controls, non-ASCII bytes,
// quoted local parts, comments, domain literals, empty labels, a
// leading/trailing dot, consecutive local dots, a leading/trailing domain
// hyphen, and domain labels over 63 bytes. The local part is 1–64 bytes from
// the exact atext set; the domain is 1–253 bytes of dot-separated labels of
// ASCII letters, digits, and internal hyphens.
func Canonicalize(raw string) (string, error) {
	if len(raw) < 5 || len(raw) > MaxBytes {
		return "", ErrEmailInvalid
	}
	// Reject non-ASCII bytes, controls (including space and DEL) up front, so
	// every later check operates only on printable ASCII and ToLower is a pure
	// ASCII fold.
	for i := 0; i < len(raw); i++ {
		b := raw[i]
		if b >= 0x80 || b < 0x21 || b == 0x7f {
			return "", ErrEmailInvalid
		}
	}

	lower := strings.ToLower(raw)

	// Exactly one '@'. strings.IndexByte on the tail checks for a second one.
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

// validLocal reports whether local is 1–64 bytes of the exact local-part
// character set with no leading/trailing or consecutive dots.
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

// isLocalAtom reports whether c is one of the exact local-part atoms:
// A-Z a-z 0-9 ! # $ % & ' * + - / = ? ^ _ ` { | } ~ (plus '.', handled by the
// caller). Callers pass an already-lowercased byte, so only a-z is checked.
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

// validDomain reports whether domain is 1–253 bytes with at least one dot and
// dot-separated labels of 1–63 bytes each, containing only ASCII letters,
// digits, and internal hyphens.
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

// isDomainLabelChar reports whether c is an ASCII letter, digit, or hyphen —
// the only characters allowed in a domain label.
func isDomainLabelChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-'
}
