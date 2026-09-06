package publicresume

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
)

func TestProjectDocumentUsesExplicitPhotoAndDetachesSource(t *testing.T) {
	name, headline, label, rich := "Ada", "Engineer", "Email", `<script>alert(1)</script><strong>safe</strong>`
	month := int64(6)
	source := schema.Resume{
		SchemaVersion: schema.CurrentVersion,
		PersonalDetails: schema.PersonalDetails{
			FullName: &name,
			Headline: &headline,
			Details: []schema.PersonalDetail{
				{ID: "shown", Label: &label, Type: schema.Email, Value: "ada@example.test"},
				{ID: "hidden", IsHidden: true, Type: schema.Phone, Value: "private"},
			},
			Photo: &schema.Photo{Key: "resumes/private/photo.jpg", Crop: &schema.PhotoCrop{X: .1, Y: .2, Width: .3, Height: .4}},
		},
		Content: map[string]schema.Section{
			"shown": schema.NewWorkSection(nil, nil, []schema.WorkEntry{{
				ID: "work", Description: &rich,
				Dates: &schema.DateRange{Start: schema.YearMonth{M: &month, Y: 2020}},
			}}),
			"hidden": schema.NewProfileSection(nil, nil, []schema.ProfileEntry{{ID: "private-entry", IsHidden: boolPtr(true), Text: stringPtr("private text")}}),
		},
		Customization: schema.Customization{Layout: schema.Layout{Sections: schema.Sections{Main: []string{"shown", "hidden"}}}},
	}

	got := ProjectDocument(source, "data:image/jpeg;base64,/9j/2Q==")
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schemaVersion":2,"personalDetails":{"details":[{"id":"shown","label":"Email","type":"email","value":"ada@example.test"}],"fullName":"Ada","headline":"Engineer","photo":{"url":"data:image/jpeg;base64,/9j/2Q==","crop":{"height":0.4,"width":0.3,"x":0.1,"y":0.2}}},"content":{"shown":{"sectionType":"work","entries":[{"dates":{"end":null,"present":false,"start":{"m":6,"y":2020}},"description":"\u003cstrong\u003esafe\u003c/strong\u003e","id":"work"}]}},"customization":{"colors":{"background":"","primary":"","text":""},"dateFormat":"","font":{"baseSizePx":0,"family":""},"heading":{"showRule":false,"style":""},"layout":{"columns":0,"sections":{"main":["shown"],"sidebar":null}},"pageFormat":"","sectionDisplay":{"language":{"style":""},"skill":{"style":""}},"spacing":{"entryGap":0,"lineHeight":0,"sectionGap":0}}}`
	if string(encoded) != want {
		t.Fatalf("ProjectDocument JSON = %s\nwant = %s", encoded, want)
	}

	name, headline, label, rich, month = "Changed", "Changed", "Changed", "<b>changed</b>", 12
	source.PersonalDetails.Details[0].Value = "changed@example.test"
	source.PersonalDetails.Photo.Crop.X = .9
	source.Customization.Layout.Sections.Main[0] = "changed"
	delete(source.Content, "shown")
	after, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != want {
		t.Fatalf("source mutation changed projection: %s", after)
	}
}

func TestProjectDocumentPreservesDetailsPresenceAndRemovesPhotoWithoutURL(t *testing.T) {
	name := "Ada"
	for _, test := range []struct {
		name        string
		details     []schema.PersonalDetail
		wantPresent bool
	}{
		{name: "absent", details: nil, wantPresent: false},
		{name: "present empty", details: []schema.PersonalDetail{}, wantPresent: true},
		{name: "present filtered", details: []schema.PersonalDetail{{ID: "hidden", IsHidden: true}}, wantPresent: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := schema.Resume{SchemaVersion: schema.CurrentVersion, PersonalDetails: schema.PersonalDetails{
				FullName: &name, Details: test.details, Photo: &schema.Photo{Key: "private.jpg"},
			}, Content: map[string]schema.Section{}}
			got := ProjectDocument(source, "")
			if got.PersonalDetails.Details.Present() != test.wantPresent {
				t.Fatalf("Details.Present() = %v, want %v", got.PersonalDetails.Details.Present(), test.wantPresent)
			}
			if got.PersonalDetails.Photo != nil {
				t.Fatalf("Photo = %#v, want nil", got.PersonalDetails.Photo)
			}
		})
	}
}

func TestProjectPublicJSONBytesRemainStable(t *testing.T) {
	origin, err := ParsePublicOrigin("https://resume.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	name, lng, slug := "Ada", "EN-us", "ada"
	projected, err := Project(resume.Resume{
		ID: uuid.MustParse("18e2e099-6f70-4727-8ea9-5d6c331989b9"), Slug: &slug,
		Revision: 7, Lng: &lng, DownloadEnabled: true,
		Doc: schema.Resume{SchemaVersion: schema.CurrentVersion, PersonalDetails: schema.PersonalDetails{FullName: &name}, Content: map[string]schema.Section{}},
	}, origin)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(projected)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"slug":"ada","revision":"7","lng":"en-US","downloadEnabled":true,"document":{"schemaVersion":2,"personalDetails":{"fullName":"Ada"},"content":{},"customization":{"colors":{"background":"","primary":"","text":""},"dateFormat":"","font":{"baseSizePx":0,"family":""},"heading":{"showRule":false,"style":""},"layout":{"columns":0,"sections":{"main":null,"sidebar":null}},"pageFormat":"","sectionDisplay":{"language":{"style":""},"skill":{"style":""}},"spacing":{"entryGap":0,"lineHeight":0,"sectionGap":0}}}}`
	if string(encoded) != want {
		t.Fatalf("Project JSON = %s\nwant = %s", encoded, want)
	}
}

func boolPtr(value bool) *bool       { return &value }
func stringPtr(value string) *string { return &value }
