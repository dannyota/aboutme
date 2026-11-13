package publicapi

import (
	"strings"

	"danny.vn/aboutme/apps/server/internal/project"
)

func renderableLink(link string) string {
	if normalized, ok := project.NormalizeLink(link); !ok || normalized != link || !rendererLink(link) {
		return ""
	}
	return link
}

func projectEntryLink(entryLink, repositoryURL, demoURL string) string {
	if !strings.HasPrefix(entryLink, "https://") || renderableLink(entryLink) == "" {
		return ""
	}
	own := comparableLink(entryLink)
	for _, other := range []string{repositoryURL, demoURL} {
		if other != "" && comparableLink(other) == own {
			return ""
		}
	}
	return entryLink
}

// net/url ends the authority at the first "/", "?" or "#", the userinfo at its last "@", and the host before its last colon outside an IPv6 literal.
type rendererLinkParts struct {
	userinfo    string
	hasUserinfo bool
	host, port  string
	rest        string
}

func splitRendererLink(link string) rendererLinkParts {
	afterScheme := strings.TrimPrefix(link, "https://")
	authority, rest := afterScheme, ""
	if end := strings.IndexAny(afterScheme, "/?#"); end >= 0 {
		authority, rest = afterScheme[:end], afterScheme[end:]
	}
	parts := rendererLinkParts{rest: rest}
	hostPort := authority
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		parts.userinfo, parts.hasUserinfo, hostPort = authority[:at], true, authority[at+1:]
	}
	parts.host = hostPort
	if colon := strings.LastIndex(hostPort, ":"); colon > strings.LastIndex(hostPort, "]") {
		parts.host, parts.port = hostPort[:colon], hostPort[colon+1:]
	}
	return parts
}

func comparableLink(link string) string {
	parts := splitRendererLink(link)
	var b strings.Builder
	b.WriteString("https://")
	if parts.hasUserinfo {
		b.WriteString(parts.userinfo + "@")
	}
	b.WriteString(foldedHost(parts.host))
	if parts.port != "" && parts.port != "443" {
		b.WriteString(":" + parts.port)
	}
	if !strings.HasPrefix(parts.rest, "/") {
		b.WriteString("/")
	}
	b.WriteString(parts.rest)
	return b.String()
}

func foldedHost(host string) string {
	host = strings.ReplaceAll(host, "\u0130", "i\u0307")
	return strings.ReplaceAll(strings.ToLower(host), "\u03c2", "\u03c3")
}
