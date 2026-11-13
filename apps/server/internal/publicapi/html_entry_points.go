package publicapi

import (
	"strings"

	"golang.org/x/net/html"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/publicresume"
)

const (
	projectsLinkClass    = "public-projects"
	publicActionsClass   = "public-actions"
	projectEntryAttr     = "data-project-entry"
	projectLinksRootAttr = "data-project-links"
)

func withEntryPoints(page publicPage, slug string, links []publicresume.ProjectLink, origin publicresume.PublicOrigin) publicPage {
	page.ProjectsURL = origin.Resolve("/" + slug + "/projects")
	page.ProjectLinks = nil
	for _, link := range links {
		page.ProjectLinks = append(page.ProjectLinks, directrender.EntryProjectLink{EntryID: link.EntryID, URL: link.URL})
	}
	return page
}

func projectsLinkText(lng string) string {
	language, _, _ := strings.Cut(lng, "-")
	if strings.EqualFold(language, "vi") {
		return "Dự án"
	}
	return "Projects"
}

type entryPointCheck struct {
	projectsURL  string
	label        string
	pages        map[string]string
	titled       map[string]bool
	seen         map[string]bool
	projectsLink int
}

func newEntryPointCheck(resume publicresume.PublicResume, page publicPage) *entryPointCheck {
	check := &entryPointCheck{projectsURL: page.ProjectsURL, label: projectsLinkText(resume.Lng), pages: map[string]string{}, titled: map[string]bool{}, seen: map[string]bool{}}
	if page.ProjectsURL == "" {
		return check
	}
	for _, link := range page.ProjectLinks {
		check.pages[link.EntryID] = link.URL
	}
	sections := resume.Document.Customization.Layout.Sections
	for _, keys := range [][]string{sections.Main, sections.Sidebar} {
		for _, key := range keys {
			section, ok := resume.Document.Content[key]
			if !ok || section.SectionType != string(schema.Project) {
				continue
			}
			for _, entry := range section.ProjectEntries {
				if entry.Title != nil && *entry.Title != "" {
					check.titled[entry.ID] = true
				}
			}
		}
	}
	return check
}

func (c *entryPointCheck) root(node *html.Node) (int, string) {
	want := boolToInt(c.projectsURL != "")
	if attributeCount(node, projectLinksRootAttr) != want || attribute(node, projectLinksRootAttr) != "" {
		return 0, "project_links_marker"
	}
	return want, ""
}

func (c *entryPointCheck) anchor(node *html.Node) (bool, string) {
	if attributeCount(node, projectEntryAttr) != 0 {
		return true, c.entryAnchor(node)
	}
	if relHasToken(attribute(node, "class"), projectsLinkClass) {
		return true, c.barAnchor(node)
	}
	return false, ""
}

func (c *entryPointCheck) entryAnchor(node *html.Node) string {
	entryID := attribute(node, projectEntryAttr)
	pageURL, mapped := c.pages[entryID]
	if !mapped || !c.titled[entryID] || c.seen[entryID] || attributeCount(node, projectEntryAttr) != 1 || attribute(node, "href") != pageURL ||
		!onlyAttributes(node, projectEntryAttr, "href", "style") || !elementNamed(node.Parent, "strong") {
		return "project_entry_link"
	}
	c.seen[entryID] = true
	return ""
}

func (c *entryPointCheck) barAnchor(node *html.Node) string {
	if len(c.pages) == 0 || c.projectsLink != 0 || len(node.Attr) != 2 || attributeCount(node, "class") != 1 || attribute(node, "class") != projectsLinkClass ||
		attribute(node, "href") != c.projectsURL || !elementNamed(node.Parent, "span") || !relHasToken(attribute(node.Parent, "class"), publicActionsClass) {
		return "projects_link"
	}
	child := node.FirstChild
	if child == nil || child.NextSibling != nil || child.Type != html.TextNode || child.Data != c.label {
		return "projects_link"
	}
	for ancestor := node.Parent; ancestor != nil; ancestor = ancestor.Parent {
		if elementNamed(ancestor, "article") {
			return "projects_link"
		}
	}
	c.projectsLink++
	return ""
}

func (c *entryPointCheck) finish() string {
	if len(c.pages) != 0 && c.projectsLink != 1 {
		return "projects_link"
	}
	for entryID := range c.pages {
		if c.titled[entryID] && !c.seen[entryID] {
			return "project_entry_link"
		}
	}
	return ""
}

func elementNamed(node *html.Node, name string) bool {
	return node != nil && node.Type == html.ElementNode && node.Data == name
}
