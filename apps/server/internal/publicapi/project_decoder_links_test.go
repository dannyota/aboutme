package publicapi

// The link checks of the Go copy of the renderer's project envelope decoder
// (project_decoder_rules_test.go).

import (
	"strings"
	"unicode/utf8"
)

// decoderLink is project-decode.ts externalLink: an https link of at most
// 2048 code points, storable, that goParsable and the WHATWG URL parser
// accept with a host.
func decoderLink(value any) bool {
	link, isString := value.(string)
	return isString && decoderStorable(link) && decoderParsable(link) && rendererLink(link)
}

// decoderSameLink is sameLink: other is present and equal to link under the
// contract's comparison. comparableLink lowers a host at least as far as
// JavaScript does, so this copy refuses whatever the decoder refuses.
func decoderSameLink(link, other any) bool {
	own, isString := link.(string)
	value, present := other.(string)
	return isString && present && comparableLink(own) == comparableLink(value)
}

// decoderNormalizedOrigin is envelope-fields.ts normalizedOrigin: an ASCII
// string of at most 512 bytes that is exactly its own http or https origin,
// with no user, path, query or fragment.
func decoderNormalizedOrigin(value any) bool {
	origin, isString := value.(string)
	if !isString || len(origin) > 512 || strings.IndexFunc(origin, func(r rune) bool { return r > 0x7f }) >= 0 {
		return false
	}
	scheme, authority, found := strings.Cut(origin, "://")
	if !found || (scheme != "http" && scheme != "https") || strings.ContainsAny(authority, "/\\?#@") {
		return false
	}
	host, port, ok := whatwgHostPort(authority)
	if !ok {
		return false
	}
	serialized := scheme + "://" + host
	defaultPort := scheme == "https" && port == "443" || scheme == "http" && port == "80"
	if port != "" && !defaultPort {
		serialized += ":" + port
	}
	return serialized == origin
}

// decoderParsable is the decoder's goParsable: no control character, a
// userinfo of RFC 3986 characters, a numeric port and host characters Go's
// host parser allows.
func decoderParsable(link string) bool {
	if strings.IndexFunc(link, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return false
	}
	parts := splitRendererLink(link)
	if parts.hasUserinfo && strings.Trim(parts.userinfo, decoderUserinfo) != "" {
		return false
	}
	if strings.Trim(parts.port, "0123456789") != "" {
		return false
	}
	for _, r := range parts.host {
		if r < utf8.RuneSelf && !strings.ContainsRune(decoderHostASCII, r) {
			return false
		}
	}
	return true
}

const (
	decoderAlphanumerics = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	decoderUserinfo      = decoderAlphanumerics + "-._:~!$&'()*+,;=%@"
	decoderHostASCII     = decoderAlphanumerics + "-._~!$&'()*+,;=:[]<>\"%"
)
