package auth

import (
	"net/url"
	"regexp"
	"strings"
)

const maxLoginReturnPathBytes = 2048

const appNewPath = "/app/new"

var appNewIDPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const maxAppNewIDBytes = 64

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

func sameOriginReturnPath(raw string) bool {
	if raw == "" || len(raw) > maxLoginReturnPathBytes || !plainPath(raw) {
		return false
	}
	pathPart, _, _ := strings.Cut(raw, "?")
	pathPart, _, _ = strings.Cut(pathPart, "#")
	decoded, err := url.PathUnescape(pathPart)
	return err == nil && plainPath(decoded) && !hasDotSegment(decoded)
}

func hasDotSegment(decodedPath string) bool {
	for segment := range strings.SplitSeq(decodedPath, "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}
	return false
}

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
