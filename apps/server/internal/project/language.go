package project

import "strings"

// ChooseText applies docs/design/projects.md "Language" for a resume whose
// language tag is resumeLng: the text of the tag's primary subtag, else the
// default language, which an und resume always uses. A text whose name is
// Blank falls back to the default language. lang is the chosen key when it is
// not the resume language's primary subtag, else "". ok is false when no
// chosen text has a name the renderer can show; such a project is not
// available on that resume.
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
