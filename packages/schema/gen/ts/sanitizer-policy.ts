// Code generated from validation/sanitizer-allowlist.v1.json. DO NOT EDIT.

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
