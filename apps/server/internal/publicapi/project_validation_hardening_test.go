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

// TestProjectValidators_StyleContentIsEmptyOrNone proves the page style may
// set content only to an empty string or none, so no text can enter the page
// through CSS.
func TestProjectValidators_StyleContentIsEmptyOrNone(t *testing.T) {
	origin := mustPublicOrigin(t)
	tc := readProjectCorpus(t)["project-en-every-field"]
	request := projectCorpusPage(t, tc)
	for css, want := range map[string]string{
		`.a::before{content:""}`:                                           "",
		`.a::before{content:''}`:                                           "",
		`.a::before{content: none}`:                                        "",
		`.a::before{content:'' !important}`:                                "",
		`.a{justify-content:center;align-content:start;place-content:end}`: "",
		`.a::before{content:"private@example.test"}`:                       "style_content",
		`.a::after{content:'x'}`:                                           "style_content",
		`.a::before{content:attr(data-x)}`:                                 "style_content",
		`.a::before{ CONTENT : counter(x) }`:                               "style_content",
		`.a::before{content:/**/"x"}`:                                      "style_content",
		`.a::before{color:red;content:"x"}`:                                "style_content",
		`.a::before{content:none "x"}`:                                     "style_content",
	} {
		source := replaceOnce(tc.HTML, "<style>/* project page style */</style>", "<style>"+css+"</style>")
		if got := projectHTMLRejection([]byte(source), request, origin); got != want {
			t.Errorf("style %s: rejection = %q, want %q", css, got, want)
		}
	}
	source := replaceOnce(tc.HTML, "<h1>", `<h1 style="content:'x'">`)
	if got := projectHTMLRejection([]byte(source), request, origin); got != "style_content" {
		t.Errorf("style attribute content: rejection = %q, want style_content", got)
	}
	list := readProjectCorpus(t)["list-vi-two-projects"]
	listSource := replaceOnce(list.HTML, "<style>/* project page style */</style>", `<style>.a::before{content:"x"}</style>`)
	if got := projectListHTMLRejection([]byte(listSource), projectCorpusList(t, list), origin); got != "style_content" {
		t.Errorf("list style content: rejection = %q, want style_content", got)
	}
	if strings.Contains(tc.HTML, "content:") {
		t.Fatal("the corpus reference style already sets content")
	}
}
