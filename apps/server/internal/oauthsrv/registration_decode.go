package oauthsrv

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
)

const (
	registerBodyLimit = 4096
	maxRedirectURIs   = 5
)

var (
	errRegisterRequestTooLarge = errors.New("oauth registration request too large")
	errRegisterRequestInvalid  = errors.New("oauth registration request invalid")
)

// registrationInput is a decoded RFC 7591 registration request. Each Set field
// reports whether the request named that optional member.
type registrationInput struct {
	// ClientName is the required client_name member, before ValidateClientName
	// checks it.
	ClientName string
	// RedirectURIs is the required redirect_uris member, before HandleRegister
	// checks its count and each URI.
	RedirectURIs []string
	// TokenEndpointAuthMethod is the token_endpoint_auth_method member: "none"
	// when present, empty when omitted.
	TokenEndpointAuthMethod string
	// ApplicationType is the application_type member; HandleRegister accepts
	// only native or web.
	ApplicationType string
	// ApplicationTypeSet reports whether the request named application_type.
	ApplicationTypeSet bool
	// GrantTypes is the grant_types member, checked by validGrantTypesSet.
	GrantTypes []string
	// GrantTypesSet reports whether the request named grant_types.
	GrantTypesSet bool
	// ResponseTypes is the response_types member, checked by
	// validResponseTypesSet.
	ResponseTypes []string
	// ResponseTypesSet reports whether the request named response_types.
	ResponseTypesSet bool
	// Scope is the scope member, checked by ParseScopes. It grants nothing at
	// registration; consent decides.
	Scope string
	// ScopeSet reports whether the request named scope.
	ScopeSet bool
}

// nativeLoopbackHosts is the set of loopback hosts an application_type
// native redirect may use. It equals the general redirect grammar's
// loopbackRedirectHosts in redirect.go, which admits "localhost" for a client
// that omits application_type, so a native client such as Claude Code may
// register a localhost loopback redirect; see
// docs/design/mcp-client-compatibility.md rule 3. A redirect must match the
// client's registered value exactly, port included.
var nativeLoopbackHosts = map[string]bool{
	"127.0.0.1": true,
	"localhost": true,
	"::1":       true,
}

func nativeLoopbackRedirect(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "http" && nativeLoopbackHosts[strings.ToLower(u.Hostname())]
}

// webRedirect reports whether raw is an https redirect, the application_type
// web requirement of docs/design/mcp-client-compatibility.md rule 3.
func webRedirect(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https"
}

// validGrantTypesForRegistration is the closed set a registration's
// grant_types member may name, per
// docs/design/mcp-client-compatibility.md rule 2.
var validGrantTypesForRegistration = map[string]bool{
	"authorization_code": true,
	"refresh_token":      true,
}

// validGrantTypesSet reports whether values is a non-empty, duplicate-free
// subset of validGrantTypesForRegistration.
func validGrantTypesSet(values []string) bool {
	if len(values) == 0 {
		return false
	}
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if !validGrantTypesForRegistration[value] || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

// validResponseTypesSet reports whether values is exactly ["code"], the only
// response_types a registration may name.
func validResponseTypesSet(values []string) bool {
	return len(values) == 1 && values[0] == "code"
}

func decodeRegistration(body io.Reader) (registrationInput, error) {
	raw, err := io.ReadAll(io.LimitReader(body, registerBodyLimit+1))
	if err != nil {
		return registrationInput{}, errRegisterRequestInvalid
	}
	if len(raw) > registerBodyLimit {
		return registrationInput{}, errRegisterRequestTooLarge
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return registrationInput{}, errRegisterRequestInvalid
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return registrationInput{}, errRegisterRequestInvalid
	}
	seen := make(map[string]bool, 8)
	fields := make(map[string]json.RawMessage, 8)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return registrationInput{}, errRegisterRequestInvalid
		}
		name, ok := token.(string)
		if !ok {
			return registrationInput{}, errRegisterRequestInvalid
		}
		if seen[name] {
			return registrationInput{}, errRegisterRequestInvalid
		}
		seen[name] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return registrationInput{}, errRegisterRequestInvalid
		}
		// A member name outside this set is ignored, per RFC 7591 section 2
		// and docs/design/mcp-client-compatibility.md rule 2: it is never
		// stored, logged, or echoed. The duplicate-member check above also
		// applies to it.
		switch name {
		case "client_name", "redirect_uris", "token_endpoint_auth_method", "application_type",
			"grant_types", "response_types", "scope":
			fields[name] = value
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return registrationInput{}, errRegisterRequestInvalid
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return registrationInput{}, errRegisterRequestInvalid
	}
	name, nameOK := fields["client_name"]
	redirects, redirectsOK := fields["redirect_uris"]
	if !nameOK || !redirectsOK {
		return registrationInput{}, errRegisterRequestInvalid
	}
	var registration registrationInput
	if err := json.Unmarshal(name, &registration.ClientName); err != nil {
		return registrationInput{}, errRegisterRequestInvalid
	}
	if err := json.Unmarshal(redirects, &registration.RedirectURIs); err != nil {
		return registrationInput{}, errRegisterRequestInvalid
	}
	if authMethod, ok := fields["token_endpoint_auth_method"]; ok {
		if err := json.Unmarshal(authMethod, &registration.TokenEndpointAuthMethod); err != nil || registration.TokenEndpointAuthMethod != "none" {
			return registrationInput{}, errRegisterRequestInvalid
		}
	}
	if applicationType, ok := fields["application_type"]; ok {
		if err := json.Unmarshal(applicationType, &registration.ApplicationType); err != nil {
			return registrationInput{}, errRegisterRequestInvalid
		}
		registration.ApplicationTypeSet = true
	}
	if grantTypes, ok := fields["grant_types"]; ok {
		if err := json.Unmarshal(grantTypes, &registration.GrantTypes); err != nil {
			return registrationInput{}, errRegisterRequestInvalid
		}
		registration.GrantTypesSet = true
	}
	if responseTypes, ok := fields["response_types"]; ok {
		if err := json.Unmarshal(responseTypes, &registration.ResponseTypes); err != nil {
			return registrationInput{}, errRegisterRequestInvalid
		}
		registration.ResponseTypesSet = true
	}
	if scope, ok := fields["scope"]; ok {
		if err := json.Unmarshal(scope, &registration.Scope); err != nil {
			return registrationInput{}, errRegisterRequestInvalid
		}
		registration.ScopeSet = true
	}
	return registration, nil
}
