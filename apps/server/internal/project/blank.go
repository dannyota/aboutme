package project

import (
	"strings"
	"unicode"
)

// Blank reports whether text is empty or only whitespace as the renderer's
// String.prototype.trim reads it: Unicode space separators, tab, vertical
// tab, form feed, the byte order mark and line terminators. It is not Go's
// strings.TrimSpace, which also trims U+0085 and keeps U+FEFF. A name must
// be non-blank by this rule, so every stored project renders.
func Blank(text string) bool {
	return strings.IndexFunc(text, func(r rune) bool {
		switch r {
		case '\t', '\v', '\f', '\n', '\r', '\ufeff', '\u2028', '\u2029':
			return false
		}
		return !unicode.Is(unicode.Zs, r)
	}) < 0
}
