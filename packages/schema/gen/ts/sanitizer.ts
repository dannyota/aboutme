// Code generated from validation/sanitizer-allowlist.v1.json and validation/hostile-corpus.json. DO NOT EDIT.

export const SANITIZER_ALLOWLIST_VERSION = 1 as const;

export const ALLOWED_TAGS = Object.freeze([
  "p",
  "br",
  "strong",
  "em",
  "u",
  "ol",
  "ul",
  "li",
  "a",
]);

export const ALLOWED_ATTRIBUTES: Readonly<Record<string, readonly string[]>> =
  Object.freeze({
    a: Object.freeze(["href", "rel", "target"]),
  });

export const ALLOWED_URL_SCHEMES = Object.freeze(["https", "mailto", "tel"]);

export const FORBIDDEN_TAGS = Object.freeze([
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
]);

export const FORBIDDEN_ATTRIBUTE_PREFIXES = Object.freeze(["on"]);

export const FORBIDDEN_URL_SCHEMES = Object.freeze([
  "javascript",
  "data",
  "vbscript",
  "file",
]);

export const EXTERNAL_REL = "noopener noreferrer" as const;

export interface HostilePayload {
  readonly id: string;
  readonly category: string;
  readonly payload: string;
}

export const HOSTILE_CORPUS: readonly Readonly<HostilePayload>[] =
  Object.freeze([
    Object.freeze({
      id: "js-scheme-bare",
      category: "javascript-scheme",
      payload: "javascript:alert(1)",
    }),
    Object.freeze({
      id: "js-scheme-in-anchor-href",
      category: "javascript-scheme",
      payload: '<a href="javascript:alert(document.cookie)">click me</a>',
    }),
    Object.freeze({
      id: "js-scheme-mixed-case",
      category: "obfuscated-scheme",
      payload: '<a href="JavaScript:alert(1)">click me</a>',
    }),
    Object.freeze({
      id: "js-scheme-upper-case",
      category: "obfuscated-scheme",
      payload: '<a href="JAVASCRIPT:alert(1)">click me</a>',
    }),
    Object.freeze({
      id: "js-scheme-leading-whitespace",
      category: "obfuscated-scheme",
      payload: '<a href="   javascript:alert(1)">click me</a>',
    }),
    Object.freeze({
      id: "js-scheme-embedded-tab",
      category: "obfuscated-scheme",
      payload: '<a href="java\tscript:alert(1)">click me</a>',
    }),
    Object.freeze({
      id: "js-scheme-embedded-newline",
      category: "obfuscated-scheme",
      payload: '<a href="java\nscript:alert(1)">click me</a>',
    }),
    Object.freeze({
      id: "data-scheme-html",
      category: "data-scheme",
      payload:
        '<a href="data:text/html,<script>alert(1)</script>">click me</a>',
    }),
    Object.freeze({
      id: "data-scheme-base64-script",
      category: "data-scheme",
      payload:
        '<a href="data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==">click me</a>',
    }),
    Object.freeze({
      id: "vbscript-scheme-in-anchor-href",
      category: "vbscript-scheme",
      payload: '<a href="vbscript:msgbox(1)">click me</a>',
    }),
    Object.freeze({
      id: "file-scheme-in-anchor-href",
      category: "file-scheme",
      payload: '<a href="file:///etc/passwd">click me</a>',
    }),
    Object.freeze({
      id: "rel-hardening-target-blank-missing-rel",
      category: "rel-hardening",
      payload: '<a href="https://example.com" target="_blank">click me</a>',
    }),
    Object.freeze({
      id: "rel-hardening-target-blank-weak-rel",
      category: "rel-hardening",
      payload:
        '<a href="https://example.com" target="_blank" rel="opener">click me</a>',
    }),
    Object.freeze({
      id: "protocol-relative",
      category: "protocol-relative",
      payload: '<a href="//evil.example.com/phish">click me</a>',
    }),
    Object.freeze({
      id: "protocol-relative-whitespace",
      category: "protocol-relative",
      payload: '<a href=" //evil.example.com/phish">click me</a>',
    }),
    Object.freeze({
      id: "event-handler-img-onerror",
      category: "event-handler-attribute",
      payload: '<img src="x" onerror="alert(1)">',
    }),
    Object.freeze({
      id: "event-handler-svg-onload",
      category: "event-handler-attribute",
      payload: '<svg onload="alert(1)"></svg>',
    }),
    Object.freeze({
      id: "event-handler-anchor-onclick",
      category: "event-handler-attribute",
      payload: '<a href="https://example.com" onclick="alert(1)">click me</a>',
    }),
    Object.freeze({
      id: "iframe-js-src",
      category: "forbidden-tag",
      payload: '<iframe src="javascript:alert(1)"></iframe>',
    }),
    Object.freeze({
      id: "style-tag-expression",
      category: "forbidden-tag",
      payload: "<style>body{background:url(javascript:alert(1))}</style>",
    }),
    Object.freeze({
      id: "object-data-js",
      category: "forbidden-tag",
      payload: '<object data="javascript:alert(1)"></object>',
    }),
    Object.freeze({
      id: "embed-src-js",
      category: "forbidden-tag",
      payload: '<embed src="javascript:alert(1)">',
    }),
    Object.freeze({
      id: "form-action-js",
      category: "forbidden-tag",
      payload: '<form action="javascript:alert(1)"><button>go</button></form>',
    }),
    Object.freeze({
      id: "input-autofocus-onfocus",
      category: "forbidden-tag",
      payload: '<input onfocus="alert(1)" autofocus>',
    }),
    Object.freeze({
      id: "link-rel-stylesheet-js",
      category: "forbidden-tag",
      payload: '<link rel="stylesheet" href="javascript:alert(1)">',
    }),
    Object.freeze({
      id: "meta-refresh-js",
      category: "forbidden-tag",
      payload:
        '<meta http-equiv="refresh" content="0;url=javascript:alert(1)">',
    }),
    Object.freeze({
      id: "base-href-js",
      category: "forbidden-tag",
      payload: '<base href="javascript:alert(1)/">',
    }),
    Object.freeze({
      id: "js-scheme-html-entity-encoded",
      category: "obfuscated-scheme",
      payload: '<a href="&#106;avascript:alert(1)">click me</a>',
    }),
    Object.freeze({
      id: "nested-script-tag-stripping-bypass",
      category: "nested-tag-normalization",
      payload: "<scr<script>ipt>alert(1)</scr</script>ipt>",
    }),
  ]);
