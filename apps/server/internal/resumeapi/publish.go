package resumeapi

import (
	"encoding/json"
	"io"
)

type optionalSlug struct {
	Present bool
	Value   string
}

// optionalText is a publication text setting: absent keeps the stored value,
// and a present value (possibly empty, which clears it) replaces it.
type optionalText struct {
	Present bool
	Value   string
}

// optionalBool is an optional publish switch: absent keeps the stored value
// (docs/design/viewer-analytics/sign-in-to-view.md "Setting"; AC-VIEW-001).
type optionalBool struct {
	Present bool
	Value   bool
}

type publishInput struct {
	Slug            optionalSlug
	Live            bool
	DownloadEnabled bool
	SEOGeoEnabled   bool
	PublicTitle     optionalText
	FaviconEmoji    optionalText
	SignInToView    optionalBool
}

type currentPublish struct {
	Slug            *string
	Live            bool
	DownloadEnabled bool
	SEOGeoEnabled   bool
	PublicTitle     *string
	FaviconEmoji    *string
	// SignInToView is the resolved effective value: for the current state it
	// is the stored switch, and for the validated result it is the caller's
	// merged final value (input.SignInToView.Value when present, else the
	// stored value), computed in resumes_publish.go before validatePublish
	// runs (docs/design/viewer-analytics/sign-in-to-view.md "Setting").
	SignInToView bool
	Revision     int64
}

type publishPrepared struct {
	Effective   currentPublish
	ChangedSlug bool
	Issues      []publishIssue
}

type publishIssue struct {
	Path    string
	Code    string
	Message string
}

// publishShapeError has no request value because its caller must map all
// malformed publish bodies to the closed request_invalid response.
type publishShapeError struct{ Field string }

func (e *publishShapeError) Error() string { return "publish request shape is invalid" }

// decodePublish accepts only the closed PublishResumeRequest object. Its caller
// owns content-type and header validation; this seam owns the bounded JSON body.
func decodePublish(body io.Reader) (publishInput, error) {
	raw, err := readBoundedBody(body, maxJSONBodyBytes)
	if err != nil {
		return publishInput{}, err
	}
	if tokenErr := validateJSONTokens(raw, maxJSONDepth); tokenErr != nil {
		return publishInput{}, &publishShapeError{Field: "body"}
	}

	var fields map[string]json.RawMessage
	if decodeErr := json.Unmarshal(raw, &fields); decodeErr != nil || fields == nil {
		return publishInput{}, &publishShapeError{Field: "body"}
	}
	for name := range fields {
		switch name {
		case "slug", "live", "downloadEnabled", "seoGeoEnabled", "publicTitle", "faviconEmoji", "signInToView":
		default:
			return publishInput{}, &publishShapeError{Field: "body"}
		}
	}

	live, err := decodePublishRequiredBool(fields, "live")
	if err != nil {
		return publishInput{}, err
	}
	downloadEnabled, err := decodePublishRequiredBool(fields, "downloadEnabled")
	if err != nil {
		return publishInput{}, err
	}
	seoGeoEnabled, err := decodePublishRequiredBool(fields, "seoGeoEnabled")
	if err != nil {
		return publishInput{}, err
	}

	input := publishInput{Live: live, DownloadEnabled: downloadEnabled, SEOGeoEnabled: seoGeoEnabled}
	if rawSlug, ok := fields["slug"]; ok {
		var slug *string
		if err := json.Unmarshal(rawSlug, &slug); err != nil || slug == nil || *slug == "" {
			return publishInput{}, &publishShapeError{Field: "slug"}
		}
		input.Slug = optionalSlug{Present: true, Value: *slug}
	}
	publicTitle, titleErr := decodePublishOptionalText(fields, "publicTitle")
	if titleErr != nil {
		return publishInput{}, titleErr
	}
	faviconEmoji, emojiErr := decodePublishOptionalText(fields, "faviconEmoji")
	if emojiErr != nil {
		return publishInput{}, emojiErr
	}
	input.PublicTitle, input.FaviconEmoji = publicTitle, faviconEmoji
	signInToView, signInErr := decodePublishOptionalBool(fields, "signInToView")
	if signInErr != nil {
		return publishInput{}, signInErr
	}
	input.SignInToView = signInToView
	return input, nil
}

// decodePublishOptionalBool accepts an absent field or a JSON boolean; null
// and every other type are malformed. Absent keeps the caller's stored
// value (docs/design/viewer-analytics/sign-in-to-view.md "Setting").
func decodePublishOptionalBool(fields map[string]json.RawMessage, field string) (optionalBool, error) {
	raw, ok := fields[field]
	if !ok {
		return optionalBool{}, nil
	}
	var value *bool
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return optionalBool{}, &publishShapeError{Field: field}
	}
	return optionalBool{Present: true, Value: *value}, nil
}

// decodePublishOptionalText accepts an absent field or a JSON string; null
// and every other type are malformed.
func decodePublishOptionalText(fields map[string]json.RawMessage, field string) (optionalText, error) {
	raw, ok := fields[field]
	if !ok {
		return optionalText{}, nil
	}
	var value *string
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return optionalText{}, &publishShapeError{Field: field}
	}
	return optionalText{Present: true, Value: *value}, nil
}

func decodePublishRequiredBool(fields map[string]json.RawMessage, field string) (bool, error) {
	raw, ok := fields[field]
	if !ok {
		return false, &publishShapeError{Field: field}
	}
	var value *bool
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return false, &publishShapeError{Field: field}
	}
	return *value, nil
}
