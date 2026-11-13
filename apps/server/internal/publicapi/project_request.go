package publicapi

// The two project page render requests, built from one admitted snapshot by
// the rules in docs/design/projects-contract.md "Render envelope" and
// docs/design/ui/project-pages.md "Head".

import (
	"errors"
	"unicode/utf8"

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

// maxOutcomeDescriptionRunes is the number of code points of its outcome a
// project page without a summary takes as its description.
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
			Slug: available.Slug, URL: available.URL, Lang: lang, Name: text.Name, Role: presentText(text.Role),
			Dates: available.Document.Dates, Summary: presentText(text.Summary), Technologies: shown, MoreTechnologies: more,
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
			Slug: available.Slug, URL: available.URL, Lang: lang, Name: text.Name, Role: presentText(text.Role), Dates: document.Dates,
			Summary: presentText(text.Summary), Outcome: presentText(text.Outcome), Description: presentText(text.Description),
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
	if project.Blank(owner) {
		owner = publicpage.EffectiveTitle(input.snapshot.PublicTitle, owner)
	}
	if project.Blank(owner) {
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

// chooseProjectText is project.ChooseText as an error. The reader leaves
// out every project without a renderable name, so the error is only a
// guard.
func chooseProjectText(document project.Document, resumeLng string) (project.Text, string, error) {
	text, lang, ok := project.ChooseText(document, resumeLng)
	if !ok {
		return project.Text{}, "", errProjectWithoutName
	}
	return text, lang, nil
}

// presentText is an optional text as the request carries it: "", which the
// request leaves out, when it is blank by the renderer's trim, so the
// renderer's presence tests read it as absent.
func presentText(text string) string {
	if project.Blank(text) {
		return ""
	}
	return text
}

// projectDescription is the summary, else the first 160 code points of the
// outcome, else "": the renderer's decoder computes the same text and
// requires it exactly, so the cut may part a letter from its accent.
func projectDescription(text project.Text) string {
	if summary := presentText(text.Summary); summary != "" {
		return summary
	}
	outcome := presentText(text.Outcome)
	if utf8.RuneCountInString(outcome) <= maxOutcomeDescriptionRunes {
		return outcome
	}
	return string([]rune(outcome)[:maxOutcomeDescriptionRunes])
}
