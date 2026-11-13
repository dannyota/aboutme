package project

import "strings"

func ChooseText(document Document, resumeLng string) (text Text, lang string, ok bool) {
	primary, _, _ := strings.Cut(strings.ToLower(resumeLng), "-")
	key := document.DefaultLanguage
	if candidate, found := document.Text[primary]; found && primary != "und" && !Blank(candidate.Name) {
		key = primary
	}
	text, found := document.Text[key]
	if !found || Blank(text.Name) {
		return Text{}, "", false
	}
	if key == primary {
		return text, "", true
	}
	return text, key, true
}
