// Package config loads and validates the server's process configuration
// from environment variables.
package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Config holds the server's validated runtime configuration.
type Config struct {
	// PrintListenAddr is the separate capability redemption listener.
	PrintListenAddr string
	// ChromiumPath identifies the pinned sandboxed browser executable.
	ChromiumPath string
	// Port is the TCP port the HTTP server listens on.
	Port int
	// ListenHost is the network interface address the HTTP server binds,
	// e.g. "127.0.0.1" (loopback only) or "0.0.0.0" (all interfaces).
	// Defaults to "127.0.0.1" and, when Env is "prod" or "staging", must
	// resolve to a loopback address: production's only supported topology
	// has Caddy as the sole process allowed to reach this server directly,
	// always over loopback, so port 8080 must never be reachable any other
	// way. Staging enforces the same boundary. A compose/self-host
	// topology that instead reaches this process over a container network
	// must set LISTEN_HOST explicitly to an interface Caddy's container can
	// reach.
	ListenHost string
	// DatabaseURL is the Postgres connection string.
	DatabaseURL string
	// LogLevel is one of "debug", "info", "warn", "error".
	LogLevel string
	// Env is one of "dev", "staging", "prod".
	Env string
	// PublicOrigin is the externally reachable origin (scheme://host, no
	// trailing slash) this server is served at, e.g. "https://aboutme.vn".
	// It has no safe default — dev, staging, and prod are each served from
	// a different origin — so, like Env, Load requires it and fails fast
	// rather than silently guessing. OAuth URLs and CSRF validation use it.
	PublicOrigin string
	// PublicRenderOrigin is the direct Nuxt listener used only for public SSR.
	PublicRenderOrigin string
	// AppBuildDigest and PublicRendererBuildDigest version private public caches.
	AppBuildDigest            string
	PublicRendererBuildDigest string
	// TrustedProxyCIDRs is the set of reverse-proxy hops this server
	// treats as able to assert a request's real client IP and scheme (see
	// api.TrustedProxies for the spoofing risk of getting this wrong).
	// Required (non-empty) when Env is "prod" or "staging": there is no safe
	// default in either direction (trusting everyone is a spoofing bypass;
	// trusting no one collapses every real client behind the actual proxy
	// into one shared bucket), so production and staging refuse to start
	// without it. Optional in dev, defaulting to nil (trust no one), since a dev
	// topology's correct value (e.g. a podman-compose network's subnet)
	// isn't something this package can know in advance.
	TrustedProxyCIDRs []netip.Prefix
	// Provider OAuth2 client credentials. Each pair is required in prod and
	// staging only when provider login is enabled; see loadProviderCredentials.
	// GitHub uses plain OAuth2; Google and LinkedIn use OIDC.
	GoogleClientID       string
	GoogleClientSecret   string
	GitHubClientID       string
	GitHubClientSecret   string
	LinkedInClientID     string
	LinkedInClientSecret string
	// GoogleOIDCIssuerURL and LinkedInOIDCIssuerURL select local development
	// OIDC issuers. Empty values retain the built-in production issuers.
	GoogleOIDCIssuerURL   string
	LinkedInOIDCIssuerURL string
	// GitHubOAuthAuthorizeURL, GitHubOAuthTokenURL, and GitHubAPIBaseURL
	// select one complete local development GitHub provider. Empty values
	// retain the built-in production endpoints.
	GitHubOAuthAuthorizeURL string
	GitHubOAuthTokenURL     string
	GitHubAPIBaseURL        string
	// MediaBackend selects the private object store: "fs" for native
	// development or "s3" for Compose and AWS.
	MediaBackend string
	// MediaFSDir is the rooted filesystem store directory. It is required
	// only when MediaBackend is "fs".
	MediaFSDir string
	// MediaBucket and MediaRegion are required when MediaBackend is "s3".
	MediaBucket string
	MediaRegion string
	// MediaEndpoint selects custom-endpoint S3 mode. An empty endpoint uses
	// AWS's default endpoint and task-role credential chain.
	MediaEndpoint string
	// MediaAccessKeyID and MediaSecretAccessKey are required only for a
	// custom endpoint. Their values must never enter errors or logs.
	MediaAccessKeyID     string
	MediaSecretAccessKey string
	// MediaForcePathStyle must be true with a custom endpoint and absent in
	// AWS mode.
	MediaForcePathStyle bool
	// AuthEmail is the validated password-mail configuration (rate HMAC key,
	// key ring, mode, and sender). See AuthEmailConfig.
	AuthEmail AuthEmailConfig
	// AgentAccess is the closed MCP/OAuth feature configuration. All numeric
	// values are the frozen budgets from docs/design/budgets.md; operators may
	// enable or disable the feature but cannot silently widen those bounds.
	AgentAccess AgentAccessConfig
	// ProviderLogin names the providers whose login, callback, link, and
	// reauthentication routes are registered (ADR 0027, ADR 0039). The zero
	// value is password-only; each provider turns on without a code change.
	ProviderLogin ProviderLogin
	// PasswordRegistrationDisabled unregisters POST /auth/password/register
	// (PASSWORD_REGISTRATION_ENABLED=false). Pending registrations still verify,
	// and every other password route is unchanged.
	PasswordRegistrationDisabled bool
	// PasskeyEnrollment reports whether new passkey enrollment is open
	// (PASSKEY_ENROLLMENT_ENABLED=true). It defaults to false. Verification,
	// removal, recovery, and state routes are unaffected either way. When
	// true, Load also requires PublicOrigin to produce a valid WebAuthn
	// relying party (see passkey.go), so an operator cannot enable enrollment
	// behind a host WebAuthn ceremonies would reject.
	PasskeyEnrollment bool
	// TOTPEnrollment reports whether new authenticator-app enrollment and
	// replacement are open (TOTP_ENROLLMENT_ENABLED=true). It defaults to
	// false. Verification, removal, recovery, and state routes are
	// unaffected either way (docs/design/totp-second-factor-contract.md
	// "Enrollment and replacement API").
	TOTPEnrollment bool
	// PreviewCards turns on stored link-preview cards
	// (PREVIEW_CARD_ENABLED=true): pages name the versioned card, og.png
	// serves it, and the card scheduler runs. It defaults to false, which
	// keeps the og.png share image, because the card needs a web renderer
	// that draws the card envelope (docs/adr/0055-stored-link-preview-card.md).
	PreviewCards bool
	// TOTPActiveKey and TOTPPreviousKey are the canonical 43-character
	// unpadded base64url TOTP sealing keys from TOTP_ACTIVE_KEY and the
	// optional TOTP_PREVIOUS_KEY. TOTPActiveKey is always required, whether
	// or not TOTPEnrollment is set, because verification of an existing
	// credential must keep working when enrollment is off. TOTPPreviousKey
	// is empty when no previous key is configured
	// (docs/design/totp-key-management.md "Key ring").
	TOTPActiveKey   string
	TOTPPreviousKey string
}

const (
	defaultPort       = 8080
	defaultListenHost = "127.0.0.1"
	defaultLogLevel   = "info"

	minPort = 1
	maxPort = 65535
)

var validLogLevels = map[string]bool{
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
}

var validEnvs = map[string]bool{
	"dev":     true,
	"staging": true,
	"prod":    true,
}

// Load reads and validates configuration using getenv to look up each
// variable. Passing os.Getenv reads the real process environment; tests can
// substitute a fake lookup so they never mutate global state.
//
// PORT defaults to 8080, LISTEN_HOST defaults to "127.0.0.1", and
// LOG_LEVEL defaults to "info" when unset. DATABASE_URL, ENV, and
// PUBLIC_ORIGIN and MEDIA_BACKEND have no safe default and are required.
// TRUSTED_PROXY_CIDRS is required when
// ENV=prod or ENV=staging (see TrustedProxyCIDRs and
// requiresProductionTrustBoundary) and optional in dev. Load fails fast
// with a descriptive error naming the offending variable when any of these
// are missing or invalid.
func Load(getenv func(string) string) (Config, error) {
	port, err := loadPort(getenv("PORT"))
	if err != nil {
		return Config{}, err
	}

	databaseURL := strings.TrimSpace(getenv("DATABASE_URL"))
	if databaseURL == "" {
		return Config{}, fmt.Errorf("config: DATABASE_URL is required")
	}

	logLevel, err := loadLogLevel(getenv("LOG_LEVEL"))
	if err != nil {
		return Config{}, err
	}

	env, err := loadEnv(getenv("ENV"))
	if err != nil {
		return Config{}, err
	}

	publicOrigin, err := loadPublicOrigin(getenv("PUBLIC_ORIGIN"))
	if err != nil {
		return Config{}, err
	}
	printListenAddr, chromiumPath, err := loadPrintConfig(getenv, env)
	if err != nil {
		return Config{}, err
	}
	publicRenderOrigin := getenv("PUBLIC_RENDER_ORIGIN")
	appBuildDigest := getenv("APP_BUILD_DIGEST")
	publicRendererBuildDigest := getenv("PUBLIC_RENDERER_BUILD_DIGEST")

	providerCfg, err := loadProviderEndpoints(getenv, env, publicOrigin)
	if err != nil {
		return Config{}, err
	}

	listenHost, err := loadListenHost(getenv("LISTEN_HOST"), env)
	if err != nil {
		return Config{}, err
	}

	trustedProxyCIDRs, err := loadTrustedProxyCIDRs(getenv("TRUSTED_PROXY_CIDRS"), env)
	if err != nil {
		return Config{}, err
	}

	providerLogin, err := loadProviderLoginFlag(getenv("PROVIDER_LOGIN_ENABLED"))
	if err != nil {
		return Config{}, err
	}

	passwordRegistrationDisabled, err := loadPasswordRegistrationFlag(getenv("PASSWORD_REGISTRATION_ENABLED"))
	if err != nil {
		return Config{}, err
	}

	passkeyEnrollment, err := loadPasskeyEnrollmentFlag(getenv("PASSKEY_ENROLLMENT_ENABLED"))
	if err != nil {
		return Config{}, err
	}
	if passkeyEnrollment {
		if rpErr := validatePasskeyRelyingPartyOrigin(publicOrigin); rpErr != nil {
			return Config{}, rpErr
		}
	}

	totpEnrollment, err := loadTOTPEnrollmentFlag(getenv("TOTP_ENROLLMENT_ENABLED"))
	if err != nil {
		return Config{}, err
	}
	previewCards, err := loadPreviewCardFlag(getenv("PREVIEW_CARD_ENABLED"))
	if err != nil {
		return Config{}, err
	}
	totpActiveKey, totpPreviousKey, err := loadTOTPKeyRing(getenv("TOTP_ACTIVE_KEY"), getenv("TOTP_PREVIOUS_KEY"))
	if err != nil {
		return Config{}, err
	}

	googleClientID, googleClientSecret, err := loadProviderCredentials("GOOGLE", "Google", getenv, env, providerLogin.Google)
	if err != nil {
		return Config{}, err
	}

	githubClientID, githubClientSecret, err := loadProviderCredentials("GITHUB", "GitHub", getenv, env, providerLogin.GitHub)
	if err != nil {
		return Config{}, err
	}

	linkedInClientID, linkedInClientSecret, err := loadProviderCredentials("LINKEDIN", "LinkedIn", getenv, env, providerLogin.LinkedIn)
	if err != nil {
		return Config{}, err
	}

	mediaCfg, err := loadMediaConfig(getenv)
	if err != nil {
		return Config{}, err
	}

	authEmail, err := loadAuthEmailConfig(getenv, env)
	if err != nil {
		return Config{}, err
	}
	agentAccess, err := loadAgentAccessConfig(getenv("MCP_ENABLED"))
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		PrintListenAddr:              printListenAddr,
		ChromiumPath:                 chromiumPath,
		Port:                         port,
		ListenHost:                   listenHost,
		DatabaseURL:                  databaseURL,
		LogLevel:                     logLevel,
		Env:                          env,
		PublicOrigin:                 publicOrigin,
		PublicRenderOrigin:           publicRenderOrigin,
		AppBuildDigest:               appBuildDigest,
		PublicRendererBuildDigest:    publicRendererBuildDigest,
		TrustedProxyCIDRs:            trustedProxyCIDRs,
		GoogleClientID:               googleClientID,
		GoogleClientSecret:           googleClientSecret,
		GitHubClientID:               githubClientID,
		GitHubClientSecret:           githubClientSecret,
		LinkedInClientID:             linkedInClientID,
		LinkedInClientSecret:         linkedInClientSecret,
		GoogleOIDCIssuerURL:          providerCfg.googleIssuer,
		LinkedInOIDCIssuerURL:        providerCfg.linkedinIssuer,
		GitHubOAuthAuthorizeURL:      providerCfg.githubAuthorize,
		GitHubOAuthTokenURL:          providerCfg.githubToken,
		GitHubAPIBaseURL:             providerCfg.githubAPI,
		MediaBackend:                 mediaCfg.backend,
		MediaFSDir:                   mediaCfg.fsDir,
		MediaBucket:                  mediaCfg.bucket,
		MediaRegion:                  mediaCfg.region,
		MediaEndpoint:                mediaCfg.endpoint,
		MediaAccessKeyID:             mediaCfg.accessKeyID,
		MediaSecretAccessKey:         mediaCfg.secretAccessKey,
		MediaForcePathStyle:          mediaCfg.forcePathStyle,
		AuthEmail:                    authEmail,
		AgentAccess:                  agentAccess,
		ProviderLogin:                providerLogin,
		PasswordRegistrationDisabled: passwordRegistrationDisabled,
		PasskeyEnrollment:            passkeyEnrollment,
		PreviewCards:                 previewCards,
		TOTPEnrollment:               totpEnrollment,
		TOTPActiveKey:                totpActiveKey,
		TOTPPreviousKey:              totpPreviousKey,
	}
	if err := cfg.ValidateAgentAccess(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// LoadEnv loads configuration from the real process environment.
func LoadEnv() (Config, error) {
	return Load(os.Getenv)
}

func loadPort(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultPort, nil
	}

	port, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("config: PORT: invalid value %q: must be an integer", raw)
	}
	if port < minPort || port > maxPort {
		return 0, fmt.Errorf("config: PORT: invalid value %d: must be between %d and %d", port, minPort, maxPort)
	}
	return port, nil
}

func loadLogLevel(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultLogLevel, nil
	}

	level := strings.ToLower(raw)
	if !validLogLevels[level] {
		return "", fmt.Errorf("config: LOG_LEVEL: invalid value %q: must be one of debug, info, warn, error", raw)
	}
	return level, nil
}

func loadEnv(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("config: ENV is required: must be one of dev, staging, prod")
	}

	env := strings.ToLower(raw)
	if !validEnvs[env] {
		return "", fmt.Errorf("config: ENV: invalid value %q: must be one of dev, staging, prod", raw)
	}
	return env, nil
}

// loadPublicOrigin validates raw as the server's public origin. Required,
// like Env: there is no safe default (dev/staging/prod each serve from a
// different origin), so Load fails fast rather than silently guessing.
//
// OAuth redirect and callback URLs concatenate PublicOrigin with a path.
// Raw must therefore parse as scheme://host[:port] with no userinfo, path
// (including a bare trailing slash, which parses as Path "/"), query, or
// fragment. An unnoticed trailing slash or path would corrupt every absolute
// URL this server builds from it, so this fails fast at startup rather
// than surfacing as a broken redirect later.
//
// After validation, scheme and host are lowercased, and a scheme's default
// port (":80" for http, ":443" for https) is stripped. This avoids common
// mismatches because csrf.go compares PublicOrigin with the Origin header by
// exact string equality, and OAuth URLs concatenate it verbatim. Operators
// must still supply the browser-serialized form for representations this
// function does not canonicalize, including internationalized domain names
// and equivalent IPv6 spellings.
func loadPublicOrigin(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("config: PUBLIC_ORIGIN is required")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("config: PUBLIC_ORIGIN: invalid value %q: %w", raw, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("config: PUBLIC_ORIGIN: invalid value %q: scheme must be http or https", raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("config: PUBLIC_ORIGIN: invalid value %q: must include a host", raw)
	}
	if u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("config: PUBLIC_ORIGIN: invalid value %q: must be scheme://host[:port] only "+
			"— no userinfo, path, trailing slash, query, or fragment", raw)
	}

	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	hostport := host
	if strings.Contains(host, ":") { // IPv6 literal: re-add the brackets url.Hostname() strips.
		hostport = "[" + host + "]"
	}
	if port != "" {
		hostport += ":" + port
	}
	return scheme + "://" + hostport, nil
}

// requiresProductionTrustBoundary reports whether env must satisfy
// production's client-IP trust-boundary strictness: a loopback LISTEN_HOST
// (loadListenHost) and a required, non-empty TRUSTED_PROXY_CIDRS
// (loadTrustedProxyCIDRs). Both "prod" and "staging" require it.
func requiresProductionTrustBoundary(env string) bool {
	return env == "prod" || env == "staging"
}

// loadListenHost validates raw as an IP address to bind, defaulting to
// defaultListenHost when unset. When env requires production trust-boundary
// strictness (see requiresProductionTrustBoundary) it additionally requires
// the result to be a loopback address: binding anything
// else would make port 8080 reachable around Caddy's origin-secret boundary
// entirely.
func loadListenHost(raw, env string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = defaultListenHost
	}

	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return "", fmt.Errorf("config: LISTEN_HOST: invalid value %q: must be a valid IP address: %w", raw, err)
	}
	if requiresProductionTrustBoundary(env) && !addr.Unmap().IsLoopback() {
		return "", fmt.Errorf("config: LISTEN_HOST: invalid value %q: ENV=%s requires a loopback address "+
			"(design spec §6: Caddy is the only process ever allowed to reach this one directly)", raw, env)
	}
	return raw, nil
}
