package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"danny.vn/aboutme/apps/server/internal/config"
)

func TestCapabilitiesRegistrarReflectsConfig(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name              string
		login             config.ProviderLogin
		registerOff       bool
		passkeyEnrollment bool
		totpEnrollment    bool
		signInToView      bool
		want              string
	}{
		{"password only", config.ProviderLogin{}, false, false, false, false, `{"data":{"providerLogin":false,"providers":[],"agentAccess":false,"passwordRegistration":true,"passkeyEnrollment":false,"totpEnrollment":false,"signInToView":false}}` + "\n"},
		{"google only", config.ProviderLogin{Google: true}, false, false, false, false, `{"data":{"providerLogin":true,"providers":["google"],"agentAccess":false,"passwordRegistration":true,"passkeyEnrollment":false,"totpEnrollment":false,"signInToView":false}}` + "\n"},
		{"every provider", config.ProviderLogin{Google: true, GitHub: true, LinkedIn: true}, false, false, false, false,
			`{"data":{"providerLogin":true,"providers":["google","github","linkedin"],"agentAccess":false,"passwordRegistration":true,"passkeyEnrollment":false,"totpEnrollment":false,"signInToView":false}}` + "\n"},
		{"google sign-up only", config.ProviderLogin{Google: true}, true, false, false, false, `{"data":{"providerLogin":true,"providers":["google"],"agentAccess":false,"passwordRegistration":false,"passkeyEnrollment":false,"totpEnrollment":false,"signInToView":false}}` + "\n"},
		{"passkey enrollment open", config.ProviderLogin{}, false, true, false, false, `{"data":{"providerLogin":false,"providers":[],"agentAccess":false,"passwordRegistration":true,"passkeyEnrollment":true,"totpEnrollment":false,"signInToView":false}}` + "\n"},
		{"totp enrollment open", config.ProviderLogin{}, false, false, true, false, `{"data":{"providerLogin":false,"providers":[],"agentAccess":false,"passwordRegistration":true,"passkeyEnrollment":false,"totpEnrollment":true,"signInToView":false}}` + "\n"},
		{"sign in to view open", config.ProviderLogin{}, false, false, false, true, `{"data":{"providerLogin":false,"providers":[],"agentAccess":false,"passwordRegistration":true,"passkeyEnrollment":false,"totpEnrollment":false,"signInToView":true}}` + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Config{ProviderLogin: tc.login, PasswordRegistrationDisabled: tc.registerOff, PasskeyEnrollment: tc.passkeyEnrollment, TOTPEnrollment: tc.totpEnrollment, SignInToViewEnabled: tc.signInToView}
			cfg.AgentAccess.Enabled = false
			mux := http.NewServeMux()
			capabilitiesRegistrar(cfg)(mux)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/capabilities", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if got := rec.Body.String(); got != tc.want {
				t.Fatalf("body = %q, want %q", got, tc.want)
			}
		})
	}
}
