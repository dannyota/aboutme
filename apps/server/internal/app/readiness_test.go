package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/previewmeta"
	"danny.vn/aboutme/apps/server/internal/publicresume"
)

func TestReadinessRenderRequestPostsValidMinimalPublicResume(t *testing.T) {
	origin, err := publicresume.ParsePublicOrigin("https://aboutme.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	renderOrigin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	var body []byte
	renderer := directrender.New(renderOrigin, &http.Client{Transport: readinessRoundTrip(func(request *http.Request) (*http.Response, error) {
		var readErr error
		body, readErr = io.ReadAll(request.Body)
		if readErr != nil {
			return nil, readErr
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(bytes.NewBufferString("<!doctype html>"))}, nil
	})})
	if err := renderer.Probe(context.Background(), readinessRenderRequest(origin)); err != nil {
		t.Fatalf("Probe() error = %v", err)
	}

	var envelope struct {
		PublicResume publicresume.PublicResume `json:"publicResume"`
		Mode         string                    `json:"mode"`
		Origin       string                    `json:"canonicalOrigin"`
		Discovery    bool                      `json:"discoveryEnabled"`
		PageTitle    string                    `json:"pageTitle"`
		FaviconHref  *string                   `json:"faviconHref"`
		Preview      *previewmeta.Meta         `json:"preview"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	// The worker reads the preview head values from every render request,
	// probes included (docs/design/link-previews.md).
	wantPreview := previewmeta.Meta{Title: "Readiness Probe", Description: "Resume on aboutme.vn", Locale: "en_US", ImageAlt: "Readiness Probe"}
	if envelope.Preview == nil || *envelope.Preview != wantPreview {
		t.Fatalf("render envelope preview = %#v, want %#v", envelope.Preview, wantPreview)
	}
	if envelope.PageTitle != "Readiness Probe — Resume" || envelope.FaviconHref == nil || *envelope.FaviconHref != "" {
		t.Fatalf("render envelope head = %q, %v; want the default title and an empty favicon", envelope.PageTitle, envelope.FaviconHref)
	}
	if envelope.Mode != directrender.PublicRenderMode || envelope.Origin != "https://aboutme.example" || envelope.Discovery {
		t.Fatalf("render envelope = %#v", envelope)
	}
	resume := envelope.PublicResume
	if resume.Slug != "readiness-probe" || resume.Revision != "1" || resume.Lng != "en" || resume.DownloadEnabled || resume.Document.SchemaVersion != schema.CurrentVersion || resume.Document.PersonalDetails.FullName != "Readiness Probe" {
		t.Fatalf("PublicResume contract fields = %#v", resume)
	}
	var contentEnvelope struct {
		PublicResume struct {
			Document struct {
				Content map[string]struct {
					SectionType string `json:"sectionType"`
					Entries     []struct {
						ID string `json:"id"`
					} `json:"entries"`
				} `json:"content"`
			} `json:"document"`
		} `json:"publicResume"`
	}
	if err := json.Unmarshal(body, &contentEnvelope); err != nil {
		t.Fatal(err)
	}
	profile, ok := contentEnvelope.PublicResume.Document.Content["profile"]
	if !ok || profile.SectionType != string(schema.Profile) || len(profile.Entries) != 1 || profile.Entries[0].ID != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("PublicContent = %#v", contentEnvelope.PublicResume.Document.Content)
	}
	customization := resume.Document.Customization
	if customization.Font.Family != schema.Inter || customization.Font.BaseSizePx != 14 || customization.Colors.Primary != "#1a1a1a" || customization.Colors.Text != "#1a1a1a" || customization.Colors.Background != "#ffffff" || customization.Spacing.SectionGap != 16 || customization.Spacing.EntryGap != 8 || customization.Spacing.LineHeight != 1.4 || customization.Heading.Style != schema.Normal || customization.Heading.ShowRule || customization.Layout.Columns != 1 || len(customization.Layout.Sections.Main) != 1 || customization.Layout.Sections.Main[0] != "profile" || len(customization.Layout.Sections.Sidebar) != 0 || customization.SectionDisplay.Skill.Style != schema.Text || customization.SectionDisplay.Language.Style != schema.Text || customization.PageFormat != schema.A4 || customization.DateFormat != schema.MmYyyy {
		t.Fatalf("PublicCustomization does not satisfy the closed minimal contract: %#v", customization)
	}
}

type readinessRoundTrip func(*http.Request) (*http.Response, error)

func (f readinessRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
