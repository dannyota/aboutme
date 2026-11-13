package publicapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/publiccache"
	"danny.vn/aboutme/apps/server/internal/publicformat"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/publicstate"
)

type sentEntryPoints struct {
	ProjectsURL  *string                          `json:"projectsUrl"`
	ProjectLinks *[]directrender.EntryProjectLink `json:"projectLinks"`
}

func serveEntryPointHTML(t *testing.T, reader *publicresume.Reader, slug, html string) (int, sentEntryPoints, string) {
	t.Helper()
	renderOrigin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	var sent sentEntryPoints
	renderer := directrender.New(renderOrigin, &http.Client{Transport: htmlRoundTrip(func(request *http.Request) (*http.Response, error) {
		body, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			return nil, readErr
		}
		if decodeErr := json.Unmarshal(body, &sent); decodeErr != nil {
			t.Errorf("decode render request: %v", decodeErr)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(bytes.NewBufferString(html))}, nil
	})})
	cache, err := publiccache.New(2, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	handler, err := NewHTMLHandler(HTMLDependencies{Reader: reader, Cache: cache, Renderer: renderer, PublicOrigin: mustPublicOrigin(t), AppDigest: "sha256:app", RendererDigest: "sha256:renderer", Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/"+slug, nil))
	return w.Code, sent, logs.String()
}

func TestPublicHTMLSendsTheProjectsURLWithoutLinksAndRequiresTheMarker(t *testing.T) {
	slug, reader, _ := formatTestReader(t, false)
	valid := validHTMLIn("en", "Ada", "https://aboutme.example/ada", "1", "")
	status, sent, _ := serveEntryPointHTML(t, reader, slug, valid)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if sent.ProjectsURL == nil || *sent.ProjectsURL != "https://aboutme.example/ada/projects" || sent.ProjectLinks != nil {
		t.Fatalf("render request entry points = %v %v, want projectsUrl and no projectLinks", sent.ProjectsURL, sent.ProjectLinks)
	}
	status, _, logged := serveEntryPointHTML(t, reader, slug, strings.Replace(valid, ` data-project-links=""`, "", 1))
	if status != http.StatusServiceUnavailable || !strings.Contains(logged, "rule=project_links_marker") {
		t.Fatalf("page without the marker = %d %q, want 503 with the marker rule", status, logged)
	}
}

func TestPublicHTMLSendsTheLinksAndValidatesTheirAnchors(t *testing.T) {
	_, reader, slug, entryID, _ := linkedPublicService(t)
	snapshot, lease, err := reader.ReadResume(context.Background(), slug, publicstate.RepresentationHTML)
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	jsonLD, err := publicformat.JSONLD(snapshot.Public, mustPublicOrigin(t), true)
	if err != nil {
		t.Fatal(err)
	}
	projects := "https://aboutme.example/" + slug + "/projects"
	page := projects + "/atlas-tool"
	bar := `<span class="public-actions"><a class="public-projects" href="` + projects + `">Projects</a></span></div>`
	anchor := `<strong><a data-project-entry="` + entryID + `" href="` + page + `">Atlas</a></strong>`
	valid := strings.Replace(strings.Replace(validHTMLIn("en", "Ada", "https://aboutme.example/"+slug, "1", string(jsonLD.Script)),
		`</a></div>`, `</a>`+bar, 1), "body</main>", anchor+"</main>", 1)
	status, sent, logged := serveEntryPointHTML(t, reader, slug, valid)
	if status != http.StatusOK {
		t.Fatalf("status = %d %q, want 200", status, logged)
	}
	want := []directrender.EntryProjectLink{{EntryID: entryID, URL: page}}
	if sent.ProjectsURL == nil || *sent.ProjectsURL != projects || sent.ProjectLinks == nil || !reflect.DeepEqual(*sent.ProjectLinks, want) {
		t.Fatalf("render request entry points = %v %v, want %s and %+v", sent.ProjectsURL, sent.ProjectLinks, projects, want)
	}
	status, _, logged = serveEntryPointHTML(t, reader, slug, strings.Replace(valid, anchor, "<strong>Atlas</strong>", 1))
	if status != http.StatusServiceUnavailable || !strings.Contains(logged, "rule=project_entry_link") {
		t.Fatalf("page without the title link = %d %q, want 503 with the title link rule", status, logged)
	}
}

func TestEntryPointsRaiseTheHTMLAndPDFFormatVersions(t *testing.T) {
	if htmlFormatVersion != 4 || publicPDFFormatVersion != 2 {
		t.Fatalf("format versions html %d pdf %d, want 4 and 2 so no cached page or PDF without entry points is reused", htmlFormatVersion, publicPDFFormatVersion)
	}
}
