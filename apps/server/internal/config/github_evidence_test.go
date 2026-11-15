package config_test

import (
	"fmt"
	"strings"
	"testing"

	"danny.vn/aboutme/apps/server/internal/config"
)

const (
	evidenceClientIDValue     = "evidence-client-id-sentinel"
	evidenceClientSecretValue = "evidence-client-secret-sentinel"
)

func evidenceVars(envName string, overrides map[string]string) map[string]string {
	vars := map[string]string{
		"DATABASE_URL":                  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":                 "https://aboutme.vn",
		"ENV":                           envName,
		"GITHUB_EVIDENCE_CLIENT_ID":     evidenceClientIDValue,
		"GITHUB_EVIDENCE_CLIENT_SECRET": evidenceClientSecretValue,
	}
	if envName != "dev" {
		vars["LISTEN_HOST"] = "127.0.0.1"
		vars["TRUSTED_PROXY_CIDRS"] = "127.0.0.1/32"
	}
	for k, v := range overrides {
		vars[k] = v
	}
	return vars
}

func TestLoad_GitHubEvidenceOffWhenUnset(t *testing.T) {
	t.Parallel()
	got, err := config.Load(env(validBaseVars()))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.GitHubEvidence.Enabled() || got.GitHubEvidence != (config.GitHubEvidenceConfig{}) {
		t.Fatalf("GitHubEvidence = %+v, want the zero value and off", got.GitHubEvidence)
	}
}

func TestLoad_GitHubEvidenceOnWhenBothSet(t *testing.T) {
	t.Parallel()
	for _, envName := range []string{"dev", "staging", "prod"} {
		t.Run(envName, func(t *testing.T) {
			t.Parallel()
			got, err := config.Load(env(evidenceVars(envName, nil)))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			want := config.GitHubEvidenceConfig{ClientID: evidenceClientIDValue, ClientSecret: config.Secret(evidenceClientSecretValue)}
			if got.GitHubEvidence != want || !got.GitHubEvidence.Enabled() {
				t.Fatalf("GitHubEvidence = %+v, want %+v and on", got.GitHubEvidence, want)
			}
			if got.ProviderLogin.Any() || got.GitHubClientID != "" || got.GitHubClientSecret != "" {
				t.Fatalf("evidence variables changed sign-in: providers=%v id=%q", got.ProviderLogin.Names(), got.GitHubClientID)
			}
			if strings.Contains(fmt.Sprintf("%v %+v %#v", got.GitHubEvidence, got.GitHubEvidence, got.GitHubEvidence), evidenceClientSecretValue) {
				t.Fatal("formatting the evidence configuration revealed the client secret")
			}
		})
	}
}

func TestLoad_GitHubEvidenceExactlyOneSetNamesTheMissingVariableOnly(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ unset, set string }{
		{"GITHUB_EVIDENCE_CLIENT_ID", "GITHUB_EVIDENCE_CLIENT_SECRET"},
		{"GITHUB_EVIDENCE_CLIENT_SECRET", "GITHUB_EVIDENCE_CLIENT_ID"},
	} {
		for _, envName := range []string{"dev", "prod"} {
			t.Run(envName+" without "+tc.unset, func(t *testing.T) {
				t.Parallel()
				_, err := config.Load(env(evidenceVars(envName, map[string]string{tc.unset: "  "})))
				if err == nil {
					t.Fatalf("Load() error = nil with only %s set, want a configuration error", tc.set)
				}
				if !strings.Contains(err.Error(), tc.unset) {
					t.Errorf("error = %q, want it to name %s", err, tc.unset)
				}
				for _, value := range []string{evidenceClientIDValue, evidenceClientSecretValue} {
					if strings.Contains(err.Error(), value) {
						t.Errorf("error = %q reveals a configured value", err)
					}
				}
			})
		}
	}
}

func TestLoad_GitHubEvidenceIsIndependentOfSignIn(t *testing.T) {
	t.Parallel()
	signIn := map[string]string{
		"PROVIDER_LOGIN_ENABLED": "github",
		"GITHUB_CLIENT_ID":       "sign-in-client-id",
		"GITHUB_CLIENT_SECRET":   "sign-in-client-secret",
	}
	without, err := config.Load(env(evidenceVars("prod", map[string]string{
		"PROVIDER_LOGIN_ENABLED":        signIn["PROVIDER_LOGIN_ENABLED"],
		"GITHUB_CLIENT_ID":              signIn["GITHUB_CLIENT_ID"],
		"GITHUB_CLIENT_SECRET":          signIn["GITHUB_CLIENT_SECRET"],
		"GITHUB_EVIDENCE_CLIENT_ID":     "",
		"GITHUB_EVIDENCE_CLIENT_SECRET": "",
	})))
	if err != nil {
		t.Fatalf("Load() without evidence error = %v", err)
	}
	with, err := config.Load(env(evidenceVars("prod", signIn)))
	if err != nil {
		t.Fatalf("Load() with evidence error = %v", err)
	}
	if without.GitHubEvidence.Enabled() || !with.GitHubEvidence.Enabled() {
		t.Fatalf("evidence enabled without=%v with=%v, want false then true", without.GitHubEvidence.Enabled(), with.GitHubEvidence.Enabled())
	}
	if with.ProviderLogin != without.ProviderLogin || with.GitHubClientID != without.GitHubClientID ||
		with.GitHubClientSecret != without.GitHubClientSecret || with.GitHubOAuthTokenURL != without.GitHubOAuthTokenURL {
		t.Fatal("setting the evidence variables changed the sign-in configuration")
	}
	if with.GitHubEvidence.ClientID == with.GitHubClientID {
		t.Fatal("evidence client id was taken from the sign-in variables")
	}

	if _, err := config.Load(env(evidenceVars("prod", map[string]string{"PROVIDER_LOGIN_ENABLED": "github"}))); err == nil ||
		!strings.Contains(err.Error(), "GITHUB_CLIENT_ID") {
		t.Fatalf("Load() with sign-in on and only evidence credentials error = %v, want the sign-in credentials still required", err)
	}
}
