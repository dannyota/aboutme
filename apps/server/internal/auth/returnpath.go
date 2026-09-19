package auth

import (
	"net/url"
	"regexp"
	"strings"
)

// Login return paths. See docs/design/security.md.

const maxLoginReturnPathBytes = 2048

// appNewPath is the new-resume route. Its query may carry only a sample, a
// template, and a language, so a shared link cannot smuggle other state
// through sign-in.
const appNewPath = "/app/new"

var appNewIDPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const maxAppNewIDBytes = 64

// validatedLoginReturnPath accepts only a same-origin relative URL. Invalid or
// absent input becomes the fixed resume-list destination before any database
// row or provider redirect is created. /app/new keeps only its checked query.
func validatedLoginReturnPath(raw string) string {
	if !sameOriginReturnPath(raw) {
		return defaultLoginReturnPath
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Opaque != "" || parsed.User != nil {
		return defaultLoginReturnPath
	}
	if isAppNewPath(parsed.Path) {
		return appNewReturnPath(parsed.RawQuery)
	}
	return raw
}

// sameOriginReturnPath requires exactly one leading slash and no backslash or
// control character, both as written and after percent-decoding the path, and
// no dot segment in the decoded path, so an encoded "//" or "\" or a dot
// segment cannot become a network path later.
func sameOriginReturnPath(raw string) bool {
	if raw == "" || len(raw) > maxLoginReturnPathBytes || !plainPath(raw) {
		return false
	}
	pathPart, _, _ := strings.Cut(raw, "?")
	pathPart, _, _ = strings.Cut(pathPart, "#")
	decoded, err := url.PathUnescape(pathPart)
	return err == nil && plainPath(decoded) && !hasDotSegment(decoded)
}

// hasDotSegment reports a "." or ".." segment in a decoded path. Resolving one
// can turn "/.//host" into the network path "//host".
func hasDotSegment(decodedPath string) bool {
	for segment := range strings.SplitSeq(decodedPath, "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}
	return false
}

// isAppNewPath matches the decoded path the way the web router does: without
// regard to case or one trailing slash.
func isAppNewPath(decodedPath string) bool {
	return strings.EqualFold(strings.TrimSuffix(decodedPath, "/"), appNewPath)
}

func plainPath(value string) bool {
	if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.Contains(value, `\`) {
		return false
	}
	for index := range len(value) {
		if value[index] < 0x20 || value[index] == 0x7f {
			return false
		}
	}
	return true
}

// appNewReturnPath keeps sample and template when each appears once as a
// well-formed template id, and lng when it is "en" or "vi". It drops every
// other parameter and the fragment. The new-resume page checks that the ids
// exist.
func appNewReturnPath(rawQuery string) string {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return appNewPath
	}
	kept := url.Values{}
	for _, key := range []string{"sample", "template"} {
		if value, ok := single(values, key); ok && len(value) <= maxAppNewIDBytes && appNewIDPattern.MatchString(value) {
			kept.Set(key, value)
		}
	}
	if value, ok := single(values, "lng"); ok && (value == "en" || value == "vi") {
		kept.Set("lng", value)
	}
	if len(kept) == 0 {
		return appNewPath
	}
	return appNewPath + "?" + kept.Encode()
}

func single(values url.Values, key string) (string, bool) {
	found := values[key]
	if len(found) != 1 {
		return "", false
	}
	return found[0], true
}
