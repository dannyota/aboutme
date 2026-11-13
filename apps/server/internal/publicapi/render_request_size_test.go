package publicapi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/publicpage"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	resumepkg "danny.vn/aboutme/apps/server/internal/resume"
)

func largestLinkedDocument(t *testing.T, links int) (schema.Resume, []string) {
	t.Helper()
	document := largestPreviewDocument(t)
	id := func(name string) string { return uuid.NewSHA1(uuid.NameSpaceURL, []byte("linked/"+name)).String() }
	projectKey := id("projects")
	entries := make([]schema.ProjectEntry, links)
	ids := make([]string, links)
	empty := ""
	for index := range entries {
		title, projectID := fmt.Sprintf("Project %02d", index), id(fmt.Sprintf("project/%d", index))
		ids[index] = id(fmt.Sprintf("entry/%d", index))
		entries[index] = schema.ProjectEntry{ID: ids[index], Title: &title, ProjectID: &projectID, Description: &empty}
	}
	document.Content[projectKey] = schema.NewProjectSection(nil, nil, entries)
	document.Customization.Layout.Sections.Main = append(document.Customization.Layout.Sections.Main, projectKey)
	for key, section := range document.Content {
		if section.SectionType != schema.SectionTypeCustom {
			continue
		}
		canonical, err := resumepkg.AssembleCanonical(document)
		if err != nil {
			t.Fatal(err)
		}
		excess := len(canonical) - resumepkg.MaxDocumentBytes
		for index := len(section.CustomEntries) - 1; index >= 0 && excess > 0; index-- {
			description := *section.CustomEntries[index].Description
			cut := min(excess, len(description))
			trimmed := description[:len(description)-cut]
			section.CustomEntries[index].Description = &trimmed
			excess -= cut
		}
		document.Content[key] = section
	}
	canonical, err := resumepkg.AssembleCanonical(document)
	if err != nil {
		t.Fatal(err)
	}
	if len(canonical) != resumepkg.MaxDocumentBytes {
		t.Fatalf("canonical size = %d, want %d", len(canonical), resumepkg.MaxDocumentBytes)
	}
	if err := resumepkg.ValidateForStore(document); err != nil {
		t.Fatalf("largest linked document is not valid: %v", err)
	}
	return document, ids
}

func longestPublicOrigin() string {
	host := strings.Repeat(strings.Repeat("a", 63)+".", 8)
	return "https://" + host[:512-len("https://")-len(".vn")] + ".vn"
}

func TestPublicRenderRequestWithTheLargestLinkedDocumentFits(t *testing.T) {
	document, ids := largestLinkedDocument(t, 30)
	origin, err := publicresume.ParsePublicOrigin(longestPublicOrigin(), "production")
	if err != nil || len(origin.String()) != 512 {
		t.Fatalf("longest origin = %d bytes, %v", len(origin.String()), err)
	}
	slug, lng := "a"+strings.Repeat("b", 28)+"c", "vi"
	public, err := publicresume.Project(resumepkg.Resume{Slug: &slug, Lng: &lng, Revision: 1, Doc: document}, origin)
	if err != nil {
		t.Fatal(err)
	}
	links := make([]publicresume.ProjectLink, len(ids))
	for index, entryID := range ids {
		projectSlug := fmt.Sprintf("%02d", index) + strings.Repeat("p", 38)
		links[index] = publicresume.ProjectLink{EntryID: entryID, ProjectSlug: projectSlug, URL: origin.Resolve("/" + slug + "/projects/" + projectSlug)}
	}
	title := strings.Repeat("\U0001F44D"+strings.Repeat("\U0001F3FD", 7), publicpage.MaxTitleGraphemes)
	emoji := "\U0001F468\u200D\U0001F469\u200D\U0001F467\u200D\U0001F466"
	page := withEntryPoints(expectedPublicPage(public, &title, &emoji), slug, links, origin)
	renderOrigin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	sent := 0
	renderer := directrender.New(renderOrigin, &http.Client{Transport: htmlRoundTrip(func(request *http.Request) (*http.Response, error) {
		body, readErr := io.ReadAll(request.Body)
		sent = len(body)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(bytes.NewReader(nil))}, readErr
	})})
	_, err = renderer.Render(context.Background(), publicRenderRequest(public, true, page, origin))
	t.Logf("largest linked render request: %d bytes, error %v", sent, err)
	if err != nil {
		t.Fatalf("largest linked render request refused: %v", err)
	}
}
