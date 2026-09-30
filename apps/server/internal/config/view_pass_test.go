package config_test

import (
	"strings"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/config"
)

// AC-VIEW-001, AC-VIEW-007: the sign-in-to-view flags default off and the
// pass-sealing key is always required, mirroring the other runtime keys.

func TestLoad_SignInToViewFlag(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]bool{"": false, "false": false, " true ": true, "true": true} {
		vars := validDevEnv()
		vars["SIGN_IN_TO_VIEW_ENABLED"] = raw
		got, err := config.Load(env(vars))
		if err != nil {
			t.Fatalf("Load(%q) error = %v", raw, err)
		}
		if got.SignInToViewEnabled != want {
			t.Fatalf("Load(%q).SignInToViewEnabled = %t, want %t", raw, got.SignInToViewEnabled, want)
		}
	}
}

func TestLoad_SignInToViewFlagRejectsInvalidValueWithoutEcho(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"yes-secret-sentinel", "TRUE", "1", "enabled"} {
		vars := validDevEnv()
		vars["SIGN_IN_TO_VIEW_ENABLED"] = raw
		_, err := config.Load(env(vars))
		if err == nil || !strings.Contains(err.Error(), "SIGN_IN_TO_VIEW_ENABLED") || strings.Contains(err.Error(), raw) {
			t.Fatalf("Load(%q) error = %v, want the variable name without the raw value", raw, err)
		}
	}
}

func TestLoad_ViewPassKeyRoundTrips(t *testing.T) {
	t.Parallel()
	vars := validDevEnv()
	vars["VIEW_PASS_KEY"] = testBase64URL32
	got, err := config.Load(env(vars))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.ViewPassKey != testBase64URL32 {
		t.Fatalf("Load().ViewPassKey = %q, want %q", got.ViewPassKey, testBase64URL32)
	}
}

func TestLoad_ViewPassKeyRequiredRegardlessOfFlag(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "true-but-not-base64-and-wrong-length", testBase64URL32[:42]} {
		vars := validDevEnv()
		vars["VIEW_PASS_KEY"] = raw
		vars["SIGN_IN_TO_VIEW_ENABLED"] = "false"
		_, err := config.Load(env(vars))
		if err == nil || !strings.Contains(err.Error(), "VIEW_PASS_KEY") {
			t.Fatalf("Load() with VIEW_PASS_KEY=%q error = %v, want a VIEW_PASS_KEY error", raw, err)
		}
		if raw != "" && strings.Contains(err.Error(), raw) {
			t.Fatalf("Load() error = %v, must not echo the raw key value", err)
		}
	}
}

func TestLoad_ViewPassKeyRejectsNonCanonicalEncoding(t *testing.T) {
	t.Parallel()
	// 43 characters of the right length whose low bits are not the
	// canonical zero padding: it decodes, but re-encoding it would not
	// reproduce the same string, so the loader must reject it rather than
	// accept an ambiguous key spelling.
	vars := validDevEnv()
	vars["VIEW_PASS_KEY"] = testBase64URL32[:42] + "B"
	_, err := config.Load(env(vars))
	if err == nil || !strings.Contains(err.Error(), "VIEW_PASS_KEY") {
		t.Fatalf("Load() error = %v, want a VIEW_PASS_KEY error for a non-canonical encoding", err)
	}
}
