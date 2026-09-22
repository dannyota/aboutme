package config_test

import (
	"strings"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/config"
)

func TestLoad_PasskeyEnrollmentFlag(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		raw     string
		want    bool
		wantErr bool
	}{
		{name: "absent is disabled", raw: "", want: false},
		{name: "explicit false", raw: "false", want: false},
		{name: "explicit true", raw: "true", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vars := validDevEnv()
			vars["PASSKEY_ENROLLMENT_ENABLED"] = tc.raw
			got, err := config.Load(env(vars))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got.PasskeyEnrollment != tc.want {
				t.Fatalf("PasskeyEnrollment = %t, want %t", got.PasskeyEnrollment, tc.want)
			}
		})
	}
}

func TestLoad_PasskeyEnrollmentFlagRejectsInvalidValueWithoutEcho(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"yes-secret-sentinel", "TRUE", "0", "off"} {
		vars := validDevEnv()
		vars["PASSKEY_ENROLLMENT_ENABLED"] = raw
		_, err := config.Load(env(vars))
		if err == nil {
			t.Fatalf("Load(%q) error = nil, want PASSKEY_ENROLLMENT_ENABLED rejection", raw)
		}
		if !strings.Contains(err.Error(), "PASSKEY_ENROLLMENT_ENABLED") || strings.Contains(err.Error(), raw) {
			t.Fatalf("Load(%q) error = %q, want the variable name without the raw value", raw, err)
		}
	}
}

// TestLoad_PasskeyEnrollmentAcceptsHTTPSAndLocalhostOrigins proves both
// accepted relying-party origin shapes: any https domain, and http on
// exactly localhost (the native and HTTPS dev harnesses).
func TestLoad_PasskeyEnrollmentAcceptsHTTPSAndLocalhostOrigins(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, publicOrigin string
	}{
		{"https domain", "https://aboutme.vn"},
		{"https domain with port", "https://aboutme.example:8443"},
		{"http localhost", "http://localhost:20080"},
		{"https localhost", "https://localhost:20443"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := config.Load(env(map[string]string{
				"DATABASE_URL":               "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN":              tc.publicOrigin,
				"ENV":                        "dev",
				"PASSKEY_ENROLLMENT_ENABLED": "true",
			}))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if !got.PasskeyEnrollment {
				t.Fatalf("PasskeyEnrollment = false, want true")
			}
		})
	}
}

// TestLoad_PasskeyEnrollmentRejectsUnacceptableOrigins is the regression test
// for "reject startup with enabled enrollment when PUBLIC_ORIGIN cannot
// produce the accepted HTTPS or localhost relying-party configuration"
// (docs/design/second-factor-authentication.md, "Passkeys"). An enrollment
// flag left off never triggers this check — see
// TestLoad_PasskeyEnrollmentDisabledIgnoresOriginShape below.
func TestLoad_PasskeyEnrollmentRejectsUnacceptableOrigins(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, publicOrigin string
	}{
		{"IP literal https", "https://203.0.113.10"},
		{"IP literal http localhost-like", "https://127.0.0.1"},
		{"http non-localhost", "http://aboutme.example"},
		{"single-label https host", "https://internalhost"},
		{"IPv6 literal", "https://[2001:db8::1]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := config.Load(env(map[string]string{
				"DATABASE_URL":               "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN":              tc.publicOrigin,
				"ENV":                        "dev",
				"PASSKEY_ENROLLMENT_ENABLED": "true",
			}))
			if err == nil {
				t.Fatalf("Load() error = nil, want a relying-party origin rejection for %q", tc.publicOrigin)
			}
			if !strings.Contains(err.Error(), "PASSKEY_ENROLLMENT_ENABLED") {
				t.Fatalf("Load() error = %q, want it to name PASSKEY_ENROLLMENT_ENABLED", err)
			}
		})
	}
}

// TestLoad_PasskeyEnrollmentDisabledIgnoresOriginShape proves the relying
// party check runs only when enrollment is enabled: a disabled flag starts
// cleanly even with an origin that could never host WebAuthn ceremonies,
// since a self-hosted operator who never turns passkeys on should not be
// forced onto a domain host to run the server at all.
func TestLoad_PasskeyEnrollmentDisabledIgnoresOriginShape(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "false"} {
		vars := map[string]string{
			"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
			"PUBLIC_ORIGIN": "https://203.0.113.10",
			"ENV":           "dev",
		}
		if raw != "" {
			vars["PASSKEY_ENROLLMENT_ENABLED"] = raw
		}
		got, err := config.Load(env(vars))
		if err != nil {
			t.Fatalf("Load() error = %v, want a disabled flag to ignore the relying-party shape", err)
		}
		if got.PasskeyEnrollment {
			t.Fatalf("PasskeyEnrollment = true, want false")
		}
	}
}
