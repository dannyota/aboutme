package config_test

import (
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/config"
)

// captureAuthEmail returns the overrides that select valid dev capture mode,
// including clearing every SES-only field so the shared env defaults never
// leak into a capture test.
func captureAuthEmail() map[string]string {
	return map[string]string{
		"AUTH_EMAIL_MODE":           "capture",
		"AUTH_EMAIL_CAPTURE_URL":    "http://127.0.0.1:20091",
		"AUTH_EMAIL_CAPTURE_BEARER": testBase64URL32,
		"SES_FROM_ADDRESS":          "",
		"SES_CONFIGURATION_SET":     "",
		"AWS_REGION":                "",
	}
}

// sesAuthEmail returns the overrides that select valid SES mode, including
// clearing every capture-only field.
func sesAuthEmail() map[string]string {
	return map[string]string{
		"AUTH_EMAIL_MODE":           "ses",
		"SES_FROM_ADDRESS":          "noreply@example.com",
		"SES_CONFIGURATION_SET":     "aboutme",
		"AWS_REGION":                "ap-southeast-1",
		"AUTH_EMAIL_CAPTURE_URL":    "",
		"AUTH_EMAIL_CAPTURE_BEARER": "",
	}
}

// applySES mutates vars (already carrying the capture-mode base) into a valid
// SES mode, so SES-specific rejection cases start from a valid SES shape.
func applySES(v map[string]string) {
	for k, val := range sesAuthEmail() {
		v[k] = val
	}
}

func TestLoad_AuthEmailCaptureModeDev(t *testing.T) {
	t.Parallel()

	vars := validDevEnv()
	for k, v := range captureAuthEmail() {
		vars[k] = v
	}
	got, err := config.Load(env(vars))
	if err != nil {
		t.Fatal(err)
	}
	a := got.AuthEmail
	if a.Mode != "capture" {
		t.Errorf("Mode = %q, want capture", a.Mode)
	}
	if a.CaptureURL != "http://127.0.0.1:20091" {
		t.Errorf("CaptureURL = %q", a.CaptureURL)
	}
	if a.ActiveKeyID != "k1" {
		t.Errorf("ActiveKeyID = %q, want k1", a.ActiveKeyID)
	}
	if a.RateHMACKey != ([32]byte{}) {
		t.Errorf("RateHMACKey not the decoded zero key")
	}
	if a.HasPrevious {
		t.Error("HasPrevious = true, want false")
	}
}

func TestLoad_AuthEmailSESMode(t *testing.T) {
	t.Parallel()

	vars := validDevEnv()
	for k, v := range sesAuthEmail() {
		vars[k] = v
	}
	got, err := config.Load(env(vars))
	if err != nil {
		t.Fatal(err)
	}
	a := got.AuthEmail
	if a.Mode != "ses" || a.SESFrom != "noreply@example.com" || a.SESConfigSet != "aboutme" || a.SESRegion != "ap-southeast-1" {
		t.Errorf("SES config = %+v", a)
	}
}

func TestLoad_AuthEmailPreviousKeyPair(t *testing.T) {
	t.Parallel()

	vars := validDevEnv()
	for k, v := range captureAuthEmail() {
		vars[k] = v
	}
	vars["AUTH_EMAIL_PREVIOUS_KEY_ID"] = "k0"
	vars["AUTH_EMAIL_PREVIOUS_KEY"] = testBase64URL32
	got, err := config.Load(env(vars))
	if err != nil {
		t.Fatal(err)
	}
	if !got.AuthEmail.HasPrevious || got.AuthEmail.PreviousKeyID != "k0" {
		t.Errorf("previous key = %+v", got.AuthEmail)
	}
}

func TestLoad_AuthEmailRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mut  func(map[string]string)
	}{
		{"missing rate key", func(v map[string]string) { v["PASSWORD_RATE_HMAC_KEY"] = "" }},
		{"short rate key", func(v map[string]string) { v["PASSWORD_RATE_HMAC_KEY"] = "AAAA" }},
		{"padded rate key", func(v map[string]string) {
			v["PASSWORD_RATE_HMAC_KEY"] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
		}},
		{"missing active key id", func(v map[string]string) { v["AUTH_EMAIL_ACTIVE_KEY_ID"] = "" }},
		{"non-ascii active key id", func(v map[string]string) { v["AUTH_EMAIL_ACTIVE_KEY_ID"] = "k\n1" }},
		{"missing active key", func(v map[string]string) { v["AUTH_EMAIL_ACTIVE_KEY"] = "" }},
		{"half-set previous id", func(v map[string]string) { v["AUTH_EMAIL_PREVIOUS_KEY_ID"] = "k0" }},
		{"half-set previous key", func(v map[string]string) { v["AUTH_EMAIL_PREVIOUS_KEY"] = testBase64URL32 }},
		{"duplicate previous id", func(v map[string]string) {
			v["AUTH_EMAIL_PREVIOUS_KEY_ID"] = "k1"
			v["AUTH_EMAIL_PREVIOUS_KEY"] = testBase64URL32
		}},
		{"unknown mode", func(v map[string]string) { v["AUTH_EMAIL_MODE"] = "mailgun" }},
		{"capture missing url", func(v map[string]string) { v["AUTH_EMAIL_CAPTURE_URL"] = "" }},
		{"capture https url", func(v map[string]string) { v["AUTH_EMAIL_CAPTURE_URL"] = "https://127.0.0.1:20091" }},
		{"capture non-loopback url", func(v map[string]string) { v["AUTH_EMAIL_CAPTURE_URL"] = "http://example.com:20091" }},
		{"capture no port", func(v map[string]string) { v["AUTH_EMAIL_CAPTURE_URL"] = "http://127.0.0.1" }},
		{"capture missing bearer", func(v map[string]string) { v["AUTH_EMAIL_CAPTURE_BEARER"] = "" }},
		{"capture with ses field", func(v map[string]string) { v["SES_FROM_ADDRESS"] = "noreply@example.com" }},
		{"ses with capture url", func(v map[string]string) {
			applySES(v)
			v["AUTH_EMAIL_CAPTURE_URL"] = "http://127.0.0.1:20091"
		}},
		{"ses wrong region", func(v map[string]string) {
			applySES(v)
			v["AWS_REGION"] = "us-east-1"
		}},
		{"ses noncanonical from", func(v map[string]string) {
			applySES(v)
			v["SES_FROM_ADDRESS"] = "not an email"
		}},
		{"ses bad config set", func(v map[string]string) {
			applySES(v)
			v["SES_CONFIGURATION_SET"] = "bad set!"
		}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vars := validDevEnv()
			for k, v := range captureAuthEmail() {
				vars[k] = v
			}
			tt.mut(vars)
			if _, err := config.Load(env(vars)); err == nil {
				t.Fatal("Load() error = nil, want rejection")
			}
		})
	}
}

func TestLoad_AuthEmailCaptureRejectedOutsideDev(t *testing.T) {
	t.Parallel()

	for _, environment := range []string{"staging", "prod"} {
		vars := validDevEnv()
		vars["ENV"] = environment
		for k, v := range sesAuthEmail() {
			vars[k] = v
		}
		// Switch to capture mode.
		vars["AUTH_EMAIL_MODE"] = "capture"
		vars["AUTH_EMAIL_CAPTURE_URL"] = "http://127.0.0.1:20091"
		vars["AUTH_EMAIL_CAPTURE_BEARER"] = testBase64URL32
		vars["SES_FROM_ADDRESS"] = ""
		vars["SES_CONFIGURATION_SET"] = ""
		vars["AWS_REGION"] = ""

		if _, err := config.Load(env(vars)); err == nil {
			t.Errorf("ENV=%s capture mode Load() error = nil, want rejection", environment)
		}
	}
}
