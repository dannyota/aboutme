package config

import (
	"fmt"
	"os"
	"strings"
)

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
	cimd, err := loadCIMDConfig(getenv, env)
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
	agentAccess, err := loadAgentAccessConfig(getenv("MCP_ENABLED"), getenv("OAUTH_REGISTER_EGRESS_CIDRS"))
	if err != nil {
		return Config{}, err
	}
	agentAccess.CIMD = cimd

	signInToViewEnabled, err := loadSignInToViewFlag(getenv("SIGN_IN_TO_VIEW_ENABLED"))
	if err != nil {
		return Config{}, err
	}
	viewPassKey, err := loadViewPassKey(getenv("VIEW_PASS_KEY"))
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
		SignInToViewEnabled:          signInToViewEnabled,
		ViewPassKey:                  viewPassKey,
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
