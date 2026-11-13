package publicapi

import (
	"encoding/json"

	"danny.vn/aboutme/apps/server/internal/publicresume"
)

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

type publicJSONData struct {
	publicresume.PublicResume
	ProjectLinks []publicresume.ProjectLink `json:"projectLinks"`
}
