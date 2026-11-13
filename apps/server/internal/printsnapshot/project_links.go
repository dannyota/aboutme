package printsnapshot

import (
	"encoding/json"
	"net/url"
	"strings"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/publicroots"
)

func WithProjectLinks(envelope Envelope, origin publicresume.PublicOrigin, slug string, links []publicresume.ProjectLink) (Envelope, error) {
	if envelope.Version != 1 || len(envelope.ProjectLinks) != 0 {
		return Envelope{}, errInvalid
	}
	if len(links) == 0 {
		return envelope, nil
	}
	if origin.String() == "" || !publicroots.ValidSlug(slug) {
		return Envelope{}, errInvalid
	}
	pages := make(map[string]string, len(links))
	for _, link := range links {
		_, repeated := pages[link.EntryID]
		if repeated || !project.ValidSlug(link.ProjectSlug) || link.URL != origin.Resolve("/"+slug+"/projects/"+link.ProjectSlug) {
			return Envelope{}, errInvalid
		}
		pages[link.EntryID] = link.URL
	}
	envelope.Version, envelope.ProjectLinks = 2, pages
	if _, err := Marshal(envelope); err != nil {
		return Envelope{}, err
	}
	return envelope, nil
}

func validVersionLinks(source Envelope) bool {
	switch source.Version {
	case 1:
		return len(source.ProjectLinks) == 0
	case 2:
		if len(source.ProjectLinks) == 0 {
			return false
		}
		visible := visibleProjectEntries(source.Document)
		for entryID, pageURL := range source.ProjectLinks {
			if !visible[entryID] || !pageAddress(pageURL) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func ProjectLinksMatch(document, links json.RawMessage) bool {
	var pages map[string]string
	if json.Unmarshal(links, &pages) != nil || len(pages) == 0 {
		return false
	}
	var shape struct {
		Content map[string]struct {
			SectionType string `json:"sectionType"`
			Entries     []struct {
				ID string `json:"id"`
			} `json:"entries"`
		} `json:"content"`
		Customization struct {
			Layout struct {
				Sections struct {
					Main    []string `json:"main"`
					Sidebar []string `json:"sidebar"`
				} `json:"sections"`
			} `json:"layout"`
		} `json:"customization"`
	}
	if json.Unmarshal(document, &shape) != nil {
		return false
	}
	visible := map[string]bool{}
	sections := shape.Customization.Layout.Sections
	for _, keys := range [][]string{sections.Main, sections.Sidebar} {
		for _, key := range keys {
			section, ok := shape.Content[key]
			if !ok || section.SectionType != string(schema.Project) {
				continue
			}
			for _, entry := range section.Entries {
				if entry.ID != "" {
					visible[entry.ID] = true
				}
			}
		}
	}
	for entryID, pageURL := range pages {
		if !visible[entryID] || !pageAddress(pageURL) {
			return false
		}
	}
	return true
}

func visibleProjectEntries(document publicresume.PublicResumeDocument) map[string]bool {
	ids := map[string]bool{}
	sections := document.Customization.Layout.Sections
	for _, keys := range [][]string{sections.Main, sections.Sidebar} {
		for _, key := range keys {
			section, ok := document.Content[key]
			if !ok || section.SectionType != string(schema.Project) {
				continue
			}
			for _, entry := range section.ProjectEntries {
				if entry.ID != "" {
					ids[entry.ID] = true
				}
			}
		}
	}
	return ids
}

func pageAddress(value string) bool {
	if !strings.HasPrefix(value, "https://") && !strings.HasPrefix(value, "http://") || strings.ContainsAny(value, "?#%") {
		return false
	}
	for index := range len(value) {
		if value[index] < 0x21 || value[index] > 0x7e {
			return false
		}
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	segments := strings.Split(parsed.Path, "/")
	return len(segments) == 4 && segments[0] == "" && publicroots.ValidSlug(segments[1]) && segments[2] == "projects" && project.ValidSlug(segments[3])
}
