package githubevidence

import (
	"net/url"
	"strings"
)

const (
	kindRepository  = "repository"
	kindPullRequest = "pullRequest"
	kindRelease     = "release"
	githubWebPrefix = "https://github.com/"
)

type evidenceLink struct {
	kind   string
	owner  string
	name   string
	number int64
	tag    string
}

func parseEvidenceLink(kind, raw string) (evidenceLink, error) {
	link, ok := parseLink(strings.TrimSpace(raw))
	if !ok || link.kind != kind {
		return evidenceLink{}, errUnsupportedLink
	}
	return link, nil
}

func parseLink(text string) (evidenceLink, bool) {
	rest, found := strings.CutPrefix(text, githubWebPrefix)
	if !found || strings.ContainsAny(rest, "?#") {
		return evidenceLink{}, false
	}
	segments := strings.Split(strings.TrimSuffix(rest, "/"), "/")
	if len(segments) < 2 || !validOwner(segments[0]) || !validRepositoryName(segments[1]) {
		return evidenceLink{}, false
	}
	link := evidenceLink{owner: segments[0], name: segments[1]}
	switch {
	case len(segments) == 2:
		link.kind = kindRepository
		return link, true
	case len(segments) == 4 && segments[2] == "pull":
		number, valid := canonicalID(segments[3])
		link.kind, link.number = kindPullRequest, number
		return link, valid
	case len(segments) == 5 && segments[2] == "releases" && segments[3] == "tag":
		tag, err := url.PathUnescape(segments[4])
		link.kind, link.tag = kindRelease, tag
		return link, err == nil && validTag(tag)
	}
	return evidenceLink{}, false
}
