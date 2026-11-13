package project

import (
	"strings"
	"unicode"
)

// JavaScript's String.prototype.trim differs from strings.TrimSpace, which also trims U+0085 and keeps U+FEFF.
func Blank(text string) bool {
	return strings.IndexFunc(text, func(r rune) bool {
		switch r {
		case '\t', '\v', '\f', '\n', '\r', '\ufeff', '\u2028', '\u2029':
			return false
		}
		return !unicode.Is(unicode.Zs, r)
	}) < 0
}
