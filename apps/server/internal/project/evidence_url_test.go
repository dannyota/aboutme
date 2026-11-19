package project

import (
	"strings"
	"testing"
)

func TestValidEvidenceURL_FollowsTheStoredGrammar(t *testing.T) {
	prefix := "https://github.com/octocat/hello"
	longest := prefix + "/releases/tag/" + strings.Repeat("t", 300-len(prefix+"/releases/tag/"))
	for _, test := range []struct {
		kind, url string
		valid     bool
	}{
		{EvidenceRepository, prefix, true},
		{EvidenceRepository, "https://github.com/a/b", true},
		{EvidenceRepository, "https://github.com/octocat/h%C3%A9llo", true},
		{EvidenceRepository, prefix + "/", false},
		{EvidenceRepository, prefix + "/pull/1", false},
		{EvidenceRepository, "https://github.com/octocat", false},
		{EvidenceRepository, "https://github.com//hello", false},
		{EvidenceRepository, prefix + "?tab=readme", false},
		{EvidenceRepository, prefix + "#readme", false},
		{EvidenceRepository, "https://github.com/octo cat/hello", false},
		{EvidenceRepository, "https://github.com/octo\tcat/hello", false},
		{EvidenceRepository, "https://github.com/octo\vcat/hello", false},
		{EvidenceRepository, "http://github.com/octocat/hello", false},
		{EvidenceRepository, "https://www.github.com/octocat/hello", false},
		{EvidenceRepository, "https://github.com.evil.example/octocat/hello", false},
		{EvidenceRepository, "HTTPS://github.com/octocat/hello", false},
		{EvidencePullRequest, prefix + "/pull/42", true},
		{EvidencePullRequest, prefix + "/pull/0", false},
		{EvidencePullRequest, prefix + "/pull/042", false},
		{EvidencePullRequest, prefix + "/pull/4a", false},
		{EvidencePullRequest, prefix + "/pull/42/", false},
		{EvidencePullRequest, prefix, false},
		{EvidenceRelease, prefix + "/releases/tag/v1.0%2Bbuild", true},
		{EvidenceRelease, longest, true},
		{EvidenceRelease, longest + "t", false},
		{EvidenceRelease, prefix + "/releases/tag/", false},
		{EvidenceRelease, prefix + "/releases/tag/a/b", false},
		{EvidenceRelease, prefix + "/pull/42", false},
		{"gist", prefix, false},
		{"", prefix, false},
	} {
		if got := ValidEvidenceURL(test.kind, test.url); got != test.valid {
			t.Errorf("ValidEvidenceURL(%q, %q) = %t, want %t", test.kind, test.url, got, test.valid)
		}
	}
}

func TestValidEvidenceURL_CountsCodePoints(t *testing.T) {
	prefix := "https://github.com/octocat/"
	wide := prefix + strings.Repeat("é", 300-len([]rune(prefix)))
	if !ValidEvidenceURL(EvidenceRepository, wide) {
		t.Fatalf("a %d-byte url of 300 code points was refused", len(wide))
	}
	if ValidEvidenceURL(EvidenceRepository, wide+"é") {
		t.Fatal("a url of 301 code points was accepted")
	}
}
