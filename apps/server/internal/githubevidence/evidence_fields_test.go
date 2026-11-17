package githubevidence

import (
	"strings"
	"testing"
	"unicode"
)

func requireNoControl(t *testing.T, field, text string) {
	t.Helper()
	if strings.IndexFunc(text, unicode.IsControl) >= 0 {
		t.Fatalf("%s %q holds a control character", field, text)
	}
}

func TestEvidenceFields_ControlCharactersBecomeSpaces(t *testing.T) {
	repo := githubRepository{id: 101, fullName: "octocat/hello"}
	pull := pullRequestFields(repo, githubPullRequest{id: 9, number: 42, title: "Fix\r\nthe\tthing\x00\x7f\u0085"})
	if pull.Title != "Fix  the thing   " {
		t.Fatalf("pull request title = %q, want each control character as one space", pull.Title)
	}
	requireNoControl(t, "pull request title", pull.Title)
	requireNoControl(t, "repository name", pull.RepositoryName)

	blank := pullRequestFields(repo, githubPullRequest{id: 9, number: 42, title: "\r\n\t"})
	if blank.Title != "octocat/hello#42" {
		t.Fatalf("control-only pull request title = %q, want the owner/repo#n fallback", blank.Title)
	}

	release, fits := releaseFields(repo, githubRelease{id: 7, tag: "v1.0.0", name: "First\rrelease"})
	if !fits || release.Title != "First release" {
		t.Fatalf("release title = %q (fits %t), want the CR as a space", release.Title, fits)
	}
	tagOnly, fits := releaseFields(repo, githubRelease{id: 7, tag: "v1.0.0", name: "\n"})
	if !fits || tagOnly.Title != "v1.0.0" {
		t.Fatalf("control-only release name = %q (fits %t), want the tag", tagOnly.Title, fits)
	}

	long := pullRequestFields(repo, githubPullRequest{id: 9, number: 42, title: strings.Repeat("a\n", 150)})
	if got := []rune(long.Title); len(got) != maxTextCharacters || strings.ContainsRune(long.Title, '\n') {
		t.Fatalf("long title = %d runes %q, want %d with no newline", len(got), long.Title, maxTextCharacters)
	}
}
