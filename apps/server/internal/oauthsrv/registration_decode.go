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

type registrationInput struct {
	ClientName              string
	RedirectURIs            []string
	TokenEndpointAuthMethod string
	ApplicationType         string
	ApplicationTypeSet      bool
	GrantTypes              []string
	GrantTypesSet           bool
	ResponseTypes           []string
	ResponseTypesSet        bool
	Scope                   string
	ScopeSet                bool
}

func nativeLoopbackRedirect(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "http" && loopbackRedirectHost(strings.ToLower(u.Hostname()))
}

func webRedirect(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https"
}

func registrationGrantType(value string) bool {
	switch value {
	case "authorization_code", "refresh_token":
		return true
	default:
		return false
	}
}

func validGrantTypesSet(values []string) bool {
	if len(values) == 0 {
		return false
	}
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if !registrationGrantType(value) || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

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
