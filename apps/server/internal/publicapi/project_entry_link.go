package publicapi

// The links a project page sends, checked as the renderer's decoder checks
// them (apps/web/server/utils/public-render/project-decode.ts).

import (
	"strings"

	"danny.vn/aboutme/apps/server/internal/project"
)

// renderableLink is link when the library would store it unchanged and the
// renderer's URL parser accepts it, else "". Go's URL parser accepts no more
// than the decoder's own check of what it would accept.
func renderableLink(link string) string {
	if normalized, ok := project.NormalizeLink(link); !ok || normalized != link || !rendererLink(link) {
		return ""
	}
	return link
}

// projectEntryLink is the entry's own link as stored when it is a
// renderable https link that differs from both the repository and the demo
// address under the contract's link comparison, else "".
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

// rendererLinkParts is an https link split as the decoder splits it, after
// Go's net/url: the authority ends at the first "/", "?" or "#", the
// userinfo at its last "@", and the port follows the host's last colon
// outside an IPv6 literal.
type rendererLinkParts struct {
	userinfo    string
	hasUserinfo bool
	host, port  string
	// rest is the path, query and fragment, as written.
	rest string
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

// comparableLink is link in the contract's comparison form: the host
// lowercased, the port dropped when empty or 443, and an empty path read as
// "/"; the rest byte for byte. The host is lowered at least as far as
// JavaScript's toLowerCase lowers it, so two links the renderer reads as
// equal are always equal here.
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

// foldedHost is host lowercased, with the two lowerings JavaScript adds to
// Go's: a capital dotted I becomes "i" and a combining dot above, and every
// sigma becomes the small non-final sigma, whatever its place in a word.
func foldedHost(host string) string {
	host = strings.ReplaceAll(host, "\u0130", "i\u0307")
	return strings.ReplaceAll(strings.ToLower(host), "\u03c2", "\u03c3")
}
