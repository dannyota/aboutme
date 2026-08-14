package publicformat

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

func TestSitemapBytewiseSortedGolden(t *testing.T) {
	// This fails if discovery exposes an unsorted slug or changes XML bytes.
	origin, err := publicresume.ParsePublicOrigin("https://aboutme.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "public-format", "sitemap-many.xml"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Sitemap(origin, []string{"zeta", "ada", "m"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Sitemap() = %q, want %q", got, want)
	}
}

func TestDiscoveryZeroOneAndRobotsGolden(t *testing.T) {
	// This fails if empty discovery leaks a URL or robots loses its canonical origin.
	origin, err := publicresume.ParsePublicOrigin("https://aboutme.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, fixture string
		got           []byte
	}{
		{"sitemap zero", "sitemap-zero.xml", mustSitemap(t, origin, nil)},
		{"sitemap one", "sitemap-one.xml", mustSitemap(t, origin, []string{"ada"})},
		{"llms zero", "llms-zero.txt", mustLLMS(t, origin, nil)},
		{"llms one", "llms-one.txt", mustLLMS(t, origin, []string{"ada"})},
		{"llms many", "llms-many.txt", mustLLMS(t, origin, []string{"zeta", "ada", "m"})},
		{"robots", "robots.txt", Robots(origin)},
	} {
		want, readErr := os.ReadFile(filepath.Join("testdata", "public-format", test.fixture))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(test.got, want) {
			t.Fatalf("%s = %q, want %q", test.name, test.got, want)
		}
	}
}

func mustSitemap(t *testing.T, origin publicresume.PublicOrigin, slugs []string) []byte {
	t.Helper()
	got, err := Sitemap(origin, slugs)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func mustLLMS(t *testing.T, origin publicresume.PublicOrigin, slugs []string) []byte {
	t.Helper()
	got, err := LLMS(origin, slugs)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
