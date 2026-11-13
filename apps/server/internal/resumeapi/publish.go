package resumeapi

import (
	"encoding/json"
	"io"
)

type optionalSlug struct {
	Present bool
	Value   string
}

type optionalText struct {
	Present bool
	Value   string
}

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
	ShowcaseEnabled optionalBool
	ShowcaseRole    optionalText
}

type currentPublish struct {
	Slug            *string
	Live            bool
	DownloadEnabled bool
	SEOGeoEnabled   bool
	PublicTitle     *string
	FaviconEmoji    *string
	SignInToView    bool
	ShowcaseEnabled bool
	ShowcaseRole    *string
	Revision        int64
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

type publishShapeError struct{ Field string }

func (e *publishShapeError) Error() string { return "publish request shape is invalid" }

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
		case "slug", "live", "downloadEnabled", "seoGeoEnabled", "publicTitle", "faviconEmoji", "signInToView", "showcaseEnabled", "showcaseRole":
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
	showcaseEnabled, showcaseErr := decodePublishOptionalBool(fields, "showcaseEnabled")
	if showcaseErr != nil {
		return publishInput{}, showcaseErr
	}
	showcaseRole, roleErr := decodePublishOptionalText(fields, "showcaseRole")
	if roleErr != nil {
		return publishInput{}, roleErr
	}
	input.ShowcaseEnabled, input.ShowcaseRole = showcaseEnabled, showcaseRole
	return input, nil
}

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
