package githubevidence

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	listingPageSize   = 30
	maxListingPages   = 10
	maxTagBytes       = 255
	searchQueryPrefix = "is:pr is:merged is:public author:"
)

var (
	ownerPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
	repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
)

func validOwner(owner string) bool {
	return ownerPattern.MatchString(owner)
}

func validRepositoryName(name string) bool {
	return name != "." && name != ".." && repositoryPattern.MatchString(name)
}

func splitFullName(fullName string) (string, string, bool) {
	owner, name, ok := strings.Cut(fullName, "/")
	return owner, name, ok && validOwner(owner) && validRepositoryName(name)
}

func canonicalID(text string) (int64, bool) {
	id, err := strconv.ParseInt(text, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != text {
		return 0, false
	}
	return id, true
}

func validPage(page int) bool {
	return page >= 1 && page <= maxListingPages
}

func validTag(tag string) bool {
	if tag == "" || tag == "@" || len(tag) > maxTagBytes || !utf8.ValidString(tag) {
		return false
	}
	if strings.ContainsAny(tag, "~^:?*[\\") || strings.Contains(tag, "..") || strings.Contains(tag, "@{") {
		return false
	}
	if strings.HasSuffix(tag, ".") {
		return false
	}
	for _, component := range strings.Split(tag, "/") {
		if component == "" || strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return false
		}
	}
	for _, r := range tag {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func tagFromSegment(segment string) (string, bool) {
	tag, err := url.PathUnescape(segment)
	if err != nil || !validTag(tag) || url.PathEscape(tag) != segment {
		return "", false
	}
	return tag, true
}

func repositoryListingQuery(page int) url.Values {
	return url.Values{"affiliation": {"owner"}, "visibility": {"public"}, "sort": {"pushed"},
		"per_page": {strconv.Itoa(listingPageSize)}, "page": {strconv.Itoa(page)}}
}

func searchListingQuery(login string, page int) url.Values {
	return url.Values{"q": {searchQueryPrefix + login}, "sort": {"updated"}, "order": {"desc"},
		"per_page": {strconv.Itoa(listingPageSize)}, "page": {strconv.Itoa(page)}}
}

func releaseListingQuery(page int) url.Values {
	return url.Values{"per_page": {strconv.Itoa(listingPageSize)}, "page": {strconv.Itoa(page)}}
}

func permittedRead(path, rawQuery string) bool {
	rest, ok := strings.CutPrefix(path, "/")
	if !ok {
		return false
	}
	segments := strings.Split(rest, "/")
	switch {
	case len(segments) == 2 && segments[0] == "user" && segments[1] == "repos":
		return permittedListing(rawQuery, func(page int, _ url.Values) (url.Values, bool) {
			return repositoryListingQuery(page), true
		})
	case len(segments) == 2 && segments[0] == "search" && segments[1] == "issues":
		return permittedListing(rawQuery, func(page int, values url.Values) (url.Values, bool) {
			login, found := strings.CutPrefix(values.Get("q"), searchQueryPrefix)
			return searchListingQuery(login, page), found && validOwner(login)
		})
	case len(segments) == 2 && segments[0] == "repositories":
		_, valid := canonicalID(segments[1])
		return valid && rawQuery == ""
	case len(segments) >= 3 && segments[0] == "repos" && validOwner(segments[1]) && validRepositoryName(segments[2]):
		return permittedRepositoryRead(segments[3:], rawQuery)
	}
	return false
}

func permittedRepositoryRead(rest []string, rawQuery string) bool {
	switch {
	case len(rest) == 1 && rest[0] == "releases":
		return permittedListing(rawQuery, func(page int, _ url.Values) (url.Values, bool) {
			return releaseListingQuery(page), true
		})
	case rawQuery != "":
		return false
	case len(rest) == 0:
		return true
	case len(rest) == 2 && (rest[0] == "pulls" || rest[0] == "releases"):
		_, valid := canonicalID(rest[1])
		return valid
	case len(rest) == 3 && rest[0] == "releases" && rest[1] == "tags":
		_, valid := tagFromSegment(rest[2])
		return valid
	}
	return false
}

func permittedListing(rawQuery string, build func(page int, values url.Values) (url.Values, bool)) bool {
	values, err := url.ParseQuery(rawQuery)
	if err != nil || len(values["page"]) != 1 {
		return false
	}
	text := values.Get("page")
	page, err := strconv.Atoi(text)
	if err != nil || strconv.Itoa(page) != text || !validPage(page) {
		return false
	}
	want, valid := build(page, values)
	return valid && want.Encode() == rawQuery
}
