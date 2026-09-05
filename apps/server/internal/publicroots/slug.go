// Package publicroots defines route ownership and public slug validation.
package publicroots

// ValidSlug accepts a non-reserved public resume slug.
func ValidSlug(slug string) bool {
	if len(slug) < 4 || len(slug) > 30 || Reserved(slug) || slug[0] == '-' || slug[len(slug)-1] == '-' {
		return false
	}
	previousHyphen := false
	for i := range slug {
		letter := slug[i] >= 'a' && slug[i] <= 'z'
		digit := slug[i] >= '0' && slug[i] <= '9'
		if slug[i] == '-' {
			if previousHyphen {
				return false
			}
			previousHyphen = true
			continue
		}
		if !letter && !digit {
			return false
		}
		previousHyphen = false
	}
	return true
}
