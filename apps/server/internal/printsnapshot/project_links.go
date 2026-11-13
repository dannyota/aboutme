package printsnapshot

import (
	"net/url"
	"strings"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/publicresume"
)

// validVersionLinks reports whether the envelope's version and project links
// agree (docs/design/projects-contract.md "Print"). Version 1 carries no
// link; an empty map encodes as no map, so it stays byte for byte version 1.
// Version 2 carries at least one link, each from the id of a visible project
// entry of the document to a page address on the public origin's scheme:
// https in production, http only on a local development origin.
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

// visibleProjectEntries is the set of entry ids of the project sections the
// layout shows. The projection has already dropped hidden entries.
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

// pageAddress accepts an absolute https or http address of printable ASCII
// with a host and no user info, query or fragment, the schemes a public
// origin may have (publicresume.ParsePublicOrigin).
func pageAddress(value string) bool {
	if !strings.HasPrefix(value, "https://") && !strings.HasPrefix(value, "http://") || strings.ContainsAny(value, "?#") {
		return false
	}
	for index := range len(value) {
		if value[index] < 0x21 || value[index] > 0x7e {
			return false
		}
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Host != "" && parsed.User == nil
}
