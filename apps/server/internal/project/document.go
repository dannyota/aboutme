// Package project validates and stores private account library projects.
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
	// MaxProjects is the number of library projects one account may store.
	MaxProjects = 30
	// MaxStoredBytes bounds the PostgreSQL JSON text of one document.
	MaxStoredBytes = 96 * 1024
	// SlugHoldDays is the account-local reservation after release, in days.
	SlugHoldDays = 180
	// MaxLinkCharacters bounds a repository or demo link in Unicode code
	// points, matching the resume entry link it prefills.
	MaxLinkCharacters = 2048
)

var (
	// ErrInvalid reports invalid project content without disclosing text.
	ErrInvalid  = errors.New("project document is invalid")
	languageKey = regexp.MustCompile(`^[a-z]{2,8}$`)
)

// Document is the complete replaceable project content.
type Document struct {
	// Dates is the optional resume-entry date range.
	Dates *schema.DateRange `json:"dates,omitempty"`
	// RepositoryURL is the optional HTTPS repository address.
	RepositoryURL string `json:"repositoryUrl,omitempty"`
	// DemoURL is the optional HTTPS demonstration address.
	DemoURL string `json:"demoUrl,omitempty"`
	// Technologies is the ordered list of plain technology names.
	Technologies []string `json:"technologies,omitempty"`
	// DefaultLanguage names one required text translation.
	DefaultLanguage string `json:"defaultLanguage"`
	// Text maps lowercase primary language subtags to translations.
	Text map[string]Text `json:"text"`
}

// Text holds one language's project content.
type Text struct {
	// Name is required in the default language, in at most 120 characters.
	Name string `json:"name,omitempty"`
	// Role describes the owner's role in at most 120 characters.
	Role string `json:"role,omitempty"`
	// Summary is the print-sized summary in at most 300 characters.
	Summary string `json:"summary,omitempty"`
	// Outcome describes results in at most 600 characters.
	Outcome string `json:"outcome,omitempty"`
	// Description is sanitized rich text in at most 16 KiB of UTF-8.
	Description string `json:"description,omitempty"`
}

// Prepare validates complete content and returns a sanitized copy.
func Prepare(d Document) (Document, error) {
	if len(d.Text) < 1 || len(d.Text) > 4 || len(d.Technologies) > 20 || !languageKey.MatchString(d.DefaultLanguage) {
		return Document{}, ErrInvalid
	}
	text, ok := d.Text[d.DefaultLanguage]
	if !ok || strings.TrimSpace(text.Name) == "" {
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

// NormalizeLink returns s with its scheme lowercase, as a resume entry link
// requires, and reports whether the result is an absolute HTTPS link of at
// most MaxLinkCharacters. An empty link is valid. Every byte after the scheme
// is kept: lowercasing an internationalised host can change where it points.
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
