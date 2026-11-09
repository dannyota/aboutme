package publicformat

import (
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

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
