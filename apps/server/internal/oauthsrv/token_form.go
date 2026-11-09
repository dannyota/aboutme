package oauthsrv

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const oauthFormBodyLimit = 4096

const rawResourceFormKey = "\x00oauthsrv_raw_resource"

func decodeOAuthForm(r *http.Request, allowed map[string]bool) (url.Values, error) {
	values := r.Header.Values("Content-Type")
	if len(values) != 1 || values[0] != "application/x-www-form-urlencoded" {
		return nil, errors.New("oauth form media type")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, oauthFormBodyLimit+1))
	if err != nil || len(raw) > oauthFormBodyLimit {
		return nil, errors.New("oauth form body")
	}
	form, err := url.ParseQuery(string(raw))
	if err != nil {
		if malformedAuthorizationCodeResource(form, string(raw)) {
			return nil, errOAuthResourceParse
		}
		return nil, errors.New("oauth form parse")
	}
	for key, values := range form {
		if !allowed[key] || (key != "resource" && len(values) != 1) {
			return nil, errors.New("oauth form keys")
		}
	}
	if _, ok := form["resource"]; ok {
		form[rawResourceFormKey] = []string{string(raw)}
	}
	return form, nil
}

func malformedAuthorizationCodeResource(form url.Values, raw string) bool {
	fields := strings.Split(raw, "&")
	if len(fields) != 6 {
		return false
	}
	resources := 0
	for _, field := range fields {
		if strings.HasPrefix(field, "resource=") {
			resources++
		}
	}
	if resources != 1 || len(form) != 5 || form.Get("grant_type") != "authorization_code" {
		return false
	}
	return exactFormKeys(form, "grant_type", "code", "redirect_uri", "client_id", "code_verifier")
}

func exactFormKeys(form url.Values, keys ...string) bool {
	count := len(form)
	if _, ok := form[rawResourceFormKey]; ok {
		count--
	}
	if count != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := form[key]; !ok || form.Get(key) == "" {
			return false
		}
	}
	return true
}

// exactRefreshForm allows grant_type and refresh_token, plus at most one
// non-empty client_id and a resource that canonicalResource checks. See rule 4
// of docs/design/mcp-client-compatibility.md.
func exactRefreshForm(form url.Values) bool {
	count := len(form)
	if _, ok := form[rawResourceFormKey]; ok {
		count--
	}
	want := 2
	for _, optional := range []string{"client_id", "resource"} {
		if _, ok := form[optional]; ok {
			want++
		}
	}
	if count != want {
		return false
	}
	for _, key := range []string{"grant_type", "refresh_token"} {
		if len(form[key]) != 1 || form.Get(key) == "" {
			return false
		}
	}
	if clientIDs, ok := form["client_id"]; ok && (len(clientIDs) != 1 || clientIDs[0] == "") {
		return false
	}
	return true
}

func exactAuthorizationCodeForm(form url.Values) bool {
	keys := []string{"grant_type", "code", "redirect_uri", "client_id", "code_verifier"}
	if _, ok := form["resource"]; ok {
		keys = append(keys, "resource")
	}
	count := len(form)
	if _, ok := form[rawResourceFormKey]; ok {
		count--
	}
	if count != len(keys) {
		return false
	}
	for _, key := range keys[:5] {
		if len(form[key]) != 1 || form.Get(key) == "" {
			return false
		}
	}
	return true
}
