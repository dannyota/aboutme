package project

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidSlug checks the account-local project slug grammar.
func ValidSlug(s string) bool { return len(s) >= 2 && len(s) <= 40 && slugPattern.MatchString(s) }

// ProposeSlug folds a name into an ASCII project slug.
func ProposeSlug(s string) string {
	var b strings.Builder
	hyphen := false
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if r == 'đ' {
			r = 'd'
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if hyphen && b.Len() > 0 && b.Len() < 39 {
				b.WriteByte('-')
			}
			hyphen = false
			if b.Len() < 40 {
				b.WriteRune(r)
			}
		} else {
			hyphen = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) < 2 {
		return "project"
	}
	return out
}
