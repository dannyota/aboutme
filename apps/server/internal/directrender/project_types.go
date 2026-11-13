package directrender

import (
	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/previewmeta"
)

// ProjectListRenderMode and ProjectRenderMode identify the renderer modes for
// a resume's project list and for one of its project pages
// (docs/design/projects-contract.md "Render envelope").
const (
	ProjectListRenderMode = "project-list"
	ProjectRenderMode     = "project"
)

// ProjectPageResume is everything a project page shows of its resume. It
// carries no contact detail, photo, entry or other resume content.
type ProjectPageResume struct {
	Slug string `json:"slug"`
	// URL is the resume page's absolute address.
	URL string `json:"url"`
	// OwnerName is the resume's full name, or its effective page title when
	// the name is blank.
	OwnerName string `json:"ownerName"`
	// Lng is the resume language tag, for the document's lang attribute.
	Lng string `json:"lng"`
	// UILng is "vi" or "en": the language of the page's own copy.
	UILng string `json:"uiLng"`
	// ColorScheme is the resume's public color scheme, "light" when unset.
	ColorScheme schema.ColorScheme `json:"colorScheme"`
	// DateFormat formats the project dates as on the resume page.
	DateFormat schema.DateFormat `json:"dateFormat"`
}

// ProjectSummary is one row of the project list, from the library project's
// text in one language.
type ProjectSummary struct {
	Slug string `json:"slug"`
	// URL is the project page's absolute address.
	URL string `json:"url"`
	// Lang is set only when the text is not in the resume's language.
	Lang         string            `json:"lang,omitempty"`
	Name         string            `json:"name"`
	Role         string            `json:"role,omitempty"`
	Dates        *schema.DateRange `json:"dates,omitempty"`
	Summary      string            `json:"summary,omitempty"`
	Technologies []string          `json:"technologies"`
}

// ProjectDetail is the project a project page shows, from the library
// project's fields and its text in one language.
type ProjectDetail struct {
	Slug string `json:"slug"`
	// URL is the project page's absolute address.
	URL string `json:"url"`
	// Lang is set only when the text is not in the resume's language.
	Lang    string            `json:"lang,omitempty"`
	Name    string            `json:"name"`
	Role    string            `json:"role,omitempty"`
	Dates   *schema.DateRange `json:"dates,omitempty"`
	Summary string            `json:"summary,omitempty"`
	Outcome string            `json:"outcome,omitempty"`
	// Description is the stored rich text, already sanitized on write.
	Description   string   `json:"description,omitempty"`
	Technologies  []string `json:"technologies"`
	RepositoryURL string   `json:"repositoryUrl,omitempty"`
	DemoURL       string   `json:"demoUrl,omitempty"`
}

// ProjectListRenderRequest contains all renderer inputs for
// /{slug}/projects. Projects are in the resume's layout and entry order.
type ProjectListRenderRequest struct {
	Mode             string `json:"mode"`
	CanonicalOrigin  string `json:"canonicalOrigin"`
	PageURL          string `json:"pageUrl"`
	DiscoveryEnabled bool   `json:"discoveryEnabled"`
	// PageTitle and FaviconHref are the exact head values the validator
	// expects; FaviconHref is "" when the owner set no icon.
	PageTitle   string `json:"pageTitle"`
	FaviconHref string `json:"faviconHref"`
	// Preview carries this page's title and description and the resume's
	// locale, card image and alt text. An empty description means the page
	// has no description.
	Preview  previewmeta.Meta  `json:"preview"`
	Resume   ProjectPageResume `json:"resume"`
	Projects []ProjectSummary  `json:"projects"`
}

// ProjectRenderRequest contains all renderer inputs for
// /{slug}/projects/{project-slug}.
type ProjectRenderRequest struct {
	Mode             string            `json:"mode"`
	CanonicalOrigin  string            `json:"canonicalOrigin"`
	PageURL          string            `json:"pageUrl"`
	DiscoveryEnabled bool              `json:"discoveryEnabled"`
	PageTitle        string            `json:"pageTitle"`
	FaviconHref      string            `json:"faviconHref"`
	Preview          previewmeta.Meta  `json:"preview"`
	Resume           ProjectPageResume `json:"resume"`
	Project          ProjectDetail     `json:"project"`
	// EntryLink is the resume entry's own https link, sent only when it
	// differs from both the repository and the demo address.
	EntryLink string `json:"entryLink,omitempty"`
	// ListURL is the project list's absolute address.
	ListURL string `json:"listUrl"`
	// OtherProjects reports whether the resume has another available project.
	OtherProjects bool `json:"otherProjects"`
}
