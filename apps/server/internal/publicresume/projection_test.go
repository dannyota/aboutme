package publicresume

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
)

func TestProjectionPrivacyAndSanitizer(t *testing.T) {
	origin, err := ParsePublicOrigin("https://resume.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	slug, lng, fullName, hiddenText, shownText := "ada", "en", "Ada", "private", `<script>x</script><strong>safe</strong>`
	hidden := true
	source := resume.Resume{
		ID: uuid.New(), UserID: uuid.New(), Title: "owner title", Slug: &slug, Live: true,
		DownloadEnabled: true, Revision: 7, Lng: &lng,
		Doc: schema.Resume{SchemaVersion: schema.CurrentVersion, PersonalDetails: schema.PersonalDetails{
			FullName: &fullName,
			Details:  []schema.PersonalDetail{{ID: "shown", Type: schema.Email, Value: "ada@example.test"}, {ID: "hidden", IsHidden: true, Type: schema.Phone, Value: "555"}},
			Photo:    &schema.Photo{Key: "private/key.jpg"},
		}, Content: map[string]schema.Section{
			"hidden": schema.NewProfileSection(nil, nil, []schema.ProfileEntry{{ID: "h", IsHidden: &hidden, Text: &hiddenText}}),
			"shown":  schema.NewProfileSection(nil, nil, []schema.ProfileEntry{{ID: "s", Text: &shownText}}),
		}, Customization: schema.Customization{Layout: schema.Layout{Sections: schema.Sections{Main: []string{"hidden", "shown"}}}},
		},
	}
	got, err := Project(source, origin)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{source.ID.String(), source.UserID.String(), "owner title", "private/key.jpg", "isHidden", "<script"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("public JSON leaked %q: %s", forbidden, text)
		}
	}
	if _, ok := got.Document.Content["hidden"]; ok {
		t.Fatal("hidden-only section remained public")
	}
	if len(got.Document.Customization.Layout.Sections.Main) != 1 || got.Document.Customization.Layout.Sections.Main[0] != "shown" {
		t.Fatalf("layout was not pruned: %#v", got.Document.Customization.Layout.Sections)
	}
	if got.Document.PersonalDetails.Photo == nil || got.Document.PersonalDetails.Photo.URL != "https://resume.example/api/v1/public/resumes/ada/photo" {
		t.Fatalf("photo = %#v", got.Document.PersonalDetails.Photo)
	}
	if !strings.Contains(text, `\u003cstrong\u003esafe\u003c/strong\u003e`) {
		t.Fatalf("sanitized rich text absent: %s", text)
	}
}

func TestProjectionDetailsPresence(t *testing.T) {
	origin, err := ParsePublicOrigin("https://resume.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	slug, lng, fullName := "ada", "en", "Ada"
	base := resume.Resume{Slug: &slug, Revision: 1, Lng: &lng, Doc: schema.Resume{SchemaVersion: schema.CurrentVersion, PersonalDetails: schema.PersonalDetails{FullName: &fullName}, Content: map[string]schema.Section{}}}
	for _, test := range []struct {
		name        string
		details     []schema.PersonalDetail
		wantPresent bool
	}{
		{"absent", nil, false},
		{"empty", []schema.PersonalDetail{}, true},
		{"filtered", []schema.PersonalDetail{{ID: "x", IsHidden: true, Type: schema.Email, Value: "x@example.test"}}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := base
			source.Doc.PersonalDetails.Details = test.details
			got, err := Project(source, origin)
			if err != nil {
				t.Fatal(err)
			}
			if got.Document.PersonalDetails.Details.Present() != test.wantPresent {
				t.Fatalf("Present() = %v", got.Document.PersonalDetails.Details.Present())
			}
			if test.wantPresent && len(got.Document.PersonalDetails.Details.Value()) != 0 {
				t.Fatalf("details = %#v", got.Document.PersonalDetails.Details.Value())
			}
		})
	}
}

func TestProjectionLanguageIsCanonicalAndTotal(t *testing.T) {
	origin, err := ParsePublicOrigin("https://resume.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	slug, fullName := "ada", "Ada"
	for _, test := range []struct {
		name string
		lng  *string
		want string
	}{
		{"nil", nil, "und"},
		{"empty", ptr(""), "und"},
		{"invalid", ptr("not a language"), "und"},
		{"overlong", ptr(strings.Repeat("a", 36)), "und"},
		{"canonical", ptr("EN-us"), "en-US"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := resume.Resume{Slug: &slug, Revision: 1, Lng: test.lng, Doc: schema.Resume{SchemaVersion: schema.CurrentVersion, PersonalDetails: schema.PersonalDetails{FullName: &fullName}, Content: map[string]schema.Section{}}}
			got, err := Project(source, origin)
			if err != nil {
				t.Fatal(err)
			}
			if got.Lng != test.want {
				t.Fatalf("Lng = %q, want %q", got.Lng, test.want)
			}
		})
	}
}

func TestProjectionEveryPublicLeafAndRichText(t *testing.T) {
	origin, err := ParsePublicOrigin("https://resume.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	slug, lng, fullName, visible, empty := "ada", "en", "Ada", false, ""
	level, month, year := int64(4), int64(2), int64(2024)
	dates := &schema.DateRange{Start: schema.YearMonth{Y: 2020}, End: &schema.YearMonth{M: &month, Y: year}}
	rich := `<script>bad</script><strong>safe</strong>`
	source := resume.Resume{Slug: &slug, Revision: 9, Lng: &lng, Doc: schema.Resume{SchemaVersion: schema.CurrentVersion,
		PersonalDetails: schema.PersonalDetails{FullName: &fullName, Details: []schema.PersonalDetail{
			{ID: "kept", Label: ptr("Email"), Type: schema.Email, Value: "ada@example.test"},
			{ID: "hidden", IsHidden: true, Type: schema.Phone, Value: "555"},
			{ID: "empty", Type: schema.Location, Value: empty},
		}, Photo: &schema.Photo{Key: "private/photo.png", Crop: &schema.PhotoCrop{X: .1, Y: .2, Width: .3, Height: .4}}},
		Content: map[string]schema.Section{
			"profile":     schema.NewProfileSection(ptr("Profile"), ptr("user"), []schema.ProfileEntry{{ID: "p", IsHidden: &visible, Text: &rich}}),
			"work":        schema.NewWorkSection(ptr("Work"), nil, []schema.WorkEntry{{ID: "w", IsHidden: &visible, City: ptr("London"), Country: ptr("UK"), Dates: dates, Description: &rich, Employer: ptr("ACME"), EmployerLink: ptr("https://acme.test"), JobTitle: ptr("Engineer")}}),
			"education":   schema.NewEducationSection(ptr("Education"), nil, []schema.EducationEntry{{ID: "e", IsHidden: &visible, City: ptr("Paris"), Country: ptr("FR"), Dates: dates, Degree: ptr("BSc"), Description: &rich, School: ptr("School"), SchoolLink: ptr("https://school.test")}}),
			"skill":       schema.NewSkillSection(ptr("Skills"), nil, []schema.SkillEntry{{ID: "s", IsHidden: &visible, InfoHTML: &rich, Level: &level, Name: ptr("Go")}}),
			"language":    schema.NewLanguageSection(ptr("Languages"), nil, []schema.LanguageEntry{{ID: "l", IsHidden: &visible, Level: &level, Name: ptr("English")}}),
			"certificate": schema.NewCertificateSection(ptr("Certificates"), nil, []schema.CertificateEntry{{ID: "c", IsHidden: &visible, Date: &schema.YearMonth{Y: year}, Description: &rich, Issuer: ptr("Issuer"), Title: ptr("Cert"), TitleLink: ptr("https://cert.test")}}),
			"project":     schema.NewProjectSection(ptr("Projects"), nil, []schema.ProjectEntry{{ID: "r", IsHidden: &visible, Dates: dates, Description: &rich, Link: ptr("https://project.test"), Title: ptr("Project")}}),
			"custom":      schema.NewCustomSection(ptr("Custom"), nil, []schema.CustomEntry{{ID: "u", IsHidden: &visible, City: ptr("Rome"), Dates: dates, Description: &rich, Subtitle: ptr("Sub"), Title: ptr("Title"), TitleLink: ptr("https://custom.test")}}),
		}, Customization: schema.Customization{Layout: schema.Layout{Sections: schema.Sections{Main: []string{"profile", "work", "education", "skill"}, Sidebar: []string{"language", "certificate", "project", "custom", "missing"}}}},
	}}
	got, err := Project(source, origin)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if len(got.Document.Content) != 8 || len(got.Document.PersonalDetails.Details.Value()) != 1 {
		t.Fatalf("projection pruned visible fields: %#v", got)
	}
	if strings.Contains(text, "private/photo.png") || strings.Contains(text, "isHidden") || strings.Contains(text, "<script") {
		t.Fatalf("private or hostile bytes remained: %s", text)
	}
	for _, required := range []string{"profile", "work", "education", "skill", "language", "certificate", "project", "custom", "ada@example.test", "https://resume.example/api/v1/public/resumes/ada/photo", `\u003cstrong\u003esafe\u003c/strong\u003e`} {
		if !strings.Contains(text, required) {
			t.Fatalf("missing public leaf %q in %s", required, text)
		}
	}
	if got.Document.Customization.Layout.Sections.Sidebar[len(got.Document.Customization.Layout.Sections.Sidebar)-1] == "missing" {
		t.Fatalf("layout retained missing section: %#v", got.Document.Customization.Layout.Sections)
	}
}

func ptr(value string) *string { return &value }
