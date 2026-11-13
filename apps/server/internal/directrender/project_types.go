package directrender

import (
	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/previewmeta"
)

const (
	ProjectListRenderMode = "project-list"
	ProjectRenderMode     = "project"
)

type ProjectPageResume struct {
	Slug        string             `json:"slug"`
	URL         string             `json:"url"`
	OwnerName   string             `json:"ownerName"`
	Lng         string             `json:"lng"`
	UILng       string             `json:"uiLng"`
	ColorScheme schema.ColorScheme `json:"colorScheme"`
	DateFormat  schema.DateFormat  `json:"dateFormat"`
}

type ProjectSummary struct {
	Slug             string            `json:"slug"`
	URL              string            `json:"url"`
	Lang             string            `json:"lang,omitempty"`
	Name             string            `json:"name"`
	Role             string            `json:"role,omitempty"`
	Dates            *schema.DateRange `json:"dates,omitempty"`
	Summary          string            `json:"summary,omitempty"`
	Technologies     []string          `json:"technologies"`
	MoreTechnologies int               `json:"moreTechnologies,omitempty"`
}

type ProjectDetail struct {
	Slug          string            `json:"slug"`
	URL           string            `json:"url"`
	Lang          string            `json:"lang,omitempty"`
	Name          string            `json:"name"`
	Role          string            `json:"role,omitempty"`
	Dates         *schema.DateRange `json:"dates,omitempty"`
	Summary       string            `json:"summary,omitempty"`
	Outcome       string            `json:"outcome,omitempty"`
	Description   string            `json:"description,omitempty"`
	Technologies  []string          `json:"technologies"`
	RepositoryURL string            `json:"repositoryUrl,omitempty"`
	DemoURL       string            `json:"demoUrl,omitempty"`
}

type ProjectListRenderRequest struct {
	Mode             string            `json:"mode"`
	CanonicalOrigin  string            `json:"canonicalOrigin"`
	PageURL          string            `json:"pageUrl"`
	DiscoveryEnabled bool              `json:"discoveryEnabled"`
	PageTitle        string            `json:"pageTitle"`
	FaviconHref      string            `json:"faviconHref"`
	Preview          previewmeta.Meta  `json:"preview"`
	Resume           ProjectPageResume `json:"resume"`
	Projects         []ProjectSummary  `json:"projects"`
}

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
	EntryLink        string            `json:"entryLink,omitempty"`
	ListURL          string            `json:"listUrl"`
	OtherProjects    bool              `json:"otherProjects"`
}
