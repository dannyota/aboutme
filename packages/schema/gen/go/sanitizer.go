// Code generated from validation/sanitizer-allowlist.v1.json and validation/hostile-corpus.json. DO NOT EDIT.

package schema

const (
	SanitizerAllowlistVersion = 1
	ExternalRel               = "noopener noreferrer"
)

var AllowedTags = []string{
	"p",
	"br",
	"strong",
	"em",
	"u",
	"ol",
	"ul",
	"li",
	"a",
}

var AllowedAttributes = map[string][]string{
	"a": {
		"href",
		"rel",
		"target",
	},
}

var AllowedURLSchemes = []string{
	"https",
	"mailto",
	"tel",
}

var ForbiddenTags = []string{
	"script",
	"style",
	"iframe",
	"svg",
	"img",
	"object",
	"embed",
	"form",
	"input",
	"link",
	"meta",
	"base",
}

var ForbiddenAttributePrefixes = []string{
	"on",
}

var ForbiddenURLSchemes = []string{
	"javascript",
	"data",
	"vbscript",
	"file",
}

type HostilePayload struct {
	ID       string
	Category string
	Payload  string
}

var HostileCorpus = []HostilePayload{
	{
		ID:       "js-scheme-bare",
		Category: "javascript-scheme",
		Payload:  "javascript:alert(1)",
	},
	{
		ID:       "js-scheme-in-anchor-href",
		Category: "javascript-scheme",
		Payload:  "<a href=\"javascript:alert(document.cookie)\">click me</a>",
	},
	{
		ID:       "js-scheme-mixed-case",
		Category: "obfuscated-scheme",
		Payload:  "<a href=\"JavaScript:alert(1)\">click me</a>",
	},
	{
		ID:       "js-scheme-upper-case",
		Category: "obfuscated-scheme",
		Payload:  "<a href=\"JAVASCRIPT:alert(1)\">click me</a>",
	},
	{
		ID:       "js-scheme-leading-whitespace",
		Category: "obfuscated-scheme",
		Payload:  "<a href=\"   javascript:alert(1)\">click me</a>",
	},
	{
		ID:       "js-scheme-embedded-tab",
		Category: "obfuscated-scheme",
		Payload:  "<a href=\"java\tscript:alert(1)\">click me</a>",
	},
	{
		ID:       "js-scheme-embedded-newline",
		Category: "obfuscated-scheme",
		Payload:  "<a href=\"java\nscript:alert(1)\">click me</a>",
	},
	{
		ID:       "data-scheme-html",
		Category: "data-scheme",
		Payload:  "<a href=\"data:text/html,<script>alert(1)</script>\">click me</a>",
	},
	{
		ID:       "data-scheme-base64-script",
		Category: "data-scheme",
		Payload:  "<a href=\"data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==\">click me</a>",
	},
	{
		ID:       "vbscript-scheme-in-anchor-href",
		Category: "vbscript-scheme",
		Payload:  "<a href=\"vbscript:msgbox(1)\">click me</a>",
	},
	{
		ID:       "file-scheme-in-anchor-href",
		Category: "file-scheme",
		Payload:  "<a href=\"file:///etc/passwd\">click me</a>",
	},
	{
		ID:       "rel-hardening-target-blank-missing-rel",
		Category: "rel-hardening",
		Payload:  "<a href=\"https://example.com\" target=\"_blank\">click me</a>",
	},
	{
		ID:       "rel-hardening-target-blank-weak-rel",
		Category: "rel-hardening",
		Payload:  "<a href=\"https://example.com\" target=\"_blank\" rel=\"opener\">click me</a>",
	},
	{
		ID:       "protocol-relative",
		Category: "protocol-relative",
		Payload:  "<a href=\"//evil.example.com/phish\">click me</a>",
	},
	{
		ID:       "protocol-relative-whitespace",
		Category: "protocol-relative",
		Payload:  "<a href=\" //evil.example.com/phish\">click me</a>",
	},
	{
		ID:       "event-handler-img-onerror",
		Category: "event-handler-attribute",
		Payload:  "<img src=\"x\" onerror=\"alert(1)\">",
	},
	{
		ID:       "event-handler-svg-onload",
		Category: "event-handler-attribute",
		Payload:  "<svg onload=\"alert(1)\"></svg>",
	},
	{
		ID:       "event-handler-anchor-onclick",
		Category: "event-handler-attribute",
		Payload:  "<a href=\"https://example.com\" onclick=\"alert(1)\">click me</a>",
	},
	{
		ID:       "iframe-js-src",
		Category: "forbidden-tag",
		Payload:  "<iframe src=\"javascript:alert(1)\"></iframe>",
	},
	{
		ID:       "style-tag-expression",
		Category: "forbidden-tag",
		Payload:  "<style>body{background:url(javascript:alert(1))}</style>",
	},
	{
		ID:       "object-data-js",
		Category: "forbidden-tag",
		Payload:  "<object data=\"javascript:alert(1)\"></object>",
	},
	{
		ID:       "embed-src-js",
		Category: "forbidden-tag",
		Payload:  "<embed src=\"javascript:alert(1)\">",
	},
	{
		ID:       "form-action-js",
		Category: "forbidden-tag",
		Payload:  "<form action=\"javascript:alert(1)\"><button>go</button></form>",
	},
	{
		ID:       "input-autofocus-onfocus",
		Category: "forbidden-tag",
		Payload:  "<input onfocus=\"alert(1)\" autofocus>",
	},
	{
		ID:       "link-rel-stylesheet-js",
		Category: "forbidden-tag",
		Payload:  "<link rel=\"stylesheet\" href=\"javascript:alert(1)\">",
	},
	{
		ID:       "meta-refresh-js",
		Category: "forbidden-tag",
		Payload:  "<meta http-equiv=\"refresh\" content=\"0;url=javascript:alert(1)\">",
	},
	{
		ID:       "base-href-js",
		Category: "forbidden-tag",
		Payload:  "<base href=\"javascript:alert(1)/\">",
	},
	{
		ID:       "js-scheme-html-entity-encoded",
		Category: "obfuscated-scheme",
		Payload:  "<a href=\"&#106;avascript:alert(1)\">click me</a>",
	},
	{
		ID:       "nested-script-tag-stripping-bypass",
		Category: "nested-tag-normalization",
		Payload:  "<scr<script>ipt>alert(1)</scr</script>ipt>",
	},
}
