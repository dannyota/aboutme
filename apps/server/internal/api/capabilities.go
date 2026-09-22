package api

import (
	"fmt"
	"net/http"
)

// Capabilities is the closed, unauthenticated feature-flag read the web uses
// before rendering sign-in and settings surfaces (docs/design/api.md,
// "Endpoint groups"). It carries flags and the enabled provider path names
// (google, github, linkedin) only, never credentials or other configuration.
// ProviderLogin reports whether Providers is non-empty.
type Capabilities struct {
	ProviderLogin bool     `json:"providerLogin"`
	Providers     []string `json:"providers"`
	AgentAccess   bool     `json:"agentAccess"`
	// PasswordRegistration reports whether email-and-password sign-up is open.
	PasswordRegistration bool `json:"passwordRegistration"`
	// PasskeyEnrollment reports whether new passkey enrollment is open
	// (PASSKEY_ENROLLMENT_ENABLED). It is never derived from any account's
	// own state: this is a deployment-wide switch, not a per-user flag.
	PasskeyEnrollment bool `json:"passkeyEnrollment"`
}

// CapabilitiesHandler serves GET /api/v1/capabilities. The router's default
// NoStoreCache chain supplies Cache-Control; this handler sets none. Unlike
// ordinary GET resources, this closed configuration read rejects HEAD.
func CapabilitiesHandler(c Capabilities) http.Handler {
	if c.Providers == nil {
		c.Providers = []string{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
				fmt.Sprintf("method not allowed on %s; use %s", r.URL.Path, http.MethodGet))
			return
		}
		WriteData(w, http.StatusOK, c)
	})
}
