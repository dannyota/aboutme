package project

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/sanitize"
)

const (
	MaxProjects       = 30
	MaxStoredBytes    = 96 * 1024
	SlugHoldDays      = 180
	MaxLinkCharacters = 2048
)

var (
	ErrInvalid  = errors.New("project document is invalid")
	languageKey = regexp.MustCompile(`^[a-z]{2,8}$`)
)

type Document struct {
	Dates           *schema.DateRange `json:"dates,omitempty"`
	RepositoryURL   string            `json:"repositoryUrl,omitempty"`
	DemoURL         string            `json:"demoUrl,omitempty"`
	Technologies    []string          `json:"technologies,omitempty"`
	DefaultLanguage string            `json:"defaultLanguage"`
	Text            map[string]Text   `json:"text"`
}

type Text struct {
	Name        string `json:"name,omitempty"`
	Role        string `json:"role,omitempty"`
	Summary     string `json:"summary,omitempty"`
	Outcome     string `json:"outcome,omitempty"`
	Description string `json:"description,omitempty"`
}

func Prepare(d Document) (Document, error) {
	if len(d.Text) < 1 || len(d.Text) > 4 || len(d.Technologies) > 20 || !languageKey.MatchString(d.DefaultLanguage) {
		return Document{}, ErrInvalid
	}
	text, ok := d.Text[d.DefaultLanguage]
	if !ok || Blank(text.Name) {
		return Document{}, ErrInvalid
	}
	repository, repositoryOK := NormalizeLink(d.RepositoryURL)
	demo, demoOK := NormalizeLink(d.DemoURL)
	if !repositoryOK || !demoOK || !validDates(d.Dates) {
		return Document{}, ErrInvalid
	}
	for _, v := range d.Technologies {
		if !validString(v, 40) {
			return Document{}, ErrInvalid
		}
	}
	out := d
	out.RepositoryURL, out.DemoURL = repository, demo
	out.Technologies = append([]string(nil), d.Technologies...)
	out.Text = make(map[string]Text, len(d.Text))
	for key, value := range d.Text {
		if !languageKey.MatchString(key) || !validString(value.Name, 120) || !validString(value.Role, 120) || !validString(value.Summary, 300) || !validString(value.Outcome, 600) || !validString(value.Description, 16384) || len(value.Description) > schema.MaxRichTextBytes {
			return Document{}, ErrInvalid
		}
		value.Description = sanitize.RichText(value.Description)
		if len(value.Description) > schema.MaxRichTextBytes {
			return Document{}, ErrInvalid
		}
		out.Text[key] = value
	}
	return out, nil
}

func validString(s string, limit int) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0) && utf8.RuneCountInString(s) <= limit
}

func NormalizeLink(s string) (string, bool) {
	if s == "" {
		return "", true
	}
	if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
		return "", false
	}
	scheme, rest, found := strings.Cut(s, "://")
	if !found || !strings.EqualFold(scheme, "https") {
		return "", false
	}
	normalized := "https://" + rest
	if utf8.RuneCountInString(normalized) > MaxLinkCharacters {
		return "", false
	}
	u, err := url.Parse(normalized)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.Opaque != "" {
		return "", false
	}
	return normalized, true
}
func validDates(d *schema.DateRange) bool {
	if d == nil {
		return true
	}
	if !validYearMonth(d.Start) || d.Present != (d.End == nil) {
		return false
	}
	return (d.End == nil || validYearMonth(*d.End)) && len(schema.ValidateDateRange(d, "dates")) == 0
}
func validYearMonth(v schema.YearMonth) bool {
	return v.Y >= 1900 && v.Y <= 2100 && (v.M == nil || (*v.M >= 1 && *v.M <= 12))
}
