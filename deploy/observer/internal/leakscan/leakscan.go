// Package leakscan finds infrastructure identifiers in deployment document
// bytes: the patterns the sanitizer rules forbid
// (docs/design/deployment-transparency/document.md, "Sanitizer"). The
// observer's leak test and the production document check share it, so both
// apply the same list.
package leakscan

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

var (
	allowedHex = regexp.MustCompile(`sha256:[0-9a-f]{64}|\b[0-9a-f]{40}\b`)
	timestamp  = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z`)
	forbidden  = map[string]*regexp.Regexp{
		"12-digit number":   regexp.MustCompile(`\d{12}`),
		"arn":               regexp.MustCompile(`(?i)arn:`),
		"IPv4 address":      regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}\b`),
		"IPv6 address":      regexp.MustCompile(`(?i)\b[0-9a-f]{0,4}(:[0-9a-f]{0,4}){2,7}\b`),
		"availability zone": regexp.MustCompile(`\b[a-z]{2}(-gov)?-[a-z]+-\d[a-z]\b`),
		"32-hex ID":         regexp.MustCompile(`(?i)[0-9a-f]{32}`),
		"UUID":              regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`),
	}
	scheme = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://`)
	// urlPrefixes are the only URL starts the document may hold. The two
	// entries ending in a quote are whole JSON string values.
	urlPrefixes = []string{
		"https://aboutme.vn\"",
		"https://github.com/dannyota/aboutme\"",
		"https://github.com/dannyota/aboutme/",
		"https://api.github.com/repos/dannyota/aboutme/attestations/",
		"https://search.sigstore.dev/?logIndex=",
	}
)

// Find returns one sorted line per forbidden match in b, or nil when b holds
// none. Digests, commit hashes, and timestamps are allowed values; Find
// removes them before looking for identifiers hidden inside other strings.
func Find(b []byte) []string {
	s := string(b)
	var found []string
	scrubbed := timestamp.ReplaceAllString(allowedHex.ReplaceAllString(s, "HEX"), "TIME")
	for name, re := range forbidden {
		if m := re.FindString(scrubbed); m != "" {
			found = append(found, fmt.Sprintf("holds a %s: %q", name, m))
		}
	}
	for _, loc := range scheme.FindAllStringIndex(s, -1) {
		rest := s[loc[0]:]
		if !slices.ContainsFunc(urlPrefixes, func(p string) bool { return strings.HasPrefix(rest, p) }) {
			found = append(found, fmt.Sprintf("holds a URL outside the allowed prefixes: %.80q", rest))
		}
	}
	sort.Strings(found)
	return found
}
