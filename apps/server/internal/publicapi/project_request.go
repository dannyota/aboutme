package publicapi

// The two project page render requests, built from one admitted snapshot by
// the rules in docs/design/projects-contract.md "Render envelope" and
// docs/design/ui/project-pages.md "Head".

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/previewmeta"
	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/publicpage"
	"danny.vn/aboutme/apps/server/internal/publicresume"
)

// maxListTechnologies is the number of technologies one list row shows.
const maxListTechnologies = 6

// The bounds of a project page's preview text, agreed with the renderer:
// a title of a 120-character name, " · " and the longest owner name, and a
// description of the longest summary. Nothing is cut to fit them.
const (
	maxProjectPreviewTitleRunes       = 683
	maxProjectPreviewTitleBytes       = 2727
	maxProjectPreviewDescriptionRunes = 300
	maxProjectPreviewDescriptionBytes = 1200
)

// maxOutcomeDescriptionRunes bounds a project page description taken from
// its outcome.
const maxOutcomeDescriptionRunes = 160

// errProjectWithoutName reports a project with no name in the chosen or the
// default language, which no stored project has.
var errProjectWithoutName = errors.New("publicapi: project has no name")

// projectPageInput is everything one project request is built from.
type projectPageInput struct {
	snapshot publicresume.Snapshot
	pages    publicresume.ProjectPages
	origin   publicresume.PublicOrigin
	// imageURL is the resume's current preview card address, or "" while
	// stored cards are off.
	imageURL string
}

// buildProjectListRequest builds the request for /{slug}/projects. The list
// has no description.
func buildProjectListRequest(input projectPageInput) (directrender.ProjectListRenderRequest, error) {
	resume := projectPageResume(input)
	title := "Projects · " + resume.OwnerName
	if resume.UILng == "vi" {
		title = "Dự án · " + resume.OwnerName
	}
	rows := make([]directrender.ProjectSummary, 0, len(input.pages.Projects))
	for _, available := range input.pages.Projects {
		text, lang, err := chooseProjectText(available.Document, resume.Lng)
		if err != nil {
			return directrender.ProjectListRenderRequest{}, err
		}
		shown := append([]string{}, available.Document.Technologies...)
		more := 0
		if len(shown) > maxListTechnologies {
			shown, more = shown[:maxListTechnologies], len(shown)-maxListTechnologies
		}
		rows = append(rows, directrender.ProjectSummary{
			Slug: available.Slug, URL: available.URL, Lang: lang, Name: text.Name, Role: text.Role,
			Dates: available.Document.Dates, Summary: text.Summary, Technologies: shown, MoreTechnologies: more,
		})
	}
	pageURL := input.origin.Resolve("/" + input.snapshot.Public.Slug + "/projects")
	return directrender.ProjectListRenderRequest{
		Mode: directrender.ProjectListRenderMode, CanonicalOrigin: input.origin.String(), PageURL: pageURL,
		DiscoveryEnabled: input.snapshot.DiscoveryEnabled, PageTitle: title, FaviconHref: projectFaviconHref(input.snapshot),
		Preview: projectPreview(input, title, ""), Resume: resume, Projects: rows,
	}, nil
}

// buildProjectRequest builds the request for /{slug}/projects/{project-slug}
// from the one project input.pages holds.
func buildProjectRequest(input projectPageInput) (directrender.ProjectRenderRequest, error) {
	if len(input.pages.Projects) != 1 {
		return directrender.ProjectRenderRequest{}, errors.New("publicapi: a project page needs exactly one project")
	}
	available := input.pages.Projects[0]
	resume := projectPageResume(input)
	text, lang, err := chooseProjectText(available.Document, resume.Lng)
	if err != nil {
		return directrender.ProjectRenderRequest{}, err
	}
	title := text.Name + " · " + resume.OwnerName
	document := available.Document
	return directrender.ProjectRenderRequest{
		Mode: directrender.ProjectRenderMode, CanonicalOrigin: input.origin.String(), PageURL: available.URL,
		DiscoveryEnabled: input.snapshot.DiscoveryEnabled, PageTitle: title, FaviconHref: projectFaviconHref(input.snapshot),
		Preview: projectPreview(input, title, projectDescription(text)), Resume: resume,
		Project: directrender.ProjectDetail{
			Slug: available.Slug, URL: available.URL, Lang: lang, Name: text.Name, Role: text.Role, Dates: document.Dates,
			Summary: text.Summary, Outcome: text.Outcome, Description: text.Description,
			Technologies: append([]string{}, document.Technologies...), RepositoryURL: renderableLink(document.RepositoryURL), DemoURL: renderableLink(document.DemoURL),
		},
		EntryLink:     projectEntryLink(available.EntryLink, document.RepositoryURL, document.DemoURL),
		ListURL:       input.origin.Resolve("/" + input.snapshot.Public.Slug + "/projects"),
		OtherProjects: input.pages.Available > 1,
	}, nil
}

// projectPageResume is what both pages show of their resume: the owner's
// full name, or the effective page title when the name is blank, the
// language, the color scheme ("light" when unset) and the date format. A
// public title the renderer reads as blank gives way to the default title,
// which never is.
func projectPageResume(input projectPageInput) directrender.ProjectPageResume {
	public := input.snapshot.Public
	owner := public.Document.PersonalDetails.FullName
	if blankText(owner) {
		owner = publicpage.EffectiveTitle(input.snapshot.PublicTitle, owner)
	}
	if blankText(owner) {
		owner = publicpage.EffectiveTitle(nil, public.Document.PersonalDetails.FullName)
	}
	scheme := schema.Light
	if public.Document.Customization.ColorScheme != nil {
		scheme = *public.Document.Customization.ColorScheme
	}
	return directrender.ProjectPageResume{
		Slug: public.Slug, URL: input.origin.Resolve("/" + public.Slug), OwnerName: owner, Lng: public.Lng,
		UILng: gateLng(public.Lng), ColorScheme: scheme, DateFormat: public.Document.Customization.DateFormat,
	}
}

// projectPreview is this page's title and description, uncut, with the
// resume's locale, card image and alt text.
func projectPreview(input projectPageInput, title, description string) previewmeta.Meta {
	resume := previewmeta.For(input.snapshot.Public, input.snapshot.PublicTitle)
	return previewmeta.Meta{Title: title, Description: description, Locale: resume.Locale, ImageAlt: resume.ImageAlt, ImageURL: input.imageURL}
}

// projectFaviconHref is the resume's favicon, or "" when it has none.
func projectFaviconHref(snapshot publicresume.Snapshot) string {
	if snapshot.FaviconEmoji == nil || *snapshot.FaviconEmoji == "" {
		return ""
	}
	return publicpage.FaviconHref(*snapshot.FaviconEmoji)
}

// chooseProjectText applies docs/design/projects.md "Language": the text of
// the resume language's primary subtag, else the default language, which an
// und resume always uses. A chosen text with a blank name falls back to the
// default language. lang is the chosen key when it is not the resume
// language's primary subtag, else "".
func chooseProjectText(document project.Document, resumeLng string) (project.Text, string, error) {
	primary, _, _ := strings.Cut(strings.ToLower(resumeLng), "-")
	key := document.DefaultLanguage
	if text, found := document.Text[primary]; found && primary != "und" && !blankText(text.Name) {
		key = primary
	}
	text, found := document.Text[key]
	if !found || blankText(text.Name) {
		return project.Text{}, "", errProjectWithoutName
	}
	if key == primary {
		return text, "", nil
	}
	return text, key, nil
}

// projectDescription is the summary, else the first 160 characters of the
// outcome, else "".
func projectDescription(text project.Text) string {
	if !blankText(text.Summary) {
		return text.Summary
	}
	if blankText(text.Outcome) {
		return ""
	}
	return firstGraphemes(text.Outcome, maxOutcomeDescriptionRunes)
}

// firstGraphemes returns text up to the last grapheme boundary at or below
// maxRunes runes, so a letter is never parted from its combining marks.
func firstGraphemes(text string, maxRunes int) string {
	if utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	end, runes := 0, 0
	graphemes := uniseg.NewGraphemes(text)
	for graphemes.Next() {
		runes += len(graphemes.Runes())
		if runes > maxRunes {
			break
		}
		_, end = graphemes.Positions()
	}
	return text[:end]
}

// blankText reports whether text is empty or only whitespace as the
// renderer's String.prototype.trim reads it: Unicode space separators, tab,
// vertical tab, form feed, the byte order mark and line terminators. It is
// not Go's strings.TrimSpace, which also trims U+0085 and keeps U+FEFF.
func blankText(text string) bool {
	return strings.IndexFunc(text, func(r rune) bool {
		switch r {
		case '\t', '\v', '\f', '\n', '\r', '\ufeff', '\u2028', '\u2029':
			return false
		}
		return !unicode.Is(unicode.Zs, r)
	}) < 0
}

// renderableLink is link when the renderer's URL parser accepts it, else "".
func renderableLink(link string) string {
	if !rendererLink(link) {
		return ""
	}
	return link
}

// projectEntryLink is the entry's own link as stored when it is an https
// link that differs from both the repository and the demo address, else "".
// The comparison lowercases the scheme and host, drops the default port and
// reads an empty path as "/"; the rest must match byte for byte.
func projectEntryLink(entryLink, repositoryURL, demoURL string) string {
	if !strings.HasPrefix(entryLink, "https://") {
		return ""
	}
	if normalized, ok := project.NormalizeLink(entryLink); !ok || normalized != entryLink || !rendererLink(entryLink) {
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

// comparableLink is link in the comparison form of projectEntryLink.
func comparableLink(link string) string {
	scheme, rest, found := strings.Cut(link, "://")
	if !found {
		return link
	}
	scheme = strings.ToLower(scheme)
	authority, tail := rest, ""
	if end := strings.IndexAny(rest, "/?#"); end >= 0 {
		authority, tail = rest[:end], rest[end:]
	}
	userinfo, host := "", authority
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		userinfo, host = authority[:at+1], authority[at+1:]
	}
	host = strings.ToLower(host)
	if scheme == "https" {
		host = strings.TrimSuffix(host, ":443")
	}
	if !strings.HasPrefix(tail, "/") {
		tail = "/" + tail
	}
	return scheme + "://" + userinfo + host + tail
}
