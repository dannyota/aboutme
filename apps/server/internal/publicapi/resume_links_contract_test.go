package publicapi

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/publicresume"
)

func strictShape(t *testing.T, typ reflect.Type, raw string) any {
	t.Helper()
	fields := make([]reflect.StructField, typ.NumField())
	found := false
	for index := range fields {
		field := typ.Field(index)
		fields[index] = reflect.StructField{Name: field.Name, Type: field.Type, Tag: field.Tag}
		if field.Name == raw {
			fields[index].Type, found = reflect.TypeFor[json.RawMessage](), true
		}
	}
	if !found {
		t.Fatalf("%s has no field %s", typ, raw)
	}
	return reflect.New(reflect.StructOf(fields)).Interface()
}

func TestResumeLinksRenderCorpusMatchesRequestType(t *testing.T) {
	raw, err := os.ReadFile(resumeLinksCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	var corpus resumeLinksCorpus
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) != len(resumeLinksSources()) {
		t.Fatalf("corpus has %d cases, want %d", len(corpus.Cases), len(resumeLinksSources()))
	}
	for _, entry := range corpus.Cases {
		t.Run(entry.Name, func(t *testing.T) {
			shape := strictShape(t, reflect.TypeFor[directrender.PublicRenderRequest](), "PublicResume")
			decoder := json.NewDecoder(bytes.NewReader(entry.Envelope))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(shape); err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			var want bytes.Buffer
			if err := json.Compact(&want, entry.Envelope); err != nil {
				t.Fatal(err)
			}
			if got := encodeCorpusJSON(t, shape, ""); !bytes.Equal(got, want.Bytes()) {
				t.Fatalf("envelope does not round-trip:\n got %s\nwant %s", got, want.Bytes())
			}
			requireEntryPointRules(t, entry.Envelope)
		})
	}
}

func requireEntryPointRules(t *testing.T, envelope json.RawMessage) {
	t.Helper()
	var request struct {
		CanonicalOrigin string `json:"canonicalOrigin"`
		PublicResume    struct {
			Slug     string `json:"slug"`
			Document struct {
				Content map[string]struct {
					SectionType string `json:"sectionType"`
					Entries     []struct {
						ID string `json:"id"`
					} `json:"entries"`
				} `json:"content"`
				Customization struct {
					Layout struct {
						Sections struct {
							Main    []string `json:"main"`
							Sidebar []string `json:"sidebar"`
						} `json:"sections"`
					} `json:"layout"`
				} `json:"customization"`
			} `json:"document"`
		} `json:"publicResume"`
		ProjectLinks []directrender.EntryProjectLink `json:"projectLinks"`
		ProjectsURL  string                          `json:"projectsUrl"`
	}
	if err := json.Unmarshal(envelope, &request); err != nil {
		t.Fatal(err)
	}
	if want := request.CanonicalOrigin + "/" + request.PublicResume.Slug + "/projects"; request.ProjectsURL != want {
		t.Fatalf("projectsUrl = %q, want %q", request.ProjectsURL, want)
	}
	if request.ProjectLinks != nil && len(request.ProjectLinks) == 0 {
		t.Fatal("projectLinks is present but empty")
	}
	linked := map[string]bool{}
	var got []string
	for _, link := range request.ProjectLinks {
		slug, under := strings.CutPrefix(link.URL, request.ProjectsURL+"/")
		if !under || !project.ValidSlug(slug) || link.EntryID == "" || linked[link.EntryID] {
			t.Fatalf("link %+v is not one entry's own project page under %q", link, request.ProjectsURL)
		}
		linked[link.EntryID] = true
		got = append(got, link.EntryID)
	}
	var visible []string
	document := request.PublicResume.Document
	for _, key := range append(document.Customization.Layout.Sections.Main, document.Customization.Layout.Sections.Sidebar...) {
		if section := document.Content[key]; section.SectionType == "project" {
			for _, item := range section.Entries {
				if linked[item.ID] {
					visible = append(visible, item.ID)
				}
			}
		}
	}
	if !slices.Equal(got, visible) {
		t.Fatalf("linked entries %v, want each once among the visible project entries in layout order %v", got, visible)
	}
}

func TestResumeLinksCasesKeepTheirPurpose(t *testing.T) {
	requests := map[string]directrender.PublicRenderRequest{}
	for _, source := range resumeLinksSources() {
		request := resumeLinksRequest(t, readResumeLinksSnapshot(t, source))
		requireEntryPointRules(t, encodeCorpusJSON(t, request, ""))
		requests[source.name] = request
	}

	two := requests["en-two-linked-entries"]
	entries := projectEntries(two)
	if len(entries) != 2 || len(two.ProjectLinks) != 2 || entries[0].Link == nil || entries[1].Link != nil {
		t.Fatalf("en-two-linked-entries: want two linked entries, the first with its own link and the second without; got %+v", entries)
	}

	hidden := requests["en-hidden-linked-entry"]
	hiddenID := entries[1].ID
	if len(projectEntries(hidden)) != 1 || len(hidden.ProjectLinks) != 1 || slices.Contains(linkedIDs(hidden), hiddenID) {
		t.Fatalf("en-hidden-linked-entry: want only the visible entry linked; got %+v", hidden.ProjectLinks)
	}

	none := requests["en-no-available-project"]
	if len(projectEntries(none)) == 0 || none.ProjectLinks != nil || none.ProjectsURL != "https://aboutme.example/do-hoang-nam/projects" {
		t.Fatalf("en-no-available-project: want a project section, projectsUrl and no projectLinks; got %+v %q", none.ProjectLinks, none.ProjectsURL)
	}

	vi := requests["vi-linked-entries"]
	if vi.PublicResume.Lng != "vi" || vi.Preview.Locale != "vi_VN" || len(vi.ProjectLinks) != 2 {
		t.Fatalf("vi-linked-entries: want a Vietnamese resume with two links; got lng %q, locale %q, %+v", vi.PublicResume.Lng, vi.Preview.Locale, vi.ProjectLinks)
	}
}

func projectEntries(request directrender.PublicRenderRequest) []publicresume.PublicProjectEntry {
	return request.PublicResume.Document.Content["project"].ProjectEntries
}

func linkedIDs(request directrender.PublicRenderRequest) []string {
	var ids []string
	for _, link := range request.ProjectLinks {
		ids = append(ids, link.EntryID)
	}
	return ids
}
