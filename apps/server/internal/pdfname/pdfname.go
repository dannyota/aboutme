package pdfname

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	suffix       = "-Resume.pdf"
	maxBaseRunes = 64
)

func Disposition(fullName string) string {
	words := nameWords(fullName)
	ascii := make([]string, 0, len(words))
	for _, word := range words {
		if folded := foldASCII(word); folded != "" {
			ascii = append(ascii, folded)
		}
	}
	header := `attachment; filename="` + withSuffix(ascii) + `"`
	if len(words) == 0 {
		return header
	}
	return header + "; filename*=UTF-8''" + percentEncode(withSuffix(words))
}

func withSuffix(words []string) string {
	if len(words) == 0 {
		return "Resume.pdf"
	}
	return strings.Join(words, "-") + suffix
}

func nameWords(fullName string) []string {
	fields := strings.FieldsFunc(norm.NFC.String(fullName), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsMark(r) && !unicode.IsNumber(r)
	})
	words := make([]string, 0, len(fields))
	length := 0
	for _, field := range fields {
		next := length + utf8.RuneCountInString(field)
		if len(words) > 0 {
			next++
		}
		if next > maxBaseRunes {
			break
		}
		words = append(words, field)
		length = next
	}
	return words
}

func foldASCII(word string) string {
	var out strings.Builder
	for _, r := range norm.NFD.String(word) {
		switch {
		case r == 'đ':
			out.WriteByte('d')
		case r == 'Đ':
			out.WriteByte('D')
		case r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			out.WriteRune(r)
		}
	}
	return out.String()
}

func percentEncode(value string) string {
	const hex = "0123456789ABCDEF"
	var out strings.Builder
	for index := range len(value) {
		c := value[index]
		if isAttrChar(c) {
			out.WriteByte(c)
			continue
		}
		out.WriteByte('%')
		out.WriteByte(hex[c>>4])
		out.WriteByte(hex[c&0x0f])
	}
	return out.String()
}

func isAttrChar(c byte) bool {
	switch {
	case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9':
		return true
	}
	return strings.IndexByte("!#$&+-.^_`|~", c) >= 0
}
