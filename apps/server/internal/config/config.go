// Package config loads and validates the server's process configuration
// from environment variables.
package config

import "net/netip"

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
	// It has no safe default (dev, staging, and prod are each served from
	// a different origin), so, like Env, Load requires it and fails fast
	// rather than silently guessing. OAuth URLs and CSRF validation use it.
	PublicOrigin string
	// PublicRenderOrigin is the direct Nuxt listener used only for public SSR.
	PublicRenderOrigin string
	// AppBuildDigest is the application build digest (APP_BUILD_DIGEST)
	// that versions private public caches.
	AppBuildDigest string
	// PublicRendererBuildDigest is the public renderer build digest
	// (PUBLIC_RENDERER_BUILD_DIGEST) that versions private public caches.
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
	// GoogleClientID is the Google OIDC client ID (GOOGLE_CLIENT_ID).
	// Provider OAuth2 client credentials are read only for an enabled
	// provider, and each pair is required in prod and staging only when
	// that provider's login is enabled; see loadProviderCredentials. GitHub
	// uses plain OAuth2; Google and LinkedIn use OIDC.
	GoogleClientID string
	// GoogleClientSecret is the Google OIDC client secret
	// (GOOGLE_CLIENT_SECRET).
	GoogleClientSecret string
	// GitHubClientID is the GitHub OAuth2 client ID (GITHUB_CLIENT_ID).
	GitHubClientID string
	// GitHubClientSecret is the GitHub OAuth2 client secret
	// (GITHUB_CLIENT_SECRET).
	GitHubClientSecret string
	// LinkedInClientID is the LinkedIn OIDC client ID (LINKEDIN_CLIENT_ID).
	LinkedInClientID string
	// LinkedInClientSecret is the LinkedIn OIDC client secret
	// (LINKEDIN_CLIENT_SECRET).
	LinkedInClientSecret string
	// GoogleOIDCIssuerURL selects a local development Google OIDC issuer
	// (GOOGLE_OIDC_ISSUER_URL). Empty retains the built-in production issuer.
	GoogleOIDCIssuerURL string
	// LinkedInOIDCIssuerURL selects a local development LinkedIn OIDC issuer
	// (LINKEDIN_OIDC_ISSUER_URL). Empty retains the built-in production
	// issuer.
	LinkedInOIDCIssuerURL string
	// GitHubOAuthAuthorizeURL is the authorization endpoint of a local
	// development GitHub provider (GITHUB_OAUTH_AUTHORIZE_URL). The three
	// GitHub endpoint fields are set together to select one complete
	// provider. Empty values retain the built-in production endpoints.
	GitHubOAuthAuthorizeURL string
	// GitHubOAuthTokenURL is the token endpoint of a local development
	// GitHub provider (GITHUB_OAUTH_TOKEN_URL). Empty retains the built-in
	// production endpoint.
	GitHubOAuthTokenURL string
	// GitHubAPIBaseURL is the API base URL of a local development GitHub
	// provider (GITHUB_API_BASE_URL). Empty retains the built-in production
	// endpoint.
	GitHubAPIBaseURL string
	// MediaBackend selects the private object store: "fs" for native
	// development or "s3" for Compose and AWS.
	MediaBackend string
	// MediaFSDir is the rooted filesystem store directory. It is required
	// only when MediaBackend is "fs".
	MediaFSDir string
	// MediaBucket is the S3 bucket name. It is required when MediaBackend
	// is "s3".
	MediaBucket string
	// MediaRegion is the S3 region. It is required when MediaBackend is
	// "s3".
	MediaRegion string
	// MediaEndpoint selects custom-endpoint S3 mode. An empty endpoint uses
	// AWS's default endpoint and task-role credential chain.
	MediaEndpoint string
	// MediaAccessKeyID is the S3 access key ID. It is required only for a
	// custom endpoint, and its value must never enter errors or logs.
	MediaAccessKeyID string
	// MediaSecretAccessKey is the S3 secret access key. It is required only
	// for a custom endpoint, and its value must never enter errors or logs.
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
	// reauthentication routes are registered (ADR 0016). The zero
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
	// that draws the card envelope (docs/adr/0014-public-page-head-and-link-preview.md).
	PreviewCards bool
	// TOTPActiveKey is the canonical 43-character unpadded base64url TOTP
	// sealing key from TOTP_ACTIVE_KEY. It is always required, whether or
	// not TOTPEnrollment is set, because verification of an existing
	// credential must keep working when enrollment is off
	// (docs/design/totp-key-management.md "Key ring").
	TOTPActiveKey string
	// TOTPPreviousKey is the canonical 43-character unpadded base64url TOTP
	// sealing key from the optional TOTP_PREVIOUS_KEY. It is empty when no
	// previous key is configured (docs/design/totp-key-management.md
	// "Key ring").
	TOTPPreviousKey string
	// SignInToViewEnabled reports whether owners may turn the per-resume
	// sign-in-to-view switch on (SIGN_IN_TO_VIEW_ENABLED=true). It defaults
	// to false. It never lifts a gate already set on a resume; only turning
	// the switch off ever bypasses it (docs/design/viewer-analytics/sign-in-to-view.md
	// "Setting").
	SignInToViewEnabled bool
	// ViewPassKey is the canonical 43-character unpadded base64url encoding
	// of the 32 random bytes that seal the sign-in-to-view pass cookie
	// (docs/design/viewer-analytics/sign-in-to-view.md "Pass cookie"). It is
	// always required, like TOTPActiveKey, and never logged.
	ViewPassKey string
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
