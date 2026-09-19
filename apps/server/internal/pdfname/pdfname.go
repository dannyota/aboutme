// Package pdfname builds the download name of a resume PDF from the owner's
// full name. See docs/adr/0045-pdf-download-name-and-metadata.md.
package pdfname

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	suffix = "-Resume.pdf"
	// maxBaseRunes caps the name part so the decoded filename stays well
	// under the common 255-byte file name limit.
	maxBaseRunes = 64
)

// Disposition returns the Content-Disposition value for a resume PDF.
// filename carries the name folded to ASCII letters and digits, words joined
// by hyphens. filename* (RFC 5987) carries the same words in UTF-8. A name
// with no usable words downloads as Resume.pdf.
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

// nameWords splits the NFC name on every rune that is not a letter, mark, or
// number, and keeps whole words while the hyphen-joined result fits
// maxBaseRunes.
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

// foldASCII removes diacritics and keeps ASCII letters and digits. The
// Vietnamese d with stroke has no decomposition, so it maps explicitly.
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

// percentEncode keeps RFC 5987 attr-char bytes and percent-encodes the rest.
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
