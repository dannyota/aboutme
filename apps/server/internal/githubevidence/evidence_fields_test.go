package githubevidence

import (
	"errors"
	"net/url"
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

func requireNoFormat(t *testing.T, field, text string) {
	t.Helper()
	if i := strings.IndexFunc(text, func(r rune) bool { return unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp) }); i >= 0 {
		t.Fatalf("%s %q holds a control, format or separator character at byte %d", field, text, i)
	}
}

func TestEvidenceFields_FormatCharactersBecomeSpaces(t *testing.T) {
	repo := githubRepository{id: 101, fullName: "octocat/hello"}
	marks := "\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069\u200b\u200c\u200d\u200e\u200f\u2028\u2029\u061c\ufeff\u00ad\u2060"
	pull := pullRequestFields(repo, githubPullRequest{id: 9, number: 42, title: "Fix" + marks + "it"})
	if want := "Fix" + strings.Repeat(" ", len([]rune(marks))) + "it"; pull.Title != want {
		t.Fatalf("pull request title = %q, want each format character as one space", pull.Title)
	}
	requireNoFormat(t, "pull request title", pull.Title)
	for _, only := range []string{"\u202e", "\u2066\u2069", "\u200b\u200f", "\u2028\u2029", "\ufeff\u00ad"} {
		blank := pullRequestFields(repo, githubPullRequest{id: 9, number: 42, title: only})
		if blank.Title != "octocat/hello#42" {
			t.Fatalf("format-only pull request title %q = %q, want the owner/repo#n fallback", only, blank.Title)
		}
		tagOnly, fits := releaseFields(repo, githubRelease{id: 7, tag: "v1.0.0", name: only})
		if !fits || tagOnly.Title != "v1.0.0" {
			t.Fatalf("format-only release name %q = %q (fits %t), want the tag", only, tagOnly.Title, fits)
		}
	}
	release, fits := releaseFields(repo, githubRelease{id: 7, tag: "v1.0.0", name: "First\u202erelease"})
	if !fits || release.Title != "First release" {
		t.Fatalf("release title = %q (fits %t), want the override as a space", release.Title, fits)
	}
}

func TestEvidenceFields_ReleaseTagMustBeAValidTag(t *testing.T) {
	repo := githubRepository{id: 101, fullName: "octocat/hello"}
	for _, tag := range []string{"v1\u202e", "\u200bv1", "v1\u2066", "v1\ufeff", "a b", "a..b", "v1~", "v1.lock", "v1\x00"} {
		if fields, fits := releaseFields(repo, githubRelease{id: 7, tag: tag, name: "First"}); fits {
			t.Errorf("releaseFields(tag %q) = %+v, want refused", tag, fields)
		}
	}
	if _, fits := releaseFields(repo, githubRelease{id: 7, tag: "release/1.0+build", name: "First"}); !fits {
		t.Fatal("a valid tag was refused")
	}
}

func TestValidTag_RefusesFormatCharacters(t *testing.T) {
	for _, tag := range []string{"v1\u202e", "v1\u200b", "v1\u200d", "\u2066v1", "v1\ufeff", "v1\u00ad", "v1\u061c", "v1\u2028"} {
		if validTag(tag) {
			t.Errorf("validTag(%q) = true, want false", tag)
		}
		link := "https://github.com/octocat/hello/releases/tag/" + url.PathEscape(tag)
		if got, err := parseEvidenceLink(kindRelease, link); !errors.Is(err, errUnsupportedLink) {
			t.Errorf("parseEvidenceLink(%q) = %+v, %v; want unsupported", link, got, err)
		}
		if _, valid := tagFromSegment(url.PathEscape(tag)); valid {
			t.Errorf("tagFromSegment(%q) admitted a format character", url.PathEscape(tag))
		}
	}
}
