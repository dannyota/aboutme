package config_test

import (
	"fmt"
	"log/slog"
	"strings"
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
		"SES_FROM_NAME":             "",
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
		"SES_FROM_NAME":             "",
		"SES_CONFIGURATION_SET":     "aboutme",
		"AWS_REGION":                "ap-southeast-1",
		"AUTH_EMAIL_CAPTURE_URL":    "",
		"AUTH_EMAIL_CAPTURE_BEARER": "",
	}
}

// smtpAuthEmail returns the overrides that select valid SMTP mode, including
// clearing every capture-only and SES-only field.
func smtpAuthEmail() map[string]string {
	return map[string]string{
		"AUTH_EMAIL_MODE":           "smtp",
		"SES_FROM_ADDRESS":          "noreply@example.com",
		"SES_FROM_NAME":             "Danny from aboutme.vn",
		"SES_CONFIGURATION_SET":     "",
		"AWS_REGION":                "",
		"AUTH_EMAIL_CAPTURE_URL":    "",
		"AUTH_EMAIL_CAPTURE_BEARER": "",
		"SMTP_HOST":                 "smtp.example.com",
		"SMTP_PORT":                 "465",
		"SMTP_TLS":                  "implicit",
		"SMTP_USERNAME":             "smtp-user",
		"SMTP_PASSWORD":             "smtp-password-value",
	}
}

// applySMTP mutates vars into a valid SMTP mode.
func applySMTP(v map[string]string) {
	for k, val := range smtpAuthEmail() {
		v[k] = val
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

func TestLoad_AuthEmailSESFromName(t *testing.T) {
	t.Parallel()

	vars := validDevEnv()
	for k, v := range sesAuthEmail() {
		vars[k] = v
	}
	vars["SES_FROM_NAME"] = "  Danny from aboutme "
	got, err := config.Load(env(vars))
	if err != nil {
		t.Fatal(err)
	}
	if got.AuthEmail.SESFromName != "Danny from aboutme" {
		t.Errorf("SESFromName = %q, want trimmed display name", got.AuthEmail.SESFromName)
	}
}

func TestLoad_AuthEmailSMTPMode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ port, tls string }{{"465", "implicit"}, {"587", "starttls"}} {
		vars := validDevEnv()
		applySMTP(vars)
		vars["SMTP_PORT"], vars["SMTP_TLS"] = tc.port, tc.tls
		vars["SMTP_HOST"] = "SMTP.Example.com"
		vars["AWS_REGION"] = "ap-southeast-1"
		got, err := config.Load(env(vars))
		if err != nil {
			t.Fatalf("%s/%s: %v", tc.port, tc.tls, err)
		}
		a := got.AuthEmail
		if a.Mode != "smtp" || a.SMTPHost != "smtp.example.com" || fmt.Sprint(a.SMTPPort) != tc.port || a.SMTPTLS != tc.tls {
			t.Errorf("SMTP config = host %q port %d tls %q", a.SMTPHost, a.SMTPPort, a.SMTPTLS)
		}
		if a.SMTPUsername != "smtp-user" || a.SMTPPassword.Reveal() != "smtp-password-value" {
			t.Error("SMTP credentials not loaded")
		}
		if a.SESFrom != "noreply@example.com" || a.SESFromName != "Danny from aboutme.vn" {
			t.Errorf("From = %q %q", a.SESFrom, a.SESFromName)
		}
	}
}

func TestSecretNeverFormatsItsValue(t *testing.T) {
	t.Parallel()

	vars := validDevEnv()
	applySMTP(vars)
	got, err := config.Load(env(vars))
	if err != nil {
		t.Fatal(err)
	}
	var logs strings.Builder
	slog.New(slog.NewTextHandler(&logs, nil)).Info("cfg", "password", got.AuthEmail.SMTPPassword)
	for _, out := range []string{fmt.Sprintf("%v %+v %#v %s", got.AuthEmail, got.AuthEmail, got.AuthEmail, got.AuthEmail.SMTPPassword), logs.String()} {
		if strings.Contains(out, "smtp-password-value") {
			t.Errorf("secret formatted: %q", out)
		}
	}
}

func TestLoad_AuthEmailSMTPErrorsNameVariableNotValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, key, value, wantVar string
	}{
		{"missing host", "SMTP_HOST", "", "SMTP_HOST"},
		{"host with scheme", "SMTP_HOST", "smtps://smtp.example.com", "SMTP_HOST"},
		{"host with port", "SMTP_HOST", "smtp.example.com:465", "SMTP_HOST"},
		{"ip host", "SMTP_HOST", "203.0.113.7", "SMTP_HOST"},
		{"missing port", "SMTP_PORT", "", "SMTP_PORT"},
		{"non-numeric port", "SMTP_PORT", "smtps", "SMTP_PORT"},
		{"port 25", "SMTP_PORT", "25", "SMTP_PORT"},
		{"implicit on 587", "SMTP_PORT", "587", "SMTP_TLS"},
		{"missing tls", "SMTP_TLS", "", "SMTP_TLS"},
		{"plain tls mode", "SMTP_TLS", "none", "SMTP_TLS"},
		{"missing username", "SMTP_USERNAME", "", "SMTP_USERNAME"},
		{"username with control", "SMTP_USERNAME", "user\x00name", "SMTP_USERNAME"},
		{"missing password", "SMTP_PASSWORD", "", "SMTP_PASSWORD"},
		{"password with control", "SMTP_PASSWORD", "pass\x07word", "SMTP_PASSWORD"},
		{"overlong password", "SMTP_PASSWORD", strings.Repeat("p", 257), "SMTP_PASSWORD"},
		{"missing from", "SES_FROM_ADDRESS", "", "SES_FROM_ADDRESS"},
		{"configuration set", "SES_CONFIGURATION_SET", "aboutme-auth", "SES_CONFIGURATION_SET"},
		{"capture url", "AUTH_EMAIL_CAPTURE_URL", "http://127.0.0.1:20091", "AUTH_EMAIL_CAPTURE_URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vars := validDevEnv()
			applySMTP(vars)
			vars[tt.key] = tt.value
			_, err := config.Load(env(vars))
			if err == nil {
				t.Fatal("Load() error = nil, want rejection")
			}
			if !strings.Contains(err.Error(), tt.wantVar) {
				t.Errorf("error %q does not name %s", err, tt.wantVar)
			}
			for _, secret := range []string{"smtp-password-value", "smtp-user", "smtp.example.com"} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("error %q leaks a value", err)
				}
			}
			if tt.value != "" && len(tt.value) > 4 && strings.Contains(err.Error(), tt.value) {
				t.Errorf("error %q echoes the value", err)
			}
		})
	}
}

func TestLoad_AuthEmailSMTPFieldsRejectedInOtherModes(t *testing.T) {
	t.Parallel()

	for _, mode := range []func(map[string]string){applySES, func(v map[string]string) {
		for k, val := range captureAuthEmail() {
			v[k] = val
		}
	}} {
		for _, key := range []string{"SMTP_HOST", "SMTP_PORT", "SMTP_TLS", "SMTP_USERNAME", "SMTP_PASSWORD"} {
			vars := validDevEnv()
			mode(vars)
			vars[key] = "x"
			if _, err := config.Load(env(vars)); err == nil || !strings.Contains(err.Error(), key) {
				t.Errorf("%s set in %s mode: err = %v", key, vars["AUTH_EMAIL_MODE"], err)
			}
		}
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
		{"capture with ses from name", func(v map[string]string) { v["SES_FROM_NAME"] = "Danny" }},
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
		{"ses from name with control character", func(v map[string]string) {
			applySES(v)
			v["SES_FROM_NAME"] = "Danny\x07"
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
