package publicapi

import (
	"strings"
	"testing"
)

// TestProjectHTMLRejection_EqualRepositoryAndDemoRenderTwoButtons proves an
// external link may appear once per request field that supplies it, so a
// project whose repository and demo are the same address keeps both buttons.
func TestProjectHTMLRejection_EqualRepositoryAndDemoRenderTwoButtons(t *testing.T) {
	origin := mustPublicOrigin(t)
	tc := readProjectCorpus(t)["project-en-every-field"]
	request := projectCorpusPage(t, tc)
	request.Project.DemoURL = request.Project.RepositoryURL
	source := replaceOnce(tc.HTML, `href="https://ada.example/demo"`, `href="`+request.Project.RepositoryURL+`"`)
	if rule := projectHTMLRejection([]byte(source), request, origin); rule != "" {
		t.Fatalf("rejection = %q, want two buttons for one address supplied twice", rule)
	}
	third := `<a class="project-external" href="` + request.Project.RepositoryURL + `" rel="noopener noreferrer nofollow">Again</a>`
	if rule := projectHTMLRejection([]byte(replaceOnce(source, "</main>", third+"</main>")), request, origin); rule != "external_link" {
		t.Fatalf("third button rejection = %q, want external_link", rule)
	}
}

// projectValidatorCases applies one addition before </main> to both corpus
// modes and returns each mode's rejection.
func projectValidatorRules(t *testing.T, addition string) map[string]string {
	t.Helper()
	origin := mustPublicOrigin(t)
	corpus := readProjectCorpus(t)
	page, list := corpus["project-en-every-field"], corpus["list-vi-two-projects"]
	return map[string]string{
		"project":      projectHTMLRejection([]byte(replaceOnce(page.HTML, "</main>", addition+"</main>")), projectCorpusPage(t, page), origin),
		"project-list": projectListHTMLRejection([]byte(replaceOnce(list.HTML, "</main>", addition+"</main>")), projectCorpusList(t, list), origin),
	}
}

// staticIcon uses every SVG element and attribute a static icon may carry.
const staticIcon = `<svg class="icon" viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" ` +
	`stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false" xmlns="http://www.w3.org/2000/svg">` +
	`<title>Icon</title><g><path d="M1 1h2"></path><circle cx="1" cy="1" r="1"></circle><rect x="1" y="1" width="2" height="2" rx="1" ry="1"></rect>` +
	`<line x1="0" y1="0" x2="1" y2="1"></line><polyline points="0,0 1,1"></polyline><polygon points="0,0 1,1 1,0"></polygon></g></svg>`

// TestProjectValidators_SVGIsStaticIconsOnly proves both validators accept a
// static icon and reject every other SVG element or attribute, so SVG
// cannot embed, animate or point at a resource.
func TestProjectValidators_SVGIsStaticIconsOnly(t *testing.T) {
	for mode, rule := range projectValidatorRules(t, staticIcon) {
		if rule != "" {
			t.Fatalf("%s: static icon rejection = %q, want accepted", mode, rule)
		}
	}
	cases := map[string]string{
		"review example": `<svg><image width="100" height="100"><set attributeName="href" to="/api/v1/public/resumes/other-resume/photo" begin="0s"/></image></svg>`,
	}
	for _, element := range []string{
		"image", "use", "set", "animate", "animateMotion", "animateTransform", "animateColor", "mpath", "discard",
		"foreignObject", "a", "style", "filter", "feImage", "pattern", "mask", "clipPath", "defs", "symbol", "marker", "text", "view",
	} {
		cases[element] = "<svg><" + element + "></" + element + "></svg>"
	}
	for name, addition := range cases {
		for mode, rule := range projectValidatorRules(t, addition) {
			if rule != "svg_element" {
				t.Errorf("%s in %s: rejection = %q, want svg_element", name, mode, rule)
			}
		}
	}
	for name, attribute := range map[string]string{
		"href": `href="#icon"`, "xlink:href": `xlink:href="#icon"`, "style": `style="fill:red"`, "event": `onload="x()"`,
		"id": `id="icon"`, "fill url": `fill="url(#paint)"`, "stroke url": `stroke="URL(https://evil.example/p.svg#p)"`, "mask": `mask="url(#m)"`,
	} {
		for mode, rule := range projectValidatorRules(t, `<svg viewBox="0 0 1 1"><path d="M0 0" `+attribute+`></path></svg>`) {
			if rule != "svg_attribute" {
				t.Errorf("%s in %s: rejection = %q, want svg_attribute", name, mode, rule)
			}
		}
	}
}

// TestProjectValidators_StyleDeclaresNoContent proves project page CSS may
// not declare content at all, whatever its value or where the declaration
// sits, in both modes, in the style element and in style attributes, while
// names that only contain the word still pass.
func TestProjectValidators_StyleDeclaresNoContent(t *testing.T) {
	origin := mustPublicOrigin(t)
	corpus := readProjectCorpus(t)
	page, list := corpus["project-en-every-field"], corpus["list-vi-two-projects"]
	modes := map[string]struct {
		source   string
		validate func(string) string
	}{
		"project": {page.HTML, func(source string) string {
			return projectHTMLRejection([]byte(source), projectCorpusPage(t, page), origin)
		}},
		"project-list": {list.HTML, func(source string) string {
			return projectListHTMLRejection([]byte(source), projectCorpusList(t, list), origin)
		}},
	}
	styles := map[string]string{
		"after a nested rule":         `h1::before{@media all{}content:"unbound text"}`,
		"after a closing brace":       `h1::before{h2{}content:"x"}`,
		"after a semicolon":           `h1::before{color:red;content:none}`,
		"after an opening brace":      `h1::before{content :""}`,
		"uppercase":                   `h1::before{CONTENT:none}`,
		"empty string":                `h1::before{content:""}`,
		"none":                        `h1::before{content:none}`,
		"after a comment":             `h1::before{/**/content:''}`,
		"value inside a string":       `h1::before{--a:"x";content:"y"}`,
		"start of the style":          `content:none`,
		"before whitespace and colon": "h1::before{content\n:none}",
		"text in another declaration": `h1::before{--a:"}content:x"}`,
	}
	attributes := map[string]string{
		"start of the attribute": `content:none`,
		"empty string":           `content:""`,
		"after a semicolon":      `color:red;content:none`,
		"uppercase":              `CONTENT : none`,
	}
	allowed := map[string]string{
		"justify-content": `.a{justify-content:center;align-content:start;place-content:end}`,
		"class name":      `.content:hover{color:red}`,
		"id":              `#content{color:red}`,
		"underscore":      `.a{--my_content:1}`,
	}
	for mode, m := range modes {
		for name, css := range styles {
			if got := m.validate(replaceOnce(m.source, "<style>/* project page style */</style>", "<style>"+css+"</style>")); got != "style_content" {
				t.Errorf("%s style %s: rejection = %q, want style_content", mode, name, got)
			}
		}
		for name, declarations := range attributes {
			if got := m.validate(replaceOnce(m.source, "<h1>", `<h1 style='`+declarations+`'>`)); got != "style_content" {
				t.Errorf("%s style attribute %s: rejection = %q, want style_content", mode, name, got)
			}
		}
		for name, css := range allowed {
			if got := m.validate(replaceOnce(m.source, "<style>/* project page style */</style>", "<style>"+css+"</style>")); got != "" {
				t.Errorf("%s style %s: rejection = %q, want accepted", mode, name, got)
			}
		}
	}
	if strings.Contains(page.HTML, "content:") || strings.Contains(list.HTML, "content:") {
		t.Fatal("the corpus reference style already declares content")
	}
}

// TestProjectValidators_AcceptTheFormatDetectionMeta proves both modes accept
// the resume page's format-detection element exactly, at most once.
func TestProjectValidators_AcceptTheFormatDetectionMeta(t *testing.T) {
	const element = `<meta name="format-detection" content="telephone=no, date=no, address=no, email=no">`
	for _, addition := range []struct {
		name, head, want string
	}{
		{"once", element, ""},
		{"twice", element + element, "meta_format_detection"},
		{"other content", `<meta name="format-detection" content="telephone=no">`, "meta_format_detection"},
	} {
		for mode, rule := range projectHeadRules(t, addition.head) {
			if rule != addition.want {
				t.Errorf("%s format-detection in %s: rejection = %q, want %q", addition.name, mode, rule, addition.want)
			}
		}
	}
}

// projectHeadRules adds head elements after the viewport meta of both corpus
// modes and returns each mode's rejection.
func projectHeadRules(t *testing.T, head string) map[string]string {
	t.Helper()
	origin := mustPublicOrigin(t)
	corpus := readProjectCorpus(t)
	page, list := corpus["project-en-every-field"], corpus["list-vi-two-projects"]
	viewport := `<meta name="viewport" content="width=device-width, initial-scale=1">`
	return map[string]string{
		"project":      projectHTMLRejection([]byte(replaceOnce(page.HTML, viewport, viewport+head)), projectCorpusPage(t, page), origin),
		"project-list": projectListHTMLRejection([]byte(replaceOnce(list.HTML, viewport, viewport+head)), projectCorpusList(t, list), origin),
	}
}

// TestProjectValidators_QuotedCommentMarkersStayInStrings proves the style
// checks read comment markers inside a CSS string as string text, and reject
// an unterminated string or comment, in both modes, in the style element and
// in style attributes.
func TestProjectValidators_QuotedCommentMarkersStayInStrings(t *testing.T) {
	origin := mustPublicOrigin(t)
	corpus := readProjectCorpus(t)
	page, list := corpus["project-en-every-field"], corpus["list-vi-two-projects"]
	modes := map[string]struct {
		source   string
		validate func(string) string
	}{
		"project": {page.HTML, func(source string) string {
			return projectHTMLRejection([]byte(source), projectCorpusPage(t, page), origin)
		}},
		"project-list": {list.HTML, func(source string) string {
			return projectListHTMLRejection([]byte(source), projectCorpusList(t, list), origin)
		}},
	}
	const photo = "url(/api/v1/public/resumes/other-resume/photo)"
	for name, test := range map[string]struct {
		rule, declarations, styleRule, attributeRule string
	}{
		"comment markers in content":    {"h1::before", `content:"/*private@example.test*/"`, "style_content", "style_content"},
		"comment markers around a url":  {"h1", `--a:"/*";background-image:` + photo + `;--b:"*/"`, "style_element", "inline_style"},
		"escaped quote before a marker": {"h1", `--a:"\"/*";background-image:` + photo + `;--b:"*/"`, "style_element", "inline_style"},
		"unterminated string":           {"h1", `color:red;--a:"unterminated`, "style_element", "inline_style"},
		"unterminated comment":          {"h1", `color:red;/* unterminated`, "style_element", "inline_style"},
	} {
		for mode, m := range modes {
			styled := replaceOnce(m.source, "<style>/* project page style */</style>", "<style>"+test.rule+"{"+test.declarations+"}</style>")
			if got := m.validate(styled); got != test.styleRule {
				t.Errorf("%s in a %s style element: rejection = %q, want %q", name, mode, got, test.styleRule)
			}
			attributed := replaceOnce(m.source, "<h1>", `<h1 style='`+test.declarations+`'>`)
			if got := m.validate(attributed); got != test.attributeRule {
				t.Errorf("%s in a %s style attribute: rejection = %q, want %q", name, mode, got, test.attributeRule)
			}
		}
	}
}
