package githubevidence

import (
	"errors"
	"testing"
)

func TestLink_AcceptedGrammar(t *testing.T) {
	for _, tc := range []struct {
		kind, raw string
		want      evidenceLink
	}{
		{kindRepository, "https://github.com/octocat/hello", evidenceLink{kind: kindRepository, owner: "octocat", name: "hello"}},
		{kindRepository, "https://github.com/octocat/hello/", evidenceLink{kind: kindRepository, owner: "octocat", name: "hello"}},
		{kindRepository, " \thttps://github.com/Octo-Cat/hello.world_2 \n", evidenceLink{kind: kindRepository, owner: "Octo-Cat", name: "hello.world_2"}},
		{kindPullRequest, "https://github.com/octocat/hello/pull/42", evidenceLink{kind: kindPullRequest, owner: "octocat", name: "hello", number: 42}},
		{kindPullRequest, "https://github.com/octocat/hello/pull/42/", evidenceLink{kind: kindPullRequest, owner: "octocat", name: "hello", number: 42}},
		{kindRelease, "https://github.com/octocat/hello/releases/tag/v1.0.0", evidenceLink{kind: kindRelease, owner: "octocat", name: "hello", tag: "v1.0.0"}},
		{kindRelease, "https://github.com/octocat/hello/releases/tag/v1.0.0/", evidenceLink{kind: kindRelease, owner: "octocat", name: "hello", tag: "v1.0.0"}},
		{kindRelease, "https://github.com/octocat/hello/releases/tag/release%2F1.0", evidenceLink{kind: kindRelease, owner: "octocat", name: "hello", tag: "release/1.0"}},
		{kindRelease, "https://github.com/octocat/hello/releases/tag/v1+build", evidenceLink{kind: kindRelease, owner: "octocat", name: "hello", tag: "v1+build"}},
	} {
		got, err := parseEvidenceLink(tc.kind, tc.raw)
		if err != nil || got != tc.want {
			t.Errorf("parseEvidenceLink(%s, %q) = %+v, %v; want %+v", tc.kind, tc.raw, got, err, tc.want)
		}
	}
}

func TestLink_RejectedGrammar(t *testing.T) {
	for _, raw := range []string{
		"",
		"   ",
		"github.com/octocat/hello",
		"http://github.com/octocat/hello",
		"https://www.github.com/octocat/hello",
		"https://gist.github.com/octocat/hello",
		"https://GITHUB.COM/octocat/hello",
		"https://github.com.evil.example/octocat/hello",
		"https://github.com:443/octocat/hello",
		"https://user@github.com/octocat/hello",
		"https://github.com/octocat",
		"https://github.com/octocat/",
		"https://github.com/octocat/hello//",
		"https://github.com/octocat//hello",
		"https://github.com//octocat/hello",
		"https://github.com/octocat/hello?tab=readme",
		"https://github.com/octocat/hello?",
		"https://github.com/octocat/hello#readme",
		"https://github.com/octocat/hello/tree/main",
		"https://github.com/octocat/hello/issues/42",
		"https://github.com/octocat/hello/pull/42/files",
		"https://github.com/octocat/hello/pull/42?diff=split",
		"https://github.com/octocat/hello/pull/42#issuecomment-1",
		"https://github.com/octocat/hello/pull/0",
		"https://github.com/octocat/hello/pull/042",
		"https://github.com/octocat/hello/pull/abc",
		"https://github.com/octocat/hello/pull/",
		"https://github.com/octocat/hello/pull/99999999999999999999",
		"https://github.com/octocat/hello/pulls/42",
		"https://github.com/octocat/hello/releases",
		"https://github.com/octocat/hello/releases/latest",
		"https://github.com/octocat/hello/releases/tag/",
		"https://github.com/octocat/hello/releases/tag/a/b",
		"https://github.com/octocat/hello/releases/tag/%2E%2E",
		"https://github.com/octocat/hello/releases/tag/%zz",
		"https://github.com/octocat/hello/releases/tag/a%20b",
		"https://github.com/-octocat/hello",
		"https://github.com/octo_cat/hello",
		"https://github.com/octo cat/hello",
		"https://github.com/octocat/..",
		"https://github.com/octocat/hel%6Co",
		"https://github.com/octocat/hello\n/pull/1",
	} {
		for _, kind := range []string{kindRepository, kindPullRequest, kindRelease} {
			if got, err := parseEvidenceLink(kind, raw); !errors.Is(err, errUnsupportedLink) {
				t.Errorf("parseEvidenceLink(%s, %q) = %+v, %v; want unsupported", kind, raw, got, err)
			}
		}
	}
}

func TestLink_KindMustMatchTheLink(t *testing.T) {
	for _, tc := range []struct{ kind, raw string }{
		{kindPullRequest, "https://github.com/octocat/hello"},
		{kindRelease, "https://github.com/octocat/hello/pull/42"},
		{kindRepository, "https://github.com/octocat/hello/releases/tag/v1"},
		{"commit", "https://github.com/octocat/hello"},
		{"", "https://github.com/octocat/hello"},
	} {
		if _, err := parseEvidenceLink(tc.kind, tc.raw); !errors.Is(err, errUnsupportedLink) {
			t.Errorf("parseEvidenceLink(%q, %q) error = %v, want unsupported", tc.kind, tc.raw, err)
		}
	}
	if failure := evidenceFailure(errUnsupportedLink); failure.status != 422 || failure.code != "evidence_unsupported_link" {
		t.Fatalf("failure = %+v, want 422 evidence_unsupported_link", failure)
	}
}
