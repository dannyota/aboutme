package auth

// The page a sign-in-to-view start returns to below its resume: the resume
// page itself, its project list, or one project page
// (docs/design/projects-contract.md "Render envelope"). The page is stored in
// the view transaction's return_path and validated again when read, so a
// callback can only ever redirect to /{current slug} plus one of the two
// project shapes.

import (
	"net/http"
	"strings"

	"danny.vn/aboutme/apps/server/internal/project"
)

// viewPage reads the optional page parameter of a view start. It accepts
// exactly "projects" or "projects/{project-slug}", spelled without any
// escape, and returns the return-path suffix "/projects" or
// "/projects/{project-slug}"; an absent page returns "". Anything else,
// including an empty, repeated, or escaped page, is not ok.
func viewPage(r *http.Request) (string, bool) {
	decoded, present := r.URL.Query()["page"]
	var raw []string
	for part := range strings.SplitSeq(r.URL.RawQuery, "&") {
		if key, value, _ := strings.Cut(part, "="); key == "page" {
			raw = append(raw, value)
		}
	}
	if !present && len(raw) == 0 {
		return "", true
	}
	if len(decoded) != 1 || len(raw) != 1 || raw[0] != decoded[0] {
		return "", false
	}
	return viewPageSuffix(decoded[0])
}

// viewReturnSuffix re-validates a stored view return path. It returns the
// path only when it is one of the two project page suffixes; the resume
// page's closed default and every other value give "".
func viewReturnSuffix(returnPath string) string {
	page, found := strings.CutPrefix(returnPath, "/")
	if !found {
		return ""
	}
	suffix, ok := viewPageSuffix(page)
	if !ok {
		return ""
	}
	return suffix
}

// viewPageSuffix maps a page in the closed grammar to its return-path
// suffix.
func viewPageSuffix(page string) (string, bool) {
	if page == "projects" {
		return "/projects", true
	}
	slug, found := strings.CutPrefix(page, "projects/")
	if !found || !project.ValidSlug(slug) {
		return "", false
	}
	return "/projects/" + slug, true
}
