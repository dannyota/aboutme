package publicapi

import "errors"

// NewPublicPhoto creates a cacheable public photo response.
func NewPublicPhoto(body []byte, contentType string) (SelectedResponse, error) {
	if contentType != "image/jpeg" && contentType != "image/png" {
		return SelectedResponse{}, errors.New("unsupported public photo content type")
	}
	return NewSelectedResponse(200, contentType, "no-cache, must-revalidate", body, nil)
}
