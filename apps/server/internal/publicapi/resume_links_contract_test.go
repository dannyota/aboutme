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

// strictShape returns a new value of a struct type with typ's fields and
// tags, except that the field named raw holds its JSON undecoded. The public
// resume types encode but have no decoder, so a strict decode reads every
// other field into its Go type and the golden comparison covers the raw one.
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

// TestResumeLinksRenderCorpusMatchesRequestType proves each envelope decodes
// into PublicRenderRequest's fields with no unknown field, encodes back to the
// same JSON, and keeps the entry point rules: both fields or neither,
// projectsUrl at the resume's /projects, and each link one project page
// under it.
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

			var request struct {
				CanonicalOrigin string `json:"canonicalOrigin"`
				PublicResume    struct {
					Slug string `json:"slug"`
				} `json:"publicResume"`
				ProjectLinks []directrender.EntryProjectLink `json:"projectLinks"`
				ProjectsURL  string                          `json:"projectsUrl"`
			}
			if err := json.Unmarshal(entry.Envelope, &request); err != nil {
				t.Fatal(err)
			}
			if (len(request.ProjectLinks) == 0) != (request.ProjectsURL == "") {
				t.Fatalf("projectLinks %v and projectsUrl %q are not present together", request.ProjectLinks, request.ProjectsURL)
			}
			if request.ProjectsURL == "" {
				return
			}
			if want := request.CanonicalOrigin + "/" + request.PublicResume.Slug + "/projects"; request.ProjectsURL != want {
				t.Fatalf("projectsUrl = %q, want %q", request.ProjectsURL, want)
			}
			entries, urls := map[string]bool{}, map[string]bool{}
			for _, link := range request.ProjectLinks {
				slug, under := strings.CutPrefix(link.URL, request.ProjectsURL+"/")
				if !under || !project.ValidSlug(slug) || urls[link.URL] || link.EntryID == "" || entries[link.EntryID] {
					t.Fatalf("link %+v is not one entry's own project page under %q", link, request.ProjectsURL)
				}
				entries[link.EntryID], urls[link.URL] = true, true
			}
		})
	}
}

// TestResumeLinksCasesFollowTheEntryPointRules proves, on the values the
// corpus is generated from, that the links are the visible linked project
// entries in layout and entry order, and pins what each case is for, so a
// regenerated corpus cannot quietly lose one.
func TestResumeLinksCasesFollowTheEntryPointRules(t *testing.T) {
	requests := map[string]directrender.PublicRenderRequest{}
	for _, source := range resumeLinksSources() {
		request := resumeLinksRequest(t, readResumeLinksSnapshot(t, source))
		requests[source.name] = request
		stored := resumeLinksSourceDocument(t, source).Content["project"].ProjectEntries
		linked := map[string]bool{}
		for index, slug := range source.linked {
			linked[stored[index].ID] = slug != ""
		}
		// The public document holds only visible entries, so walking it in
		// layout and entry order gives the links the contract expects.
		document := request.PublicResume.Document
		var visible []string
		for _, keys := range [][]string{document.Customization.Layout.Sections.Main, document.Customization.Layout.Sections.Sidebar} {
			for _, key := range keys {
				for _, entry := range document.Content[key].ProjectEntries {
					if linked[entry.ID] {
						visible = append(visible, entry.ID)
					}
				}
			}
		}
		if got := linkedIDs(request); !slices.Equal(got, visible) {
			t.Fatalf("%s: linked entries %v, want the visible linked entries in layout order %v", source.name, got, visible)
		}
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
	if len(projectEntries(none)) == 0 || none.ProjectLinks != nil || none.ProjectsURL != "" {
		t.Fatalf("en-no-available-project: want a project section and neither field; got %+v %q", none.ProjectLinks, none.ProjectsURL)
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
