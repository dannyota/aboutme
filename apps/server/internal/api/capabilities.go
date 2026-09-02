package api

import (
	"fmt"
	"net/http"
)

// Capabilities is the closed, unauthenticated feature-flag read the web uses
// before rendering sign-in and settings surfaces (docs/design/api.md,
// "Endpoint groups"). It carries flags only, never configuration values.
type Capabilities struct {
	ProviderLogin bool `json:"providerLogin"`
	AgentAccess   bool `json:"agentAccess"`
}

// CapabilitiesHandler serves GET /api/v1/capabilities. The router's default
// NoStoreCache chain supplies Cache-Control; this handler sets none. Unlike
// ordinary GET resources, this closed configuration read rejects HEAD.
func CapabilitiesHandler(c Capabilities) http.Handler {
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
