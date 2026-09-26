package previewmeta

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
	"golang.org/x/net/html"
)

const (
	// minSentenceGraphemes is the shortest run of whole sentences a cut keeps
	// before it falls back to a word cut.
	minSentenceGraphemes = 60
	// wordCutWindow is how far back from the cut point a word cut looks for a
	// space. Text without one there, as in Chinese or Japanese, is cut mid-word.
	wordCutWindow = 40
	ellipsis      = "…"
)

var (
	// emailPattern matches a token shaped like local@domain.tld.
	emailPattern = regexp.MustCompile(`[^\s@]+@[^\s@]+\.[^\s@]+`)
	// phonePattern matches a phone-shaped token: an optional leading plus
	// and parenthesis, then digit groups joined by single spaces, dots, or
	// hyphens, each group optionally in parentheses. scrubPhones decides
	// which matches are contact data.
	phonePattern = regexp.MustCompile(`[+(]{0,2}[0-9]+\)?(?:[ .\-]\(?[0-9]+\)?)*`)
)

// Normalize replaces control and bidirectional formatting characters with a
// space, collapses white space runs to one space, and trims the result.
func Normalize(text string) string {
	var out strings.Builder
	pendingSpace := false
	for _, character := range text {
		if unicode.IsSpace(character) || unicode.Is(unicode.Cc, character) || bidiFormatting(character) {
			pendingSpace = true
			continue
		}
		if pendingSpace && out.Len() != 0 {
			out.WriteByte(' ')
		}
		pendingSpace = false
		out.WriteRune(character)
	}
	return out.String()
}

// Scrub removes contact data from normalized text: every exact contact value,
// every email-shaped token, and every phone-shaped token with nine or more
// digits. It normalizes again after removal.
func Scrub(text string, contacts []string) string {
	text = Normalize(text)
	for _, contact := range contacts {
		text = strings.ReplaceAll(text, contact, " ")
	}
	text = emailPattern.ReplaceAllString(text, " ")
	text = scrubPhones(text)
	return Normalize(text)
}

// minPhoneDigits is the fewest digits a phone-shaped token needs to count as
// contact data.
const minPhoneDigits = 9

// scrubPhones removes each phone-shaped token with at least minPhoneDigits
// digits. A token touching a letter or digit, such as an order ID, stays, and
// so does a run of years such as 2012-2016-2020.
func scrubPhones(text string) string {
	var out strings.Builder
	last := 0
	for _, span := range phonePattern.FindAllStringIndex(text, -1) {
		start, end := span[0], span[1]
		if !phoneToken(text, start, end) {
			continue
		}
		out.WriteString(text[last:start])
		out.WriteByte(' ')
		last = end
	}
	out.WriteString(text[last:])
	return out.String()
}

func phoneToken(text string, start, end int) bool {
	before, _ := utf8.DecodeLastRuneInString(text[:start])
	after, _ := utf8.DecodeRuneInString(text[end:])
	if start > 0 && (unicode.IsLetter(before) || unicode.IsDigit(before)) {
		return false
	}
	if end < len(text) && (unicode.IsLetter(after) || unicode.IsDigit(after)) {
		return false
	}
	groups := strings.FieldsFunc(text[start:end], func(character rune) bool { return character < '0' || character > '9' })
	digits, years := 0, 0
	for _, group := range groups {
		digits += len(group)
		if len(group) == 4 && (strings.HasPrefix(group, "19") || strings.HasPrefix(group, "20")) {
			years++
		}
	}
	return digits >= minPhoneDigits && years != len(groups)
}

// Cut shortens a normalized description to MaxDescriptionGraphemes. It keeps
// the longest run of whole sentences that fits when that run has at least
// minSentenceGraphemes; otherwise it cuts at the last space at or before
// grapheme 159, drops trailing punctuation, and appends an ellipsis.
func Cut(text string) string {
	if fitsDescription(text) {
		return text
	}
	run, rest, state := "", text, -1
	for rest != "" {
		var sentence string
		sentence, rest, state = uniseg.FirstSentenceInString(rest, state)
		if !fitsDescription(strings.TrimRight(run+sentence, " ")) {
			break
		}
		run += sentence
	}
	run = strings.TrimRight(run, " ")
	if uniseg.GraphemeClusterCount(run) >= minSentenceGraphemes {
		return run
	}
	clusters := graphemes(text)
	keep, size := 0, 0
	for keep < len(clusters) && keep < MaxDescriptionGraphemes-1 && size+len(clusters[keep]) <= MaxDescriptionBytes-len(ellipsis) {
		size += len(clusters[keep])
		keep++
	}
	end := keep
	for index := keep - 1; index >= 0 && index >= keep-wordCutWindow; index-- {
		if clusters[index] == " " {
			end = index
			break
		}
	}
	return strings.TrimRight(strings.Join(clusters[:end], ""), ",;:–- ") + ellipsis
}

func fitsDescription(text string) bool {
	return len(text) <= MaxDescriptionBytes && uniseg.GraphemeClusterCount(text) <= MaxDescriptionGraphemes
}

// firstGraphemes keeps at most limit whole grapheme clusters of text and, when
// maxBytes is positive, at most maxBytes bytes, then trims trailing spaces.
func firstGraphemes(text string, limit, maxBytes int) string {
	var out strings.Builder
	rest, state := text, -1
	for count := 0; rest != "" && count < limit; count++ {
		var cluster string
		cluster, rest, _, state = uniseg.FirstGraphemeClusterInString(rest, state)
		if maxBytes > 0 && out.Len()+len(cluster) > maxBytes {
			break
		}
		out.WriteString(cluster)
	}
	return strings.TrimRight(out.String(), " ")
}

func graphemes(text string) []string {
	var clusters []string
	rest, state := text, -1
	for rest != "" {
		var cluster string
		cluster, rest, _, state = uniseg.FirstGraphemeClusterInString(rest, state)
		clusters = append(clusters, cluster)
	}
	return clusters
}

// richTextPlain is the text of sanitized rich text with entities decoded.
// Paragraph, list, list-item, and line-break boundaries become spaces.
func richTextPlain(source string) string {
	root, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return ""
	}
	var out strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			out.WriteString(node.Data)
			return
		}
		block := node.Type == html.ElementNode && blockElement(node.Data)
		if block {
			out.WriteByte(' ')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if block {
			out.WriteByte(' ')
		}
	}
	walk(root)
	return Normalize(out.String())
}

func blockElement(name string) bool {
	switch name {
	case "p", "ul", "ol", "li", "br":
		return true
	default:
		return false
	}
}

// bidiFormatting reports the bidirectional formatting characters: U+061C,
// U+200E, U+200F, U+202A to U+202E, and U+2066 to U+2069.
func bidiFormatting(character rune) bool {
	return character == 0x061C || character == 0x200E || character == 0x200F ||
		(character >= 0x202A && character <= 0x202E) || (character >= 0x2066 && character <= 0x2069)
}

func sortLongestFirst(values []string) {
	slices.SortStableFunc(values, func(a, b string) int { return len(b) - len(a) })
}
