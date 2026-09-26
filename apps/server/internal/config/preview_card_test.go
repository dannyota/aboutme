package config_test

import (
	"strings"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/config"
)

func TestLoad_PreviewCardFlag(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]bool{"": false, "false": false, " true ": true, "true": true} {
		vars := validDevEnv()
		vars["PREVIEW_CARD_ENABLED"] = raw
		got, err := config.Load(env(vars))
		if err != nil {
			t.Fatalf("Load(%q) error = %v", raw, err)
		}
		if got.PreviewCards != want {
			t.Fatalf("Load(%q).PreviewCards = %t, want %t", raw, got.PreviewCards, want)
		}
	}
}

func TestLoad_PreviewCardFlagRejectsInvalidValueWithoutEcho(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"yes-secret-sentinel", "TRUE", "1", "enabled"} {
		vars := validDevEnv()
		vars["PREVIEW_CARD_ENABLED"] = raw
		_, err := config.Load(env(vars))
		if err == nil || !strings.Contains(err.Error(), "PREVIEW_CARD_ENABLED") || strings.Contains(err.Error(), raw) {
			t.Fatalf("Load(%q) error = %v, want the variable name without the raw value", raw, err)
		}
	}
}
