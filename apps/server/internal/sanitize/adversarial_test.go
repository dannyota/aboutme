package sanitize_test

import (
	"fmt"
	"math/rand"
	"net/url"
	"strings"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/sanitize"
	schemagen "github.com/dannyota/aboutme/packages/schema/gen/go"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	blindPropertySeed  int64 = 0x5033474f5a17
	blindPropertyCases       = 2048
)

type blindPolicy struct {
	allowedTags       map[string]struct{}
	allowedAttributes map[string]map[string]struct{}
	allowedSchemes    map[string]struct{}
	forbiddenPrefixes []string
	externalRel       string
}

func newBlindPolicy(t *testing.T) blindPolicy {
	t.Helper()

	policy := blindPolicy{
		allowedTags:       make(map[string]struct{}, len(schemagen.AllowedTags)),
		allowedAttributes: make(map[string]map[string]struct{}, len(schemagen.AllowedAttributes)),
		allowedSchemes:    make(map[string]struct{}, len(schemagen.AllowedURLSchemes)),
		forbiddenPrefixes: make([]string, 0, len(schemagen.ForbiddenAttributePrefixes)),
		externalRel:       schemagen.ExternalRel,
	}
	for _, tag := range schemagen.AllowedTags {
		tag = strings.ToLower(tag)
		if _, duplicate := policy.allowedTags[tag]; duplicate {
			t.Fatalf("blind policy: duplicate allowed tag %q", tag)
		}
		policy.allowedTags[tag] = struct{}{}
	}
	for tag, attributes := range schemagen.AllowedAttributes {
		tag = strings.ToLower(tag)
		allowed := make(map[string]struct{}, len(attributes))
		for _, attribute := range attributes {
			attribute = strings.ToLower(attribute)
			if _, duplicate := allowed[attribute]; duplicate {
				t.Fatalf("blind policy: duplicate %q attribute %q", tag, attribute)
			}
			allowed[attribute] = struct{}{}
		}
		policy.allowedAttributes[tag] = allowed
	}
	for _, scheme := range schemagen.AllowedURLSchemes {
		scheme = strings.ToLower(scheme)
		if _, duplicate := policy.allowedSchemes[scheme]; duplicate {
			t.Fatalf("blind policy: duplicate allowed URL scheme %q", scheme)
		}
		policy.allowedSchemes[scheme] = struct{}{}
	}
	for _, prefix := range schemagen.ForbiddenAttributePrefixes {
		policy.forbiddenPrefixes = append(policy.forbiddenPrefixes, strings.ToLower(prefix))
	}

	return policy
}

func (p blindPolicy) check(fragment string) error {
	nodes, err := parseFragment(fragment)
	if err != nil {
		return fmt.Errorf("parse fragment: %w", err)
	}
	for _, node := range nodes {
		if err := p.checkNode(node); err != nil {
			return err
		}
	}
	return nil
}

func (p blindPolicy) checkNode(node *html.Node) error {
	switch node.Type {
	case html.TextNode, html.CommentNode:
		return nil
	case html.ElementNode:
		if node.Namespace != "" {
			return fmt.Errorf("foreign namespace %q on <%s>", node.Namespace, node.Data)
		}
		tag := strings.ToLower(node.Data)
		if _, ok := p.allowedTags[tag]; !ok {
			return fmt.Errorf("tag <%s> is not allowed", node.Data)
		}

		values := make(map[string]string, len(node.Attr))
		for _, attribute := range node.Attr {
			if attribute.Namespace != "" {
				return fmt.Errorf("attribute namespace %q on <%s %s>", attribute.Namespace, tag, attribute.Key)
			}
			key := strings.ToLower(attribute.Key)
			if _, duplicate := values[key]; duplicate {
				return fmt.Errorf("duplicate attribute %q on <%s>", key, tag)
			}
			for _, prefix := range p.forbiddenPrefixes {
				if strings.HasPrefix(key, prefix) {
					return fmt.Errorf("attribute %q on <%s> has forbidden prefix %q", key, tag, prefix)
				}
			}
			if _, ok := p.allowedAttributes[tag][key]; !ok {
				return fmt.Errorf("attribute %q is not allowed on <%s>", key, tag)
			}
			values[key] = attribute.Val
		}

		if tag == "a" {
			rel, ok := values["rel"]
			if !ok {
				return fmt.Errorf("<a> is missing rel")
			}
			if rel != p.externalRel {
				return fmt.Errorf("<a> rel is %q, want %q", rel, p.externalRel)
			}
			if target, ok := values["target"]; ok && target != "_blank" {
				return fmt.Errorf("<a> target is %q, want _blank or absent", target)
			}
			if href, ok := values["href"]; ok {
				if err := p.checkURL(href); err != nil {
					return fmt.Errorf("<a> href %q: %w", href, err)
				}
			}
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := p.checkNode(child); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("node type %d is not admitted", node.Type)
	}
}

func (p blindPolicy) checkURL(raw string) error {
	// The URL Standard removes leading/trailing C0 controls and spaces, and
	// removes ASCII tab/newline/carriage-return characters before parsing.
	// Apply that browser-facing normalization before deciding the scheme.
	normalized := normalizeBrowserURL(raw)
	parsed, err := url.Parse(normalized)
	if err != nil {
		return fmt.Errorf("URL does not parse: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme == "" {
		return fmt.Errorf("URL has no explicit scheme")
	}
	if _, ok := p.allowedSchemes[scheme]; !ok {
		return fmt.Errorf("scheme %q is not allowed", scheme)
	}
	return nil
}

func normalizeBrowserURL(raw string) string {
	start, end := 0, len(raw)
	for start < end && raw[start] <= 0x20 {
		start++
	}
	for end > start && raw[end-1] <= 0x20 {
		end--
	}

	var normalized strings.Builder
	normalized.Grow(end - start)
	for i := start; i < end; i++ {
		switch raw[i] {
		case '\t', '\n', '\r':
			continue
		default:
			normalized.WriteByte(raw[i])
		}
	}
	return normalized.String()
}

func parseFragment(fragment string) ([]*html.Node, error) {
	context := &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: "div"}
	return html.ParseFragment(strings.NewReader(fragment), context)
}

func parserRoundTrip(fragment string) (string, error) {
	nodes, err := parseFragment(fragment)
	if err != nil {
		return "", err
	}

	var rendered strings.Builder
	for _, node := range nodes {
		if err := html.Render(&rendered, node); err != nil {
			return "", err
		}
	}
	return rendered.String(), nil
}

func TestBlindNeutralizationPredicateNegativeControls(t *testing.T) {
	policy := newBlindPolicy(t)

	unsafeCorpusCases := 0
	for _, payload := range schemagen.HostileCorpus {
		err := policy.check(payload.Payload)
		if payload.ID == "js-scheme-bare" {
			if err != nil {
				t.Errorf("raw corpus %q is dangerous-looking text, but the predicate rejected it: %v", payload.ID, err)
			}
			continue
		}
		unsafeCorpusCases++
		if err == nil {
			t.Errorf("raw corpus %q contains an active policy violation, but the predicate accepted it", payload.ID)
		}
	}
	if unsafeCorpusCases == 0 {
		t.Fatal("negative control exercised no unsafe corpus cases")
	}

	violations := map[string]string{
		"forbidden element":     `<script>alert(1)</script>`,
		"forbidden prefix":      `<p onclick="alert(1)">text</p>`,
		"unlisted attribute":    `<p style="background:url(javascript:alert(1))">text</p>`,
		"forbidden scheme":      `<a href="javascript:alert(1)" rel="noopener noreferrer">text</a>`,
		"relative URL":          `<a href="/relative" rel="noopener noreferrer">text</a>`,
		"protocol-relative URL": `<a href="//example.test" rel="noopener noreferrer">text</a>`,
		"missing rel":           `<a href="https://example.test">text</a>`,
		"forged rel":            `<a href="https://example.test" rel="opener">text</a>`,
		"forged target":         `<a href="https://example.test" rel="noopener noreferrer" target="_self">text</a>`,
		"foreign namespace":     `<svg><a href="https://example.test">text</a></svg>`,
	}
	for name, fragment := range violations {
		t.Run(name, func(t *testing.T) {
			if err := policy.check(fragment); err == nil {
				t.Fatalf("predicate accepted hand-built violation %q", fragment)
			}
		})
	}

	bareText := `javascript:alert(1) &lt;img src=x onerror=alert(1)&gt; data:text/html,&lt;script&gt;alert(1)&lt;/script&gt;`
	if err := policy.check(bareText); err != nil {
		t.Fatalf("predicate rejected dangerous-looking bare text: %v", err)
	}
	nodes, err := parseFragment(bareText)
	if err != nil {
		t.Fatalf("parse dangerous-looking bare text: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Type != html.TextNode {
		t.Fatalf("dangerous-looking bare text parsed as active markup: %#v", nodes)
	}
}

func TestRichTextBlindHostileCorpus(t *testing.T) {
	policy := newBlindPolicy(t)
	if sanitize.AllowlistVersion != schemagen.SanitizerAllowlistVersion {
		t.Fatalf("sanitizer policy version = %d, generated policy version = %d", sanitize.AllowlistVersion, schemagen.SanitizerAllowlistVersion)
	}

	for _, payload := range schemagen.HostileCorpus {
		t.Run(payload.ID, func(t *testing.T) {
			assertSanitizerProperties(t, policy, payload.Payload)
		})
	}
}

func TestRichTextBlindSpecDerivedPayloads(t *testing.T) {
	policy := newBlindPolicy(t)
	payloads := map[string]string{
		"mXSS noscript pivot":          `<noscript><p title="</noscript><img src=x onerror=alert(1)>">pivot</p></noscript>`,
		"mXSS template pivot":          `<template><p><svg><foreignObject><style><!--</style><img title="--><img src=x onerror=alert(1)>"></foreignObject></svg></p></template>`,
		"mXSS foreign-content pivot":   `<math><mtext><table><mglyph><style><!--</style><img title="--><img src=x onerror=alert(1)>"></mglyph></table></mtext></math>`,
		"URL-encoded colon":            `<a href="javascript%3Aalert(1)" rel="noopener noreferrer">encoded</a>`,
		"mixed entity and case":        `<a href="JaVa&#x53;CrIpT&#x3a;alert(1)" rel="noopener noreferrer">mixed</a>`,
		"entity and whitespace scheme": `<a href="&#x09;DaTa&#58;text/html,&lt;script&gt;alert(1)&lt;/script&gt;" rel="noopener noreferrer">mixed</a>`,
		"formaction smuggling":         `<form><button formaction="javascript:alert(1)">submit</button></form>`,
		"srcdoc smuggling":             `<iframe srcdoc="&lt;script&gt;alert(1)&lt;/script&gt;"></iframe>`,
		"xlink href smuggling":         `<svg><a xlink:href="javascript:alert(1)"><text>svg link</text></a></svg>`,
		"style smuggling":              `<p style="background-image:url(javascript:alert(1))">styled</p>`,
		"math namespace confusion":     `<math><mtext><a href="javascript:alert(1)">math link</a></mtext></math>`,
		"SVG namespace confusion":      `<svg><a href="https://example.test" rel="noopener noreferrer"><text>svg link</text></a></svg>`,
		"rel token forgery":            `<a href="https://example.test" rel="noopener noreferrer opener" target="_blank">forged</a>`,
		"target forgery":               `<a href="https://example.test" rel="noopener noreferrer" target="_parent">forged</a>`,
		"attribute duplication":        `<a href="javascript:alert(1)" href="https://example.test" rel="noopener noreferrer">duplicate</a>`,
	}

	for name, payload := range payloads {
		t.Run(name, func(t *testing.T) {
			if err := policy.check(payload); err == nil {
				t.Fatalf("spec-derived adversarial input unexpectedly satisfies the blind predicate: %q", payload)
			}
			assertSanitizerProperties(t, policy, payload)
		})
	}
}

func TestRichTextBlindDeterministicProperties(t *testing.T) {
	policy := newBlindPolicy(t)
	random := rand.New(rand.NewSource(blindPropertySeed))

	for caseIndex := 0; caseIndex < blindPropertyCases; caseIndex++ {
		input := randomHTMLLikeString(random)
		output := sanitize.RichText(input)
		if err := policy.check(output); err != nil {
			t.Fatalf("seed=%#x case=%d sanitizer output violates policy: %v\ninput=%q\noutput=%q", blindPropertySeed, caseIndex, err, input, output)
		}
		if second := sanitize.RichText(output); second != output {
			t.Fatalf("seed=%#x case=%d sanitizer is not idempotent\ninput=%q\nfirst=%q\nsecond=%q", blindPropertySeed, caseIndex, input, output, second)
		}

		parserOutput, err := parserRoundTrip(output)
		if err != nil {
			t.Fatalf("seed=%#x case=%d parser round trip failed: %v\noutput=%q", blindPropertySeed, caseIndex, err, output)
		}
		if err := policy.check(parserOutput); err != nil {
			t.Fatalf("seed=%#x case=%d parser round trip reintroduced a violation: %v\ninput=%q\noutput=%q\nround_trip=%q", blindPropertySeed, caseIndex, err, input, output, parserOutput)
		}
		crossFeed := sanitize.RichText(parserOutput)
		if err := policy.check(crossFeed); err != nil {
			t.Fatalf("seed=%#x case=%d parser output fed back through the public sanitizer violates policy: %v\nround_trip=%q\ncross_feed=%q", blindPropertySeed, caseIndex, err, parserOutput, crossFeed)
		}
		if second := sanitize.RichText(crossFeed); second != crossFeed {
			t.Fatalf("seed=%#x case=%d parser-fed output is not idempotent\nfirst=%q\nsecond=%q", blindPropertySeed, caseIndex, crossFeed, second)
		}
	}
}

func assertSanitizerProperties(t *testing.T, policy blindPolicy, input string) {
	t.Helper()

	output := sanitize.RichText(input)
	if err := policy.check(output); err != nil {
		t.Fatalf("sanitizer output violates blind policy: %v\ninput=%q\noutput=%q", err, input, output)
	}
	if second := sanitize.RichText(output); second != output {
		t.Fatalf("sanitizer is not idempotent\ninput=%q\nfirst=%q\nsecond=%q", input, output, second)
	}

	parserOutput, err := parserRoundTrip(output)
	if err != nil {
		t.Fatalf("parser round trip: %v", err)
	}
	if err := policy.check(parserOutput); err != nil {
		t.Fatalf("parser round trip reintroduced a violation: %v\ninput=%q\noutput=%q\nround_trip=%q", err, input, output, parserOutput)
	}
	crossFeed := sanitize.RichText(parserOutput)
	if err := policy.check(crossFeed); err != nil {
		t.Fatalf("parser output fed back through the public sanitizer violates policy: %v\nround_trip=%q\ncross_feed=%q", err, parserOutput, crossFeed)
	}
}

func randomHTMLLikeString(random *rand.Rand) string {
	fragments := [...]string{
		`<p>`, `</p>`, `<br>`, `<strong>`, `</strong>`, `<em>`, `</em>`,
		`<a href="https://example.test" rel="noopener noreferrer" target="_blank">`, `</a>`,
		`<script>`, `</script>`, `<style>`, `</style>`, `<template>`, `</template>`,
		`<noscript>`, `</noscript>`, `<svg>`, `</svg>`, `<math>`, `</math>`,
		` href="javascript:alert(1)"`, ` href="//example.test"`, ` href="javascript%3Aalert(1)"`,
		` onclick="alert(1)"`, ` style="background:url(javascript:alert(1))"`,
		` srcdoc="&lt;script&gt;alert(1)&lt;/script&gt;"`, ` xlink:href="javascript:alert(1)"`,
		` rel="opener"`, ` target="_self"`, `<!--`, `-->`, `&lt;`, `&#x3a;`, `&amp;`,
		"\x00", "\t", "\n", "\r", "javascript:alert(1)", "data:text/html,",
	}

	parts := random.Intn(48)
	var generated strings.Builder
	for i := 0; i < parts; i++ {
		switch random.Intn(4) {
		case 0, 1:
			generated.WriteString(fragments[random.Intn(len(fragments))])
		case 2:
			generated.WriteByte(byte(random.Intn(256)))
		case 3:
			length := 1 + random.Intn(12)
			for j := 0; j < length; j++ {
				generated.WriteByte(byte(random.Intn(256)))
			}
		}
	}
	return generated.String()
}
