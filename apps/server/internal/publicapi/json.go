package publicapi

import (
	"encoding/json"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

// NewPublicJSON encodes resume in the public JSON envelope.
func NewPublicJSON(resume publicresume.PublicResume) (SelectedResponse, error) {
	body, err := json.Marshal(struct {
		Data publicresume.PublicResume `json:"data"`
	}{Data: resume})
	if err != nil {
		return SelectedResponse{}, err
	}
	body = append(body, '\n')
	return NewSelectedResponse(200, "application/json; charset=utf-8", "no-cache, must-revalidate", body, nil)
}
