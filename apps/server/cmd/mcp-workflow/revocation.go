package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	maxOAuthResponseBytes = 4 << 10
	revocationCallTimeout = 30 * time.Second
	formContentType       = "application/x-www-form-urlencoded"
)

// httpRevocation sends the design's narrow RFC 7009 exception, the bearer
// probe, and the refresh-authority test through the restricted client. Tokens
// never enter errors or logs.
type httpRevocation struct {
	client *http.Client
	origin string
}

// Revoke posts exactly token=<refresh>&token_type_hint=refresh_token with no
// cookie or bearer header. A transport error or 5xx permits one
// byte-equivalent retry.
func (h httpRevocation) Revoke(ctx context.Context, refreshToken string) error {
	if !validBearerValue(refreshToken) {
		return errRevocation
	}
	form := []byte(url.Values{"token": {refreshToken}, "token_type_hint": {tokenHintRefresh}}.Encode())
	for attempt := 0; attempt < 2; attempt++ {
		status, _, _, err := h.postWithDate(ctx, "/oauth/revoke", form)
		if err == nil && status >= http.StatusOK && status < http.StatusMultipleChoices {
			return nil
		}
		if err == nil && status < http.StatusInternalServerError {
			return errRevocation
		}
	}
	return errRevocation
}

// Probe sends one authenticated GET to /mcp and returns the status and the
// response's server Date.
func (h httpRevocation) Probe(ctx context.Context, accessToken string) (int, time.Time, error) {
	if !validBearerValue(accessToken) {
		return 0, time.Time{}, errRevocation
	}
	requestContext, cancel := context.WithTimeout(ctx, revocationCallTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, h.origin+mcpEndpointPath, nil)
	if err != nil {
		return 0, time.Time{}, errRevocation
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Accept", "text/event-stream")
	response, err := h.client.Do(request)
	if err != nil {
		return 0, time.Time{}, errRevocation
	}
	date, dated := responseDate(response)
	if closeErr := response.Body.Close(); closeErr != nil {
		return 0, time.Time{}, errRevocation
	}
	if !dated {
		return response.StatusCode, time.Time{}, nil
	}
	return response.StatusCode, date, nil
}

// Refresh submits the exact current refresh token. invalid_grant proves dead
// authority; a well-formed rotation returns the successor tokens.
func (h httpRevocation) Refresh(ctx context.Context, refreshToken string) (refreshOutcome, error) {
	if !validBearerValue(refreshToken) {
		return refreshOutcome{}, errRevocation
	}
	form := []byte(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}}.Encode())
	status, body, date, err := h.postWithDate(ctx, "/oauth/token", form)
	if err != nil || date.IsZero() {
		return refreshOutcome{}, errRevocation
	}
	if status == http.StatusBadRequest {
		var failure struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &failure) == nil && failure.Error == "invalid_grant" {
			return refreshOutcome{Dead: true, ServerDate: date}, nil
		}
		return refreshOutcome{}, errRevocation
	}
	if status != http.StatusOK {
		return refreshOutcome{}, errRevocation
	}
	var token struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
	}
	if !strictJSON(body, &token) || token.TokenType != "Bearer" || token.ExpiresIn <= 0 || token.Scope != "resumes:read resumes:write" ||
		!validBearerValue(token.AccessToken) || !validBearerValue(token.RefreshToken) || token.RefreshToken == refreshToken {
		return refreshOutcome{}, errRevocation
	}
	return refreshOutcome{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, ServerDate: date}, nil
}

func (h httpRevocation) postWithDate(ctx context.Context, path string, form []byte) (int, []byte, time.Time, error) {
	requestContext, cancel := context.WithTimeout(ctx, revocationCallTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, h.origin+path, bytes.NewReader(form))
	if err != nil {
		return 0, nil, time.Time{}, errRevocation
	}
	request.Header.Set("Content-Type", formContentType)
	response, err := h.client.Do(request)
	if err != nil {
		return 0, nil, time.Time{}, errRevocation
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxOAuthResponseBytes+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(body) > maxOAuthResponseBytes {
		return 0, nil, time.Time{}, errRevocation
	}
	date, dated := responseDate(response)
	if !dated {
		return response.StatusCode, body, time.Time{}, nil
	}
	return response.StatusCode, body, date, nil
}

// responseDate returns the response's same-origin server Date.
func responseDate(response *http.Response) (time.Time, bool) {
	values := response.Header.Values("Date")
	if len(values) != 1 {
		return time.Time{}, false
	}
	parsed, err := http.ParseTime(strings.TrimSpace(values[0]))
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}
