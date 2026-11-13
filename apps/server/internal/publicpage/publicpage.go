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
	MaxTitleGraphemes = 70
	MaxTitleRunes     = 560
	MaxFaviconRunes   = 16
)

const (
	CodeTooLong           = "too_long"
	CodeInvalidCharacters = "invalid_characters"
	CodeInvalidEmoji      = "invalid_emoji"
)

type Result struct {
	Value *string
	Codes []string
}

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

func EffectiveTitle(publicTitle *string, fullName string) string {
	if publicTitle != nil && *publicTitle != "" {
		return *publicTitle
	}
	return fullName + " — Resume"
}

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

func emojiComponent(r rune) bool {
	return r == zeroWidthJoiner || r == 0xFE0E || r == 0xFE0F ||
		(r >= 0x1F3FB && r <= 0x1F3FF) || (r >= 0xE0020 && r <= 0xE007F)
}

func isExtendedPictographic(r rune) bool {
	index := sort.Search(len(extendedPictographic), func(i int) bool { return extendedPictographic[i][1] >= r })
	return index < len(extendedPictographic) && extendedPictographic[index][0] <= r
}
