// Package publicpage validates and renders the owner-set public page title
// and emoji favicon, publication settings stored beside the slug. See
// docs/adr/0042-public-page-title-and-favicon.md.
package publicpage

//go:generate go run extpict_gen.go

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

const (
	// MaxTitleGraphemes bounds the title as a reader sees it.
	MaxTitleGraphemes = 70
	// MaxTitleRunes bounds its storage, since one grapheme may hold many
	// code points; the resumes_public_title_check constraint matches.
	MaxTitleRunes = 560
	// MaxFaviconRunes bounds one emoji grapheme. The longest standard
	// sequences, family ZWJ sequences with skin tones, use 11 code points.
	MaxFaviconRunes = 16
)

// Issue codes reported under the publish request's field paths.
const (
	// CodeTooLong reports a title over MaxTitleGraphemes or MaxTitleRunes.
	CodeTooLong = "too_long"
	// CodeInvalidCharacters reports a control or format character in a title.
	CodeInvalidCharacters = "invalid_characters"
	// CodeInvalidEmoji reports a favicon that is not exactly one emoji.
	CodeInvalidEmoji = "invalid_emoji"
)

// Result is one normalized field. Value is nil when the field clears; Codes
// lists every issue, sorted, and is empty when the value is valid.
type Result struct {
	Value *string
	Codes []string
}

// NormalizeTitle trims Unicode white space and validates a public title.
func NormalizeTitle(raw string) Result {
	title := trim(raw)
	if title == "" {
		return Result{}
	}
	var codes []string
	for _, r := range title {
		if unicode.Is(unicode.Cc, r) || (unicode.Is(unicode.Cf, r) && r != zeroWidthJoiner) {
			codes = append(codes, CodeInvalidCharacters)
			break
		}
	}
	if utf8.RuneCountInString(title) > MaxTitleRunes || uniseg.GraphemeClusterCount(title) > MaxTitleGraphemes {
		codes = append(codes, CodeTooLong)
	}
	if len(codes) != 0 {
		sort.Strings(codes)
		return Result{Codes: codes}
	}
	return Result{Value: &title}
}

// NormalizeFaviconEmoji trims Unicode white space and accepts exactly one
// emoji grapheme: an Extended_Pictographic base with only emoji modifiers,
// variation selectors, ZWJ, and tag characters after it, or one flag of two
// Regional Indicators.
func NormalizeFaviconEmoji(raw string) Result {
	emoji := trim(raw)
	if emoji == "" {
		return Result{}
	}
	if utf8.RuneCountInString(emoji) > MaxFaviconRunes || uniseg.GraphemeClusterCount(emoji) != 1 || !emojiCluster(emoji) {
		return Result{Codes: []string{CodeInvalidEmoji}}
	}
	return Result{Value: &emoji}
}

// EffectiveTitle is the page's <title>: the owner's title, or the default.
func EffectiveTitle(publicTitle *string, fullName string) string {
	if publicTitle != nil && *publicTitle != "" {
		return *publicTitle
	}
	return fullName + " — Resume"
}

// FaviconHref is the exact data: URL of an emoji favicon. Every byte outside
// the URL-unreserved set is percent-encoded, so the value cannot leave its
// attribute or the SVG text node whatever the emoji holds.
func FaviconHref(emoji string) string {
	svg := "<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'>" +
		"<text y='.9em' font-size='90'>" + emoji + "</text></svg>"
	var out strings.Builder
	out.WriteString("data:image/svg+xml,")
	for index := range len(svg) {
		b := svg[index]
		if unreserved(b) {
			out.WriteByte(b)
			continue
		}
		out.WriteByte('%')
		out.WriteByte(upperHex[b>>4])
		out.WriteByte(upperHex[b&0x0F])
	}
	return out.String()
}

const (
	zeroWidthJoiner = 0x200D
	upperHex        = "0123456789ABCDEF"
)

func trim(raw string) string {
	return strings.TrimFunc(raw, func(r rune) bool { return unicode.Is(unicode.White_Space, r) })
}

func unreserved(b byte) bool {
	return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9' ||
		b == '-' || b == '.' || b == '_' || b == '~'
}

func emojiCluster(cluster string) bool {
	runes := []rune(cluster)
	if len(runes) == 2 && regionalIndicator(runes[0]) && regionalIndicator(runes[1]) {
		return true
	}
	if !isExtendedPictographic(runes[0]) {
		return false
	}
	for _, r := range runes[1:] {
		if !isExtendedPictographic(r) && !emojiComponent(r) {
			return false
		}
	}
	return true
}

func regionalIndicator(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }

// emojiComponent covers what may follow an emoji base inside one sequence:
// ZWJ, text and emoji variation selectors, skin-tone modifiers, and tags.
func emojiComponent(r rune) bool {
	return r == zeroWidthJoiner || r == 0xFE0E || r == 0xFE0F ||
		(r >= 0x1F3FB && r <= 0x1F3FF) || (r >= 0xE0020 && r <= 0xE007F)
}

func isExtendedPictographic(r rune) bool {
	index := sort.Search(len(extendedPictographic), func(i int) bool { return extendedPictographic[i][1] >= r })
	return index < len(extendedPictographic) && extendedPictographic[index][0] <= r
}
