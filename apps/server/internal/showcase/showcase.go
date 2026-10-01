// Package showcase implements the community showcase: the opt-in row, the
// review key, the derived template, the operator review commands, the startup
// recompute, and the uncached public listing. See docs/design/showcase.md and
// docs/adr/0029-community-showcase.md.
package showcase

import "regexp"

// Roles is the closed role list: the nine Library roles plus Other.
var Roles = []string{
	"backend", "frontend", "mobile", "devops", "data-ai",
	"qa", "fresher", "brse", "security", "other",
}

// ValidRole reports whether role is one of Roles.
func ValidRole(role string) bool {
	for _, candidate := range Roles {
		if candidate == role {
			return true
		}
	}
	return false
}

// ValidLanguageFilter reports whether lang is a language filter value.
func ValidLanguageFilter(lang string) bool { return lang == "vi" || lang == "en" }

// CustomTemplate is the template filter value for a resume that matches no
// preset.
const CustomTemplate = "custom"

// ValidTemplateFilter reports whether template is a preset ID or CustomTemplate.
func ValidTemplateFilter(template string) bool {
	if template == CustomTemplate {
		return true
	}
	for _, preset := range Presets {
		if preset.ID == template {
			return true
		}
	}
	return false
}

var keyPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// ValidKey reports whether key has the exact review key or card version form.
func ValidKey(key string) bool { return keyPattern.MatchString(key) }
