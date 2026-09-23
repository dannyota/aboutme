package authmail

import (
	"strings"
	"testing"
)

func TestSecondFactorTemplatesRenderBilingualSecretFreeNotices(t *testing.T) {
	cases := []struct {
		kind Kind
		json string
	}{
		{Kind("second_factor_enabled"), `{"version":2,"to":"alice@example.com","occurredAt":"2026-09-20T09:00:00Z"}`},
		{Kind("passkey_added"), `{"version":2,"to":"alice@example.com","occurredAt":"2026-09-20T09:00:00Z"}`},
		{Kind("passkey_removed"), `{"version":2,"to":"alice@example.com","occurredAt":"2026-09-20T09:00:00Z"}`},
		{Kind("second_factor_disabled"), `{"version":2,"to":"alice@example.com","occurredAt":"2026-09-20T09:00:00Z"}`},
		{Kind("recovery_codes_regenerated"), `{"version":2,"to":"alice@example.com","occurredAt":"2026-09-20T09:00:00Z"}`},
		{Kind("recovery_code_used"), `{"version":2,"to":"alice@example.com","occurredAt":"2026-09-20T09:00:00Z","remainingRecoveryCodes":9}`},
		{Kind("second_factor_attempts_exhausted"), `{"version":2,"to":"alice@example.com","occurredAt":"2026-09-20T09:00:00Z"}`},
		{Kind("totp_added"), `{"version":2,"to":"alice@example.com","occurredAt":"2026-09-20T09:00:00Z"}`},
		{Kind("totp_replaced"), `{"version":2,"to":"alice@example.com","occurredAt":"2026-09-20T09:00:00Z"}`},
		{Kind("totp_removed"), `{"version":2,"to":"alice@example.com","occurredAt":"2026-09-20T09:00:00Z"}`},
	}

	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			payload, err := decodePayloadStrict([]byte(tc.json))
			if err != nil {
				t.Fatalf("decodePayloadStrict: %v", err)
			}
			message := buildMessage(tc.kind, payload)
			if message.Subject == "" {
				t.Fatal("security message has no subject")
			}
			for _, body := range []string{message.TextBody, message.HTMLBody} {
				for _, want := range []string{"2026-09-20T09:00:00Z", "mật khẩu", "password", "phiên", "sessions"} {
					if !strings.Contains(body, want) {
						t.Errorf("body missing %q: %s", want, body)
					}
				}
				for _, forbidden := range []string{
					"credential-id", "public-key", "challenge", "signature", "alice@example.com",
					"otpauth", "secret", "provisioning",
				} {
					if strings.Contains(body, forbidden) {
						t.Errorf("body contains forbidden material %q: %s", forbidden, body)
					}
				}
			}
			if tc.kind == Kind("recovery_code_used") {
				if !strings.Contains(message.TextBody, "9") || !strings.Contains(message.HTMLBody, "9") {
					t.Fatal("recovery-code notice does not show the remaining count")
				}
			}
			switch tc.kind {
			case Kind("totp_added"), Kind("totp_replaced"), Kind("totp_removed"):
				for _, body := range []string{message.TextBody, message.HTMLBody} {
					if !strings.Contains(body, "ứng dụng xác thực") || !strings.Contains(body, "authenticator app") {
						t.Errorf("TOTP notice does not name an authenticator app: %s", body)
					}
				}
			}
		})
	}
}
