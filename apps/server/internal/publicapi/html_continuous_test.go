package publicapi

import (
	"encoding/json"
	stdhtml "html"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/publicformat"
	"danny.vn/aboutme/apps/server/internal/publicresume"
)

var goldenPhotoSource = regexp.MustCompile(`src="data:image/png;base64,[^"]*"`)

func continuousGoldenResume(t *testing.T) publicresume.PublicResume {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packages", "schema", "fixtures", "full.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document schema.Resume
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	photo := mustPublicOrigin(t).Resolve("/api/v1/public/resumes/ada/photo")
	return publicresume.PublicResume{Slug: "ada", Revision: "1", Lng: "en", Document: publicresume.ProjectDocument(document, photo)}
}

func publicPageAround(public publicresume.PublicResume, article string) string {
	page := expectedPublicPage(public, nil, nil)
	html := replaceOnce(pageWithHead(public, previewHead(public, page.Preview, "https://aboutme.example")),
		"<title>Ada — Resume</title>", "<title>"+stdhtml.EscapeString(page.Title)+"</title>")
	return replaceOnce(html, "body</main>", article+"</main>")
}

func TestContinuousGoldensPassTheResumePageValidator(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "web", "test", "renderer", "golden", "*--continuous.html"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("continuous goldens: %v %v", paths, err)
	}
	public := continuousGoldenResume(t)
	origin := mustPublicOrigin(t)
	jsonLD, err := publicformat.JSONLD(public, origin, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			golden, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			article := string(golden)
			if len(goldenPhotoSource.FindAllString(article, -1)) != 1 {
				t.Fatal("golden does not hold exactly one fixed photo")
			}
			article = goldenPhotoSource.ReplaceAllLiteralString(article, `src="`+public.Document.PersonalDetails.Photo.URL+`"`)
			if rule := publicHTMLRejection([]byte(publicPageAround(public, article)), public, origin, jsonLD, false); rule != "" {
				t.Fatalf("continuous page rejected by %q", rule)
			}
		})
	}
}
