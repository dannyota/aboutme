package resumeapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"
)

// linkExistingEntry returns raw, a canonical current-version document, with
// projectID set on the entry entryID and nothing else changed: the entry
// keeps its text, position and visibility (docs/design/projects.md
// "Selection"). The entry must be an unlinked entry of a project section.
// Ownership and uniqueness of the link are left to the store's link check,
// which every document write runs.
func linkExistingEntry(raw json.RawMessage, entryID string, projectID uuid.UUID) (json.RawMessage, error) {
	parts, err := decodeRawDocument(raw)
	if err != nil {
		return nil, err
	}
	for key, sectionRaw := range parts.Content {
		section, sectionErr := decodeRawSection(sectionRaw)
		if sectionErr != nil {
			return nil, sectionErr
		}
		for _, entryRaw := range section.Entries {
			id, idErr := rawEntryID(entryRaw)
			if idErr != nil {
				return nil, idErr
			}
			if id != entryID {
				continue
			}
			if section.SectionType != string(schema.Project) {
				return nil, validationIssueAt("entryId", "entryId must name an entry of a project section")
			}
			linked, linkErr := withProjectID(entryRaw, projectID)
			if linkErr != nil {
				return nil, linkErr
			}
			return applyEntryUpsert(raw, key, linked)
		}
	}
	return nil, &clientError{Status: http.StatusNotFound, Code: "entry_not_found", Message: "entry not found"}
}

// withProjectID adds projectId to an unlinked entry, keeping every other
// member's encoding.
func withProjectID(entry json.RawMessage, projectID uuid.UUID) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(entry, &fields); err != nil {
		return nil, fmt.Errorf("resumeapi: decode project entry: %w", err)
	}
	if _, linked := fields["projectId"]; linked {
		return nil, &clientError{Status: http.StatusConflict, Code: "project_already_linked", Message: "the entry already links a project"}
	}
	link, err := json.Marshal(projectID.String())
	if err != nil {
		return nil, fmt.Errorf("resumeapi: encode project id: %w", err)
	}
	fields["projectId"] = link
	linked, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("resumeapi: encode project entry: %w", err)
	}
	return linked, nil
}
