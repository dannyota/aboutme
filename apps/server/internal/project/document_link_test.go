package project

import (
	"strings"
	"testing"
)

// A library link is copied into a resume entry link, which requires a
// lowercase scheme and at most 2048 characters, so a save lowercases the
// scheme, keeps every other byte, and refuses a longer link. Lowercasing a
// host could change where an internationalised link points.
func TestPrepare_NormalizesAndBoundsLinks(t *testing.T) {
	d := draft()
	d.RepositoryURL = "HTTPS://Example.COM/Atlas/Path?Query=Kept#Frag"
	d.DemoURL = "Https://user:Pass@Demo.Example:8443/Run"
	prepared, err := Prepare(d)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if prepared.RepositoryURL != "https://Example.COM/Atlas/Path?Query=Kept#Frag" {
		t.Fatalf("repositoryUrl = %q", prepared.RepositoryURL)
	}
	if prepared.DemoURL != "https://user:Pass@Demo.Example:8443/Run" {
		t.Fatalf("demoUrl = %q", prepared.DemoURL)
	}
	for given, want := range map[string]string{
		"https://İ.example/Atlas":      "https://İ.example/Atlas",
		"HTTPS://Example.COM/Path?Q=A": "https://Example.COM/Path?Q=A",
	} {
		d := draft()
		d.RepositoryURL = given
		prepared, err := Prepare(d)
		if err != nil || prepared.RepositoryURL != want {
			t.Fatalf("repositoryUrl %q stored as %q (%v), want %q", given, prepared.RepositoryURL, err, want)
		}
	}
	base := "https://example.com/"
	for _, n := range []int{2048, 2049} {
		for _, field := range []string{"repositoryUrl", "demoUrl"} {
			d := draft()
			link := base + strings.Repeat("a", n-len(base))
			if field == "repositoryUrl" {
				d.RepositoryURL = link
			} else {
				d.DemoURL = link
			}
			if _, err := Prepare(d); (err != nil) != (n > 2048) {
				t.Fatalf("%s characters=%d error=%v", field, n, err)
			}
		}
	}
	for _, bad := range []string{"http://example.com", "HTTPS:example.com", "https://", "ftp://Example.com"} {
		d := draft()
		d.RepositoryURL = bad
		if _, err := Prepare(d); err == nil {
			t.Fatalf("repositoryUrl %q accepted", bad)
		}
	}
}
