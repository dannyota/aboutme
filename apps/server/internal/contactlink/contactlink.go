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

const forbiddenEmailRunes = "<>\"'`()\\,;:?#%"

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
