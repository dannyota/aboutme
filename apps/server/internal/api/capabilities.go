package api

import (
	"fmt"
	"net/http"
)

type Capabilities struct {
	ProviderLogin        bool     `json:"providerLogin"`
	Providers            []string `json:"providers"`
	AgentAccess          bool     `json:"agentAccess"`
	PasswordRegistration bool     `json:"passwordRegistration"`
	PasskeyEnrollment    bool     `json:"passkeyEnrollment"`
	TotpEnrollment       bool     `json:"totpEnrollment"`
	SignInToView         bool     `json:"signInToView"`
}

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
