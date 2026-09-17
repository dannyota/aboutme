package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

func loadProviderLoginFlag(raw string) (bool, error) {
	switch strings.TrimSpace(raw) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, errors.New("config: PROVIDER_LOGIN_ENABLED must be true or false")
	}
}

type providerEndpoints struct {
	googleIssuer    string
	linkedinIssuer  string
	githubAuthorize string
	githubToken     string
	githubAPI       string
}

func loadProviderEndpoints(getenv func(string) string, env, publicOrigin string) (providerEndpoints, error) {
	values := map[string]string{
		"GOOGLE_OIDC_ISSUER_URL":     strings.TrimSpace(getenv("GOOGLE_OIDC_ISSUER_URL")),
		"LINKEDIN_OIDC_ISSUER_URL":   strings.TrimSpace(getenv("LINKEDIN_OIDC_ISSUER_URL")),
		"GITHUB_OAUTH_AUTHORIZE_URL": strings.TrimSpace(getenv("GITHUB_OAUTH_AUTHORIZE_URL")),
		"GITHUB_OAUTH_TOKEN_URL":     strings.TrimSpace(getenv("GITHUB_OAUTH_TOKEN_URL")),
		"GITHUB_API_BASE_URL":        strings.TrimSpace(getenv("GITHUB_API_BASE_URL")),
	}

	if env != "dev" {
		for _, name := range []string{
			"GOOGLE_OIDC_ISSUER_URL",
			"LINKEDIN_OIDC_ISSUER_URL",
			"GITHUB_OAUTH_AUTHORIZE_URL",
			"GITHUB_OAUTH_TOKEN_URL",
			"GITHUB_API_BASE_URL",
		} {
			if values[name] != "" {
				return providerEndpoints{}, fmt.Errorf("config: %s is permitted only when ENV=dev", name)
			}
		}
		return providerEndpoints{}, nil
	}

	if err := validateLoopbackProviderURL("GOOGLE_OIDC_ISSUER_URL", values["GOOGLE_OIDC_ISSUER_URL"], "/google", false); err != nil {
		return providerEndpoints{}, err
	}
	if err := validateLoopbackProviderURL("LINKEDIN_OIDC_ISSUER_URL", values["LINKEDIN_OIDC_ISSUER_URL"], "/linkedin", false); err != nil {
		return providerEndpoints{}, err
	}

	githubNames := []string{"GITHUB_OAUTH_AUTHORIZE_URL", "GITHUB_OAUTH_TOKEN_URL", "GITHUB_API_BASE_URL"}
	githubSet := 0
	for _, name := range githubNames {
		if values[name] != "" {
			githubSet++
		}
	}
	if githubSet != 0 && githubSet != len(githubNames) {
		return providerEndpoints{}, fmt.Errorf("config: GITHUB_OAUTH_AUTHORIZE_URL, GITHUB_OAUTH_TOKEN_URL, and GITHUB_API_BASE_URL must be set together")
	}
	if githubSet == len(githubNames) {
		if err := validateLoopbackProviderURL("GITHUB_OAUTH_AUTHORIZE_URL", values["GITHUB_OAUTH_AUTHORIZE_URL"], "/__uat/oauth/github/authorize", true); err != nil {
			return providerEndpoints{}, err
		}
		wantAuthorize := publicOrigin + "/__uat/oauth/github/authorize"
		if values["GITHUB_OAUTH_AUTHORIZE_URL"] != wantAuthorize {
			return providerEndpoints{}, fmt.Errorf("config: GITHUB_OAUTH_AUTHORIZE_URL must equal PUBLIC_ORIGIN + /__uat/oauth/github/authorize")
		}
		if err := validateLoopbackProviderURL("GITHUB_OAUTH_TOKEN_URL", values["GITHUB_OAUTH_TOKEN_URL"], "/github/token", false); err != nil {
			return providerEndpoints{}, err
		}
		if err := validateLoopbackProviderURL("GITHUB_API_BASE_URL", values["GITHUB_API_BASE_URL"], "/github", false); err != nil {
			return providerEndpoints{}, err
		}
	}

	return providerEndpoints{
		googleIssuer:    values["GOOGLE_OIDC_ISSUER_URL"],
		linkedinIssuer:  values["LINKEDIN_OIDC_ISSUER_URL"],
		githubAuthorize: values["GITHUB_OAUTH_AUTHORIZE_URL"],
		githubToken:     values["GITHUB_OAUTH_TOKEN_URL"],
		githubAPI:       values["GITHUB_API_BASE_URL"],
	}, nil
}

func validateLoopbackProviderURL(name, raw, wantPath string, requireHTTPS bool) error {
	if raw == "" {
		return nil
	}

	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("config: %s: invalid URL: %w", name, err)
	}
	if u.Opaque != "" || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("config: %s must be an absolute URL", name)
	}
	if requireHTTPS {
		if u.Scheme != "https" {
			return fmt.Errorf("config: %s must use https", name)
		}
	} else if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("config: %s must use http or https", name)
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return fmt.Errorf("config: %s must not contain user info, query, or fragment", name)
	}
	if u.Port() == "" {
		return fmt.Errorf("config: %s must include an explicit port", name)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < minPort || port > maxPort {
		return fmt.Errorf("config: %s has an invalid port", name)
	}
	host := strings.ToLower(u.Hostname())
	address, parseErr := netip.ParseAddr(host)
	if host != "localhost" && (parseErr != nil || !address.IsLoopback()) {
		return fmt.Errorf("config: %s host must be loopback", name)
	}
	if u.EscapedPath() != wantPath {
		return fmt.Errorf("config: %s path must equal %s", name, wantPath)
	}
	return nil
}

// loadProviderCredentials reads <PREFIX>_CLIENT_ID and <PREFIX>_CLIENT_SECRET.
// Both are required in prod and staging only when provider login is enabled,
// so a server that offers the login cannot boot without real credentials.
func loadProviderCredentials(prefix, provider string, getenv func(string) string, env string, providerLogin bool) (clientID, clientSecret string, err error) {
	clientID = strings.TrimSpace(getenv(prefix + "_CLIENT_ID"))
	clientSecret = strings.TrimSpace(getenv(prefix + "_CLIENT_SECRET"))
	if !providerLogin || (env != "prod" && env != "staging") {
		return clientID, clientSecret, nil
	}
	for _, field := range []struct{ name, value string }{
		{prefix + "_CLIENT_ID", clientID},
		{prefix + "_CLIENT_SECRET", clientSecret},
	} {
		if field.value == "" {
			return "", "", fmt.Errorf("config: %s is required when ENV=%s and PROVIDER_LOGIN_ENABLED=true: "+
				"%s login needs real credentials", field.name, env, provider)
		}
	}
	return clientID, clientSecret, nil
}
