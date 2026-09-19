package publicformat

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

func TestJSONLDDiscoverableGoldenAndCSP(t *testing.T) {
	// This fails if JSON order, script bytes, sameAs filtering, or CSP hash changes.
	origin, err := publicresume.ParsePublicOrigin("https://aboutme.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	headline := "Build <safe> things\n"
	name := `Ada & \"`
	photo := "https://aboutme.example/api/v1/public/resumes/ada/photo"
	resume := publicresume.PublicResume{Slug: "ada", Lng: "en-US", Document: publicresume.PublicResumeDocument{PersonalDetails: publicresume.PublicPersonalDetails{
		FullName: name, Headline: &headline, Photo: &publicresume.PublicPhoto{URL: photo},
		Details: publicresume.PresentPublicDetails([]publicresume.PublicPersonalDetail{
			{Type: "website", Value: "https://example.test/a"}, {Type: "github", Value: "https://example.test/a"},
			{Type: "linkedin", Value: "http://not-https.test/a"}, {Type: "email", Value: "ada@example.test"},
		}),
	}}}
	want, err := os.ReadFile(filepath.Join("testdata", "public-format", "jsonld-discoverable.golden"))
	if err != nil {
		t.Fatal(err)
	}
	want = bytes.TrimSuffix(want, []byte("\n"))
	got, err := JSONLD(resume, origin, true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.JSON, want) {
		t.Fatalf("JSON = %q, want %q", got.JSON, want)
	}
	if wantScript := append(append([]byte("<script type=\"application/ld+json\">"), want...), []byte("</script>")...); !bytes.Equal(got.Script, wantScript) {
		t.Fatalf("Script = %q, want %q", got.Script, wantScript)
	}
	digest := sha256.Sum256(want)
	wantCSP := strings.Replace(BaseCSP, "script-src 'self';", "script-src 'self' 'sha256-"+base64.StdEncoding.EncodeToString(digest[:])+"';", 1)
	if got.CSP != wantCSP || strings.Count(got.CSP, "'sha256-") != 1 {
		t.Fatalf("CSP = %q", got.CSP)
	}
}

func TestJSONLDNondiscoverableUsesBaseCSP(t *testing.T) {
	// This fails if a nondiscoverable response gets an inline script or hash.
	origin, err := publicresume.ParsePublicOrigin("https://aboutme.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	got, err := JSONLD(publicresume.PublicResume{}, origin, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.JSON != nil || got.Script != nil || got.CSP != BaseCSP {
		t.Fatalf("JSONLD(false) = %#v, want nil JSON/script and base CSP", got)
	}
}

func TestJSONLDSameAsUsesTheRendererExactHTTPSPrefix(t *testing.T) {
	got := jsonLDSameAs(publicresume.PresentPublicDetails([]publicresume.PublicPersonalDetail{
		{Type: "custom", Value: "https://orcid.example/ada"},
		{Type: "custom", Value: "HTTPS://upper.example/ada"},
		{Type: "custom", Value: "javascript:alert(1)"},
		{Type: "custom", Value: "//evil.example"},
		{Type: "custom", Value: " https://space.example"},
		{Type: "email", Value: "https://mail.example"},
		{Type: "github", Value: "HTTPS://github.example/ada"},
		{Type: "website", Value: "https://ada.example"},
	}))
	if len(got) != 2 || got[0] != "https://orcid.example/ada" || got[1] != "https://ada.example" {
		t.Fatalf("sameAs = %v, want only exact lowercase https links", got)
	}
}

// The public HTML validator requires the renderer's JSON-LD to byte-equal
// JSONLD's output, so the TS renderer must pick the same sameAs values. Both
// suites read this corpus (apps/web/test/public-render/render.test.ts).
func TestJSONLDSameAsMatchesTheSharedRendererCorpus(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "public-format", "sameas-parity.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Details []struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"details"`
		SameAs []string `json:"sameAs"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	details := make([]publicresume.PublicPersonalDetail, 0, len(corpus.Details))
	for _, detail := range corpus.Details {
		details = append(details, publicresume.PublicPersonalDetail{Type: detail.Type, Value: detail.Value})
	}
	got := jsonLDSameAs(publicresume.PresentPublicDetails(details))
	if !slices.Equal(got, corpus.SameAs) {
		t.Fatalf("sameAs = %v, want %v", got, corpus.SameAs)
	}
}
