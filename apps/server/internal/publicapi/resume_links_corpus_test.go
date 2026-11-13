package publicapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/previewcard"
	"danny.vn/aboutme/apps/server/internal/printsnapshot"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata/resume-links-render.json and ../printsnapshot/testdata/print-envelopes.json from the current code")

const (
	resumeLinksCorpusPath = "testdata/resume-links-render.json"
	printCorpusPath       = "../printsnapshot/testdata/print-envelopes.json"
)

type resumeLinksCorpus struct {
	StyleVersion string            `json:"styleVersion,omitempty"`
	Cases        []resumeLinksCase `json:"cases"`
}

type resumeLinksCase struct {
	Name     string          `json:"name"`
	Envelope json.RawMessage `json:"envelope"`
	HTML     string          `json:"html,omitempty"`
}

type resumeLinksSource struct {
	name, sample, lng string
	revision          int64
	discoverable      bool
	download          bool
	linked            []string
	prepare           func(entries []schema.ProjectEntry)
	print             bool
}

func resumeLinksSources() []resumeLinksSource {
	hidden := true
	return []resumeLinksSource{
		{
			name: "en-two-linked-entries", sample: "engineer-compact.en.json", lng: "en", revision: 7, discoverable: true, download: true,
			linked:  []string{"vn-address-kit", "nextjs-perf-checklist"},
			prepare: func(entries []schema.ProjectEntry) { entries[1].Link = nil },
			print:   true,
		},
		{
			name: "en-hidden-linked-entry", sample: "engineer-compact.en.json", lng: "en", revision: 8, discoverable: true, download: true,
			linked:  []string{"vn-address-kit", "nextjs-perf-checklist"},
			prepare: func(entries []schema.ProjectEntry) { entries[1].IsHidden = &hidden },
		},
		{
			name: "en-no-available-project", sample: "engineer-compact.en.json", lng: "en", revision: 9, discoverable: true, download: true,
			print: true,
		},
		{
			name: "vi-linked-entries", sample: "engineer-compact.vi.json", lng: "vi", revision: 12,
			linked: []string{"vn-address-kit", "nextjs-perf-checklist"},
		},
	}
}

func resumeLinksSourceDocument(t *testing.T, source resumeLinksSource) schema.Resume {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packages", "schema", "samples", source.sample))
	if err != nil {
		t.Fatal(err)
	}
	var document schema.Resume
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	section, ok := document.Content["project"]
	if !ok || section.SectionType != schema.Project || len(section.ProjectEntries) < len(source.linked) {
		t.Fatalf("sample %s has no project section with %d entries", source.sample, len(source.linked))
	}
	for index, slug := range source.linked {
		if slug != "" {
			projectID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("project:"+slug)).String()
			section.ProjectEntries[index].ProjectID = &projectID
		}
	}
	if source.prepare != nil {
		source.prepare(section.ProjectEntries)
	}
	document.Content["project"] = section
	return document
}

func statementLinkContext(t *testing.T, document schema.Resume, source resumeLinksSource) json.RawMessage {
	t.Helper()
	type storedLink struct {
		EntryID     string `json:"entryId"`
		ProjectSlug string `json:"projectSlug"`
	}
	links := []storedLink{}
	for index, entry := range document.Content["project"].ProjectEntries {
		if index < len(source.linked) && source.linked[index] != "" && entry.ProjectID != nil && (entry.IsHidden == nil || !*entry.IsHidden) {
			links = append(links, storedLink{EntryID: entry.ID, ProjectSlug: source.linked[index]})
		}
	}
	raw, err := json.Marshal(links)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func readResumeLinksSnapshot(t *testing.T, source resumeLinksSource) publicresume.Snapshot {
	t.Helper()
	document := resumeLinksSourceDocument(t, source)
	parts := make([]json.RawMessage, 3)
	for index, part := range []any{document.PersonalDetails, document.Content, document.Customization} {
		var err error
		if parts[index], err = json.Marshal(part); err != nil {
			t.Fatal(err)
		}
	}
	slug, lng := "do-hoang-nam", source.lng
	backing := &publicServiceStore{
		row: store.Resume{ID: uuid.MustParse("6f1d7c52-8f0e-4a43-9a51-7d3c2b1e0a11"), Slug: &slug, Live: true, DownloadEnabled: source.download,
			SEOGeoEnabled: source.discoverable, Revision: source.revision, Lng: &lng, SchemaVersion: int32(document.SchemaVersion),
			PersonalDetails: parts[0], Content: parts[1], Customization: parts[2]},
		links: statementLinkContext(t, document, source),
	}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{Store: backing, Projector: docmigrate.NewIdentityProjector(), Coordinator: coordinator, Origin: mustPublicOrigin(t)})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, lease, err := reader.ReadResume(context.Background(), slug, publicstate.RepresentationHTML)
	if err != nil {
		t.Fatalf("ReadResume: %v", err)
	}
	lease.Release()
	return snapshot
}

func resumeLinksRequest(t *testing.T, snapshot publicresume.Snapshot) directrender.PublicRenderRequest {
	t.Helper()
	origin := mustPublicOrigin(t)
	page := expectedPublicPage(snapshot.Public, snapshot.PublicTitle, snapshot.FaviconEmoji)
	page.Preview.ImageURL = origin.Resolve(previewcard.Path(snapshot.Public.Slug, "0123456789abcdef"))
	return withEntryPoints(publicRenderRequest(snapshot.Public, snapshot.DiscoveryEnabled, page, origin), snapshot.ProjectLinks, origin)
}

func withEntryPoints(request directrender.PublicRenderRequest, links []publicresume.ProjectLink, origin publicresume.PublicOrigin) directrender.PublicRenderRequest {
	for _, link := range links {
		request.ProjectLinks = append(request.ProjectLinks, directrender.EntryProjectLink{EntryID: link.EntryID, URL: link.URL})
	}
	request.ProjectsURL = origin.Resolve("/" + request.PublicResume.Slug + "/projects")
	return request
}

func printEnvelope(t *testing.T, snapshot publicresume.Snapshot) []byte {
	t.Helper()
	envelope, err := printsnapshot.FromPublic(snapshot, nil, "")
	if err != nil {
		t.Fatalf("FromPublic: %v", err)
	}
	envelope, err = printsnapshot.WithProjectLinks(envelope, mustPublicOrigin(t), snapshot.Public.Slug, snapshot.ProjectLinks)
	if err != nil {
		t.Fatalf("WithProjectLinks: %v", err)
	}
	encoded, err := printsnapshot.Marshal(envelope)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return encoded
}

func encodeCorpusJSON(t *testing.T, value any, indent string) []byte {
	t.Helper()
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if indent != "" {
		encoder.SetIndent("", indent)
	}
	if err := encoder.Encode(value); err != nil {
		t.Fatal(err)
	}
	if indent == "" {
		return bytes.TrimSuffix(out.Bytes(), []byte("\n"))
	}
	return out.Bytes()
}

func encodePrintCorpus(t *testing.T, names []string, envelopes [][]byte) []byte {
	t.Helper()
	var out bytes.Buffer
	out.WriteString("{\n  \"cases\": [\n")
	for index, envelope := range envelopes {
		name, err := json.Marshal(names[index])
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&out, "    {\n      \"name\": %s,\n      \"envelope\": %s\n    }", name, envelope)
		if index < len(envelopes)-1 {
			out.WriteByte(',')
		}
		out.WriteByte('\n')
	}
	out.WriteString("  ]\n}\n")
	return out.Bytes()
}

func readCorpusFile(t *testing.T, path string) []byte {
	t.Helper()
	current, err := os.ReadFile(path)
	if err != nil && (!errors.Is(err, fs.ErrNotExist) || !*updateGolden) {
		t.Fatalf("read corpus: %v", err)
	}
	return current
}

func TestResumeLinksRenderCorpusIsCurrent(t *testing.T) {
	currentResume := readCorpusFile(t, resumeLinksCorpusPath)
	var existing resumeLinksCorpus
	if currentResume != nil {
		if err := json.Unmarshal(currentResume, &existing); err != nil {
			t.Fatal(err)
		}
	}
	html := map[string]string{}
	for _, entry := range existing.Cases {
		html[entry.Name] = entry.HTML
	}
	generated := resumeLinksCorpus{StyleVersion: existing.StyleVersion}
	var printNames []string
	var printEnvelopes [][]byte
	for _, source := range resumeLinksSources() {
		snapshot := readResumeLinksSnapshot(t, source)
		envelope := encodeCorpusJSON(t, resumeLinksRequest(t, snapshot), "")
		generated.Cases = append(generated.Cases, resumeLinksCase{Name: source.name, Envelope: envelope, HTML: html[source.name]})
		if source.print {
			printNames = append(printNames, source.name)
			printEnvelopes = append(printEnvelopes, printEnvelope(t, snapshot))
		}
	}
	files := map[string][2][]byte{
		resumeLinksCorpusPath: {currentResume, encodeCorpusJSON(t, generated, "  ")},
		printCorpusPath:       {readCorpusFile(t, printCorpusPath), encodePrintCorpus(t, printNames, printEnvelopes)},
	}
	for path, file := range files {
		if *updateGolden {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, file[1], 0o644); err != nil {
				t.Fatal(err)
			}
		} else if !bytes.Equal(file[0], file[1]) {
			t.Errorf("%s is stale; after reviewing the change run: go test ./internal/publicapi -run '^TestResumeLinksRenderCorpusIsCurrent$' -update", path)
		}
	}
}
