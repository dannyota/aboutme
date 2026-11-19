package publicresume

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"danny.vn/aboutme/apps/server/internal/store"
)

type evidenceReaderStore struct {
	*readerStore
	withEvidence []bool
}

func (s *evidenceReaderStore) GetPublicResumeProject(ctx context.Context, arg store.GetPublicResumeProjectParams) (store.GetPublicResumeProjectRow, error) {
	s.withEvidence = append(s.withEvidence, arg.WithEvidence)
	return s.readerStore.GetPublicResumeProject(ctx, arg)
}

func newEvidenceTestReader(t *testing.T, readStore store.PublicReadQueries, evidence bool) *Reader {
	t.Helper()
	base := newTestReader(t, &readerStore{})
	reader, err := NewReader(ReaderDependencies{Store: readStore, Projector: base.projector, Coordinator: base.coordinator, Origin: base.origin, Evidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	return reader
}

func evidenceItem(kind, title, url string) map[string]any {
	return map[string]any{
		"kind": kind, "title": title, "url": url, "repositoryName": "octocat/hello",
		"itemDate": "2026-01-01T06:30:00.123456+07:00", "checkedAt": "2026-11-02T08:00:00+00:00", "confirmed": true,
	}
}

func evidenceColumn(t *testing.T, rows ...[3]any) json.RawMessage {
	t.Helper()
	column := []map[string]any{}
	for _, row := range rows {
		slug, isString := row[1].(string)
		if !isString {
			t.Fatalf("row %v has no project slug", row)
		}
		document := map[string]any{"defaultLanguage": "en", "text": map[string]any{"en": map[string]string{"name": slug}}}
		entry := map[string]any{"entryId": row[0], "projectSlug": slug, "document": document}
		if row[2] != nil {
			entry["evidence"] = row[2]
		}
		column = append(column, entry)
	}
	raw, err := json.Marshal(column)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func validEvidence() []any {
	return []any{
		evidenceItem("release", "Release v1", "https://github.com/octocat/hello/releases/tag/v1.0%2Bbuild"),
		evidenceItem("repository", "octocat/hello", "https://github.com/octocat/hello"),
		evidenceItem("pullRequest", "Fix the parser", "https://github.com/octocat/hello/pull/42"),
	}
}

func evidencePage(t *testing.T, evidence any) (*evidenceReaderStore, map[string]string) {
	t.Helper()
	ids := projectPagesIDs()
	return &evidenceReaderStore{readerStore: &readerStore{
		row:      projectPagesRow(t, "ada", ids),
		links:    projectColumn(t, ids["first"], "first-tool", ids["second"], "second-tool"),
		projects: evidenceColumn(t, [3]any{ids["second"], "second-tool", evidence}),
	}}, ids
}

func TestReaderReadProjects_PageDecodesTheEvidenceInOrder(t *testing.T) {
	readStore, _ := evidencePage(t, validEvidence())
	_, pages := readProjectPages(t, newEvidenceTestReader(t, readStore, true), "second-tool")
	itemDate := time.Date(2025, time.December, 31, 23, 30, 0, 123456000, time.UTC)
	checkedAt := time.Date(2026, time.November, 2, 8, 0, 0, 0, time.UTC)
	want := []PublicEvidence{
		{Kind: "release", Title: "Release v1", URL: "https://github.com/octocat/hello/releases/tag/v1.0%2Bbuild", RepositoryName: "octocat/hello", ItemDate: itemDate, CheckedAt: checkedAt, Confirmed: true},
		{Kind: "repository", Title: "octocat/hello", URL: "https://github.com/octocat/hello", RepositoryName: "octocat/hello", ItemDate: itemDate, CheckedAt: checkedAt, Confirmed: true},
		{Kind: "pullRequest", Title: "Fix the parser", URL: "https://github.com/octocat/hello/pull/42", RepositoryName: "octocat/hello", ItemDate: itemDate, CheckedAt: checkedAt, Confirmed: true},
	}
	if len(pages.Projects) != 1 {
		t.Fatalf("projects = %+v, want the requested one", pages.Projects)
	}
	got := pages.Projects[0].Evidence
	for i := range got {
		if !got[i].ItemDate.Equal(want[i].ItemDate) || !got[i].CheckedAt.Equal(want[i].CheckedAt) {
			t.Fatalf("item %d instants = %s, %s; want %s, %s", i, got[i].ItemDate, got[i].CheckedAt, want[i].ItemDate, want[i].CheckedAt)
		}
		got[i].ItemDate, got[i].CheckedAt = want[i].ItemDate, want[i].CheckedAt
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("evidence = %+v, want %+v", got, want)
	}
	if !reflect.DeepEqual(readStore.withEvidence, []bool{true}) {
		t.Fatalf("statement evidence argument = %v, want [true]", readStore.withEvidence)
	}
}

func TestReaderReadProjects_DecodesOnlyTheRequestedProjectsEvidence(t *testing.T) {
	ids := projectPagesIDs()
	malformed := []any{evidenceItem("gist", "Not evidence", "https://gist.github.com/x")}
	readStore := &readerStore{
		row:   projectPagesRow(t, "ada", ids),
		links: evidenceColumn(t, [3]any{ids["first"], "first-tool", malformed}, [3]any{ids["second"], "second-tool", malformed}),
		projects: evidenceColumn(t, [3]any{ids["first"], "first-tool", "not an array"},
			[3]any{ids["second"], "second-tool", validEvidence()}),
	}
	_, pages := readProjectPages(t, newEvidenceTestReader(t, readStore, true), "second-tool")
	if len(pages.Projects) != 1 || len(pages.Projects[0].Evidence) != 3 || pages.Available != 2 {
		t.Fatalf("pages = %+v, want the requested project's three items and two available", pages)
	}
	_, list := readProjectPages(t, newEvidenceTestReader(t, &readerStore{row: readStore.row, projects: readStore.projects}, true), "")
	for _, listed := range list.Projects {
		if listed.Evidence != nil {
			t.Fatalf("list project %s = %+v, want no evidence decoded", listed.Slug, listed.Evidence)
		}
	}
}

func TestReaderReadProjects_MalformedEvidenceIsUnavailable(t *testing.T) {
	item := func(change func(map[string]any)) []any {
		items := validEvidence()
		second, isObject := items[1].(map[string]any)
		if !isObject {
			t.Fatal("the second item is not an object")
		}
		change(second)
		return items
	}
	set := func(key string, value any) []any { return item(func(m map[string]any) { m[key] = value }) }
	eleven := []any{}
	for range 11 {
		eleven = append(eleven, evidenceItem("repository", "octocat/hello", "https://github.com/octocat/hello"))
	}
	long := "https://github.com/octocat/" + strings.Repeat("a", 300-len("https://github.com/octocat/")+1)
	for name, evidence := range map[string]any{
		"absent":                    nil,
		"null":                      json.RawMessage(`null`),
		"not an array":              map[string]any{},
		"eleven items":              eleven,
		"unknown kind":              set("kind", "gist"),
		"empty kind":                set("kind", ""),
		"repository url of a pull":  set("url", "https://github.com/octocat/hello/pull/42"),
		"pull url for a repository": item(func(m map[string]any) { m["kind"] = "pullRequest" }),
		"release url without a tag": item(func(m map[string]any) {
			m["kind"], m["url"] = "release", "https://github.com/octocat/hello/releases/tag/"
		}),
		"pull number zero":              item(func(m map[string]any) { m["kind"], m["url"] = "pullRequest", "https://github.com/octocat/hello/pull/0" }),
		"http":                          set("url", "http://github.com/octocat/hello"),
		"another host":                  set("url", "https://gitlab.com/octocat/hello"),
		"www host":                      set("url", "https://www.github.com/octocat/hello"),
		"trailing slash":                set("url", "https://github.com/octocat/hello/"),
		"query":                         set("url", "https://github.com/octocat/hello?tab=readme"),
		"fragment":                      set("url", "https://github.com/octocat/hello#readme"),
		"space in a segment":            set("url", "https://github.com/octo cat/hello"),
		"url over 300 code points":      set("url", long),
		"empty title":                   set("title", ""),
		"title over 200 code points":    set("title", strings.Repeat("é", 201)),
		"empty repository name":         set("repositoryName", ""),
		"repository over 200":           set("repositoryName", strings.Repeat("a", 201)),
		"title not a string":            set("title", 1),
		"confirmed not a boolean":       set("confirmed", "true"),
		"item date not an instant":      set("itemDate", "yesterday"),
		"item date without an offset":   set("itemDate", "2026-01-01T06:30:00"),
		"checked date absent":           item(func(m map[string]any) { delete(m, "checkedAt") }),
		"checked date beyond year 9999": set("checkedAt", "9999-12-31T23:00:00-07:00"),
	} {
		t.Run(name, func(t *testing.T) {
			readStore, _ := evidencePage(t, evidence)
			_, read, lease, err := newEvidenceTestReader(t, readStore, true).ReadProjects(context.Background(), "ada", "second-tool")
			if err != nil || lease == nil {
				t.Fatalf("ReadProjects() error = %v, lease %v; want the resume admitted before evidence is decoded", err, lease)
			}
			lease.Release()
			if _, err := read.Pages(); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("Pages() error = %v, want ErrUnavailable", err)
			}
		})
	}
}

func TestReaderReadProjects_EvidenceAtEveryBound(t *testing.T) {
	ten := []any{}
	for range 10 {
		ten = append(ten, map[string]any{
			"kind": "release", "title": strings.Repeat("😀", 200), "repositoryName": strings.Repeat("é", 200),
			"url":      "https://github.com/octocat/hello/releases/tag/" + strings.Repeat("t", 300-len("https://github.com/octocat/hello/releases/tag/")),
			"itemDate": "0001-01-01T00:00:00Z", "checkedAt": "9999-12-31T23:59:59Z", "confirmed": false,
		})
	}
	readStore, _ := evidencePage(t, ten)
	_, pages := readProjectPages(t, newEvidenceTestReader(t, readStore, true), "second-tool")
	if len(pages.Projects) != 1 || len(pages.Projects[0].Evidence) != 10 {
		t.Fatalf("pages = %+v, want ten items at every bound", pages)
	}
}

func TestReaderReadProjects_EvidenceFollowsTheReaderFlag(t *testing.T) {
	readStore, _ := evidencePage(t, validEvidence())
	_, pages := readProjectPages(t, newEvidenceTestReader(t, readStore, false), "second-tool")
	if !reflect.DeepEqual(readStore.withEvidence, []bool{false}) {
		t.Fatalf("statement evidence argument = %v, want [false]", readStore.withEvidence)
	}
	if len(pages.Projects) != 1 || pages.Projects[0].Evidence == nil || len(pages.Projects[0].Evidence) != 0 {
		t.Fatalf("evidence with the capability off = %#v, want an empty list", pages.Projects)
	}
	_, pages = readProjectPages(t, newTestReader(t, &readerStore{row: readStore.row, links: readStore.links, projects: readStore.projects}), "second-tool")
	if len(pages.Projects[0].Evidence) != 0 {
		t.Fatalf("a reader built without the flag decoded evidence: %+v", pages.Projects[0].Evidence)
	}
}
