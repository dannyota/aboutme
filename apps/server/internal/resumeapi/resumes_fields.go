package resumeapi

// Resume document field decoding and validation: title, language, and the
// default document seeded for a new resume.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"unicode/utf8"

	"golang.org/x/text/language"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
)

func decodeResumeTitle(raw json.RawMessage, required bool) (string, error) {
	if len(raw) == 0 {
		if required {
			return "", documentInvalid("title", "title is required")
		}
		return "", nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", documentInvalid("title", "title must be a string")
	}
	var title string
	if err := json.Unmarshal(raw, &title); err != nil {
		return "", documentInvalid("title", "title must be a string")
	}
	if utf8.RuneCountInString(title) > resume.MaxTitleCharacters {
		return "", documentInvalid("title", "title exceeds 160 code points")
	}
	return title, nil
}

func decodeResumeLanguage(raw json.RawMessage) (*string, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, documentInvalid("lng", "language must be a string or null")
	}
	if value == "" {
		return nil, nil
	}
	tag, err := language.Parse(value)
	if err != nil {
		return nil, documentInvalid("lng", "language must be a valid BCP 47 tag")
	}
	canonical := tag.String()
	if utf8.RuneCountInString(canonical) > resume.MaxLngCharacters {
		return nil, documentInvalid("lng", "canonical language tag exceeds 35 code points")
	}
	return &canonical, nil
}

func projectResumeLanguage(value *string) string {
	if value == nil || *value == "" {
		return language.Und.String()
	}
	tag, err := language.Parse(*value)
	if err != nil {
		return language.Und.String()
	}
	canonical := tag.String()
	if utf8.RuneCountInString(canonical) > resume.MaxLngCharacters {
		return language.Und.String()
	}
	return canonical
}

func documentInvalid(path, message string) *clientError {
	return &clientError{
		Status: http.StatusUnprocessableEntity, Code: "document_invalid", Message: "resume document is invalid",
		Details: map[string]any{"issues": []map[string]string{{"path": path, "code": "invalid", "message": message}}},
	}
}

func seedCarriesPhoto(raw json.RawMessage) bool {
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		return false
	}
	var personal map[string]json.RawMessage
	if json.Unmarshal(root["personalDetails"], &personal) != nil {
		return false
	}
	_, present := personal["photo"]
	return present
}

func defaultResumeDocument() schema.Resume {
	return schema.Resume{
		SchemaVersion:   int64(docmigrate.CurrentVersion),
		PersonalDetails: schema.PersonalDetails{Details: []schema.PersonalDetail{}},
		Content:         map[string]schema.Section{},
		Customization: schema.Customization{
			Font:    schema.Font{Family: schema.Inter, BaseSizePx: 14},
			Colors:  schema.Colors{Primary: "#1a1a1a", Text: "#1a1a1a", Background: "#ffffff"},
			Spacing: schema.Spacing{SectionGap: 16, EntryGap: 8, LineHeight: 1.4},
			Heading: schema.Heading{Style: schema.Normal, ShowRule: false},
			Layout:  schema.Layout{Columns: 1, Sections: schema.Sections{Main: []string{}, Sidebar: []string{}}},
			SectionDisplay: schema.SectionDisplay{
				Skill: schema.SkillClass{Style: schema.Text}, Language: schema.LanguageClass{Style: schema.Text},
			},
			PageFormat: schema.A4, DateFormat: schema.MmYyyy,
		},
	}
}
