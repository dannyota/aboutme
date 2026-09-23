package config_test

import (
	"strings"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/config"
)

// testBase64URL32Alt is a second canonical 32-byte value, distinct from
// testBase64URL32, so tests can prove TOTP_ACTIVE_KEY and TOTP_PREVIOUS_KEY
// derive different key IDs when they hold different key bytes
// (docs/design/totp-key-management.md "Key ring").
const testBase64URL32Alt = "AQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func TestLoad_TOTPEnrollmentFlag(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		raw  string
		want bool
	}{
		{name: "absent is disabled", raw: "", want: false},
		{name: "explicit false", raw: "false", want: false},
		{name: "explicit true", raw: "true", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vars := validDevEnv()
			vars["TOTP_ENROLLMENT_ENABLED"] = tc.raw
			got, err := config.Load(env(vars))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got.TOTPEnrollment != tc.want {
				t.Fatalf("TOTPEnrollment = %t, want %t", got.TOTPEnrollment, tc.want)
			}
		})
	}
}

func TestLoad_TOTPEnrollmentFlagRejectsInvalidValueWithoutEcho(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"yes-secret-sentinel", "TRUE", "0", "off"} {
		vars := validDevEnv()
		vars["TOTP_ENROLLMENT_ENABLED"] = raw
		_, err := config.Load(env(vars))
		if err == nil {
			t.Fatalf("Load(%q) error = nil, want TOTP_ENROLLMENT_ENABLED rejection", raw)
		}
		if !strings.Contains(err.Error(), "TOTP_ENROLLMENT_ENABLED") || strings.Contains(err.Error(), raw) {
			t.Fatalf("Load(%q) error = %q, want the variable name without the raw value", raw, err)
		}
	}
}

// TestLoad_TOTPActiveKeyIsAlwaysRequired proves TOTP_ACTIVE_KEY is required
// regardless of TOTP_ENROLLMENT_ENABLED: verification of an existing
// credential must keep working when enrollment is off
// (docs/design/totp-second-factor-contract.md "Migration, mixed versions,
// and loss").
func TestLoad_TOTPActiveKeyIsAlwaysRequired(t *testing.T) {
	t.Parallel()
	for _, enrollment := range []string{"", "true"} {
		vars := validDevEnv()
		vars["TOTP_ENROLLMENT_ENABLED"] = enrollment
		vars["TOTP_ACTIVE_KEY"] = ""
		_, err := config.Load(env(vars))
		if err == nil {
			t.Fatalf("Load(enrollment=%q) error = nil, want a missing TOTP_ACTIVE_KEY rejection", enrollment)
		}
		if !strings.Contains(err.Error(), "TOTP_ACTIVE_KEY") {
			t.Fatalf("Load(enrollment=%q) error = %q, want it to name TOTP_ACTIVE_KEY", enrollment, err)
		}
	}
}

func TestLoad_TOTPActiveKeyRejectsMalformedValueWithoutEcho(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, raw string
	}{
		{"too short", "AAAA"},
		{"wrong alphabet", strings.Repeat("!", 43)},
		{"padded", testBase64URL32[:42] + "="},
		// The last character's two low bits are unused padding bits in a
		// 32-byte value's 43-character encoding; setting them nonzero still
		// decodes to 32 bytes but re-encodes to a different (canonical)
		// string, so DecodeTOTPKey's round-trip check rejects it.
		{"non-canonical", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vars := validDevEnv()
			vars["TOTP_ACTIVE_KEY"] = tc.raw
			_, err := config.Load(env(vars))
			if err == nil {
				t.Fatalf("Load() error = nil, want a TOTP_ACTIVE_KEY rejection for %q", tc.raw)
			}
			if !strings.Contains(err.Error(), "TOTP_ACTIVE_KEY") || strings.Contains(err.Error(), tc.raw) {
				t.Fatalf("Load() error = %q, want it to name TOTP_ACTIVE_KEY without echoing the raw value", err)
			}
		})
	}
}

func TestLoad_TOTPPreviousKeyIsOptional(t *testing.T) {
	t.Parallel()
	vars := validDevEnv()
	vars["TOTP_ACTIVE_KEY"] = testBase64URL32
	delete(vars, "TOTP_PREVIOUS_KEY")
	got, err := config.Load(env(vars))
	if err != nil {
		t.Fatalf("Load() error = %v, want an absent TOTP_PREVIOUS_KEY to be accepted", err)
	}
	if got.TOTPActiveKey != testBase64URL32 || got.TOTPPreviousKey != "" {
		t.Fatalf("TOTPActiveKey = %q, TOTPPreviousKey = %q, want active set and previous empty", got.TOTPActiveKey, got.TOTPPreviousKey)
	}
}

func TestLoad_TOTPPreviousKeyAcceptsADistinctValidKey(t *testing.T) {
	t.Parallel()
	vars := validDevEnv()
	vars["TOTP_ACTIVE_KEY"] = testBase64URL32
	vars["TOTP_PREVIOUS_KEY"] = testBase64URL32Alt
	got, err := config.Load(env(vars))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.TOTPPreviousKey != testBase64URL32Alt {
		t.Fatalf("TOTPPreviousKey = %q, want %q", got.TOTPPreviousKey, testBase64URL32Alt)
	}
}

func TestLoad_TOTPPreviousKeyRejectsMalformedValueWithoutEcho(t *testing.T) {
	t.Parallel()
	vars := validDevEnv()
	vars["TOTP_ACTIVE_KEY"] = testBase64URL32
	vars["TOTP_PREVIOUS_KEY"] = "not-a-valid-key"
	_, err := config.Load(env(vars))
	if err == nil {
		t.Fatal("Load() error = nil, want a TOTP_PREVIOUS_KEY rejection")
	}
	if !strings.Contains(err.Error(), "TOTP_PREVIOUS_KEY") || strings.Contains(err.Error(), "not-a-valid-key") {
		t.Fatalf("Load() error = %q, want it to name TOTP_PREVIOUS_KEY without echoing the raw value", err)
	}
}

// TestLoad_TOTPKeysRejectDuplicateKeyID proves startup fails when the active
// and previous keys derive the same key ID. The simplest colliding pair is
// the same 32 key bytes given for both variables
// (docs/design/totp-key-management.md "Key ring": "Startup fails ... when
// ... both keys derive the same ID").
func TestLoad_TOTPKeysRejectDuplicateKeyID(t *testing.T) {
	t.Parallel()
	vars := validDevEnv()
	vars["TOTP_ACTIVE_KEY"] = testBase64URL32
	vars["TOTP_PREVIOUS_KEY"] = testBase64URL32
	_, err := config.Load(env(vars))
	if err == nil {
		t.Fatal("Load() error = nil, want a duplicate key id rejection")
	}
	if !strings.Contains(err.Error(), "TOTP_PREVIOUS_KEY") {
		t.Fatalf("Load() error = %q, want it to name TOTP_PREVIOUS_KEY", err)
	}
}

// TestLoad_TOTPAcceptsNoKeyIDVariable proves the loader never reads a
// TOTP_ACTIVE_KEY_ID or TOTP_PREVIOUS_KEY_ID variable: the key ID is always
// derived from the key value (docs/design/totp-key-management.md "Key
// ring").
func TestLoad_TOTPAcceptsNoKeyIDVariable(t *testing.T) {
	t.Parallel()
	vars := validDevEnv()
	vars["TOTP_ACTIVE_KEY"] = testBase64URL32
	vars["TOTP_ACTIVE_KEY_ID"] = "should-be-ignored"
	vars["TOTP_PREVIOUS_KEY_ID"] = "should-be-ignored"
	got, err := config.Load(env(vars))
	if err != nil {
		t.Fatalf("Load() error = %v, want stray key-id variables to be ignored", err)
	}
	if got.TOTPActiveKey != testBase64URL32 {
		t.Fatalf("TOTPActiveKey = %q, want %q", got.TOTPActiveKey, testBase64URL32)
	}
}

func TestLoadTOTPReencryptJob_AcceptsTheReducedTaskEnvironment(t *testing.T) {
	t.Parallel()
	getenv := func(key string) string {
		switch key {
		case "DATABASE_URL":
			return "postgres://user:pass@localhost:5432/aboutme"
		case "TOTP_ACTIVE_KEY":
			return testBase64URL32
		case "TOTP_PREVIOUS_KEY":
			return testBase64URL32Alt
		default:
			return ""
		}
	}
	got, err := config.LoadTOTPReencryptJob(getenv)
	if err != nil {
		t.Fatalf("LoadTOTPReencryptJob() error = %v", err)
	}
	if got.DatabaseURL == "" || got.TOTPActiveKey != testBase64URL32 || got.TOTPPreviousKey != testBase64URL32Alt {
		t.Fatalf("LoadTOTPReencryptJob() = %#v, want DatabaseURL, TOTPActiveKey, and TOTPPreviousKey populated", got)
	}
}

func TestLoadTOTPReencryptJob_AcceptsAnAbsentPreviousKey(t *testing.T) {
	t.Parallel()
	getenv := func(key string) string {
		switch key {
		case "DATABASE_URL":
			return "postgres://user:pass@localhost:5432/aboutme"
		case "TOTP_ACTIVE_KEY":
			return testBase64URL32
		default:
			return ""
		}
	}
	got, err := config.LoadTOTPReencryptJob(getenv)
	if err != nil {
		t.Fatalf("LoadTOTPReencryptJob() error = %v", err)
	}
	if got.TOTPPreviousKey != "" {
		t.Fatalf("TOTPPreviousKey = %q, want empty", got.TOTPPreviousKey)
	}
}

func TestLoadTOTPReencryptJob_RejectsMissingDatabaseURL(t *testing.T) {
	t.Parallel()
	getenv := func(key string) string {
		if key == "TOTP_ACTIVE_KEY" {
			return testBase64URL32
		}
		return ""
	}
	_, err := config.LoadTOTPReencryptJob(getenv)
	if err == nil {
		t.Fatal("LoadTOTPReencryptJob() error = nil, want a missing DATABASE_URL rejection")
	}
}

func TestLoadTOTPReencryptJob_RejectsMissingOrMalformedActiveKeyWithoutEcho(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, raw string
	}{
		{"missing", ""},
		{"malformed", "not-a-valid-key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			getenv := func(key string) string {
				switch key {
				case "DATABASE_URL":
					return "postgres://user:pass@localhost:5432/aboutme"
				case "TOTP_ACTIVE_KEY":
					return tc.raw
				default:
					return ""
				}
			}
			_, err := config.LoadTOTPReencryptJob(getenv)
			if err == nil {
				t.Fatalf("LoadTOTPReencryptJob() error = nil, want a TOTP_ACTIVE_KEY rejection for %q", tc.raw)
			}
			if !strings.Contains(err.Error(), "TOTP_ACTIVE_KEY") || (tc.raw != "" && strings.Contains(err.Error(), tc.raw)) {
				t.Fatalf("LoadTOTPReencryptJob() error = %q, want it to name TOTP_ACTIVE_KEY without echoing the raw value", err)
			}
		})
	}
}
