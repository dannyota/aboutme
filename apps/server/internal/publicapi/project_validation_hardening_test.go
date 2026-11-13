package publicapi

import (
	"strings"
	"testing"
)

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

const staticIcon = `<svg class="icon" viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" ` +
	`stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false" xmlns="http://www.w3.org/2000/svg">` +
	`<title>Icon</title><g><path d="M1 1h2"></path><circle cx="1" cy="1" r="1"></circle><rect x="1" y="1" width="2" height="2" rx="1" ry="1"></rect>` +
	`<line x1="0" y1="0" x2="1" y2="1"></line><polyline points="0,0 1,1"></polyline><polygon points="0,0 1,1 1,0"></polygon></g></svg>`

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

func projectModes(t *testing.T, edit func(source string) string) map[string]string {
	t.Helper()
	origin := mustPublicOrigin(t)
	corpus := readProjectCorpus(t)
	page, list := corpus["project-en-every-field"], corpus["list-vi-two-projects"]
	return map[string]string{
		"project":      projectHTMLRejection([]byte(edit(page.HTML)), projectCorpusPage(t, page), origin),
		"project-list": projectListHTMLRejection([]byte(edit(list.HTML)), projectCorpusList(t, list), origin),
	}
}

func TestProjectValidators_CarryNoInlineCSS(t *testing.T) {
	for name, test := range map[string]struct{ old, replacement, rule string }{
		"empty style element in the head":  {"</head>", "<style></style></head>", "style_element"},
		"harmless style element":           {"</head>", "<style>h1{color:red}</style></head>", "style_element"},
		"style element in the body":        {"</main>", "<style>h1{color:red}</style></main>", "style_element"},
		"style attribute":                  {"<h1>", `<h1 style="color:red">`, "style_attribute"},
		"empty style attribute":            {"<h1>", `<h1 style="">`, "style_attribute"},
		"style attribute with a resource":  {"<h1>", `<h1 style="background:url(/x)">`, "style_attribute"},
		"style attribute on an svg":        {"<svg ", `<svg style="color:red" `, "svg_attribute"},
		"content through a style element":  {"</head>", `<style>h1::before{content:"x"}</style></head>`, "style_element"},
		"marker text through an attribute": {"<h1>", `<h1 style='display:list-item;list-style-type:"x"'>`, "style_attribute"},
	} {
		for mode, rule := range projectModes(t, func(source string) string {
			return strings.Replace(source, test.old, test.replacement, 1)
		}) {
			if rule != test.rule {
				t.Errorf("%s in %s: rejection = %q, want %q", name, mode, rule, test.rule)
			}
		}
	}
}

func TestProjectValidators_LoadExactlyTheTwoStylesheets(t *testing.T) {
	const fonts = `<link rel="stylesheet" href="/_nuxt/assets/print-fonts.css?v=0123456789abcdef">`
	const pages = `<link rel="stylesheet" href="/_nuxt/assets/project-pages.css?v=0123456789abcdef">`
	for name, test := range map[string]struct{ old, replacement, rule string }{
		"as rendered":              {fonts, fonts, ""},
		"reversed order":           {fonts + pages, pages + fonts, ""},
		"missing fonts":            {fonts, "", "stylesheet"},
		"missing project styles":   {pages, "", "stylesheet"},
		"fonts twice":              {fonts, fonts + fonts, "stylesheet"},
		"project styles twice":     {pages, pages + pages, "stylesheet"},
		"different versions":       {pages, strings.Replace(pages, "0123456789abcdef", "fedcba9876543210", 1), "stylesheet"},
		"unversioned":              {pages, `<link rel="stylesheet" href="/_nuxt/assets/project-pages.css">`, "stylesheet"},
		"uppercase version":        {pages, strings.Replace(pages, "abcdef", "ABCDEF", 1), "stylesheet"},
		"short version":            {pages, strings.Replace(pages, "0123456789abcdef", "0123456789abcde", 1), "stylesheet"},
		"extra attribute":          {pages, strings.Replace(pages, "<link ", `<link media="all" `, 1), "stylesheet"},
		"resume template styles":   {pages, `<link rel="stylesheet" href="/_nuxt/assets/print.css?v=0123456789abcdef">`, "stylesheet"},
		"third stylesheet":         {"</head>", `<link rel="stylesheet" href="/_nuxt/assets/print.css?v=0123456789abcdef"></head>`, "stylesheet"},
		"other origin":             {pages, `<link rel="stylesheet" href="https://evil.example/_nuxt/assets/project-pages.css?v=0123456789abcdef">`, "stylesheet"},
		"alternate stylesheet":     {pages, strings.Replace(pages, `rel="stylesheet"`, `rel="alternate stylesheet"`, 1), "link_element"},
		"query beyond the version": {pages, strings.Replace(pages, "abcdef", "abcdef&x=1", 1), "stylesheet"},
	} {
		for mode, rule := range projectModes(t, func(source string) string {
			return replaceOnce(source, test.old, test.replacement)
		}) {
			if rule != test.rule {
				t.Errorf("%s in %s: rejection = %q, want %q", name, mode, rule, test.rule)
			}
		}
	}
}

func TestProjectValidators_AcceptTheFormatDetectionMeta(t *testing.T) {
	const element = `<meta name="format-detection" content="telephone=no, date=no, address=no, email=no">`
	for name, test := range map[string]struct{ replacement, rule string }{
		"as rendered":   {element, ""},
		"absent":        {"", ""},
		"twice":         {element + element, "meta_format_detection"},
		"other content": {`<meta name="format-detection" content="telephone=no">`, "meta_format_detection"},
	} {
		for mode, rule := range projectModes(t, func(source string) string {
			return replaceOnce(source, element, test.replacement)
		}) {
			if rule != test.rule {
				t.Errorf("%s format-detection in %s: rejection = %q, want %q", name, mode, rule, test.rule)
			}
		}
	}
}

func TestProjectValidators_SVGTransformIsNumericOnly(t *testing.T) {
	for value, accepted := range map[string]bool{
		"rotate(-8 15 16)":            true,
		"rotate(45)":                  true,
		"rotate(1,2,3)":               true,
		"translate(1 2)":              true,
		"translate(-.5)":              true,
		"scale(1.5, 2)":               true,
		"rotate(1) translate(2,3)":    true,
		"rotate(1),scale(+2)":         true,
		" translate(1)scale(2) ":      true,
		"rotate(1) url(#x)":           false,
		"url(#x)":                     false,
		"matrix(1,0,0,1,0,0)":         false,
		"rotate(var(--a))":            false,
		"translate(1px)":              false,
		"rotate(1deg)":                false,
		"rotate(1e3)":                 false,
		"skewX(1)":                    false,
		"ROTATE(1)":                   false,
		"rotate()":                    false,
		"rotate(1 2)":                 false,
		"translate(1 2 3)":            false,
		"scale(1,,2)":                 false,
		"rotate(calc(1 + 2))":         false,
		"rotate(1)\u0000translate(1)": false,
		"":                            false,
	} {
		want := "svg_attribute"
		if accepted {
			want = ""
		}
		for mode, rule := range projectValidatorRules(t, `<svg viewBox="0 0 1 1"><g transform="`+value+`"><path d="M0 0"></path></g></svg>`) {
			if rule != want {
				t.Errorf("transform %q in %s: rejection = %q, want %q", value, mode, rule, want)
			}
		}
	}
	for mode, rule := range projectValidatorRules(t, `<svg viewBox="0 0 1 1"><image transform="rotate(1)"></image></svg>`) {
		if rule != "svg_element" {
			t.Errorf("transform on an image in %s: rejection = %q, want svg_element", mode, rule)
		}
	}
}
