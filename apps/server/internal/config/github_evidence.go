package config

import (
	"fmt"
	"strings"
)

const (
	gitHubEvidenceClientIDName     = "GITHUB_EVIDENCE_CLIENT_ID"
	gitHubEvidenceClientSecretName = "GITHUB_EVIDENCE_CLIENT_SECRET"
)

type GitHubEvidenceConfig struct {
	ClientID     string
	ClientSecret Secret
}

func (c GitHubEvidenceConfig) Enabled() bool {
	return c.ClientID != "" && c.ClientSecret != ""
}

func loadGitHubEvidenceConfig(getenv func(string) string) (GitHubEvidenceConfig, error) {
	clientID := strings.TrimSpace(getenv(gitHubEvidenceClientIDName))
	clientSecret := strings.TrimSpace(getenv(gitHubEvidenceClientSecretName))
	switch {
	case clientID == "" && clientSecret == "":
		return GitHubEvidenceConfig{}, nil
	case clientID == "":
		return GitHubEvidenceConfig{}, missingGitHubEvidenceVariable(gitHubEvidenceClientIDName)
	case clientSecret == "":
		return GitHubEvidenceConfig{}, missingGitHubEvidenceVariable(gitHubEvidenceClientSecretName)
	}
	return GitHubEvidenceConfig{ClientID: clientID, ClientSecret: Secret(clientSecret)}, nil
}

func missingGitHubEvidenceVariable(name string) error {
	return fmt.Errorf("config: %s is required when the other GITHUB_EVIDENCE_ variable is set: "+
		"GitHub evidence needs its client id and client secret together", name)
}
