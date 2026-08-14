package resumeapi

import (
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/publicroots"
	"github.com/dannyota/aboutme/apps/server/internal/sanitize"
)

const (
	publishRequiredMessage = "field is required for publication"
	slugFormatMessage      = "slug must be 4 to 30 characters and match ^[a-z0-9]+(-[a-z0-9]+)*$"
)

// validatePublish resolves the command against the current publish state and
// reports every independent, closed publish-policy issue.
func validatePublish(source schema.Resume, current currentPublish, input publishInput) publishPrepared {
	effective := current
	effective.Live = input.Live
	effective.DownloadEnabled = input.DownloadEnabled
	effective.SEOGeoEnabled = input.SEOGeoEnabled
	changedSlug := false
	if input.Slug.Present {
		changedSlug = current.Slug == nil || *current.Slug != input.Slug.Value
		slug := input.Slug.Value
		effective.Slug = &slug
	}

	issues := make([]publishIssue, 0)
	if effective.Live && effective.Slug == nil {
		issues = append(issues, publishIssue{Path: "slug", Code: "required_for_live", Message: "slug is required when live is enabled"})
	}
	if !effective.Live && effective.SEOGeoEnabled {
		issues = append(issues, publishIssue{Path: "seoGeoEnabled", Code: "requires_live", Message: "discovery requires live to be enabled"})
	}
	if effective.Slug != nil {
		if !validPublishSlug(*effective.Slug) {
			issues = append(issues, publishIssue{Path: "slug", Code: "invalid_format", Message: slugFormatMessage})
		}
		if publicroots.Reserved(*effective.Slug) {
			issues = append(issues, publishIssue{Path: "slug", Code: "reserved", Message: "slug is reserved"})
		}
	}
	if effective.Live {
		issues = append(issues, completenessIssues(source)...)
	}

	return publishPrepared{
		Effective:   effective,
		ChangedSlug: changedSlug,
		Issues:      sortedUniquePublishIssues(issues),
	}
}

// publishRequiresRecentReauth reports whether this fresh command releases an
// existing public link. Initial claims and unpublishes preserve no release.
func publishRequiresRecentReauth(current currentPublish, prepared publishPrepared) bool {
	return current.Slug != nil && prepared.ChangedSlug
}

// admitChangedSlugAttempt keeps invalid and unchanged commands out of the
// dedicated slug-attempt budget. A caller maps false after a clean preflight to
// the closed rate_limited response before any claim availability detail.
func admitChangedSlugAttempt(limiter slugAttemptLimiter, accountID uuid.UUID, now time.Time, prepared publishPrepared) bool {
	if len(prepared.Issues) != 0 || !prepared.ChangedSlug {
		return len(prepared.Issues) == 0
	}
	return limiter.AllowChangedSlug(accountID, now)
}

func completenessIssues(source schema.Resume) []publishIssue {
	issues := make([]publishIssue, 0)
	if !nonblankString(source.PersonalDetails.FullName) {
		issues = append(issues, publishIssue{Path: "personalDetails.fullName", Code: "required", Message: "full name is required for publication"})
	}

	visible := false
	for sectionKey, section := range source.Content {
		switch section.SectionType {
		case schema.Profile:
			for index, entry := range section.ProfileEntries {
				if !visibleEntry(entry.IsHidden) {
					continue
				}
				visible = true
				if !nonblankRichText(entry.Text) {
					issues = appendRequiredIssue(issues, sectionKey, index, "text")
				}
			}
		case schema.Work:
			for index, entry := range section.WorkEntries {
				if !visibleEntry(entry.IsHidden) {
					continue
				}
				visible = true
				if !nonblankString(entry.JobTitle) {
					issues = appendRequiredIssue(issues, sectionKey, index, "jobTitle")
				}
				if !nonblankString(entry.Employer) {
					issues = appendRequiredIssue(issues, sectionKey, index, "employer")
				}
			}
		case schema.Education:
			for index, entry := range section.EducationEntries {
				if !visibleEntry(entry.IsHidden) {
					continue
				}
				visible = true
				if !nonblankString(entry.Degree) {
					issues = appendRequiredIssue(issues, sectionKey, index, "degree")
				}
				if !nonblankString(entry.School) {
					issues = appendRequiredIssue(issues, sectionKey, index, "school")
				}
			}
		case schema.Skill:
			for index, entry := range section.SkillEntries {
				if !visibleEntry(entry.IsHidden) {
					continue
				}
				visible = true
				if !nonblankString(entry.Name) {
					issues = appendRequiredIssue(issues, sectionKey, index, "name")
				}
			}
		case schema.Language:
			for index, entry := range section.LanguageEntries {
				if !visibleEntry(entry.IsHidden) {
					continue
				}
				visible = true
				if !nonblankString(entry.Name) {
					issues = appendRequiredIssue(issues, sectionKey, index, "name")
				}
			}
		case schema.Certificate:
			for index, entry := range section.CertificateEntries {
				if !visibleEntry(entry.IsHidden) {
					continue
				}
				visible = true
				if !nonblankString(entry.Title) {
					issues = appendRequiredIssue(issues, sectionKey, index, "title")
				}
			}
		case schema.Project:
			for index, entry := range section.ProjectEntries {
				if !visibleEntry(entry.IsHidden) {
					continue
				}
				visible = true
				if !nonblankString(entry.Title) {
					issues = appendRequiredIssue(issues, sectionKey, index, "title")
				}
			}
		case schema.SectionTypeCustom:
			for index, entry := range section.CustomEntries {
				if !visibleEntry(entry.IsHidden) {
					continue
				}
				visible = true
				if !nonblankString(entry.Title) {
					issues = appendRequiredIssue(issues, sectionKey, index, "title")
				}
			}
		}
	}
	if !visible {
		issues = append(issues, publishIssue{Path: "content", Code: "visible_entry_required", Message: "at least one visible entry is required"})
	}
	return issues
}

func appendRequiredIssue(issues []publishIssue, section string, index int, field string) []publishIssue {
	return append(issues, publishIssue{
		Path:    "content." + section + ".entries[" + strconv.Itoa(index) + "]." + field,
		Code:    "required",
		Message: publishRequiredMessage,
	})
}

func visibleEntry(isHidden *bool) bool {
	return isHidden == nil || !*isHidden
}

func nonblankString(value *string) bool {
	return value != nil && strings.TrimFunc(*value, unicode.IsSpace) != ""
}

func nonblankRichText(value *string) bool {
	if value == nil {
		return false
	}
	context := &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: "div"}
	nodes, err := html.ParseFragment(strings.NewReader(sanitize.RichText(*value)), context)
	if err != nil {
		return false
	}
	var text strings.Builder
	for _, node := range nodes {
		appendHTMLText(&text, node)
	}
	return strings.TrimFunc(text.String(), unicode.IsSpace) != ""
}

func appendHTMLText(text *strings.Builder, node *html.Node) {
	if node.Type == html.TextNode {
		text.WriteString(node.Data)
		return
	}
	if node.Type == html.ElementNode {
		text.WriteByte(' ')
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		appendHTMLText(text, child)
	}
	if node.Type == html.ElementNode {
		text.WriteByte(' ')
	}
}

func validPublishSlug(value string) bool {
	if len(value) < 4 || len(value) > 30 {
		return false
	}
	previousHyphen := false
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			previousHyphen = false
			continue
		}
		if character != '-' || index == 0 || index == len(value)-1 || previousHyphen {
			return false
		}
		previousHyphen = true
	}
	return true
}

func sortedUniquePublishIssues(issues []publishIssue) []publishIssue {
	sorted := append([]publishIssue(nil), issues...)
	sort.Slice(sorted, func(left, right int) bool {
		if sorted[left].Path != sorted[right].Path {
			return sorted[left].Path < sorted[right].Path
		}
		if sorted[left].Code != sorted[right].Code {
			return sorted[left].Code < sorted[right].Code
		}
		return sorted[left].Message < sorted[right].Message
	})
	unique := sorted[:0]
	for _, issue := range sorted {
		if len(unique) != 0 && unique[len(unique)-1] == issue {
			continue
		}
		unique = append(unique, issue)
	}
	return unique
}
