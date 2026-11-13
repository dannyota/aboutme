package config

import "net/netip"

type Config struct {
	PrintListenAddr              string
	ChromiumPath                 string
	Port                         int
	ListenHost                   string
	DatabaseURL                  string
	LogLevel                     string
	Env                          string
	PublicOrigin                 string
	PublicRenderOrigin           string
	AppBuildDigest               string
	PublicRendererBuildDigest    string
	TrustedProxyCIDRs            []netip.Prefix
	GoogleClientID               string
	GoogleClientSecret           string
	GitHubClientID               string
	GitHubClientSecret           string
	LinkedInClientID             string
	LinkedInClientSecret         string
	GoogleOIDCIssuerURL          string
	LinkedInOIDCIssuerURL        string
	GitHubOAuthAuthorizeURL      string
	GitHubOAuthTokenURL          string
	GitHubAPIBaseURL             string
	MediaBackend                 string
	MediaFSDir                   string
	MediaBucket                  string
	MediaRegion                  string
	MediaEndpoint                string
	MediaAccessKeyID             string
	MediaSecretAccessKey         string
	MediaForcePathStyle          bool
	AuthEmail                    AuthEmailConfig
	AgentAccess                  AgentAccessConfig
	ProviderLogin                ProviderLogin
	PasswordRegistrationDisabled bool
	PasskeyEnrollment            bool
	TOTPEnrollment               bool
	PreviewCards                 bool
	TOTPActiveKey                string
	TOTPPreviousKey              string
	SignInToViewEnabled          bool
	ViewPassKey                  string
}

const (
	defaultPort       = 8080
	defaultListenHost = "127.0.0.1"
	defaultLogLevel   = "info"

	minPort = 1
	maxPort = 65535
)

func validLogLevel(level string) bool {
	switch level {
	case "debug", "info", "warn", "error":
		return true
	default:
		return false
	}
}

func validEnv(env string) bool {
	switch env {
	case "dev", "staging", "prod":
		return true
	default:
		return false
	}
}
