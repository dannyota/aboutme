// Package publicformat encodes public-resume representations with fixed bytes.
package publicformat

import (
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

const MarkdownFormatVersion = 1

func Markdown(resume publicresume.PublicResume) ([]byte, error) {
	blocks := []string{"# " + plain(resume.Document.PersonalDetails.FullName)}
	if line := plainPointer(resume.Document.PersonalDetails.Headline); line != "" {
		blocks = append(blocks, line)
	}
	if resume.Document.PersonalDetails.Details.Present() {
		contacts := make([]string, 0, len(resume.Document.PersonalDetails.Details.Value()))
		for _, detail := range resume.Document.PersonalDetails.Details.Value() {
			value := plain(detail.Value)
			if value == "" {
				continue
			}
			label := plainPointer(detail.Label)
			if label == "" {
				label = contactLabel(detail.Type)
			}
			if isLinkContact(detail.Type) {
				value = link(value, detail.Value)
			}
			contacts = append(contacts, "- "+label+": "+value)
		}
		if len(contacts) != 0 {
			blocks = append(blocks, strings.Join(contacts, "\n"))
		}
	}

	sections := resume.Document.Customization.Layout.Sections
	for _, key := range append(append([]string{}, sections.Main...), sections.Sidebar...) {
		section, ok := resume.Document.Content[key]
		if !ok {
			continue
		}
		if name := plainPointer(section.DisplayName); name != "" {
			blocks = append(blocks, "## "+name)
		}
		entries, err := markdownEntries(section, resume.Document.Customization.DateFormat)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, entries...)
	}
	return []byte(strings.Join(nonempty(blocks), "\n\n") + "\n"), nil
}

func markdownEntries(section publicresume.PublicSection, format schema.DateFormat) ([]string, error) {
	blocks := make([]string, 0)
	appendEntry := func(lines ...string) { blocks = append(blocks, strings.Join(nonempty(lines), "\n")) }
	switch section.SectionType {
	case "profile":
		for _, entry := range section.ProfileEntries {
			appendEntry(richPointer(entry.Text))
		}
	case "work":
		for _, entry := range section.WorkEntries {
			appendEntry(heading(entry.JobTitle, nil), linkedLine(entry.Employer, entry.EmployerLink), metadata(dateRange(entry.Dates, format), place(entry.City, entry.Country)), richPointer(entry.Description))
		}
	case "education":
		for _, entry := range section.EducationEntries {
			appendEntry(heading(entry.Degree, nil), linkedLine(entry.School, entry.SchoolLink), metadata(dateRange(entry.Dates, format), place(entry.City, entry.Country)), richPointer(entry.Description))
		}
	case "skill":
		for _, entry := range section.SkillEntries {
			level := ""
			if entry.Level != nil {
				level = "Level: " + strconv.FormatInt(*entry.Level, 10) + "/5"
			}
			appendEntry(heading(entry.Name, nil), level, richPointer(entry.InfoHTML))
		}
	case "language":
		for _, entry := range section.LanguageEntries {
			level := ""
			if entry.Level != nil {
				level = "Level: " + strconv.FormatInt(*entry.Level, 10) + "/5"
			}
			appendEntry(heading(entry.Name, nil), level)
		}
	case "certificate":
		for _, entry := range section.CertificateEntries {
			appendEntry(heading(entry.Title, entry.TitleLink), plainPointer(entry.Issuer), yearMonth(entry.Date, format), richPointer(entry.Description))
		}
	case "project":
		for _, entry := range section.ProjectEntries {
			appendEntry(heading(entry.Title, entry.Link), dateRange(entry.Dates, format), richPointer(entry.Description))
		}
	case "custom":
		for _, entry := range section.CustomEntries {
			appendEntry(heading(entry.Title, entry.TitleLink), plainPointer(entry.Subtitle), metadata(dateRange(entry.Dates, format), plainPointer(entry.City)), richPointer(entry.Description))
		}
	}
	return nonempty(blocks), nil
}

func heading(text, destination *string) string {
	line := linkedLine(text, destination)
	if line == "" {
		return ""
	}
	return "### " + line
}

func linkedLine(text, destination *string) string {
	value := plainPointer(text)
	if value == "" {
		return ""
	}
	if destination == nil || plain(*destination) == "" {
		return value
	}
	return link(value, *destination)
}

func link(text, destination string) string {
	return "[" + text + "](" + escapeDestination(destination) + ")"
}

func plainPointer(value *string) string {
	if value == nil {
		return ""
	}
	return plain(*value)
}

func plain(value string) string {
	value = strings.Map(func(r rune) rune {
		switch r {
		case '\t', '\r', '\n', '\u2028', '\u2029':
			return ' '
		default:
			return r
		}
	}, value)
	value = trimASCIISpaces(collapseSpaces(value))
	return escapeMarkdown(value)
}

func collapseSpaces(value string) string {
	var out strings.Builder
	space := false
	for _, r := range value {
		if r == ' ' {
			if !space {
				out.WriteByte(' ')
			}
			space = true
			continue
		}
		space = false
		out.WriteRune(r)
	}
	return out.String()
}

func escapeMarkdown(value string) string {
	var out strings.Builder
	for _, r := range value {
		if strings.ContainsRune("`\\*_{}[]<>()#+-.!|", r) {
			out.WriteByte('\\')
		}
		out.WriteRune(r)
	}
	return out.String()
}

func escapeDestination(value string) string {
	var out strings.Builder
	for _, b := range []byte(value) {
		if b == ' ' || b == '(' || b == ')' || b < 0x20 || b == 0x7f {
			out.WriteString(fmt.Sprintf("%%%02X", b))
			continue
		}
		out.WriteByte(b)
	}
	return out.String()
}

func contactLabel(kind string) string {
	switch kind {
	case "email":
		return "Email"
	case "phone":
		return "Phone"
	case "location":
		return "Location"
	case "website":
		return "Website"
	case "linkedin":
		return "LinkedIn"
	case "github":
		return "GitHub"
	case "twitter":
		return "Twitter"
	default:
		return "Detail"
	}
}

func isLinkContact(kind string) bool {
	return kind == "website" || kind == "linkedin" || kind == "github" || kind == "twitter"
}

func metadata(values ...string) string { return strings.Join(nonempty(values), " · ") }
func place(city, country *string) string {
	return strings.Join(nonempty([]string{plainPointer(city), plainPointer(country)}), ", ")
}

func dateRange(value *publicresume.PublicDateRange, format schema.DateFormat) string {
	if value == nil {
		return ""
	}
	end := ""
	if value.Present {
		end = "Present"
	} else {
		end = yearMonth(value.End, format)
	}
	if end == "" {
		return yearMonth(&value.Start, format)
	}
	return yearMonth(&value.Start, format) + " – " + end
}

func yearMonth(value *publicresume.PublicYearMonth, format schema.DateFormat) string {
	if value == nil {
		return ""
	}
	year := strconv.FormatInt(value.Y, 10)
	if value.M == nil || format == schema.Yyyy {
		return year
	}
	month := *value.M
	if format == schema.MonYYYY {
		names := []string{"", "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
		if month >= 1 && month <= 12 {
			return names[month] + " " + year
		}
	}
	return fmt.Sprintf("%02d/%s", month, year)
}

func richPointer(value *string) string {
	if value == nil || *value == "" {
		return ""
	}
	document, err := html.Parse(strings.NewReader(*value))
	if err != nil {
		return ""
	}
	body := findElement(document, "body")
	if body == nil {
		return ""
	}
	blocks := richNodes(children(body), 0)
	return strings.Join(nonempty(blocks), "\n\n")
}

func richNodes(nodes []*html.Node, indent int) []string {
	blocks := []string{}
	for _, node := range nodes {
		switch node.Type {
		case html.TextNode:
			if text := richText(node.Data); text != "" {
				blocks = append(blocks, text)
			}
		case html.ElementNode:
			switch node.Data {
			case "p":
				if text := richInlineChildren(node); text != "" {
					blocks = append(blocks, text)
				}
			case "ul", "ol":
				if list := richList(node, indent); list != "" {
					blocks = append(blocks, list)
				}
			case "br":
				blocks = append(blocks, "\\\n")
			default:
				blocks = append(blocks, richNodes(children(node), indent)...)
			}
		}
	}
	return blocks
}

func richInlineChildren(node *html.Node) string {
	var out strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		out.WriteString(richInline(child))
	}
	return trimASCIISpaces(out.String())
}

func richInline(node *html.Node) string {
	switch node.Type {
	case html.TextNode:
		return richText(node.Data)
	case html.ElementNode:
		if node.Data == "br" {
			return "\\\n"
		}
		content := richInlineChildren(node)
		switch node.Data {
		case "strong":
			return "**" + content + "**"
		case "em":
			return "*" + content + "*"
		case "a":
			if content == "" {
				return ""
			}
			for _, attr := range node.Attr {
				if attr.Key == "href" {
					return link(content, attr.Val)
				}
			}
		}
		return content
	}
	return ""
}

func richText(value string) string {
	value = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == '\t' || r == '\f' {
			return ' '
		}
		return r
	}, value)
	return escapeMarkdown(collapseSpaces(value))
}

func richList(node *html.Node, indent int) string {
	lines := []string{}
	ordered := node.Data == "ol"
	index := 1
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode || child.Data != "li" {
			continue
		}
		prefix := "- "
		if ordered {
			prefix = strconv.Itoa(index) + ". "
			index++
		}
		var inline strings.Builder
		nested := []string{}
		for grandchild := child.FirstChild; grandchild != nil; grandchild = grandchild.NextSibling {
			if grandchild.Type == html.ElementNode && (grandchild.Data == "ul" || grandchild.Data == "ol") {
				nested = append(nested, richList(grandchild, indent+1))
				continue
			}
			inline.WriteString(richInline(grandchild))
		}
		text := trimASCIISpaces(inline.String())
		line := strings.Repeat("  ", indent) + prefix + text
		if text != "" {
			lines = append(lines, line)
		}
		lines = append(lines, nested...)
	}
	return strings.Join(lines, "\n")
}

func children(node *html.Node) []*html.Node {
	var out []*html.Node
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		out = append(out, child)
	}
	return out
}

func findElement(node *html.Node, name string) *html.Node {
	if node.Type == html.ElementNode && node.Data == name {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findElement(child, name); found != nil {
			return found
		}
	}
	return nil
}
func nonempty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimASCIISpaces(value) != "" {
			out = append(out, value)
		}
	}
	return out
}

func trimASCIISpaces(value string) string { return strings.Trim(value, " ") }
