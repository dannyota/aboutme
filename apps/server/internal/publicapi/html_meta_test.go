package publicapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	stdhtml "html"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/directrender"
	"github.com/dannyota/aboutme/apps/server/internal/previewmeta"
	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/publicformat"
	"github.com/dannyota/aboutme/apps/server/internal/publicpage"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	resumepkg "github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// previewTag is one preview meta element as the renderer writes it.
type previewTag struct{ attribute, name, content string }

// previewTags lists the preview meta elements for resume in the order of
// docs/design/link-previews.md, "Page head", leaving out og:locale when the
// language maps to none.
func previewTags(resume publicresume.PublicResume, preview previewmeta.Meta, root string) []previewTag {
	imageURL := preview.ImageURL
	if imageURL == "" {
		imageURL = root + "/api/v1/public/resumes/" + resume.Slug + "/og.png"
	}
	tags := []previewTag{
		{"name", "description", preview.Description},
		{"property", "og:type", "profile"},
		{"property", "og:site_name", "aboutme.vn"},
		{"property", "og:title", preview.Title},
		{"property", "og:description", preview.Description},
		{"property", "og:url", root + "/" + resume.Slug},
		{"property", "og:locale", preview.Locale},
		{"property", "og:image", imageURL},
		{"property", "og:image:type", "image/png"},
		{"property", "og:image:width", "1200"},
		{"property", "og:image:height", "630"},
		{"property", "og:image:alt", preview.ImageAlt},
		{"name", "twitter:card", "summary_large_image"},
		{"name", "twitter:image", imageURL},
		{"name", "twitter:image:alt", preview.ImageAlt},
	}
	if preview.Locale == "" {
		tags = append(tags[:6], tags[7:]...)
	}
	return tags
}

func (tag previewTag) html() string {
	return `<meta ` + tag.attribute + `="` + tag.name + `" content="` + stdhtml.EscapeString(tag.content) + `">`
}

// previewHead is the preview part of the page head the renderer writes.
func previewHead(resume publicresume.PublicResume, preview previewmeta.Meta, root string) string {
	var out strings.Builder
	for _, tag := range previewTags(resume, preview, root) {
		out.WriteString(tag.html())
	}
	return out.String()
}

// previewResume is a live Vietnamese resume with a summary, every contact
// type, and sentinels in hidden entries, a hidden contact, and a profile
// section outside the layout.
func previewResume() schema.Resume {
	name, headline := `Nguyễn "An" <Dev> & co`, "Kỹ sư phần mềm"
	visible := "<p>Kỹ sư thanh toán. Email an@example.com.</p>"
	hiddenProfile := "<p>SENTINEL-HIDDEN-PROFILE</p>"
	outside := "<p>SENTINEL-OUTSIDE-LAYOUT</p>"
	hiddenJob := "SENTINEL-HIDDEN-WORK"
	hidden := true
	return schema.Resume{
		SchemaVersion: schema.CurrentVersion,
		PersonalDetails: schema.PersonalDetails{FullName: &name, Headline: &headline, Details: []schema.PersonalDetail{
			{ID: "email", Type: schema.Email, Value: "an@example.com"},
			{ID: "phone", Type: schema.Phone, Value: "+84 912 345 678"},
			{ID: "location", Type: schema.Location, Value: "Hà Nội"},
			{ID: "hidden", Type: schema.TypeCustom, Value: "SENTINEL-HIDDEN-CONTACT", IsHidden: true},
		}},
		Content: map[string]schema.Section{
			"summary": schema.NewProfileSection(nil, nil, []schema.ProfileEntry{{ID: "hidden", IsHidden: &hidden, Text: &hiddenProfile}, {ID: "visible", Text: &visible}}),
			"outside": schema.NewProfileSection(nil, nil, []schema.ProfileEntry{{ID: "outside", Text: &outside}}),
			"work":    schema.NewWorkSection(nil, nil, []schema.WorkEntry{{ID: "hidden-work", IsHidden: &hidden, JobTitle: &hiddenJob}}),
		},
		Customization: schema.Customization{Layout: schema.Layout{Columns: 1, Sections: schema.Sections{Main: []string{"work", "summary"}, Sidebar: []string{}}}},
	}
}

var previewSentinels = []string{"SENTINEL-HIDDEN-CONTACT", "SENTINEL-HIDDEN-PROFILE", "SENTINEL-HIDDEN-WORK", "SENTINEL-OUTSIDE-LAYOUT"}

func projectedPreviewResume(t *testing.T) publicresume.PublicResume {
	t.Helper()
	slug, lng := "ada", "vi"
	public, err := publicresume.Project(resumepkg.Resume{Slug: &slug, Lng: &lng, Revision: 1, Doc: previewResume()}, mustPublicOrigin(t))
	if err != nil {
		t.Fatal(err)
	}
	return public
}

// pageWithHead is a valid page for public, with the credit in its language,
// whose head holds head in place of its own preview elements.
func pageWithHead(public publicresume.PublicResume, head string) string {
	base := strings.Replace(validHTMLIn(public.Lng, "Ada", "https://aboutme.example/"+public.Slug, public.Revision, ""),
		"Built with aboutme.vn", creditText(public.Lng), 1)
	start := strings.Index(base, `<meta name="description"`)
	end := strings.Index(base, "</head>")
	return base[:start] + head + base[end:]
}

func TestPublicHTMLAcceptsExactlyThePreviewHead(t *testing.T) {
	origin := mustPublicOrigin(t)
	public := projectedPreviewResume(t)
	jsonLD, err := publicformat.JSONLD(public, origin, false)
	if err != nil {
		t.Fatal(err)
	}
	page := expectedPublicPage(public, nil, nil)
	want := previewmeta.Meta{Title: `Nguyễn "An" <Dev> & co`, Description: "Kỹ sư thanh toán. Email .", Locale: "vi_VN", ImageAlt: `Nguyễn "An" <Dev> & co · Kỹ sư phần mềm`}
	if page.Preview != want {
		t.Fatalf("preview = %#v, want %#v", page.Preview, want)
	}
	tags := previewTags(public, page.Preview, "https://aboutme.example")
	head := previewHead(public, page.Preview, "https://aboutme.example")
	valid := strings.Replace(pageWithHead(public, head), "<title>Ada — Resume</title>", "<title>"+stdhtml.EscapeString(page.Title)+"</title>", 1)
	if rule := publicHTMLRejectionForPage([]byte(valid), public, page, origin, jsonLD, false); rule != "" {
		t.Fatalf("exact preview head rejected by %q", rule)
	}
	for _, tag := range tags {
		rule := "meta_" + strings.ReplaceAll(tag.name, ":", "_")
		changed := tag
		changed.content += "x"
		for name, candidate := range map[string]string{
			"missing":  strings.Replace(valid, tag.html(), "", 1),
			"repeated": strings.Replace(valid, tag.html(), tag.html()+tag.html(), 1),
			"changed":  strings.Replace(valid, tag.html(), changed.html(), 1),
		} {
			t.Run(tag.name+" "+name, func(t *testing.T) {
				wantRule := map[string]string{"missing": "meta_missing", "repeated": "meta_repeated", "changed": rule}[name]
				if got := publicHTMLRejectionForPage([]byte(candidate), public, page, origin, jsonLD, false); got != wantRule {
					t.Fatalf("rule = %q, want %q", got, wantRule)
				}
			})
		}
	}
	for name, test := range map[string]struct{ extra, want string }{
		"twitter title":           {`<meta name="twitter:title" content="x">`, "meta_unknown"},
		"twitter description":     {`<meta name="twitter:description" content="x">`, "meta_unknown"},
		"theme color":             {`<meta name="theme-color" content="#000000">`, "meta_unknown"},
		"description as property": {`<meta property="description" content="x">`, "meta_unknown"},
		"extra attribute":         {`<meta name="robots" content="noindex" data-x="1">`, "meta_unknown"},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := strings.Replace(valid, "</head>", test.extra+"</head>", 1)
			if got := publicHTMLRejectionForPage([]byte(candidate), public, page, origin, jsonLD, false); got != test.want {
				t.Fatalf("rule = %q, want %q", got, test.want)
			}
		})
	}
	for _, sentinel := range previewSentinels {
		if strings.Contains(valid, sentinel) {
			t.Fatalf("preview head leaks %s", sentinel)
		}
	}
}

func TestPublicHTMLRejectsALocaleTheLanguageDoesNotMap(t *testing.T) {
	origin := mustPublicOrigin(t)
	resume := publicresume.PublicResume{Slug: "ada", Revision: "1", Lng: "fr", Document: publicresume.PublicResumeDocument{PersonalDetails: publicresume.PublicPersonalDetails{FullName: "Ada"}}}
	jsonLD, err := publicformat.JSONLD(resume, origin, false)
	if err != nil {
		t.Fatal(err)
	}
	valid := validHTMLIn("fr", "Ada", "https://aboutme.example/ada", "1", "")
	if rule := publicHTMLRejection([]byte(valid), resume, origin, jsonLD, false); rule != "" {
		t.Fatalf("page without og:locale rejected by %q", rule)
	}
	withLocale := strings.Replace(valid, "</head>", `<meta property="og:locale" content="fr_FR"></head>`, 1)
	if rule := publicHTMLRejection([]byte(withLocale), resume, origin, jsonLD, false); rule != "meta_og_locale" {
		t.Fatalf("rule = %q, want meta_og_locale", rule)
	}
}

// previewTestReader serves one stored row, like formatTestReader.
func previewTestReader(t *testing.T, document schema.Resume, live bool, publicTitle *string) (string, *publicresume.Reader) {
	t.Helper()
	slug, lng := "ada", "vi"
	pd, err := json.Marshal(document.PersonalDetails)
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(document.Content)
	if err != nil {
		t.Fatal(err)
	}
	customization, err := json.Marshal(document.Customization)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	row := store.Resume{ID: formatResumeID, Slug: &slug, Live: live, Revision: 1, Lng: &lng, SchemaVersion: int32(schema.CurrentVersion),
		PersonalDetails: pd, Content: content, Customization: customization, PublicTitle: publicTitle}
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{Store: formatReadStore{row: row}, Projector: docmigrate.NewIdentityProjector(), Coordinator: coordinator, Origin: mustPublicOrigin(t)})
	if err != nil {
		t.Fatal(err)
	}
	return slug, reader
}

func previewHandler(t *testing.T, reader *publicresume.Reader, transport htmlRoundTrip) http.Handler {
	t.Helper()
	origin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	cache, err := publiccache.New(2, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHTMLHandler(HTMLDependencies{Reader: reader, Cache: cache, Renderer: directrender.New(origin, &http.Client{Transport: transport}), PublicOrigin: mustPublicOrigin(t), AppDigest: "sha256:app", RendererDigest: "sha256:renderer"})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func TestPublicHTMLSendsThePreviewAndServesItsHead(t *testing.T) {
	title := "An — Kỹ sư"
	slug, reader := previewTestReader(t, previewResume(), true, &title)
	public := projectedPreviewResume(t)
	want := previewmeta.For(public, &title)
	if want.Title != title {
		t.Fatalf("preview title = %q, want the public title", want.Title)
	}
	body := strings.Replace(pageWithHead(public, previewHead(public, want, "https://aboutme.example")), "<title>Ada — Resume</title>", "<title>"+title+"</title>", 1)
	var sent struct {
		Preview previewmeta.Meta `json:"preview"`
	}
	handler := previewHandler(t, reader, func(request *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(request.Body).Decode(&sent); err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/"+slug, nil))
	if w.Code != http.StatusOK || w.Body.String() != body {
		t.Fatalf("response = %d %q", w.Code, w.Body.String())
	}
	if sent.Preview != want {
		t.Fatalf("sent preview = %#v, want %#v", sent.Preview, want)
	}
	for _, sentinel := range previewSentinels {
		if strings.Contains(fmt.Sprint(sent.Preview), sentinel) || strings.Contains(w.Body.String(), sentinel) {
			t.Fatalf("preview leaks %s", sentinel)
		}
	}
}

func TestPublicHTMLExposesNoPreviewForAResumeThatIsNotLive(t *testing.T) {
	// An unpublished or private resume has live = false; the page is the
	// public 404 and the renderer never sees its text.
	slug, reader := previewTestReader(t, previewResume(), false, nil)
	handler := previewHandler(t, reader, func(*http.Request) (*http.Response, error) {
		return nil, errors.New("renderer must not run")
	})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/"+slug, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	for _, text := range append([]string{"Nguyễn", "Kỹ sư", "og:", "description"}, previewSentinels...) {
		if strings.Contains(w.Body.String(), text) {
			t.Fatalf("404 body leaks %q", text)
		}
	}
}

// The render request with the largest valid document, the longest valid
// public title, and preview text at its byte bounds stays inside the fixed
// request limit (docs/design/link-previews.md, "Security and size").
func TestPublicRenderRequestWithTheLargestDocumentFits(t *testing.T) {
	document := largestPreviewDocument(t)
	slug, lng := "ada", "vi"
	public, err := publicresume.Project(resumepkg.Resume{Slug: &slug, Lng: &lng, Revision: 1, Doc: document}, mustPublicOrigin(t))
	if err != nil {
		t.Fatal(err)
	}
	title := strings.Repeat("\U0001F44D"+strings.Repeat("\U0001F3FD", 7), publicpage.MaxTitleGraphemes)
	if result := publicpage.NormalizeTitle(title); result.Value == nil || *result.Value != title {
		t.Fatalf("title fixture is not a valid public title: %v", result.Codes)
	}
	emoji := "\U0001F468\u200D\U0001F469\u200D\U0001F467\u200D\U0001F466"
	page := expectedPublicPage(public, &title, &emoji)
	if len(page.Preview.Title) != len(title) || len(page.Preview.Description) < 512 || len(page.Preview.ImageAlt) < 512 {
		t.Fatalf("preview fixture is not at its bounds: %d, %d, %d bytes", len(page.Preview.Title), len(page.Preview.Description), len(page.Preview.ImageAlt))
	}
	origin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	sent := 0
	renderer := directrender.New(origin, &http.Client{Transport: htmlRoundTrip(func(request *http.Request) (*http.Response, error) {
		body, readErr := io.ReadAll(request.Body)
		sent = len(body)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(bytes.NewReader(nil))}, readErr
	})})
	if _, err := renderer.Render(context.Background(), publicRenderRequest(public, true, page, mustPublicOrigin(t))); err != nil {
		t.Fatalf("largest render request refused: %v", err)
	}
	t.Logf("largest render request: %d bytes", sent)
	if sent == 0 || sent > 532_480 {
		t.Fatalf("render request = %d bytes", sent)
	}
}

// largestPreviewDocument is a valid document of exactly the maximum canonical
// size whose name, headline, and summary are dense emoji text.
func largestPreviewDocument(t *testing.T) schema.Resume {
	t.Helper()
	source, err := os.ReadFile("../../../../packages/schema/fixtures/minimal.json")
	if err != nil {
		t.Fatal(err)
	}
	var document schema.Resume
	if err = json.Unmarshal(source, &document); err != nil {
		t.Fatal(err)
	}
	id := func(name string) string { return uuid.NewSHA1(uuid.NameSpaceURL, []byte("preview/"+name)).String() }
	dense := strings.Repeat("\U0001F44D\U0001F3FD", 40)
	summary := strings.Repeat("\U0001F44D\U0001F3FD ", 1000)
	document.PersonalDetails = schema.PersonalDetails{FullName: &dense, Headline: &dense, Details: []schema.PersonalDetail{}}
	profileKey, customKey := id("profile"), id("custom")
	displayName := "More"
	entries := make([]schema.CustomEntry, 40)
	empty := ""
	for index := range entries {
		entryTitle := fmt.Sprintf("Entry %02d", index)
		entries[index] = schema.CustomEntry{ID: id(fmt.Sprintf("entry/%d", index)), Title: &entryTitle, Description: &empty}
	}
	document.Content = map[string]schema.Section{
		profileKey: schema.NewProfileSection(nil, nil, []schema.ProfileEntry{{ID: id("summary"), Text: &summary}}),
		customKey:  schema.NewCustomSection(&displayName, nil, entries),
	}
	document.Customization.Layout.Sections.Main = []string{profileKey, customKey}
	canonical, err := resumepkg.AssembleCanonical(document)
	if err != nil {
		t.Fatal(err)
	}
	remaining := resumepkg.MaxDocumentBytes - len(canonical)
	for index := range entries {
		length := min(remaining, 16_384)
		description := strings.Repeat("x", length)
		entries[index].Description = &description
		remaining -= length
	}
	if remaining != 0 {
		t.Fatalf("fixture has %d bytes left over", remaining)
	}
	canonical, err = resumepkg.AssembleCanonical(document)
	if err != nil {
		t.Fatal(err)
	}
	if len(canonical) != resumepkg.MaxDocumentBytes {
		t.Fatalf("canonical size = %d, want %d", len(canonical), resumepkg.MaxDocumentBytes)
	}
	if err := resumepkg.ValidateForStore(document); err != nil {
		t.Fatalf("largest document is not valid: %v", err)
	}
	return document
}

// With stored cards on, the page names the current card version, the render
// request carries that URL, and a head naming the og.png alias is rejected.
func TestPublicHTMLNamesTheCurrentCardVersion(t *testing.T) {
	slug, reader := previewTestReader(t, previewResume(), true, nil)
	public := projectedPreviewResume(t)
	want := previewmeta.For(public, nil)
	want.ImageURL = "https://aboutme.example/api/v1/public/resumes/ada/og/" + cardVersion + ".png"
	titled := func(head string) string {
		title := stdhtml.EscapeString(expectedPublicPage(public, nil, nil).Title)
		return strings.Replace(pageWithHead(public, head), "<title>Ada — Resume</title>", "<title>"+title+"</title>", 1)
	}
	versioned := titled(previewHead(public, want, "https://aboutme.example"))
	unversioned := want
	unversioned.ImageURL = ""
	alias := titled(previewHead(public, unversioned, "https://aboutme.example"))
	for _, test := range []struct {
		body     string
		wantCode int
	}{{versioned, http.StatusOK}, {alias, http.StatusServiceUnavailable}} {
		var sent struct {
			Preview previewmeta.Meta `json:"preview"`
		}
		origin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "development")
		if err != nil {
			t.Fatal(err)
		}
		cache, err := publiccache.New(2, time.Minute, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		renderer := directrender.New(origin, &http.Client{Transport: htmlRoundTrip(func(request *http.Request) (*http.Response, error) {
			if decodeErr := json.NewDecoder(request.Body).Decode(&sent); decodeErr != nil {
				return nil, decodeErr
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(test.body))}, nil
		})})
		handler, err := NewHTMLHandler(HTMLDependencies{
			Reader: reader, Cache: cache, Renderer: renderer, PublicOrigin: mustPublicOrigin(t),
			AppDigest: "sha256:app", RendererDigest: "sha256:renderer", Cards: &fakeCards{version: cardVersion},
		})
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/"+slug, nil))
		if w.Code != test.wantCode {
			t.Fatalf("response = %d, want %d", w.Code, test.wantCode)
		}
		if sent.Preview != want {
			t.Fatalf("sent preview = %#v, want %#v", sent.Preview, want)
		}
	}
}
