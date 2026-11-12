package publicapi

import (
	"encoding/json"

	"danny.vn/aboutme/apps/server/internal/publicresume"
)

// NewPublicJSON encodes resume and its project link context in the public
// JSON envelope. projectLinks is always present, empty when no visible entry
// links an available project, so a refetch replaces the browser's links
// whole (docs/design/projects-contract.md "Project link context").
func NewPublicJSON(resume publicresume.PublicResume, links []publicresume.ProjectLink) (SelectedResponse, error) {
	if links == nil {
		links = []publicresume.ProjectLink{}
	}
	body, err := json.Marshal(struct {
		Data publicJSONData `json:"data"`
	}{Data: publicJSONData{PublicResume: resume, ProjectLinks: links}})
	if err != nil {
		return SelectedResponse{}, err
	}
	body = append(body, '\n')
	return NewSelectedResponse(200, "application/json; charset=utf-8", "no-cache, must-revalidate", body, nil)
}

// publicJSONData is the public JSON read: the public resume and, only in
// this representation, its project link context.
type publicJSONData struct {
	publicresume.PublicResume
	// ProjectLinks is each available project page, by linking entry.
	ProjectLinks []publicresume.ProjectLink `json:"projectLinks"`
}
