package printsnapshot

import (
	"bytes"
	"reflect"
	"testing"

	"danny.vn/aboutme/apps/server/internal/publicresume"
)

const (
	linkedSlug       = "ada-lovelace"
	productionOrigin = "https://aboutme.example"
)

func parsedOrigin(t *testing.T, raw, environment string) publicresume.PublicOrigin {
	t.Helper()
	origin, err := publicresume.ParsePublicOrigin(raw, environment)
	if err != nil {
		t.Fatal(err)
	}
	return origin
}

func exactLinks() []publicresume.ProjectLink {
	return []publicresume.ProjectLink{
		{EntryID: "first", ProjectSlug: "atlas", URL: linkedFirstURL},
		{EntryID: "second", ProjectSlug: "beacon", URL: linkedSecondURL},
	}
}

func TestWithProjectLinksAcceptsExactPageAddresses(t *testing.T) {
	plain := linkedOwnerEnvelope(t)
	linked, err := WithProjectLinks(plain, parsedOrigin(t, productionOrigin, "production"), linkedSlug, exactLinks())
	if err != nil {
		t.Fatalf("WithProjectLinks: %v", err)
	}
	want := map[string]string{"first": linkedFirstURL, "second": linkedSecondURL}
	if linked.Version != 2 || !reflect.DeepEqual(linked.ProjectLinks, want) {
		t.Fatalf("version %d, links %v; want 2 and %v", linked.Version, linked.ProjectLinks, want)
	}
	if _, err := Marshal(linked); err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if plain.Version != 1 || plain.ProjectLinks != nil {
		t.Fatalf("WithProjectLinks changed its input: version %d, links %v", plain.Version, plain.ProjectLinks)
	}

	development := parsedOrigin(t, "http://localhost:20080", "development")
	local := []publicresume.ProjectLink{{EntryID: "first", ProjectSlug: "atlas", URL: "http://localhost:20080/ada-lovelace/projects/atlas"}}
	if linked, err := WithProjectLinks(plain, development, linkedSlug, local); err != nil || linked.Version != 2 {
		t.Fatalf("development origin: version %d, %v; want version 2", linked.Version, err)
	}
}

func TestWithProjectLinksWithoutLinksKeepsVersionOne(t *testing.T) {
	plain := linkedOwnerEnvelope(t)
	before, err := Marshal(plain)
	if err != nil {
		t.Fatal(err)
	}
	for name, links := range map[string][]publicresume.ProjectLink{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			same, err := WithProjectLinks(plain, parsedOrigin(t, productionOrigin, "production"), linkedSlug, links)
			if err != nil {
				t.Fatalf("WithProjectLinks: %v", err)
			}
			after, err := Marshal(same)
			if err != nil {
				t.Fatal(err)
			}
			if same.Version != 1 || same.ProjectLinks != nil || !bytes.Equal(after, before) {
				t.Fatalf("version %d, links %v, bytes %s; want the version-1 envelope unchanged", same.Version, same.ProjectLinks, after)
			}
		})
	}
}

func TestWithProjectLinksRefusesAnyOtherAddressOrEntry(t *testing.T) {
	plain := linkedOwnerEnvelope(t)
	origin := parsedOrigin(t, productionOrigin, "production")
	with := func(entryID, projectSlug, url string) []publicresume.ProjectLink {
		return append(exactLinks()[:1], publicresume.ProjectLink{EntryID: entryID, ProjectSlug: projectSlug, URL: url})
	}
	tests := []struct {
		name     string
		envelope Envelope
		origin   publicresume.PublicOrigin
		slug     string
		links    []publicresume.ProjectLink
	}{
		{"http value on an https origin", plain, origin, linkedSlug, with("second", "beacon", "http://aboutme.example/ada-lovelace/projects/beacon")},
		{"another resume slug", plain, origin, linkedSlug, with("second", "beacon", "https://aboutme.example/grace-hopper/projects/beacon")},
		{"another project slug", plain, origin, linkedSlug, with("second", "beacon", linkedFirstURL)},
		{"another host", plain, origin, linkedSlug, with("second", "beacon", "https://evil.example/ada-lovelace/projects/beacon")},
		{"a trailing slash", plain, origin, linkedSlug, with("second", "beacon", linkedSecondURL+"/")},
		{"an invalid project slug", plain, origin, linkedSlug, with("second", "Beacon", "https://aboutme.example/ada-lovelace/projects/Beacon")},
		{"an invalid resume slug", plain, origin, "Ada", []publicresume.ProjectLink{{EntryID: "first", ProjectSlug: "atlas", URL: "https://aboutme.example/Ada/projects/atlas"}}},
		{"a missing origin", plain, publicresume.PublicOrigin{}, linkedSlug, []publicresume.ProjectLink{{EntryID: "first", ProjectSlug: "atlas", URL: "/ada-lovelace/projects/atlas"}}},
		{"a hidden entry", plain, origin, linkedSlug, with("hidden", "beacon", linkedSecondURL)},
		{"an entry outside the layout", plain, origin, linkedSlug, with("unlisted-entry", "beacon", linkedSecondURL)},
		{"a repeated entry", plain, origin, linkedSlug, with("first", "beacon", linkedSecondURL)},
		{"an envelope already at version 2", withLinks(plain, 2, map[string]string{"first": linkedFirstURL}), origin, linkedSlug, exactLinks()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if linked, err := WithProjectLinks(test.envelope, test.origin, test.slug, test.links); err == nil {
				t.Fatalf("WithProjectLinks accepted %s: version %d, links %v", test.name, linked.Version, linked.ProjectLinks)
			}
		})
	}
}
