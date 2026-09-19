// Package contactlink decides whether an email or phone contact detail renders
// as a mailto: or tel: link, and computes its exact href. The web renderer
// applies the same rules; a shared corpus keeps them identical. See
// docs/adr/0043-email-and-phone-links.md.
package contactlink

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxEmailLength = 254
	minPhoneDigits = 3
	maxPhoneDigits = 20
)

// Href returns the anchor target for a contact detail and true, or "" and
// false when the value stays text. Only email and phone details link here;
// URL types follow the https rule of ADR 0013 and ADR 0041.
func Href(detailType, value string) (string, bool) {
	switch detailType {
	case "email":
		if validEmail(value) {
			return "mailto:" + value, true
		}
	case "phone":
		if number, ok := phoneNumber(value); ok {
			return "tel:" + number, true
		}
	}
	return "", false
}

// forbiddenEmailRunes cannot appear in a linked address: they are unusual in
// real addresses, and ? # % would let the value add mail headers or a fragment
// to the mailto: URL.
const forbiddenEmailRunes = "<>\"'`()\\,;:?#%"

// validEmail accepts at most 254 characters with exactly one @ and at least
// one character before it. The part after it contains a dot and neither
// starts nor ends with one. White space, control and format characters, and
// forbiddenEmailRunes are rejected.
func validEmail(value string) bool {
	if value == "" || utf8.RuneCountInString(value) > maxEmailLength {
		return false
	}
	for _, r := range value {
		if unicode.Is(unicode.White_Space, r) || unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) ||
			strings.ContainsRune(forbiddenEmailRunes, r) {
			return false
		}
	}
	local, domain, found := strings.Cut(value, "@")
	if !found || local == "" || strings.Contains(domain, "@") {
		return false
	}
	return strings.Contains(domain, ".") && !strings.HasPrefix(domain, ".") && !strings.HasSuffix(domain, ".")
}

// phoneNumber removes spaces, dots, hyphens, and parentheses, then accepts an
// optional leading + followed by 3 to 20 ASCII digits.
func phoneNumber(value string) (string, bool) {
	number := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '.', '-', '(', ')':
			return -1
		default:
			return r
		}
	}, value)
	digits := strings.TrimPrefix(number, "+")
	if len(digits) < minPhoneDigits || len(digits) > maxPhoneDigits {
		return "", false
	}
	for index := range len(digits) {
		if digits[index] < '0' || digits[index] > '9' {
			return "", false
		}
	}
	return number, true
}
