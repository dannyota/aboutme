package sanitizetest

import (
	"fmt"
	"net/url"
	"strings"

	schemagen "github.com/dannyota/aboutme/packages/schema/gen/go"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// AssertNeutralized verifies the generated rich-text structural policy.
func AssertNeutralized(fragment string) error {
	context := &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: "div"}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), context)
	if err != nil {
		return fmt.Errorf("parse fragment: %w", err)
	}

	allowedTags := stringSet(schemagen.AllowedTags)
	allowedSchemes := stringSet(schemagen.AllowedURLSchemes)
	for _, root := range nodes {
		if err := walk(root, func(node *html.Node) error {
			if node.Type != html.ElementNode {
				return nil
			}
			tag := strings.ToLower(node.Data)
			if _, ok := allowedTags[tag]; !ok {
				return fmt.Errorf("tag %q is not allowed", tag)
			}

			allowedAttrs := stringSet(schemagen.AllowedAttributes[tag])
			attributes := make(map[string]string, len(node.Attr))
			for _, attr := range node.Attr {
				name := strings.ToLower(attr.Key)
				if attr.Namespace != "" {
					return fmt.Errorf("attribute %q has namespace %q", name, attr.Namespace)
				}
				if _, ok := allowedAttrs[name]; !ok {
					return fmt.Errorf("attribute %s.%s is not allowed", tag, name)
				}
				for _, prefix := range schemagen.ForbiddenAttributePrefixes {
					if strings.HasPrefix(name, strings.ToLower(prefix)) {
						return fmt.Errorf("attribute %s.%s has forbidden prefix %q", tag, name, prefix)
					}
				}
				attributes[name] = attr.Val
			}

			if tag != "a" {
				return nil
			}
			if href, ok := attributes["href"]; ok {
				parsed, err := url.Parse(href)
				if err != nil || parsed.Scheme == "" {
					return fmt.Errorf("anchor href %q has no parseable explicit scheme", href)
				}
				if _, ok := allowedSchemes[strings.ToLower(parsed.Scheme)]; !ok {
					return fmt.Errorf("anchor href %q has forbidden scheme", href)
				}
			}
			if attributes["rel"] != schemagen.ExternalRel {
				return fmt.Errorf("anchor rel = %q, want %q", attributes["rel"], schemagen.ExternalRel)
			}
			if target, ok := attributes["target"]; ok && target != "_blank" {
				return fmt.Errorf("anchor target = %q, want _blank or absent", target)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func walk(node *html.Node, visit func(*html.Node) error) error {
	if err := visit(node); err != nil {
		return err
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if err := walk(child, visit); err != nil {
			return err
		}
	}
	return nil
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[strings.ToLower(value)] = struct{}{}
	}
	return set
}
