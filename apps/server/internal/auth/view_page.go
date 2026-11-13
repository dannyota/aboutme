package auth

import (
	"net/http"
	"strings"

	"danny.vn/aboutme/apps/server/internal/project"
)

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
