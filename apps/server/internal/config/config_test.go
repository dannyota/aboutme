package config_test

import (
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"danny.vn/aboutme/apps/server/internal/config"
)

const testBase64URL32 = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func env(vars map[string]string) func(string) string {
	return func(key string) string {
		if value, ok := vars[key]; ok {
			return value
		}
		switch key {
		case "PUBLIC_RENDER_ORIGIN":
			return "http://127.0.0.1:20030"
		case "APP_BUILD_DIGEST":
			return "sha256:app"
		case "PUBLIC_RENDERER_BUILD_DIGEST":
			return "sha256:renderer"
		case "MEDIA_BACKEND":
			return "fs"
		case "MEDIA_FS_DIR":
			return "/tmp/aboutme-config-test-media"
		case "PASSWORD_RATE_HMAC_KEY":
			return testBase64URL32
		case "AUTH_EMAIL_ACTIVE_KEY_ID":
			return "k1"
		case "AUTH_EMAIL_ACTIVE_KEY":
			return testBase64URL32
		case "AUTH_EMAIL_MODE":
			return "ses"
		case "SES_FROM_ADDRESS":
			return "noreply@example.com"
		case "SES_CONFIGURATION_SET":
			return "aboutme"
		case "AWS_REGION":
			return "ap-southeast-1"
		case "TOTP_ACTIVE_KEY":
			return testBase64URL32
		case "VIEW_PASS_KEY":
			return testBase64URL32
		}
		return vars[key]
	}
}

func TestPublicRenderConfigReadsSeparateRuntimeValues(t *testing.T) {
	t.Parallel()

	vars := validDevEnv()
	vars["PUBLIC_RENDER_ORIGIN"] = "http://web:3000"
	vars["APP_BUILD_DIGEST"] = "sha256:application"
	vars["PUBLIC_RENDERER_BUILD_DIGEST"] = "sha256:renderer"
	got, err := config.Load(env(vars))
	if err != nil {
		t.Fatal(err)
	}
	if got.PublicRenderOrigin != vars["PUBLIC_RENDER_ORIGIN"] || got.AppBuildDigest != vars["APP_BUILD_DIGEST"] || got.PublicRendererBuildDigest != vars["PUBLIC_RENDERER_BUILD_DIGEST"] {
		t.Fatalf("public runtime config = %#v", got)
	}
}

func validBaseVars() map[string]string {
	return map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"ENV":           "dev",
	}
}

func validDevEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://localhost:20443",
		"ENV":           "dev",
	}
}

func TestLoad_MCPEnableFlagAndFrozenBudgets(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, raw string
		enabled   bool
	}{
		{name: "absent is disabled", raw: "", enabled: false},
		{name: "explicit false", raw: "false", enabled: false},
		{name: "explicit true", raw: "true", enabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vars := validDevEnv()
			vars["MCP_ENABLED"] = tc.raw
			got, err := config.Load(env(vars))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got.AgentAccess.Enabled != tc.enabled {
				t.Fatalf("AgentAccess.Enabled = %t, want %t", got.AgentAccess.Enabled, tc.enabled)
			}
			want := config.AgentAccessConfig{
				CIMD: config.CIMDConfig{
					MaxURLBytes: 256, MaxBodyBytes: 5120,
					ConnectTimeout: 2 * time.Second, CompleteTimeout: 5 * time.Second,
					ConcurrentFetches: 4, AddressRequests: 30, AddressWindow: time.Hour,
					URLRequests: 6, URLWindow: time.Minute, GlobalRequests: 600, GlobalWindow: time.Hour,
					CacheFloor: 5 * time.Minute, CacheCeiling: time.Hour,
					MaxCacheEntries: 1000, MaxCacheBytes: 2 * 1024 * 1024,
				},
				Enabled:                     tc.enabled,
				OAuthRegisterRequests:       5,
				OAuthRegisterRangeRequests:  120,
				OAuthRegisterGlobalRequests: 600,
				OAuthRegisterWindow:         time.Hour,
				OAuthTokenRequests:          30,
				OAuthTokenWindow:            time.Minute,
				OAuthFailedGrantLimit:       10,
				OAuthFailedGrantWindow:      15 * time.Minute,
				MCPTokenRequests:            120,
				MCPTokenWindow:              time.Minute,
				MCPUserRequests:             240,
				MCPUserWindow:               time.Minute,
				MCPConcurrentPerUser:        4,
				OAuthLiveGrantLimit:         10,
				MCPBodyLimitBytes:           4_194_304,
				MaxRateKeys:                 10_000,
			}
			if !reflect.DeepEqual(got.AgentAccess, want) {
				t.Fatalf("AgentAccess = %#v, want %#v", got.AgentAccess, want)
			}
		})
	}
}

func TestLoad_MCPEnableFlagRejectsInvalidValueWithoutEcho(t *testing.T) {
	t.Parallel()
	vars := validDevEnv()
	vars["MCP_ENABLED"] = "true-secret-sentinel"
	_, err := config.Load(env(vars))
	if err == nil {
		t.Fatal("Load() error = nil, want MCP_ENABLED rejection")
	}
	if !strings.Contains(err.Error(), "MCP_ENABLED") || strings.Contains(err.Error(), vars["MCP_ENABLED"]) {
		t.Fatalf("Load() error = %q, want variable name without raw value", err)
	}
}

func TestLoad_ProviderLoginFlag(t *testing.T) {
	t.Parallel()
	all := config.ProviderLogin{Google: true, GitHub: true, LinkedIn: true}
	for _, tc := range []struct {
		name string
		raw  string
		want config.ProviderLogin
	}{
		{name: "blank is off", raw: "", want: config.ProviderLogin{}},
		{name: "false is off", raw: "false", want: config.ProviderLogin{}},
		{name: "true is every provider", raw: "true", want: all},
		{name: "google only", raw: "google", want: config.ProviderLogin{Google: true}},
		{name: "github only", raw: "github", want: config.ProviderLogin{GitHub: true}},
		{name: "linkedin only", raw: "linkedin", want: config.ProviderLogin{LinkedIn: true}},
		{name: "list with spaces", raw: " google , linkedin ", want: config.ProviderLogin{Google: true, LinkedIn: true}},
		{name: "full list", raw: "linkedin,github,google", want: all},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vars := validDevEnv()
			vars["PROVIDER_LOGIN_ENABLED"] = tc.raw
			got, err := config.Load(env(vars))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got.ProviderLogin != tc.want {
				t.Fatalf("ProviderLogin = %+v, want %+v", got.ProviderLogin, tc.want)
			}
		})
	}
}

func TestLoad_PasswordRegistrationFlag(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		raw      string
		disabled bool
	}{
		{"", false},
		{"true", false},
		{" true ", false},
		{"false", true},
	} {
		vars := validDevEnv()
		vars["PASSWORD_REGISTRATION_ENABLED"] = tc.raw
		got, err := config.Load(env(vars))
		if err != nil {
			t.Fatalf("Load(%q) error = %v", tc.raw, err)
		}
		if got.PasswordRegistrationDisabled != tc.disabled {
			t.Errorf("Load(%q) PasswordRegistrationDisabled = %t, want %t", tc.raw, got.PasswordRegistrationDisabled, tc.disabled)
		}
	}
}

func TestLoad_PasswordRegistrationFlagRejectsInvalidValueWithoutEcho(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"yes-secret-sentinel", "TRUE", "0", "off"} {
		vars := validDevEnv()
		vars["PASSWORD_REGISTRATION_ENABLED"] = raw
		_, err := config.Load(env(vars))
		if err == nil {
			t.Fatalf("Load(%q) error = nil, want PASSWORD_REGISTRATION_ENABLED rejection", raw)
		}
		if !strings.Contains(err.Error(), "PASSWORD_REGISTRATION_ENABLED") || strings.Contains(err.Error(), raw) {
			t.Fatalf("Load(%q) error = %q, want the variable name without the raw value", raw, err)
		}
	}
}

func TestProviderLogin_AnyAndNames(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in    config.ProviderLogin
		any   bool
		names []string
	}{
		{config.ProviderLogin{}, false, []string{}},
		{config.ProviderLogin{Google: true}, true, []string{"google"}},
		{config.ProviderLogin{LinkedIn: true, Google: true}, true, []string{"google", "linkedin"}},
		{config.ProviderLogin{Google: true, GitHub: true, LinkedIn: true}, true, []string{"google", "github", "linkedin"}},
	} {
		if got := tc.in.Any(); got != tc.any {
			t.Errorf("%+v Any() = %t, want %t", tc.in, got, tc.any)
		}
		got := tc.in.Names()
		if got == nil || strings.Join(got, ",") != strings.Join(tc.names, ",") {
			t.Errorf("%+v Names() = %#v, want %#v", tc.in, got, tc.names)
		}
	}
}

func TestLoad_ProviderLoginFlagRejectsBadLists(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"google,google",
		"google,unknown-secret-sentinel",
		"Google",
		"TRUE",
		"google,",
		",google",
		"google,,github",
		"true,google",
		"false,google",
		"google;github",
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			vars := validDevEnv()
			vars["PROVIDER_LOGIN_ENABLED"] = raw
			_, err := config.Load(env(vars))
			if err == nil {
				t.Fatalf("Load(%q) error = nil, want PROVIDER_LOGIN_ENABLED rejection", raw)
			}
			if !strings.Contains(err.Error(), "PROVIDER_LOGIN_ENABLED") || strings.Contains(err.Error(), "sentinel") {
				t.Fatalf("Load(%q) error = %q, want variable name without raw value", raw, err)
			}
		})
	}
}

func TestLoad_DisabledProviderCredentialsAreNotRead(t *testing.T) {
	t.Parallel()
	for _, environment := range []string{"dev", "prod"} {
		t.Run(environment, func(t *testing.T) {
			t.Parallel()
			got, err := config.Load(env(map[string]string{
				"DATABASE_URL":           "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN":          "https://aboutme.vn",
				"ENV":                    environment,
				"TRUSTED_PROXY_CIDRS":    "127.0.0.1/32",
				"PROVIDER_LOGIN_ENABLED": "google",
				"GOOGLE_CLIENT_ID":       "google-id",
				"GOOGLE_CLIENT_SECRET":   "google-secret",
				"GITHUB_CLIENT_ID":       "github-id",
				"GITHUB_CLIENT_SECRET":   "github-secret",
				"LINKEDIN_CLIENT_ID":     "linkedin-id",
				"LINKEDIN_CLIENT_SECRET": "linkedin-secret",
			}))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got.GoogleClientID != "google-id" || got.GoogleClientSecret != "google-secret" {
				t.Errorf("enabled Google credentials not loaded")
			}
			for name, value := range map[string]string{
				"GitHubClientID": got.GitHubClientID, "GitHubClientSecret": got.GitHubClientSecret,
				"LinkedInClientID": got.LinkedInClientID, "LinkedInClientSecret": got.LinkedInClientSecret,
			} {
				if value != "" {
					t.Errorf("%s = %q, want empty for a disabled provider", name, value)
				}
			}
		})
	}
}

func TestLoad_ProviderCredentialsRequiredPerEnabledProvider(t *testing.T) {
	t.Parallel()
	base := func(environment, flag string) map[string]string {
		return map[string]string{
			"DATABASE_URL":           "postgres://user:pass@localhost:5432/aboutme",
			"PUBLIC_ORIGIN":          "https://aboutme.vn",
			"ENV":                    environment,
			"TRUSTED_PROXY_CIDRS":    "127.0.0.1/32",
			"PROVIDER_LOGIN_ENABLED": flag,
		}
	}
	for _, environment := range []string{"prod", "staging"} {
		t.Run(environment+" google with google credentials", func(t *testing.T) {
			t.Parallel()
			vars := base(environment, "google")
			vars["GOOGLE_CLIENT_ID"] = "google-id"
			vars["GOOGLE_CLIENT_SECRET"] = "google-secret"
			got, err := config.Load(env(vars))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got.ProviderLogin != (config.ProviderLogin{Google: true}) {
				t.Fatalf("ProviderLogin = %+v, want google only", got.ProviderLogin)
			}
		})
		t.Run(environment+" google,linkedin with both providers' credentials", func(t *testing.T) {
			t.Parallel()
			vars := base(environment, "google,linkedin")
			vars["GOOGLE_CLIENT_ID"] = "google-id"
			vars["GOOGLE_CLIENT_SECRET"] = "google-secret"
			vars["LINKEDIN_CLIENT_ID"] = "linkedin-id"
			vars["LINKEDIN_CLIENT_SECRET"] = "linkedin-secret"
			got, err := config.Load(env(vars))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got.ProviderLogin != (config.ProviderLogin{Google: true, LinkedIn: true}) {
				t.Fatalf("ProviderLogin = %+v, want google and linkedin", got.ProviderLogin)
			}
			if got.LinkedInClientID != "linkedin-id" || got.LinkedInClientSecret != "linkedin-secret" {
				t.Fatal("LinkedIn credentials were not loaded")
			}
			if got.LinkedInOIDCIssuerURL != "" {
				t.Fatalf("LinkedInOIDCIssuerURL = %q, want empty outside dev", got.LinkedInOIDCIssuerURL)
			}
		})
		for _, tc := range []struct {
			flag, missing string
			set           map[string]string
		}{
			{"google", "GOOGLE_CLIENT_ID", map[string]string{"GOOGLE_CLIENT_SECRET": "s"}},
			{"google", "GOOGLE_CLIENT_SECRET", map[string]string{"GOOGLE_CLIENT_ID": "i"}},
			{"github", "GITHUB_CLIENT_ID", map[string]string{"GOOGLE_CLIENT_ID": "i", "GOOGLE_CLIENT_SECRET": "s"}},
			{"linkedin", "LINKEDIN_CLIENT_ID", map[string]string{"GITHUB_CLIENT_ID": "i", "GITHUB_CLIENT_SECRET": "s"}},
			{"google,linkedin", "LINKEDIN_CLIENT_ID", map[string]string{"GOOGLE_CLIENT_ID": "i", "GOOGLE_CLIENT_SECRET": "s"}},
			{"google,linkedin", "LINKEDIN_CLIENT_ID", map[string]string{"GOOGLE_CLIENT_ID": "i", "GOOGLE_CLIENT_SECRET": "s", "LINKEDIN_CLIENT_SECRET": "s"}},
			{"google,linkedin", "LINKEDIN_CLIENT_SECRET", map[string]string{"GOOGLE_CLIENT_ID": "i", "GOOGLE_CLIENT_SECRET": "s", "LINKEDIN_CLIENT_ID": "i"}},
		} {
			t.Run(environment+" "+tc.flag+" missing "+tc.missing, func(t *testing.T) {
				t.Parallel()
				vars := base(environment, tc.flag)
				for k, v := range tc.set {
					vars[k] = v
				}
				_, err := config.Load(env(vars))
				if err == nil || !strings.Contains(err.Error(), tc.missing) {
					t.Fatalf("Load() error = %v, want it to name %s", err, tc.missing)
				}
			})
		}
	}
}

func TestLoad_ProviderLoginFlagRejectsInvalidValueWithoutEcho(t *testing.T) {
	t.Parallel()
	vars := validDevEnv()
	vars["PROVIDER_LOGIN_ENABLED"] = "yes-secret-sentinel"
	_, err := config.Load(env(vars))
	if err == nil {
		t.Fatal("Load() error = nil, want PROVIDER_LOGIN_ENABLED rejection")
	}
	if !strings.Contains(err.Error(), "PROVIDER_LOGIN_ENABLED") || strings.Contains(err.Error(), vars["PROVIDER_LOGIN_ENABLED"]) {
		t.Fatalf("Load() error = %q, want variable name without raw value", err)
	}
}

func TestConfig_ValidateAgentAccessRejectsPartialEnabledConfiguration(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		PublicOrigin: "https://aboutme.example",
		AgentAccess:  config.AgentAccessConfig{Enabled: true},
	}
	if err := cfg.ValidateAgentAccess(); err == nil {
		t.Fatal("ValidateAgentAccess() error = nil for partial enabled config")
	}
}

func TestLoad_DevGoogleOIDCIssuerLoopback(t *testing.T) {
	t.Parallel()

	vars := validDevEnv()
	vars["GOOGLE_OIDC_ISSUER_URL"] = "http://127.0.0.1:20442/google"
	got, err := config.Load(env(vars))
	if err != nil {
		t.Fatal(err)
	}
	if got.GoogleOIDCIssuerURL != vars["GOOGLE_OIDC_ISSUER_URL"] {
		t.Fatalf("issuer = %q", got.GoogleOIDCIssuerURL)
	}
}

func TestLoad_ProviderEndpointsDevAccepted(t *testing.T) {
	t.Parallel()

	vars := validDevEnv()
	vars["LINKEDIN_OIDC_ISSUER_URL"] = "http://localhost:20442/linkedin"
	vars["GITHUB_OAUTH_AUTHORIZE_URL"] = "https://localhost:20443/__uat/oauth/github/authorize"
	vars["GITHUB_OAUTH_TOKEN_URL"] = "http://127.0.0.1:20442/github/token"
	vars["GITHUB_API_BASE_URL"] = "http://[::1]:20442/github"

	got, err := config.Load(env(vars))
	if err != nil {
		t.Fatal(err)
	}
	if got.LinkedInOIDCIssuerURL != vars["LINKEDIN_OIDC_ISSUER_URL"] {
		t.Errorf("LinkedInOIDCIssuerURL = %q, want %q", got.LinkedInOIDCIssuerURL, vars["LINKEDIN_OIDC_ISSUER_URL"])
	}
	if got.GitHubOAuthAuthorizeURL != vars["GITHUB_OAUTH_AUTHORIZE_URL"] {
		t.Errorf("GitHubOAuthAuthorizeURL = %q, want %q", got.GitHubOAuthAuthorizeURL, vars["GITHUB_OAUTH_AUTHORIZE_URL"])
	}
	if got.GitHubOAuthTokenURL != vars["GITHUB_OAUTH_TOKEN_URL"] {
		t.Errorf("GitHubOAuthTokenURL = %q, want %q", got.GitHubOAuthTokenURL, vars["GITHUB_OAUTH_TOKEN_URL"])
	}
	if got.GitHubAPIBaseURL != vars["GITHUB_API_BASE_URL"] {
		t.Errorf("GitHubAPIBaseURL = %q, want %q", got.GitHubAPIBaseURL, vars["GITHUB_API_BASE_URL"])
	}
}

func TestLoad_ProviderEndpointOverridesRejectedOutsideDev(t *testing.T) {
	t.Parallel()

	for _, environment := range []string{"staging", "prod"} {
		for _, override := range []struct {
			name, value string
		}{
			{name: "GOOGLE_OIDC_ISSUER_URL", value: "http://127.0.0.1:20442/google"},
			{name: "LINKEDIN_OIDC_ISSUER_URL", value: "http://127.0.0.1:20442/linkedin"},
			{name: "GITHUB_OAUTH_AUTHORIZE_URL", value: "https://localhost:20443/__uat/oauth/github/authorize"},
			{name: "GITHUB_OAUTH_TOKEN_URL", value: "http://127.0.0.1:20442/github/token"},
			{name: "GITHUB_API_BASE_URL", value: "http://127.0.0.1:20442/github"},
		} {
			t.Run(environment+"/"+override.name, func(t *testing.T) {
				t.Parallel()

				vars := validDevEnv()
				vars["ENV"] = environment
				vars["LISTEN_HOST"] = "127.0.0.1"
				vars["TRUSTED_PROXY_CIDRS"] = "127.0.0.1/32"
				vars[override.name] = override.value
				_, err := config.Load(env(vars))
				if err == nil {
					t.Fatal("Load() error = nil, want provider endpoint override rejection")
				}
				if !strings.Contains(err.Error(), override.name) {
					t.Fatalf("Load() error = %q, want %s", err, override.name)
				}
			})
		}
	}
}

func TestLoad_OIDCIssuerRejectsNonLoopback(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "google", key: "GOOGLE_OIDC_ISSUER_URL", value: "https://accounts.google.com:443/google"},
		{name: "linkedin", key: "LINKEDIN_OIDC_ISSUER_URL", value: "https://www.linkedin.com:443/linkedin"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vars := validDevEnv()
			vars[tt.key] = tt.value
			_, err := config.Load(env(vars))
			if err == nil {
				t.Fatal("Load() error = nil, want non-loopback issuer rejection")
			}
			if !strings.Contains(err.Error(), tt.key) {
				t.Fatalf("Load() error = %q, want %s", err, tt.key)
			}
		})
	}
}

func TestLoad_ProviderEndpointClosedGrammar(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		vars map[string]string
	}{
		{name: "partial github authorize", vars: map[string]string{
			"GITHUB_OAUTH_AUTHORIZE_URL": "https://localhost:20443/__uat/oauth/github/authorize",
		}},
		{name: "partial github token", vars: map[string]string{
			"GITHUB_OAUTH_TOKEN_URL": "http://127.0.0.1:20442/github/token",
		}},
		{name: "partial github api", vars: map[string]string{
			"GITHUB_API_BASE_URL": "http://127.0.0.1:20442/github",
		}},
		{name: "github external token", vars: map[string]string{
			"GITHUB_OAUTH_AUTHORIZE_URL": "https://localhost:20443/__uat/oauth/github/authorize",
			"GITHUB_OAUTH_TOKEN_URL":     "https://github.com:443/login/oauth/access_token",
			"GITHUB_API_BASE_URL":        "http://127.0.0.1:20442/github",
		}},
		{name: "github external api", vars: map[string]string{
			"GITHUB_OAUTH_AUTHORIZE_URL": "https://localhost:20443/__uat/oauth/github/authorize",
			"GITHUB_OAUTH_TOKEN_URL":     "http://127.0.0.1:20442/github/token",
			"GITHUB_API_BASE_URL":        "https://api.github.com:443/github",
		}},
		{name: "github authorize outside public origin", vars: map[string]string{
			"GITHUB_OAUTH_AUTHORIZE_URL": "https://localhost:20444/__uat/oauth/github/authorize",
			"GITHUB_OAUTH_TOKEN_URL":     "http://127.0.0.1:20442/github/token",
			"GITHUB_API_BASE_URL":        "http://127.0.0.1:20442/github",
		}},
		{name: "issuer user info", vars: map[string]string{
			"GOOGLE_OIDC_ISSUER_URL": "http://user@127.0.0.1:20442/google",
		}},
		{name: "issuer query", vars: map[string]string{
			"GOOGLE_OIDC_ISSUER_URL": "http://127.0.0.1:20442/google?next=external",
		}},
		{name: "issuer fragment", vars: map[string]string{
			"GOOGLE_OIDC_ISSUER_URL": "http://127.0.0.1:20442/google#external",
		}},
		{name: "issuer missing port", vars: map[string]string{
			"GOOGLE_OIDC_ISSUER_URL": "http://127.0.0.1/google",
		}},
		{name: "issuer zero port", vars: map[string]string{
			"GOOGLE_OIDC_ISSUER_URL": "http://127.0.0.1:0/google",
		}},
		{name: "issuer unexpected path", vars: map[string]string{
			"GOOGLE_OIDC_ISSUER_URL": "http://127.0.0.1:20442/linkedin",
		}},
		{name: "issuer encoded path", vars: map[string]string{
			"GOOGLE_OIDC_ISSUER_URL": "http://127.0.0.1:20442/%67oogle",
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vars := validDevEnv()
			for key, value := range tt.vars {
				vars[key] = value
			}
			_, err := config.Load(env(vars))
			if err == nil {
				t.Fatal("Load() error = nil, want closed provider endpoint rejection")
			}
		})
	}
}

func TestLoad_ValidConfig(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"PORT":          "9090",
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"LOG_LEVEL":     "debug",
		"ENV":           "dev",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if got.Port != 9090 {
		t.Errorf("Port = %d, want %d", got.Port, 9090)
	}
	if got.ListenHost != "127.0.0.1" {
		t.Errorf("ListenHost = %q, want %q (default)", got.ListenHost, "127.0.0.1")
	}
	if got.DatabaseURL != "postgres://user:pass@localhost:5432/aboutme" {
		t.Errorf("DatabaseURL = %q, want %q", got.DatabaseURL, "postgres://user:pass@localhost:5432/aboutme")
	}
	if got.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", got.LogLevel, "debug")
	}
	if got.Env != "dev" {
		t.Errorf("Env = %q, want %q", got.Env, "dev")
	}
	if got.PublicOrigin != "https://aboutme.vn" {
		t.Errorf("PublicOrigin = %q, want %q", got.PublicOrigin, "https://aboutme.vn")
	}
	if got.TrustedProxyCIDRs != nil {
		t.Errorf("TrustedProxyCIDRs = %v, want nil (default outside prod/staging)", got.TrustedProxyCIDRs)
	}
}

func TestLoad_ValidConfig_StagingRequiresTrustBoundary(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":           "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":          "https://aboutme.vn",
		"ENV":                    "staging",
		"LISTEN_HOST":            "127.0.0.1",
		"TRUSTED_PROXY_CIDRS":    "127.0.0.1/32,::1/128",
		"GOOGLE_CLIENT_ID":       "client-id",
		"GOOGLE_CLIENT_SECRET":   "client-secret",
		"GITHUB_CLIENT_ID":       "github-client-id",
		"GITHUB_CLIENT_SECRET":   "github-client-secret",
		"LINKEDIN_CLIENT_ID":     "client-id",
		"LINKEDIN_CLIENT_SECRET": "client-secret",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.Env != "staging" {
		t.Errorf("Env = %q, want %q", got.Env, "staging")
	}
	want := []netip.Prefix{
		netip.MustParsePrefix("127.0.0.1/32"),
		netip.MustParsePrefix("::1/128"),
	}
	if !slices.Equal(got.TrustedProxyCIDRs, want) {
		t.Errorf("TrustedProxyCIDRs = %v, want %v", got.TrustedProxyCIDRs, want)
	}
}

func TestLoad_PortDefaultsTo8080(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"LOG_LEVEL":     "info",
		"ENV":           "dev",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.Port != 8080 {
		t.Errorf("Port = %d, want 8080", got.Port)
	}
}

func TestLoad_LogLevelDefaultsToInfo(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"ENV":           "dev",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", got.LogLevel, "info")
	}
}

func TestLoad_LogLevelCaseInsensitive(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"LOG_LEVEL":     "WARN",
		"ENV":           "dev",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.LogLevel != "warn" {
		t.Errorf("LogLevel = %q, want %q", got.LogLevel, "warn")
	}
}

func TestLoad_MissingRequiredVars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		vars    map[string]string
		wantErr string
	}{
		{
			name: "missing DATABASE_URL",
			vars: map[string]string{
				"ENV": "dev",
			},
			wantErr: "DATABASE_URL",
		},
		{
			name: "missing ENV",
			vars: map[string]string{
				"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN": "https://aboutme.vn",
			},
			wantErr: "ENV",
		},
		{
			name: "missing PUBLIC_ORIGIN",
			vars: map[string]string{
				"DATABASE_URL": "postgres://user:pass@localhost:5432/aboutme",
				"ENV":          "dev",
			},
			wantErr: "PUBLIC_ORIGIN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(env(tt.vars))
			if err == nil {
				t.Fatalf("Load() error = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Load() error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestLoad_InvalidValues(t *testing.T) {
	t.Parallel()

	base := map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"LOG_LEVEL":     "info",
		"ENV":           "dev",
	}

	tests := []struct {
		name    string
		key     string
		value   string
		wantErr string
	}{
		{name: "port not numeric", key: "PORT", value: "abc", wantErr: "PORT"},
		{name: "port zero", key: "PORT", value: "0", wantErr: "PORT"},
		{name: "port negative", key: "PORT", value: "-1", wantErr: "PORT"},
		{name: "port too large", key: "PORT", value: "70000", wantErr: "PORT"},
		{name: "log level invalid", key: "LOG_LEVEL", value: "verbose", wantErr: "LOG_LEVEL"},
		{name: "env invalid", key: "ENV", value: "production", wantErr: "ENV"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vars := make(map[string]string, len(base))
			for k, v := range base {
				vars[k] = v
			}
			vars[tt.key] = tt.value

			_, err := config.Load(env(vars))
			if err == nil {
				t.Fatalf("Load() error = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Load() error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestLoad_ValidEnvValues(t *testing.T) {
	t.Parallel()

	for _, envValue := range []string{"dev", "staging", "prod"} {
		envValue := envValue
		t.Run(envValue, func(t *testing.T) {
			t.Parallel()

			vars := map[string]string{
				"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN": "https://aboutme.vn",
				"ENV":           envValue,
			}
			if envValue == "prod" || envValue == "staging" {
				vars["TRUSTED_PROXY_CIDRS"] = "127.0.0.1/32,::1/128"
			}

			got, err := config.Load(env(vars))
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
			if got.Env != envValue {
				t.Errorf("Env = %q, want %q", got.Env, envValue)
			}
		})
	}
}

func TestLoad_ListenHostDefaultsToLoopback(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"ENV":           "dev",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.ListenHost != "127.0.0.1" {
		t.Errorf("ListenHost = %q, want %q", got.ListenHost, "127.0.0.1")
	}
}

func TestLoad_ListenHostCustomValueAcceptedOutsideProd(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"ENV":           "dev",
		"LISTEN_HOST":   "0.0.0.0",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.ListenHost != "0.0.0.0" {
		t.Errorf("ListenHost = %q, want %q", got.ListenHost, "0.0.0.0")
	}
}

func TestLoad_ListenHostInvalidValue(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"ENV":           "dev",
		"LISTEN_HOST":   "not-an-ip",
	}))
	if err == nil {
		t.Fatal("Load() error = nil, want error for invalid LISTEN_HOST")
	}
	if !strings.Contains(err.Error(), "LISTEN_HOST") {
		t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "LISTEN_HOST")
	}
}

func TestLoad_ListenHostNonLoopbackRejectedInProd(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{
		"DATABASE_URL":        "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":       "https://aboutme.vn",
		"ENV":                 "prod",
		"LISTEN_HOST":         "0.0.0.0",
		"TRUSTED_PROXY_CIDRS": "127.0.0.1/32",
	}))
	if err == nil {
		t.Fatal("Load() error = nil, want error for non-loopback LISTEN_HOST in prod")
	}
	if !strings.Contains(err.Error(), "LISTEN_HOST") {
		t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "LISTEN_HOST")
	}
}

func TestLoad_ListenHostLoopbackAcceptedInProd(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":           "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":          "https://aboutme.vn",
		"ENV":                    "prod",
		"LISTEN_HOST":            "127.0.0.1",
		"TRUSTED_PROXY_CIDRS":    "127.0.0.1/32",
		"GOOGLE_CLIENT_ID":       "client-id",
		"GOOGLE_CLIENT_SECRET":   "client-secret",
		"GITHUB_CLIENT_ID":       "github-client-id",
		"GITHUB_CLIENT_SECRET":   "github-client-secret",
		"LINKEDIN_CLIENT_ID":     "client-id",
		"LINKEDIN_CLIENT_SECRET": "client-secret",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.ListenHost != "127.0.0.1" {
		t.Errorf("ListenHost = %q, want %q", got.ListenHost, "127.0.0.1")
	}
}

func TestLoad_ListenHostNonLoopbackRejectedInStaging(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{
		"DATABASE_URL":        "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":       "https://aboutme.vn",
		"ENV":                 "staging",
		"LISTEN_HOST":         "0.0.0.0",
		"TRUSTED_PROXY_CIDRS": "127.0.0.1/32",
	}))
	if err == nil {
		t.Fatal("Load() error = nil, want error for non-loopback LISTEN_HOST in staging")
	}
	if !strings.Contains(err.Error(), "LISTEN_HOST") {
		t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "LISTEN_HOST")
	}
}

func TestLoad_ListenHostLoopbackAcceptedInStaging(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":           "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":          "https://aboutme.vn",
		"ENV":                    "staging",
		"LISTEN_HOST":            "127.0.0.1",
		"TRUSTED_PROXY_CIDRS":    "127.0.0.1/32",
		"GOOGLE_CLIENT_ID":       "client-id",
		"GOOGLE_CLIENT_SECRET":   "client-secret",
		"GITHUB_CLIENT_ID":       "github-client-id",
		"GITHUB_CLIENT_SECRET":   "github-client-secret",
		"LINKEDIN_CLIENT_ID":     "client-id",
		"LINKEDIN_CLIENT_SECRET": "client-secret",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.ListenHost != "127.0.0.1" {
		t.Errorf("ListenHost = %q, want %q", got.ListenHost, "127.0.0.1")
	}
}

func TestLoad_TrustedProxyCIDRs_RequiredInProd(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"ENV":           "prod",
	}))
	if err == nil {
		t.Fatal("Load() error = nil, want error: TRUSTED_PROXY_CIDRS is required when ENV=prod")
	}
	if !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
		t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "TRUSTED_PROXY_CIDRS")
	}
}

func TestLoad_TrustedProxyCIDRs_RequiredInStaging(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"ENV":           "staging",
	}))
	if err == nil {
		t.Fatal("Load() error = nil, want error: TRUSTED_PROXY_CIDRS is required when ENV=staging")
	}
	if !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
		t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "TRUSTED_PROXY_CIDRS")
	}
}

func TestLoad_TrustedProxyCIDRs_OptionalOutsideProd(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"ENV":           "dev",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.TrustedProxyCIDRs != nil {
		t.Errorf("TrustedProxyCIDRs = %v, want nil", got.TrustedProxyCIDRs)
	}
}

func TestLoad_TrustedProxyCIDRs_ParsesCommaSeparatedList(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":           "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":          "https://aboutme.vn",
		"ENV":                    "prod",
		"LISTEN_HOST":            "127.0.0.1",
		"TRUSTED_PROXY_CIDRS":    " 127.0.0.1/32 , ::1/128 ",
		"GOOGLE_CLIENT_ID":       "client-id",
		"GOOGLE_CLIENT_SECRET":   "client-secret",
		"GITHUB_CLIENT_ID":       "github-client-id",
		"GITHUB_CLIENT_SECRET":   "github-client-secret",
		"LINKEDIN_CLIENT_ID":     "client-id",
		"LINKEDIN_CLIENT_SECRET": "client-secret",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	want := []netip.Prefix{
		netip.MustParsePrefix("127.0.0.1/32"),
		netip.MustParsePrefix("::1/128"),
	}
	if !slices.Equal(got.TrustedProxyCIDRs, want) {
		t.Errorf("TrustedProxyCIDRs = %v, want %v", got.TrustedProxyCIDRs, want)
	}
}

func TestLoad_TrustedProxyCIDRs_RejectsTrustEveryone(t *testing.T) {
	t.Parallel()

	for _, cidr := range []string{"0.0.0.0/0", "::/0"} {
		t.Run(cidr, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(env(map[string]string{
				"DATABASE_URL":        "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN":       "https://aboutme.vn",
				"ENV":                 "dev",
				"TRUSTED_PROXY_CIDRS": cidr,
			}))
			if err == nil {
				t.Fatalf("Load() error = nil, want error rejecting %q as an implausible trust-everyone CIDR", cidr)
			}
			if !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
				t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "TRUSTED_PROXY_CIDRS")
			}
		})
	}
}

func TestLoad_TrustedProxyCIDRs_RejectsBroaderThanMinimumPrefix(t *testing.T) {
	t.Parallel()

	cases := []string{
		"0.0.0.0/1",
		"128.0.0.0/1",
		"0.0.0.0/1,128.0.0.0/1",
		"10.0.0.0/7",
		"::/47",
	}
	for _, cidr := range cases {
		t.Run(cidr, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(env(map[string]string{
				"DATABASE_URL":        "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN":       "https://aboutme.vn",
				"ENV":                 "dev",
				"TRUSTED_PROXY_CIDRS": cidr,
			}))
			if err == nil {
				t.Fatalf("Load() error = nil, want error rejecting %q as broader than the minimum allowed prefix", cidr)
			}
			if !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
				t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "TRUSTED_PROXY_CIDRS")
			}
		})
	}
}

func TestLoad_TrustedProxyCIDRs_NarrowPrefixesStillAccepted(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":        "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":       "https://aboutme.vn",
		"ENV":                 "dev",
		"TRUSTED_PROXY_CIDRS": "10.0.0.0/8, 2001:db8::/48",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	want := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("2001:db8::/48"),
	}
	if !slices.Equal(got.TrustedProxyCIDRs, want) {
		t.Errorf("TrustedProxyCIDRs = %v, want %v", got.TrustedProxyCIDRs, want)
	}
}

func TestLoad_TrustedProxyCIDRs_RejectsIPv4MappedPrefix(t *testing.T) {
	t.Parallel()

	cases := []string{
		"::ffff:0.0.0.0/104",
		"::ffff:203.0.113.0/120",
		"::ffff:10.90.0.0/124",
	}
	for _, cidr := range cases {
		t.Run(cidr, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(env(map[string]string{
				"DATABASE_URL":        "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN":       "https://aboutme.vn",
				"ENV":                 "dev",
				"TRUSTED_PROXY_CIDRS": cidr,
			}))
			if err == nil {
				t.Fatalf("Load() error = nil, want error rejecting %q as an IPv4-in-IPv6 mapped prefix", cidr)
			}
			if !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
				t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "TRUSTED_PROXY_CIDRS")
			}
			if !strings.Contains(err.Error(), "IPv4-in-IPv6") {
				t.Errorf("Load() error = %q, want it to explain the IPv4-in-IPv6 mapping so the "+
					"operator knows to use the plain IPv4 form", err.Error())
			}
		})
	}
}

func TestLoad_GoogleCredentials_ValidConfig(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":           "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":          "https://aboutme.vn",
		"ENV":                    "dev",
		"PROVIDER_LOGIN_ENABLED": "google",
		"GOOGLE_CLIENT_ID":       "test-client-id",
		"GOOGLE_CLIENT_SECRET":   "test-client-secret",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.GoogleClientID != "test-client-id" {
		t.Errorf("GoogleClientID = %q, want %q", got.GoogleClientID, "test-client-id")
	}
	if got.GoogleClientSecret != "test-client-secret" {
		t.Errorf("GoogleClientSecret = %q, want %q", got.GoogleClientSecret, "test-client-secret")
	}
}

func TestLoad_GoogleCredentials_OptionalInDev(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"ENV":           "dev",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.GoogleClientID != "" {
		t.Errorf("GoogleClientID = %q, want empty (optional in dev)", got.GoogleClientID)
	}
	if got.GoogleClientSecret != "" {
		t.Errorf("GoogleClientSecret = %q, want empty (optional in dev)", got.GoogleClientSecret)
	}
}

func TestLoad_GoogleCredentials_RequiredInProd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		vars map[string]string
	}{
		{
			name: "missing client id",
			vars: map[string]string{"GOOGLE_CLIENT_SECRET": "secret"},
		},
		{
			name: "missing client secret",
			vars: map[string]string{"GOOGLE_CLIENT_ID": "client-id"},
		},
		{
			name: "missing both",
			vars: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vars := map[string]string{
				"DATABASE_URL":           "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN":          "https://aboutme.vn",
				"ENV":                    "prod",
				"PROVIDER_LOGIN_ENABLED": "true",
				"LISTEN_HOST":            "127.0.0.1",
				"TRUSTED_PROXY_CIDRS":    "127.0.0.1/32",
			}
			for k, v := range tt.vars {
				vars[k] = v
			}

			_, err := config.Load(env(vars))
			if err == nil {
				t.Fatal("Load() error = nil, want error: GOOGLE_CLIENT_ID/SECRET required when ENV=prod")
			}
			if !strings.Contains(err.Error(), "GOOGLE_CLIENT_ID") && !strings.Contains(err.Error(), "GOOGLE_CLIENT_SECRET") {
				t.Errorf("Load() error = %q, want it to name GOOGLE_CLIENT_ID or GOOGLE_CLIENT_SECRET", err.Error())
			}
		})
	}
}

func TestLoad_GoogleCredentials_RequiredInStaging(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{
		"DATABASE_URL":           "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":          "https://aboutme.vn",
		"ENV":                    "staging",
		"PROVIDER_LOGIN_ENABLED": "true",
		"LISTEN_HOST":            "127.0.0.1",
		"TRUSTED_PROXY_CIDRS":    "127.0.0.1/32",
	}))
	if err == nil {
		t.Fatal("Load() error = nil, want error: GOOGLE_CLIENT_ID/SECRET required when ENV=staging")
	}
	if !strings.Contains(err.Error(), "GOOGLE_CLIENT_ID") {
		t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "GOOGLE_CLIENT_ID")
	}
}

func TestLoad_GitHubCredentials_ValidConfig(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":           "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":          "https://aboutme.vn",
		"ENV":                    "dev",
		"PROVIDER_LOGIN_ENABLED": "github",
		"GITHUB_CLIENT_ID":       "test-github-client-id",
		"GITHUB_CLIENT_SECRET":   "test-github-client-secret",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.GitHubClientID != "test-github-client-id" {
		t.Errorf("GitHubClientID = %q, want %q", got.GitHubClientID, "test-github-client-id")
	}
	if got.GitHubClientSecret != "test-github-client-secret" {
		t.Errorf("GitHubClientSecret = %q, want %q", got.GitHubClientSecret, "test-github-client-secret")
	}
}

func TestLoad_GitHubCredentials_OptionalInDev(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"ENV":           "dev",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.GitHubClientID != "" {
		t.Errorf("GitHubClientID = %q, want empty (optional in dev)", got.GitHubClientID)
	}
	if got.GitHubClientSecret != "" {
		t.Errorf("GitHubClientSecret = %q, want empty (optional in dev)", got.GitHubClientSecret)
	}
}

func TestLoad_GitHubCredentials_RequiredInProd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		vars map[string]string
	}{
		{
			name: "missing client id",
			vars: map[string]string{"GITHUB_CLIENT_SECRET": "secret"},
		},
		{
			name: "missing client secret",
			vars: map[string]string{"GITHUB_CLIENT_ID": "client-id"},
		},
		{
			name: "missing both",
			vars: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vars := map[string]string{
				"DATABASE_URL":           "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN":          "https://aboutme.vn",
				"ENV":                    "prod",
				"PROVIDER_LOGIN_ENABLED": "true",
				"LISTEN_HOST":            "127.0.0.1",
				"TRUSTED_PROXY_CIDRS":    "127.0.0.1/32",
				"GOOGLE_CLIENT_ID":       "client-id",
				"GOOGLE_CLIENT_SECRET":   "client-secret",
			}
			for k, v := range tt.vars {
				vars[k] = v
			}

			_, err := config.Load(env(vars))
			if err == nil {
				t.Fatal("Load() error = nil, want error: GITHUB_CLIENT_ID/SECRET required when ENV=prod")
			}
			if !strings.Contains(err.Error(), "GITHUB_CLIENT_ID") && !strings.Contains(err.Error(), "GITHUB_CLIENT_SECRET") {
				t.Errorf("Load() error = %q, want it to name GITHUB_CLIENT_ID or GITHUB_CLIENT_SECRET", err.Error())
			}
		})
	}
}

func TestLoad_GitHubCredentials_RequiredInStaging(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{
		"DATABASE_URL":           "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":          "https://aboutme.vn",
		"ENV":                    "staging",
		"PROVIDER_LOGIN_ENABLED": "true",
		"LISTEN_HOST":            "127.0.0.1",
		"TRUSTED_PROXY_CIDRS":    "127.0.0.1/32",
		"GOOGLE_CLIENT_ID":       "client-id",
		"GOOGLE_CLIENT_SECRET":   "client-secret",
	}))
	if err == nil {
		t.Fatal("Load() error = nil, want error: GITHUB_CLIENT_ID/SECRET required when ENV=staging")
	}
	if !strings.Contains(err.Error(), "GITHUB_CLIENT_ID") {
		t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "GITHUB_CLIENT_ID")
	}
}

func TestLoad_LinkedInCredentials_ValidConfig(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":           "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":          "https://aboutme.vn",
		"ENV":                    "dev",
		"PROVIDER_LOGIN_ENABLED": "linkedin",
		"LINKEDIN_CLIENT_ID":     "test-client-id",
		"LINKEDIN_CLIENT_SECRET": "test-client-secret",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.LinkedInClientID != "test-client-id" {
		t.Errorf("LinkedInClientID = %q, want %q", got.LinkedInClientID, "test-client-id")
	}
	if got.LinkedInClientSecret != "test-client-secret" {
		t.Errorf("LinkedInClientSecret = %q, want %q", got.LinkedInClientSecret, "test-client-secret")
	}
}

func TestLoad_LinkedInCredentials_OptionalInDev(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"ENV":           "dev",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.LinkedInClientID != "" {
		t.Errorf("LinkedInClientID = %q, want empty (optional in dev)", got.LinkedInClientID)
	}
	if got.LinkedInClientSecret != "" {
		t.Errorf("LinkedInClientSecret = %q, want empty (optional in dev)", got.LinkedInClientSecret)
	}
}

func TestLoad_LinkedInCredentials_RequiredInProd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		vars map[string]string
	}{
		{
			name: "missing client id",
			vars: map[string]string{"LINKEDIN_CLIENT_SECRET": "secret"},
		},
		{
			name: "missing client secret",
			vars: map[string]string{"LINKEDIN_CLIENT_ID": "client-id"},
		},
		{
			name: "missing both",
			vars: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vars := map[string]string{
				"DATABASE_URL":           "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN":          "https://aboutme.vn",
				"ENV":                    "prod",
				"PROVIDER_LOGIN_ENABLED": "true",
				"LISTEN_HOST":            "127.0.0.1",
				"TRUSTED_PROXY_CIDRS":    "127.0.0.1/32",
				"GOOGLE_CLIENT_ID":       "google-client-id",
				"GOOGLE_CLIENT_SECRET":   "google-client-secret",
				"GITHUB_CLIENT_ID":       "github-client-id",
				"GITHUB_CLIENT_SECRET":   "github-client-secret",
			}
			for k, v := range tt.vars {
				vars[k] = v
			}

			_, err := config.Load(env(vars))
			if err == nil {
				t.Fatal("Load() error = nil, want error: LINKEDIN_CLIENT_ID/SECRET required when ENV=prod")
			}
			if !strings.Contains(err.Error(), "LINKEDIN_CLIENT_ID") && !strings.Contains(err.Error(), "LINKEDIN_CLIENT_SECRET") {
				t.Errorf("Load() error = %q, want it to name LINKEDIN_CLIENT_ID or LINKEDIN_CLIENT_SECRET", err.Error())
			}
		})
	}
}

func TestLoad_LinkedInCredentials_RequiredInStaging(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{
		"DATABASE_URL":           "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":          "https://aboutme.vn",
		"ENV":                    "staging",
		"PROVIDER_LOGIN_ENABLED": "true",
		"LISTEN_HOST":            "127.0.0.1",
		"TRUSTED_PROXY_CIDRS":    "127.0.0.1/32",
		"GOOGLE_CLIENT_ID":       "google-client-id",
		"GOOGLE_CLIENT_SECRET":   "google-client-secret",
		"GITHUB_CLIENT_ID":       "github-client-id",
		"GITHUB_CLIENT_SECRET":   "github-client-secret",
	}))
	if err == nil {
		t.Fatal("Load() error = nil, want error: LINKEDIN_CLIENT_ID/SECRET required when ENV=staging")
	}
	if !strings.Contains(err.Error(), "LINKEDIN_CLIENT_ID") {
		t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "LINKEDIN_CLIENT_ID")
	}
}

func TestLoad_PublicOrigin_ValidFormats(t *testing.T) {
	t.Parallel()

	for _, origin := range []string{
		"https://aboutme.vn",
		"https://aboutme.vn:8443",
		"http://localhost:8080",
		"http://localhost",
	} {
		t.Run(origin, func(t *testing.T) {
			t.Parallel()

			got, err := config.Load(env(map[string]string{
				"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN": origin,
				"ENV":           "dev",
			}))
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
			if got.PublicOrigin != origin {
				t.Errorf("PublicOrigin = %q, want %q", got.PublicOrigin, origin)
			}
		})
	}
}

func TestLoad_PublicOrigin_InvalidFormats(t *testing.T) {
	t.Parallel()

	cases := []string{
		"https://aboutme.vn/",
		"https://aboutme.vn/api",
		"https://aboutme.vn?x=1",
		"https://aboutme.vn#frag",
		"aboutme.vn",
		"ftp://aboutme.vn",
		"https://",
		"not a url at all : : spaces",
	}
	for _, origin := range cases {
		t.Run(origin, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(env(map[string]string{
				"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN": origin,
				"ENV":           "dev",
			}))
			if err == nil {
				t.Fatalf("Load() error = nil, want error rejecting PUBLIC_ORIGIN %q", origin)
			}
			if !strings.Contains(err.Error(), "PUBLIC_ORIGIN") {
				t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "PUBLIC_ORIGIN")
			}
		})
	}
}

func TestLoad_PublicOrigin_Normalization(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"already canonical, unchanged", "https://aboutme.vn", "https://aboutme.vn"},
		{"uppercase host lowercased", "https://ABOUTME.vn", "https://aboutme.vn"},
		{"mixed-case scheme and host lowercased", "Https://Aboutme.VN", "https://aboutme.vn"},
		{"https default port 443 stripped", "https://aboutme.vn:443", "https://aboutme.vn"},
		{"http default port 80 stripped", "http://aboutme.vn:80", "http://aboutme.vn"},
		{"https on port 80 is NOT the default for https, preserved", "https://aboutme.vn:80", "https://aboutme.vn:80"},
		{"http on port 443 is NOT the default for http, preserved", "http://aboutme.vn:443", "http://aboutme.vn:443"},
		{"non-default port preserved", "https://aboutme.vn:8443", "https://aboutme.vn:8443"},
		{"uppercase host with non-default port", "https://ABOUTME.vn:8443", "https://aboutme.vn:8443"},
		{"localhost with default http port stripped", "http://localhost:80", "http://localhost"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := config.Load(env(map[string]string{
				"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN": tc.input,
				"ENV":           "dev",
			}))
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
			if got.PublicOrigin != tc.want {
				t.Errorf("PublicOrigin = %q, want %q (input %q)", got.PublicOrigin, tc.want, tc.input)
			}
		})
	}
}

func TestLoad_TrustedProxyCIDRs_InvalidCIDR(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{
		"DATABASE_URL":        "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":       "https://aboutme.vn",
		"ENV":                 "dev",
		"TRUSTED_PROXY_CIDRS": "not-a-cidr",
	}))
	if err == nil {
		t.Fatal("Load() error = nil, want error for invalid TRUSTED_PROXY_CIDRS")
	}
	if !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
		t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "TRUSTED_PROXY_CIDRS")
	}
}

func TestLoad_MediaFilesystemMode(t *testing.T) {
	t.Parallel()
	vars := validBaseVars()
	vars["MEDIA_BACKEND"] = "fs"
	vars["MEDIA_FS_DIR"] = " /var/lib/aboutme/media "

	got, err := config.Load(env(vars))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.MediaBackend != "fs" || got.MediaFSDir != "/var/lib/aboutme/media" {
		t.Errorf("media filesystem config = %q, %q; want fs, /var/lib/aboutme/media", got.MediaBackend, got.MediaFSDir)
	}
	if got.MediaBucket != "" || got.MediaRegion != "" || got.MediaEndpoint != "" ||
		got.MediaAccessKeyID != "" || got.MediaSecretAccessKey != "" || got.MediaForcePathStyle {
		t.Errorf("filesystem mode retained S3-only values: %+v", got)
	}
}

func TestLoad_MediaS3Modes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		vars      map[string]string
		endpoint  string
		accessID  string
		secret    string
		pathStyle bool
	}{
		{
			name: "custom endpoint",
			vars: map[string]string{
				"MEDIA_BACKEND":           "s3",
				"MEDIA_BUCKET":            "private-media",
				"MEDIA_REGION":            "us-east-1",
				"MEDIA_ENDPOINT":          "http://127.0.0.1:20091",
				"MEDIA_ACCESS_KEY_ID":     "local-access",
				"MEDIA_SECRET_ACCESS_KEY": "local-secret",
				"MEDIA_FORCE_PATH_STYLE":  "true",
			},
			endpoint:  "http://127.0.0.1:20091",
			accessID:  "local-access",
			secret:    "local-secret",
			pathStyle: true,
		},
		{
			name: "AWS task role",
			vars: map[string]string{
				"MEDIA_BACKEND": "s3",
				"MEDIA_BUCKET":  "private-media",
				"MEDIA_REGION":  "ap-southeast-1",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vars := validBaseVars()
			for key, value := range tt.vars {
				vars[key] = value
			}
			vars["MEDIA_FS_DIR"] = ""

			got, err := config.Load(env(vars))
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
			if got.MediaBackend != "s3" || got.MediaFSDir != "" || got.MediaBucket != "private-media" ||
				got.MediaRegion != tt.vars["MEDIA_REGION"] || got.MediaEndpoint != tt.endpoint ||
				got.MediaAccessKeyID != tt.accessID || got.MediaSecretAccessKey != tt.secret ||
				got.MediaForcePathStyle != tt.pathStyle {
				t.Errorf("media S3 config = %+v; fields do not match selected mode", got)
			}
		})
	}
}

func TestLoad_MediaRejectsMissingAndCrossModeValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		values  map[string]string
		wantVar string
	}{
		{"missing backend", map[string]string{"MEDIA_BACKEND": ""}, "MEDIA_BACKEND"},
		{"unknown backend", map[string]string{"MEDIA_BACKEND": "disk"}, "MEDIA_BACKEND"},
		{"filesystem missing directory", map[string]string{"MEDIA_BACKEND": "fs", "MEDIA_FS_DIR": ""}, "MEDIA_FS_DIR"},
		{"filesystem with bucket", map[string]string{"MEDIA_BACKEND": "fs", "MEDIA_BUCKET": "bucket"}, "MEDIA_BUCKET"},
		{"filesystem with region", map[string]string{"MEDIA_BACKEND": "fs", "MEDIA_REGION": "region"}, "MEDIA_REGION"},
		{"filesystem with endpoint", map[string]string{"MEDIA_BACKEND": "fs", "MEDIA_ENDPOINT": "http://127.0.0.1:20091"}, "MEDIA_ENDPOINT"},
		{"filesystem with access key", map[string]string{"MEDIA_BACKEND": "fs", "MEDIA_ACCESS_KEY_ID": "access"}, "MEDIA_ACCESS_KEY_ID"},
		{"filesystem with secret key", map[string]string{"MEDIA_BACKEND": "fs", "MEDIA_SECRET_ACCESS_KEY": "secret"}, "MEDIA_SECRET_ACCESS_KEY"},
		{"filesystem with path style", map[string]string{"MEDIA_BACKEND": "fs", "MEDIA_FORCE_PATH_STYLE": "true"}, "MEDIA_FORCE_PATH_STYLE"},
		{"S3 with filesystem directory", map[string]string{"MEDIA_BACKEND": "s3", "MEDIA_FS_DIR": "/tmp/media", "MEDIA_BUCKET": "bucket", "MEDIA_REGION": "region"}, "MEDIA_FS_DIR"},
		{"S3 missing bucket", map[string]string{"MEDIA_BACKEND": "s3", "MEDIA_FS_DIR": "", "MEDIA_REGION": "region"}, "MEDIA_BUCKET"},
		{"S3 missing region", map[string]string{"MEDIA_BACKEND": "s3", "MEDIA_FS_DIR": "", "MEDIA_BUCKET": "bucket"}, "MEDIA_REGION"},
		{"AWS mode with access key", map[string]string{"MEDIA_BACKEND": "s3", "MEDIA_FS_DIR": "", "MEDIA_BUCKET": "bucket", "MEDIA_REGION": "region", "MEDIA_ACCESS_KEY_ID": "access"}, "MEDIA_ACCESS_KEY_ID"},
		{"AWS mode with secret key", map[string]string{"MEDIA_BACKEND": "s3", "MEDIA_FS_DIR": "", "MEDIA_BUCKET": "bucket", "MEDIA_REGION": "region", "MEDIA_SECRET_ACCESS_KEY": "secret"}, "MEDIA_SECRET_ACCESS_KEY"},
		{"AWS mode with explicit path style", map[string]string{"MEDIA_BACKEND": "s3", "MEDIA_FS_DIR": "", "MEDIA_BUCKET": "bucket", "MEDIA_REGION": "region", "MEDIA_FORCE_PATH_STYLE": "false"}, "MEDIA_FORCE_PATH_STYLE"},
		{"custom endpoint without path style", map[string]string{"MEDIA_BACKEND": "s3", "MEDIA_FS_DIR": "", "MEDIA_BUCKET": "bucket", "MEDIA_REGION": "region", "MEDIA_ENDPOINT": "http://127.0.0.1:20091", "MEDIA_ACCESS_KEY_ID": "access", "MEDIA_SECRET_ACCESS_KEY": "secret"}, "MEDIA_FORCE_PATH_STYLE"},
		{"custom endpoint missing access key", map[string]string{"MEDIA_BACKEND": "s3", "MEDIA_FS_DIR": "", "MEDIA_BUCKET": "bucket", "MEDIA_REGION": "region", "MEDIA_ENDPOINT": "http://127.0.0.1:20091", "MEDIA_SECRET_ACCESS_KEY": "secret", "MEDIA_FORCE_PATH_STYLE": "true"}, "MEDIA_ACCESS_KEY_ID"},
		{"custom endpoint missing secret key", map[string]string{"MEDIA_BACKEND": "s3", "MEDIA_FS_DIR": "", "MEDIA_BUCKET": "bucket", "MEDIA_REGION": "region", "MEDIA_ENDPOINT": "http://127.0.0.1:20091", "MEDIA_ACCESS_KEY_ID": "access", "MEDIA_FORCE_PATH_STYLE": "true"}, "MEDIA_SECRET_ACCESS_KEY"},
		{"relative custom endpoint", map[string]string{"MEDIA_BACKEND": "s3", "MEDIA_FS_DIR": "", "MEDIA_BUCKET": "bucket", "MEDIA_REGION": "region", "MEDIA_ENDPOINT": "127.0.0.1:20091", "MEDIA_ACCESS_KEY_ID": "access", "MEDIA_SECRET_ACCESS_KEY": "secret", "MEDIA_FORCE_PATH_STYLE": "true"}, "MEDIA_ENDPOINT"},
		{"custom endpoint with path", map[string]string{"MEDIA_BACKEND": "s3", "MEDIA_FS_DIR": "", "MEDIA_BUCKET": "bucket", "MEDIA_REGION": "region", "MEDIA_ENDPOINT": "http://127.0.0.1:20091/path", "MEDIA_ACCESS_KEY_ID": "access", "MEDIA_SECRET_ACCESS_KEY": "secret", "MEDIA_FORCE_PATH_STYLE": "true"}, "MEDIA_ENDPOINT"},
		{"malformed path-style value", map[string]string{"MEDIA_BACKEND": "s3", "MEDIA_FS_DIR": "", "MEDIA_BUCKET": "bucket", "MEDIA_REGION": "region", "MEDIA_ENDPOINT": "http://127.0.0.1:20091", "MEDIA_ACCESS_KEY_ID": "access", "MEDIA_SECRET_ACCESS_KEY": "secret", "MEDIA_FORCE_PATH_STYLE": "yes"}, "MEDIA_FORCE_PATH_STYLE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vars := validBaseVars()
			for key, value := range tt.values {
				vars[key] = value
			}
			_, err := config.Load(env(vars))
			if err == nil {
				t.Fatalf("Load() error = nil, want rejection naming %s", tt.wantVar)
			}
			if !strings.Contains(err.Error(), tt.wantVar) {
				t.Errorf("Load() error = %q, want it to name %s", err, tt.wantVar)
			}
		})
	}
}

func TestLoad_MediaErrorsDoNotExposeCredentials(t *testing.T) {
	t.Parallel()
	const accessSentinel = "CONFIG-AKID-SENTINEL-8c31"
	const secretSentinel = "CONFIG-SECRET-SENTINEL-a721"
	vars := validBaseVars()
	vars["MEDIA_BACKEND"] = "s3"
	vars["MEDIA_FS_DIR"] = ""
	vars["MEDIA_BUCKET"] = "bucket"
	vars["MEDIA_REGION"] = "region"
	vars["MEDIA_ENDPOINT"] = "http://user:" + secretSentinel + "@127.0.0.1:20091"
	vars["MEDIA_ACCESS_KEY_ID"] = accessSentinel
	vars["MEDIA_SECRET_ACCESS_KEY"] = secretSentinel
	vars["MEDIA_FORCE_PATH_STYLE"] = "true"

	_, err := config.Load(env(vars))
	if err == nil {
		t.Fatal("Load() error = nil, want malformed endpoint rejection")
	}
	if strings.Contains(err.Error(), accessSentinel) || strings.Contains(err.Error(), secretSentinel) {
		t.Errorf("Load() error leaks a credential sentinel: %v", err)
	}
}

func TestLoad_ProdStartsWithoutProviderCredentialsWhenLoginDisabled(t *testing.T) {
	t.Parallel()

	for _, environment := range []string{"prod", "staging"} {
		for _, flag := range []string{"", "false"} {
			_, err := config.Load(env(map[string]string{
				"DATABASE_URL":           "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN":          "https://aboutme.vn",
				"ENV":                    environment,
				"TRUSTED_PROXY_CIDRS":    "127.0.0.1/32",
				"PROVIDER_LOGIN_ENABLED": flag,
			}))
			if err != nil {
				t.Fatalf("ENV=%s flag %q: Load() error = %v", environment, flag, err)
			}
		}
	}
}
