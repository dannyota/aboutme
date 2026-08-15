package publicformat

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

func TestMarkdownFullHostileGolden(t *testing.T) {
	// This fails if a formatter drops layout order, rich-text conversion, or a
	// required Markdown escape.
	name := "Ada *Lovelace*"
	headline := "  Build\tthings\ncarefully  "
	label := "Portfolio"
	website := "https://example.test/a b(c)"
	workTitle := "Engineer #1"
	employer := "Analytical <Engines>"
	employerLink := "https://example.test/employer"
	description := "<p>First <strong>bold</strong> and <em>calm</em>.</p><ul><li>One</li><li>Two<ol><li>Nested</li></ol></li></ul>"
	profile := "Plain _profile_"
	sectionName := "Experience & Projects"
	month := int64(3)
	level := int64(5)
	resume := publicresume.PublicResume{
		Slug: "ada-lovelace",
		Lng:  "en",
		Document: publicresume.PublicResumeDocument{
			PersonalDetails: publicresume.PublicPersonalDetails{
				FullName: name,
				Headline: &headline,
				Details: publicresume.PresentPublicDetails([]publicresume.PublicPersonalDetail{
					{ID: "1", Type: "website", Label: &label, Value: website},
					{ID: "2", Type: "email", Value: "ada@example.test"},
				}),
			},
			Content: publicresume.PublicContent{
				"work": {SectionType: "work", DisplayName: &sectionName, WorkEntries: []publicresume.PublicWorkEntry{{
					ID: "w1", JobTitle: &workTitle, Employer: &employer, EmployerLink: &employerLink,
					Dates: &publicresume.PublicDateRange{Start: publicresume.PublicYearMonth{Y: 1843, M: &month}, Present: true}, Description: &description,
				}}},
				"profile": {SectionType: "profile", ProfileEntries: []publicresume.PublicProfileEntry{{ID: "p1", Text: &profile}}},
				"skill":   {SectionType: "skill", SkillEntries: []publicresume.PublicSkillEntry{{ID: "s1", Name: ptr("Go"), Level: &level, InfoHTML: ptr("<p>Fast &amp; safe</p>")}}},
			},
			Customization: schema.Customization{DateFormat: schema.MonYYYY, Layout: schema.Layout{Sections: schema.Sections{Main: []string{"work", "profile"}, Sidebar: []string{"skill"}}}},
		},
	}

	want, err := os.ReadFile(filepath.Join("testdata", "public-format", "markdown-full-hostile.golden"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Markdown(resume)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Markdown() = %q, want %q", got, want)
	}
}

func TestMarkdownRichTextPreservesUnicodeWhitespace(t *testing.T) {
	// This fails if rich-text folding treats NBSP or EM SPACE as ASCII spaces.
	name := "Ada"
	text := "<p>\u00a0\u2003Left\r\n\tRight\u00a0\u2003</p><ul><li>\u00a0\u2003Item\fNext\u00a0</li></ul>"
	resume := publicresume.PublicResume{Document: publicresume.PublicResumeDocument{
		PersonalDetails: publicresume.PublicPersonalDetails{FullName: name},
		Content: publicresume.PublicContent{
			"profile": {SectionType: "profile", ProfileEntries: []publicresume.PublicProfileEntry{{ID: "p1", Text: &text}}},
		},
		Customization: schema.Customization{Layout: schema.Layout{Sections: schema.Sections{Main: []string{"profile"}}}},
	}}

	got, err := Markdown(resume)
	if err != nil {
		t.Fatal(err)
	}
	const want = "# Ada\n\n\u00a0\u2003Left Right\u00a0\u2003\n\n- \u00a0\u2003Item Next\u00a0\n"
	if string(got) != want {
		t.Fatalf("Markdown() = %q, want %q", got, want)
	}
}

func TestMarkdownEveryContactEntryAndDateFormGolden(t *testing.T) {
	// This fails if any public contact, section entry, link variant, or MM/YYYY date line changes.
	month1, month2, month3, month4 := int64(1), int64(2), int64(3), int64(4)
	level := int64(4)
	resume := publicresume.PublicResume{Document: publicresume.PublicResumeDocument{
		PersonalDetails: publicresume.PublicPersonalDetails{FullName: "Full", Details: publicresume.PresentPublicDetails([]publicresume.PublicPersonalDetail{
			{Type: "email", Value: "mail@example.test"}, {Type: "phone", Value: "+1"}, {Type: "location", Value: "Hanoi"},
			{Type: "website", Value: "https://site.test"}, {Type: "linkedin", Value: "https://linkedin.test"}, {Type: "github", Value: "https://github.test"}, {Type: "twitter", Value: "https://twitter.test"}, {Type: "custom", Value: "detail"}, {Type: "email", Label: ptr("Custom label"), Value: "custom@example.test"},
		})},
		Content: publicresume.PublicContent{
			"profile":     {SectionType: "profile", DisplayName: ptr("Profile"), ProfileEntries: []publicresume.PublicProfileEntry{{ID: "1", Text: ptr("Profile body")}}},
			"work":        {SectionType: "work", DisplayName: ptr("Work"), WorkEntries: []publicresume.PublicWorkEntry{{ID: "1", JobTitle: ptr("Work title"), Employer: ptr("Employer"), EmployerLink: ptr("https://employer.test"), Dates: &publicresume.PublicDateRange{Start: publicresume.PublicYearMonth{Y: 2001, M: &month1}, End: &publicresume.PublicYearMonth{Y: 2002, M: &month2}}, City: ptr("City"), Country: ptr("Country"), Description: ptr("Work body")}}},
			"education":   {SectionType: "education", DisplayName: ptr("Education"), EducationEntries: []publicresume.PublicEducationEntry{{ID: "1", Degree: ptr("Degree"), School: ptr("School"), SchoolLink: ptr("https://school.test"), Dates: &publicresume.PublicDateRange{Start: publicresume.PublicYearMonth{Y: 2003, M: &month3}, Present: true}, Description: ptr("Education body")}}},
			"skill":       {SectionType: "skill", DisplayName: ptr("Skills"), SkillEntries: []publicresume.PublicSkillEntry{{ID: "1", Name: ptr("Skill"), Level: &level, InfoHTML: ptr("<p>Skill body</p>")}}},
			"language":    {SectionType: "language", DisplayName: ptr("Languages"), LanguageEntries: []publicresume.PublicLanguageEntry{{ID: "1", Name: ptr("Language"), Level: &level}}},
			"certificate": {SectionType: "certificate", DisplayName: ptr("Certificates"), CertificateEntries: []publicresume.PublicCertificateEntry{{ID: "1", Title: ptr("Certificate"), TitleLink: ptr("https://certificate.test"), Issuer: ptr("Issuer"), Date: &publicresume.PublicYearMonth{Y: 2005, M: &month4}, Description: ptr("Certificate body")}}},
			"project":     {SectionType: "project", DisplayName: ptr("Projects"), ProjectEntries: []publicresume.PublicProjectEntry{{ID: "1", Title: ptr("Project"), Link: ptr("https://project.test"), Dates: &publicresume.PublicDateRange{Start: publicresume.PublicYearMonth{Y: 2006}, End: &publicresume.PublicYearMonth{Y: 2007}}, Description: ptr("Project body")}}},
			"custom":      {SectionType: "custom", DisplayName: ptr("Custom"), CustomEntries: []publicresume.PublicCustomEntry{{ID: "1", Title: ptr("Custom title"), TitleLink: ptr("https://custom.test"), Subtitle: ptr("Subtitle"), Dates: &publicresume.PublicDateRange{Start: publicresume.PublicYearMonth{Y: 2008}}, City: ptr("Custom city"), Description: ptr("Custom body")}}},
		},
		Customization: schema.Customization{DateFormat: schema.MmYyyy, Layout: schema.Layout{Sections: schema.Sections{Main: []string{"profile", "work", "education", "skill", "language", "certificate", "project", "custom"}}}},
	}}
	want, err := os.ReadFile(filepath.Join("testdata", "public-format", "markdown-everything.golden"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Markdown(resume)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Markdown() = %q, want %q", got, want)
	}
}

func TestMarkdownMinimalPhotoAbsenceAndYearDates(t *testing.T) {
	// This fails if absent/empty public leaves, photo projection, or YYYY dates affect Markdown.
	date := publicresume.PublicYearMonth{Y: 2020}
	empty := ""
	base := publicresume.PublicResume{Document: publicresume.PublicResumeDocument{PersonalDetails: publicresume.PublicPersonalDetails{FullName: "Minimal", Headline: &empty, Details: publicresume.PresentPublicDetails([]publicresume.PublicPersonalDetail{{Type: "email", Value: ""}})}, Content: publicresume.PublicContent{"certificate": {SectionType: "certificate", CertificateEntries: []publicresume.PublicCertificateEntry{{ID: "1", Title: ptr("Year only"), Date: &date}}}}, Customization: schema.Customization{DateFormat: schema.Yyyy, Layout: schema.Layout{Sections: schema.Sections{Main: []string{"certificate"}}}}}}
	withoutPhoto, err := Markdown(base)
	if err != nil {
		t.Fatal(err)
	}
	base.Document.PersonalDetails.Photo = &publicresume.PublicPhoto{URL: "https://example.test/photo"}
	withPhoto, err := Markdown(base)
	if err != nil {
		t.Fatal(err)
	}
	const want = "# Minimal\n\n### Year only\n2020\n"
	if string(withoutPhoto) != want || !bytes.Equal(withoutPhoto, withPhoto) {
		t.Fatalf("minimal/photo bytes = %q / %q", withoutPhoto, withPhoto)
	}
}

func TestMarkdownMaxShapedDocumentHashAndInvariants(t *testing.T) {
	// A 512 KiB golden is wasteful; this pins a deterministic max-cardinality public shape by SHA-256 and byte invariants.
	const canonicalDocumentCeiling = 524_288
	const maxProfileTextBytes = 8_102
	const expectedRemainingMargin = 38
	entries := make([]publicresume.PublicProfileEntry, 64)
	for index := range entries {
		entries[index] = publicresume.PublicProfileEntry{ID: fmt.Sprintf("00000000-0000-0000-0000-%012d", index+1), Text: ptr(strings.Repeat("x", maxProfileTextBytes))}
	}
	details := make([]publicresume.PublicPersonalDetail, 16)
	for index := range details {
		details[index] = publicresume.PublicPersonalDetail{ID: fmt.Sprintf("10000000-0000-0000-0000-%012d", index+1), Type: "email", Value: fmt.Sprintf("person%02d@example.test", index+1)}
	}
	resume := publicresume.PublicResume{Document: publicresume.PublicResumeDocument{PersonalDetails: publicresume.PublicPersonalDetails{FullName: strings.Repeat("n", 160), Details: publicresume.PresentPublicDetails(details)}, Content: publicresume.PublicContent{"profile": {SectionType: "profile", ProfileEntries: entries}}, Customization: schema.Customization{Layout: schema.Layout{Sections: schema.Sections{Main: []string{"profile"}}}}}}
	serialized, err := json.Marshal(resume.Document)
	if err != nil {
		t.Fatal(err)
	}
	remaining := canonicalDocumentCeiling - len(serialized)
	if remaining != expectedRemainingMargin {
		t.Fatalf("serialized public document is %d bytes with %d bytes remaining", len(serialized), remaining)
	}
	body, err := Markdown(resume)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	const wantSHA256 = "b6061e7ee5004fe594b65624f8714bcf676d31a58c59cbfbc7ba8c659d768714"
	if string(body[len(body)-1:]) != "\n" || strings.Contains(string(body), "\n\n\n") || strings.Contains(string(body), "<") || strings.Contains(string(body), " \n") || fmt.Sprintf("%x", digest) != wantSHA256 {
		t.Fatalf("max shape invariant or SHA mismatch: %x", digest)
	}
}

func ptr(value string) *string { return &value }
