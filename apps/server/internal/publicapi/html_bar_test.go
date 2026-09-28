package publicapi

import (
	"strings"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/publicformat"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

// publicMarkSVG is the page bar's brand mark, the seal mark alone without the
// hairline inner ring (docs/design/public-page-theme.md, "Page bar",
// "Structure"), with the geometry from
// apps/web/app/components/app/sealMark.ts.
const publicMarkSVG = `<svg aria-hidden="true" class="public-mark" focusable="false" height="20" viewBox="0 0 30 32" width="19" xmlns="http://www.w3.org/2000/svg"><g fill="none" stroke="currentColor" transform="rotate(-8 15 16)"><circle cx="15" cy="16" r="13.4" stroke-width="2.6"/><g stroke-linecap="round" stroke-width="3.2"><circle cx="13.4" cy="17.2" r="3.7"/><path d="M17.1 13.2v7.8"></path></g></g></svg>`

// publicDownloadSVG is the download anchor's icon, Lucide's "download" glyph
// at 16 px (docs/design/public-page-theme.md, "Page bar", "Structure").
const publicDownloadSVG = `<svg aria-hidden="true" fill="none" focusable="false" height="16" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24" width="16" xmlns="http://www.w3.org/2000/svg"><path d="M12 15V3"></path><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path><path d="m7 10 5 5 5-5"></path></svg>`

// publicBar renders the page bar's exact markup for an origin, slug, and
// credit label (docs/design/public-page-theme.md, "Page bar", "Structure"),
// with the download anchor present only while download is on.
func publicBar(origin, slug, creditLabel string, download bool, downloadLabel string) string {
	downloadAnchor := ""
	if download {
		downloadAnchor = `<a class="public-download" href="/api/v1/public/resumes/` + slug + `/pdf">` +
			publicDownloadSVG + `<span>` + downloadLabel + `</span></a>`
	}
	return `<div class="public-toolbar"><div class="public-toolbar-inner"><span class="public-brand">` +
		publicMarkSVG + `<a class="public-credit" href="` + origin + `/">` + creditLabel + `</a></span>` +
		downloadAnchor + `</div></div>`
}

// The public HTML validator accepts the page bar's full markup, mark and
// download anchor included, in both resume languages and with download on
// and off (docs/design/public-page-theme.md, "Baselines and tests").
func TestPublicHTMLAcceptsTheBarMarkup(t *testing.T) {
	origin := mustPublicOrigin(t)
	oldBar := `<div class="public-toolbar"><a class="public-credit" href="https://aboutme.example/">Built with aboutme.vn</a></div>`

	for _, test := range []struct {
		name, lng, creditLabel, downloadLabel string
		download                              bool
	}{
		{"English, download on", "en", "Built with aboutme.vn", "Download PDF", true},
		{"English, download off", "en", "Built with aboutme.vn", "Download PDF", false},
		{"Vietnamese, download on", "vi", "Tạo bằng aboutme.vn", "Tải PDF", true},
		{"Vietnamese, download off", "vi", "Tạo bằng aboutme.vn", "Tải PDF", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			resume := publicresume.PublicResume{
				Slug: "ada", Revision: "1", Lng: test.lng, DownloadEnabled: test.download,
				Document: publicresume.PublicResumeDocument{
					PersonalDetails: publicresume.PublicPersonalDetails{FullName: "Ada"},
				},
			}
			jsonLD, err := publicformat.JSONLD(resume, origin, false)
			if err != nil {
				t.Fatal(err)
			}
			page := validHTMLIn(test.lng, "Ada", "https://aboutme.example/ada", "1", "")
			bar := publicBar("https://aboutme.example", "ada", test.creditLabel, test.download, test.downloadLabel)
			candidate := strings.Replace(page, oldBar, bar, 1)
			if candidate == page {
				t.Fatal("fixture replacement did not apply")
			}
			if rule := publicHTMLRejection([]byte(candidate), resume, origin, jsonLD, false); rule != "" {
				t.Fatalf("bar markup rejected by %q", rule)
			}
		})
	}
}
