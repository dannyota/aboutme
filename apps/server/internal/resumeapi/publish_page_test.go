package resumeapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

// The public page title and emoji favicon are publication settings
// (docs/adr/0042-public-page-title-and-favicon.md): absent keeps the stored
// value, "" clears it, and any other value is validated.

const publishFlags = `"live":false,"downloadEnabled":true,"seoGeoEnabled":false`

func TestPublishDecodePublicPageFields(t *testing.T) {
	t.Parallel()

	got, err := decodePublish(strings.NewReader(`{` + publishFlags + `,"publicTitle":"Danny from aboutme.vn","faviconEmoji":""}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.PublicTitle.Present || got.PublicTitle.Value != "Danny from aboutme.vn" || !got.FaviconEmoji.Present || got.FaviconEmoji.Value != "" {
		t.Fatalf("decoded = %+v", got)
	}
	absent, err := decodePublish(strings.NewReader(`{` + publishFlags + `}`))
	if err != nil || absent.PublicTitle.Present || absent.FaviconEmoji.Present {
		t.Fatalf("absent fields decoded as present: %+v %v", absent, err)
	}
	for field, value := range map[string]string{
		"publicTitle":  "null",
		"faviconEmoji": "null",
	} {
		_, err := decodePublish(strings.NewReader(`{` + publishFlags + `,"` + field + `":` + value + `}`))
		var shape *publishShapeError
		if !errors.As(err, &shape) || shape.Field != field {
			t.Fatalf("%s=%s error = %#v, want shape error", field, value, err)
		}
	}
	for field, value := range map[string]string{"publicTitle": "7", "faviconEmoji": `["x"]`} {
		_, err := decodePublish(strings.NewReader(`{` + publishFlags + `,"` + field + `":` + value + `}`))
		var shape *publishShapeError
		if !errors.As(err, &shape) || shape.Field != field {
			t.Fatalf("%s=%s error = %#v, want shape error", field, value, err)
		}
	}
}

func TestValidatePublishPublicPage(t *testing.T) {
	t.Parallel()

	stored, storedEmoji := "Stored title", "\U0001F680"
	current := currentPublish{PublicTitle: &stored, FaviconEmoji: &storedEmoji}
	doc := schema.Resume{}

	kept := validatePublish(doc, current, publishInput{})
	if kept.Effective.PublicTitle == nil || *kept.Effective.PublicTitle != stored ||
		kept.Effective.FaviconEmoji == nil || *kept.Effective.FaviconEmoji != storedEmoji {
		t.Fatalf("absent fields changed the stored values: %+v", kept.Effective)
	}

	cleared := validatePublish(doc, current, publishInput{
		PublicTitle:  optionalText{Present: true, Value: "  "},
		FaviconEmoji: optionalText{Present: true, Value: ""},
	})
	if cleared.Effective.PublicTitle != nil || cleared.Effective.FaviconEmoji != nil || len(cleared.Issues) != 0 {
		t.Fatalf("empty values did not clear: %+v %v", cleared.Effective, cleared.Issues)
	}

	set := validatePublish(doc, currentPublish{}, publishInput{
		PublicTitle:  optionalText{Present: true, Value: " Danny from aboutme.vn "},
		FaviconEmoji: optionalText{Present: true, Value: "\U0001F680"},
	})
	if set.Effective.PublicTitle == nil || *set.Effective.PublicTitle != "Danny from aboutme.vn" || len(set.Issues) != 0 {
		t.Fatalf("title not trimmed and stored: %+v %v", set.Effective, set.Issues)
	}

	invalid := validatePublish(doc, current, publishInput{
		PublicTitle:  optionalText{Present: true, Value: strings.Repeat("x", 71) + "\u202e"},
		FaviconEmoji: optionalText{Present: true, Value: "\U0001F680\U0001F680"},
	})
	got := make([]string, 0, len(invalid.Issues))
	for _, issue := range invalid.Issues {
		got = append(got, issue.Path+":"+issue.Code)
		if strings.Contains(issue.Message, "\u202e") || strings.Contains(issue.Message, "\U0001F680") {
			t.Fatalf("issue echoes input: %+v", issue)
		}
	}
	slices.Sort(got)
	want := []string{"faviconEmoji:invalid_emoji", "publicTitle:invalid_characters", "publicTitle:too_long"}
	if !slices.Equal(got, want) {
		t.Fatalf("issues = %v, want %v", got, want)
	}
}

func TestPublishStoresAndReturnsPublicPageSettings(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created, err := h.resumes.Create(h.ctx, h.userID, "Published", publishCompleteDocument(t))
	if err != nil {
		t.Fatalf("create publishable resume: %v", err)
	}
	path := apiResumePath + "/" + created.ID.String() + "/publish"
	slug := "page-" + uuid.NewString()[:8]
	set := h.mutationRequest(t, http.MethodPost, path, strings.NewReader(
		`{"slug":"`+slug+`","live":true,"downloadEnabled":true,"seoGeoEnabled":false,`+
			`"publicTitle":"Danny from aboutme.vn","faviconEmoji":"🚀"}`), created.Revision, uuid.NewString())
	if set.status != http.StatusOK {
		t.Fatalf("publish = %d %s", set.status, set.body)
	}
	resource := decodePublicPageResource(t, set.body)
	if resource.PublicTitle == nil || *resource.PublicTitle != "Danny from aboutme.vn" ||
		resource.FaviconEmoji == nil || *resource.FaviconEmoji != "\U0001F680" {
		t.Fatalf("response = %+v", resource)
	}

	kept := h.mutationRequest(t, http.MethodPost, path, strings.NewReader(
		`{"live":true,"downloadEnabled":false,"seoGeoEnabled":false}`), created.Revision+1, uuid.NewString())
	if kept.status != http.StatusOK {
		t.Fatalf("publish without fields = %d %s", kept.status, kept.body)
	}
	if resource := decodePublicPageResource(t, kept.body); resource.PublicTitle == nil || resource.FaviconEmoji == nil {
		t.Fatalf("absent fields cleared the settings: %+v", resource)
	}

	rejected := h.mutationRequest(t, http.MethodPost, path, strings.NewReader(
		`{"live":true,"downloadEnabled":false,"seoGeoEnabled":false,"faviconEmoji":"rocket"}`), created.Revision+2, uuid.NewString())
	assertResumeTestError(t, rejected, http.StatusUnprocessableEntity, "publish_invalid")

	cleared := h.mutationRequest(t, http.MethodPost, path, strings.NewReader(
		`{"live":true,"downloadEnabled":false,"seoGeoEnabled":false,"publicTitle":"","faviconEmoji":""}`), created.Revision+2, uuid.NewString())
	if cleared.status != http.StatusOK {
		t.Fatalf("clear = %d %s", cleared.status, cleared.body)
	}
	if resource := decodePublicPageResource(t, cleared.body); resource.PublicTitle != nil || resource.FaviconEmoji != nil {
		t.Fatalf("empty values did not clear: %+v", resource)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if stored.PublicTitle != nil || stored.FaviconEmoji != nil {
		t.Fatalf("stored settings = %v %v, want cleared", stored.PublicTitle, stored.FaviconEmoji)
	}
}

type publicPageResource struct {
	PublicTitle  *string `json:"publicTitle"`
	FaviconEmoji *string `json:"faviconEmoji"`
}

func decodePublicPageResource(t *testing.T, body []byte) publicPageResource {
	t.Helper()
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode resource: %v", err)
	}
	for _, key := range []string{"publicTitle", "faviconEmoji"} {
		if _, ok := envelope.Data[key]; !ok {
			t.Fatalf("resource lacks %s: %s", key, body)
		}
	}
	var resource publicPageResource
	raw, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("encode page fields: %v", err)
	}
	if err := json.Unmarshal(raw, &resource); err != nil {
		t.Fatalf("decode page fields: %v", err)
	}
	return resource
}
