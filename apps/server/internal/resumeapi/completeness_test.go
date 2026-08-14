package resumeapi

import (
	"reflect"
	"testing"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

func TestCompletenessReportsEveryIssueInBytewiseOrder(t *testing.T) {
	t.Parallel()

	blank := "\u2003\n"
	markupBlank := "<p><br></p>"
	doc := schema.Resume{
		PersonalDetails: schema.PersonalDetails{FullName: &blank},
		Content: map[string]schema.Section{
			"profile":     schema.NewProfileSection(nil, nil, []schema.ProfileEntry{{ID: "p", Text: &markupBlank}}),
			"work":        schema.NewWorkSection(nil, nil, []schema.WorkEntry{{ID: "w", JobTitle: &blank, Employer: &blank}}),
			"education":   schema.NewEducationSection(nil, nil, []schema.EducationEntry{{ID: "e", Degree: &blank, School: &blank}}),
			"skill":       schema.NewSkillSection(nil, nil, []schema.SkillEntry{{ID: "s", Name: &blank}}),
			"language":    schema.NewLanguageSection(nil, nil, []schema.LanguageEntry{{ID: "l", Name: &blank}}),
			"certificate": schema.NewCertificateSection(nil, nil, []schema.CertificateEntry{{ID: "c", Title: &blank}}),
			"project":     schema.NewProjectSection(nil, nil, []schema.ProjectEntry{{ID: "p2", Title: &blank}}),
			"custom":      schema.NewCustomSection(nil, nil, []schema.CustomEntry{{ID: "c2", Title: &blank}}),
		},
	}
	prepared := validatePublish(doc, currentPublish{}, publishInput{Live: true})
	got := prepared.Issues
	want := []publishIssue{
		{Path: "content.certificate.entries[0].title", Code: "required", Message: "field is required for publication"},
		{Path: "content.custom.entries[0].title", Code: "required", Message: "field is required for publication"},
		{Path: "content.education.entries[0].degree", Code: "required", Message: "field is required for publication"},
		{Path: "content.education.entries[0].school", Code: "required", Message: "field is required for publication"},
		{Path: "content.language.entries[0].name", Code: "required", Message: "field is required for publication"},
		{Path: "content.profile.entries[0].text", Code: "required", Message: "field is required for publication"},
		{Path: "content.project.entries[0].title", Code: "required", Message: "field is required for publication"},
		{Path: "content.skill.entries[0].name", Code: "required", Message: "field is required for publication"},
		{Path: "content.work.entries[0].employer", Code: "required", Message: "field is required for publication"},
		{Path: "content.work.entries[0].jobTitle", Code: "required", Message: "field is required for publication"},
		{Path: "personalDetails.fullName", Code: "required", Message: "full name is required for publication"},
		{Path: "slug", Code: "required_for_live", Message: "slug is required when live is enabled"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("issues = %#v, want %#v", got, want)
	}
}

func TestCompletenessIgnoresHiddenEntriesAndReportsNoVisibleEntry(t *testing.T) {
	t.Parallel()

	name := "Ada"
	hidden := true
	field := ""
	doc := schema.Resume{
		PersonalDetails: schema.PersonalDetails{FullName: &name},
		Content: map[string]schema.Section{
			"work": schema.NewWorkSection(nil, nil, []schema.WorkEntry{{ID: "work", IsHidden: &hidden, JobTitle: &field, Employer: &field}}),
		},
	}
	slug := "ada-lovelace"
	got := validatePublish(doc, currentPublish{}, publishInput{Slug: optionalSlug{Present: true, Value: slug}, Live: true}).Issues
	want := []publishIssue{{Path: "content", Code: "visible_entry_required", Message: "at least one visible entry is required"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("issues = %#v, want %#v", got, want)
	}
}

func TestCompletenessTreatsSanitizedProfileTextAsTextWithElementBoundaries(t *testing.T) {
	t.Parallel()

	name := "Ada"
	text := `<script>ignored</script><p>  Analytical engine  </p>`
	slug := "ada-lovelace"
	doc := schema.Resume{
		PersonalDetails: schema.PersonalDetails{FullName: &name},
		Content: map[string]schema.Section{
			"profile": schema.NewProfileSection(nil, nil, []schema.ProfileEntry{{ID: "profile", Text: &text}}),
		},
	}
	if issues := validatePublish(doc, currentPublish{}, publishInput{Slug: optionalSlug{Present: true, Value: slug}, Live: true}).Issues; len(issues) != 0 {
		t.Fatalf("sanitized nonblank profile issues = %#v, want none", issues)
	}
}

func TestSortedUniquePublishIssuesOrdersAndDeduplicates(t *testing.T) {
	t.Parallel()

	got := sortedUniquePublishIssues([]publishIssue{
		{Path: "z", Code: "required", Message: "last"},
		{Path: "é", Code: "required", Message: "utf8"},
		{Path: "a", Code: "reserved", Message: "second"},
		{Path: "a", Code: "invalid_format", Message: "first"},
		{Path: "a", Code: "invalid_format", Message: "first"},
	})
	want := []publishIssue{
		{Path: "a", Code: "invalid_format", Message: "first"},
		{Path: "a", Code: "reserved", Message: "second"},
		{Path: "z", Code: "required", Message: "last"},
		{Path: "é", Code: "required", Message: "utf8"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("issues = %#v, want %#v", got, want)
	}
}

func TestPublishSlugGrammarAndByteLengthAreSemantic(t *testing.T) {
	t.Parallel()

	doc := publishCompleteDocument(t)
	for _, test := range []struct {
		name  string
		slug  string
		valid bool
	}{
		{name: "minimum length", slug: "a-bc", valid: true},
		{name: "maximum length", slug: "a23456789012345678901234567890", valid: true},
		{name: "too short", slug: "abc"},
		{name: "too long", slug: "a234567890123456789012345678901"},
		{name: "uppercase", slug: "Ada-lovelace"},
		{name: "leading hyphen", slug: "-ada"},
		{name: "trailing hyphen", slug: "ada-"},
		{name: "adjacent hyphens", slug: "ada--lovelace"},
		{name: "non ascii", slug: "ádalovelace"},
	} {
		t.Run(test.name, func(t *testing.T) {
			issues := validatePublish(doc, currentPublish{}, publishInput{Slug: optionalSlug{Present: true, Value: test.slug}}).Issues
			invalid := false
			for _, issue := range issues {
				invalid = invalid || issue.Code == "invalid_format"
			}
			if invalid == test.valid {
				t.Fatalf("slug %q invalid_format = %v, want %v", test.slug, invalid, !test.valid)
			}
		})
	}
}

func TestSlugPolicyClassifiesEffectiveStateAndReauth(t *testing.T) {
	t.Parallel()

	old := "ada-lovelace"
	current := currentPublish{Slug: &old, Live: true, DownloadEnabled: true, Revision: 9}
	doc := publishCompleteDocument(t)

	unchanged := validatePublish(doc, current, publishInput{Live: true, DownloadEnabled: true})
	if unchanged.ChangedSlug || !reflect.DeepEqual(unchanged.Effective, current) || publishRequiresRecentReauth(current, unchanged) {
		t.Fatalf("unchanged publish = %#v, want preserved state without reauth", unchanged)
	}

	renamed := validatePublish(doc, current, publishInput{Slug: optionalSlug{Present: true, Value: "ada-babbage"}, Live: true, DownloadEnabled: true})
	if !renamed.ChangedSlug || renamed.Effective.Slug == nil || *renamed.Effective.Slug != "ada-babbage" || !publishRequiresRecentReauth(current, renamed) {
		t.Fatalf("rename publish = %#v, want changed slug and reauth", renamed)
	}

	initial := validatePublish(doc, currentPublish{}, publishInput{Slug: optionalSlug{Present: true, Value: old}, Live: true})
	if !initial.ChangedSlug || publishRequiresRecentReauth(currentPublish{}, initial) {
		t.Fatalf("initial claim = %#v, want changed slug without reauth", initial)
	}

	for _, test := range []struct {
		name  string
		input publishInput
		want  publishIssue
	}{
		{name: "discovery requires live", input: publishInput{Slug: optionalSlug{Present: true, Value: old}, SEOGeoEnabled: true}, want: publishIssue{Path: "seoGeoEnabled", Code: "requires_live", Message: "discovery requires live to be enabled"}},
		{name: "format is semantic", input: publishInput{Slug: optionalSlug{Present: true, Value: "Ada"}}, want: publishIssue{Path: "slug", Code: "invalid_format", Message: "slug must be 4 to 30 characters and match ^[a-z0-9]+(-[a-z0-9]+)*$"}},
		{name: "reserved root is semantic", input: publishInput{Slug: optionalSlug{Present: true, Value: "api"}}, want: publishIssue{Path: "slug", Code: "invalid_format", Message: "slug must be 4 to 30 characters and match ^[a-z0-9]+(-[a-z0-9]+)*$"}},
		{name: "reserved valid root is semantic", input: publishInput{Slug: optionalSlug{Present: true, Value: "admin"}}, want: publishIssue{Path: "slug", Code: "reserved", Message: "slug is reserved"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			prepared := validatePublish(doc, current, test.input)
			found := false
			for _, issue := range prepared.Issues {
				if issue == test.want {
					found = true
				}
			}
			if !found {
				t.Fatalf("issues = %#v, missing %#v", prepared.Issues, test.want)
			}
		})
	}
}
