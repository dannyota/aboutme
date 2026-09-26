package previewmeta

import (
	"strings"
	"testing"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

// Sentinels sit only in fields the public snapshot must drop or the preview
// must skip: hidden entries, hidden contacts, and a profile section outside
// the layout (docs/design/link-previews.md, "Tests").
var sentinels = []string{"SENTINEL-HIDDEN-CONTACT", "SENTINEL-HIDDEN-PROFILE", "SENTINEL-HIDDEN-WORK", "SENTINEL-OUTSIDE-LAYOUT"}

type fixture struct {
	lng      string
	name     *string
	headline *string
	details  []schema.PersonalDetail
	summary  []schema.ProfileEntry
	work     []schema.WorkEntry
	sidebar  bool
}

func text(value string) *string { return &value }

func hiddenFlag() *bool {
	hidden := true
	return &hidden
}

// project builds the public snapshot the way a live page does, so hidden
// entries and contacts are dropped by the same projection.
func project(t *testing.T, f fixture) publicresume.PublicResume {
	t.Helper()
	details := append([]schema.PersonalDetail{{ID: "hidden", IsHidden: true, Type: schema.TypeCustom, Value: "SENTINEL-HIDDEN-CONTACT"}}, f.details...)
	document := schema.Resume{
		SchemaVersion:   schema.CurrentVersion,
		PersonalDetails: schema.PersonalDetails{FullName: f.name, Headline: f.headline, Details: details},
		Content: map[string]schema.Section{
			"outside": schema.NewProfileSection(nil, nil, []schema.ProfileEntry{{ID: "o", Text: text("<p>SENTINEL-OUTSIDE-LAYOUT</p>")}}),
		},
	}
	var layout []string
	if f.work != nil {
		entries := append([]schema.WorkEntry{{ID: "hidden-work", IsHidden: hiddenFlag(), JobTitle: text("SENTINEL-HIDDEN-WORK")}}, f.work...)
		document.Content["work"] = schema.NewWorkSection(nil, nil, entries)
		layout = append(layout, "work")
	}
	if f.summary != nil {
		entries := append([]schema.ProfileEntry{{ID: "hidden-profile", IsHidden: hiddenFlag(), Text: text("<p>SENTINEL-HIDDEN-PROFILE</p>")}}, f.summary...)
		document.Content["summary"] = schema.NewProfileSection(nil, nil, entries)
		layout = append(layout, "summary")
	}
	if f.sidebar {
		document.Customization.Layout.Sections.Sidebar = layout
	} else {
		document.Customization.Layout.Sections.Main = layout
	}
	return publicresume.PublicResume{Slug: "ada", Revision: "1", Lng: f.lng, Document: publicresume.ProjectDocument(document, "")}
}

func TestForDerivesEveryValue(t *testing.T) {
	for _, test := range []struct {
		name        string
		fixture     fixture
		publicTitle *string
		want        Meta
	}{
		{
			name: "Vietnamese summary with contacts scrubbed",
			fixture: fixture{
				lng: "vi", name: text("Nguyễn Thị Minh Anh"), headline: text("Kỹ sư phần mềm"),
				details: []schema.PersonalDetail{
					{ID: "e", Type: schema.Email, Value: "minhanh@example.com"},
					{ID: "p", Type: schema.Phone, Value: "0912 345 678"},
					{ID: "l", Type: schema.Location, Value: "Hà Nội"},
				},
				summary: []schema.ProfileEntry{{ID: "s", Text: text("<p>Kỹ sư ở Hà Nội.</p><p>Liên hệ minhanh@example.com hoặc 0912&nbsp;345&nbsp;678.</p>")}},
				work:    []schema.WorkEntry{{ID: "w", JobTitle: text("Kỹ sư"), Employer: text("Công ty ABC")}},
			},
			want: Meta{Title: "Nguyễn Thị Minh Anh", Description: "Kỹ sư ở . Liên hệ hoặc .", Locale: "vi_VN", ImageAlt: "Nguyễn Thị Minh Anh · Kỹ sư phần mềm"},
		},
		{
			name: "English summary in the sidebar",
			fixture: fixture{
				lng: "en", name: text("Ada Lovelace"), headline: text("Analyst"), sidebar: true,
				summary: []schema.ProfileEntry{{ID: "s", Text: text("<p>I write <strong>programs</strong> for engines.</p>")}, {ID: "t", Text: text("<ul><li>Maths</li></ul>")}},
			},
			want: Meta{Title: "Ada Lovelace", Description: "I write programs for engines. Maths", Locale: "en_US", ImageAlt: "Ada Lovelace · Analyst"},
		},
		{
			name: "headline and latest role",
			fixture: fixture{
				lng: "en-GB", name: text("Ada Lovelace"), headline: text("Data engineer"),
				work: []schema.WorkEntry{{ID: "w1", JobTitle: text("Analyst"), Employer: text("Acme")}, {ID: "w2", JobTitle: text("Clerk")}},
			},
			want: Meta{Title: "Ada Lovelace", Description: "Data engineer · Analyst, Acme", Locale: "en_GB", ImageAlt: "Ada Lovelace · Data engineer"},
		},
		{
			name: "summary empty after scrubbing falls back to the role",
			fixture: fixture{
				lng: "fr", name: text("Ada"),
				summary: []schema.ProfileEntry{{ID: "s", Text: text("<p>ada@example.com</p>")}},
				work:    []schema.WorkEntry{{ID: "w", Employer: text("Acme")}},
			},
			want: Meta{Title: "Ada", Description: "Acme", Locale: "", ImageAlt: "Ada"},
		},
		{
			name:    "Vietnamese fixed line",
			fixture: fixture{lng: "vi-VN", name: text("An")},
			want:    Meta{Title: "An", Description: "CV trên aboutme.vn", Locale: "vi_VN", ImageAlt: "An"},
		},
		{
			name:    "fixed line for any other language",
			fixture: fixture{lng: "und", name: text("An")},
			want:    Meta{Title: "An", Description: "Resume on aboutme.vn", Locale: "", ImageAlt: "An"},
		},
		{
			name:        "public title as written",
			fixture:     fixture{lng: "en", name: text("Ada")},
			publicTitle: text("Ada — call 0912 345 678"),
			want:        Meta{Title: "Ada — call 0912 345 678", Description: "Resume on aboutme.vn", Locale: "en_US", ImageAlt: "Ada"},
		},
		{
			name:        "empty public title uses the name",
			fixture:     fixture{lng: "en", name: text("Ada")},
			publicTitle: text(""),
			want:        Meta{Title: "Ada", Description: "Resume on aboutme.vn", Locale: "en_US", ImageAlt: "Ada"},
		},
		{
			name:    "name that loses a token",
			fixture: fixture{lng: "en", name: text("Ada ada@example.com"), headline: text("Analyst")},
			want:    Meta{Title: "Ada", Description: "Analyst", Locale: "en_US", ImageAlt: "aboutme.vn/ada"},
		},
		{
			name:    "headline that loses a token",
			fixture: fixture{lng: "en", name: text("Ada"), headline: text("Call 0912 345 678")},
			want:    Meta{Title: "Ada", Description: "Call", Locale: "en_US", ImageAlt: "Ada"},
		},
		{
			name:    "no name",
			fixture: fixture{lng: "en"},
			want:    Meta{Title: "aboutme.vn/ada", Description: "Resume on aboutme.vn", Locale: "en_US", ImageAlt: "aboutme.vn/ada"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := For(project(t, test.fixture), test.publicTitle)
			if got != test.want {
				t.Fatalf("For() = %#v, want %#v", got, test.want)
			}
			assertNoSentinel(t, got)
		})
	}
}

func TestTitleAndImageTextCuts(t *testing.T) {
	name := strings.Repeat("Ă", 100)
	headline := strings.Repeat("b", 150)
	got := For(project(t, fixture{lng: "vi", name: &name, headline: &headline}), nil)
	if want := strings.Repeat("Ă", MaxTitleGraphemes); got.Title != want {
		t.Fatalf("Title = %q, want %q", got.Title, want)
	}
	if want := name + " · " + strings.Repeat("b", MaxImageTextGraphemes-100-3); got.ImageAlt != want {
		t.Fatalf("ImageAlt = %q, want %q", got.ImageAlt, want)
	}
	if want := strings.Repeat("b", 150); got.Description != want {
		t.Fatalf("Description = %q, want %q", got.Description, want)
	}
}

func TestLongSummaryIsCut(t *testing.T) {
	sentence := "Tôi xây dựng hệ thống thanh toán cho ngân hàng và ví điện tử."
	summary := "<p>" + strings.Repeat(sentence+" ", 5) + "</p>"
	got := For(project(t, fixture{lng: "vi", name: text("An"), summary: []schema.ProfileEntry{{ID: "s", Text: &summary}}}), nil)
	if want := sentence + " " + sentence; got.Description != want {
		t.Fatalf("Description = %q, want %q", got.Description, want)
	}
}

func assertNoSentinel(t *testing.T, meta Meta) {
	t.Helper()
	for _, value := range []string{meta.Title, meta.Description, meta.Locale, meta.ImageAlt} {
		for _, sentinel := range sentinels {
			if strings.Contains(value, sentinel) {
				t.Fatalf("preview value %q leaks %s", value, sentinel)
			}
		}
	}
}
