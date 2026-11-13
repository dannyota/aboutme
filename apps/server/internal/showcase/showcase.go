package showcase

var Roles = []string{
	"backend", "frontend", "mobile", "devops", "data-ai",
	"qa", "fresher", "brse", "security", "other",
}

func ValidRole(role string) bool {
	for _, candidate := range Roles {
		if candidate == role {
			return true
		}
	}
	return false
}

func ValidLanguageFilter(lang string) bool { return lang == "vi" || lang == "en" }

const CustomTemplate = "custom"

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
