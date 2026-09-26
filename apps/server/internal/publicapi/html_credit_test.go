package publicapi

import (
	"strings"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/publicformat"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

// Every public page carries exactly one "Built with aboutme.vn" credit in its
// chrome, outside the resume article, linking the canonical home page.
func TestPublicHTMLRequiresExactlyOneCreditLink(t *testing.T) {
	origin := mustPublicOrigin(t)
	resume := publicresume.PublicResume{Slug: "ada", Revision: "1",
		Document: publicresume.PublicResumeDocument{PersonalDetails: publicresume.PublicPersonalDetails{FullName: "Ada"}}}
	jsonLD, err := publicformat.JSONLD(resume, origin, false)
	if err != nil {
		t.Fatal(err)
	}
	valid := validHTML("Ada", "https://aboutme.example/ada", "1", "")
	credit := `<a class="public-credit" href="https://aboutme.example/">Built with aboutme.vn</a>`
	if !strings.Contains(valid, credit) {
		t.Fatal("fixture lacks the credit link")
	}
	if rule := publicHTMLRejection([]byte(valid), resume, origin, jsonLD, false); rule != "" {
		t.Fatalf("valid credit rejected by %q", rule)
	}

	for _, test := range []struct {
		name, replacement string
	}{
		{"missing", ``},
		{"twice", credit + credit},
		{"Vietnamese text on an English resume", `<a class="public-credit" href="https://aboutme.example/">Tạo bằng aboutme.vn</a>`},
		{"other text", `<a class="public-credit" href="https://aboutme.example/">aboutme.vn</a>`},
		{"nested markup", `<a class="public-credit" href="https://aboutme.example/"><b>Built with aboutme.vn</b></a>`},
		{"no trailing slash", `<a class="public-credit" href="https://aboutme.example">Built with aboutme.vn</a>`},
		{"ref parameter", `<a class="public-credit" href="https://aboutme.example/?ref=ada">Built with aboutme.vn</a>`},
		{"other host", `<a class="public-credit" href="https://evil.example/">Built with aboutme.vn</a>`},
		{"nofollow", `<a class="public-credit" href="https://aboutme.example/" rel="nofollow">Built with aboutme.vn</a>`},
		{"new window", `<a class="public-credit" href="https://aboutme.example/" target="_blank">Built with aboutme.vn</a>`},
		{"extra class", `<a class="public-credit x" href="https://aboutme.example/">Built with aboutme.vn</a>`},
		{"inside the resume article", `<article class="resume-document">` + credit + `</article>`},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := strings.Replace(valid, credit, test.replacement, 1)
			if rule := publicHTMLRejection([]byte(candidate), resume, origin, jsonLD, false); rule != "credit_link" {
				t.Fatalf("rule = %q, want credit_link", rule)
			}
		})
	}

	for _, lng := range []string{"vi", "vi-VN", "VI"} {
		t.Run("Vietnamese "+lng, func(t *testing.T) {
			vietnamese := resume
			vietnamese.Lng = lng
			english := validHTMLIn(lng, "Ada", "https://aboutme.example/ada", "1", "")
			candidate := strings.Replace(english, "Built with aboutme.vn", "Tạo bằng aboutme.vn", 1)
			if rule := publicHTMLRejection([]byte(candidate), vietnamese, origin, jsonLD, false); rule != "" {
				t.Fatalf("Vietnamese credit rejected by %q", rule)
			}
			if rule := publicHTMLRejection([]byte(english), vietnamese, origin, jsonLD, false); rule != "credit_link" {
				t.Fatalf("English credit on a Vietnamese resume: rule = %q, want credit_link", rule)
			}
		})
	}
	for _, lng := range []string{"en", "en-US", "fr", "und"} {
		other := resume
		other.Lng = lng
		page := validHTMLIn(lng, "Ada", "https://aboutme.example/ada", "1", "")
		if rule := publicHTMLRejection([]byte(page), other, origin, jsonLD, false); rule != "" {
			t.Fatalf("lng %q: English credit rejected by %q", lng, rule)
		}
	}
}

// A resume's own link to the home page is content, not the credit, and stays
// allowed like any https link.
func TestPublicHTMLKeepsOtherHomeLinksAsContent(t *testing.T) {
	origin := mustPublicOrigin(t)
	resume := publicresume.PublicResume{Slug: "ada", Revision: "1",
		Document: publicresume.PublicResumeDocument{PersonalDetails: publicresume.PublicPersonalDetails{FullName: "Ada"}}}
	jsonLD, err := publicformat.JSONLD(resume, origin, false)
	if err != nil {
		t.Fatal(err)
	}
	valid := validHTML("Ada", "https://aboutme.example/ada", "1", "")
	content := `<article class="resume-document"><a href="https://aboutme.example/" rel="noopener noreferrer">aboutme</a></article>`
	candidate := strings.Replace(valid, "body</main>", content+"</main>", 1)
	if rule := publicHTMLRejection([]byte(candidate), resume, origin, jsonLD, false); rule != "" {
		t.Fatalf("content home link rejected by %q", rule)
	}
}
