package publicapi

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/publicresume"
)

func evidenceProject(slug string, evidence ...publicresume.PublicEvidence) publicresume.PublicProject {
	available := smallProject(slug, 1, nil)
	available.Evidence = evidence
	return available
}

func evidenceAt(kind, title, url string, confirmed bool) publicresume.PublicEvidence {
	return publicresume.PublicEvidence{
		Kind: kind, Title: title, URL: url, RepositoryName: "octocat/hello", Confirmed: confirmed,
		ItemDate: time.Date(2024, time.May, 6, 7, 8, 9, 0, time.UTC), CheckedAt: time.Date(2026, time.November, 2, 8, 0, 0, 0, time.UTC),
	}
}

func TestBuildProjectRequest_CopiesEvidenceInOrder(t *testing.T) {
	items := []publicresume.PublicEvidence{
		evidenceAt(project.EvidenceRelease, "Release v1", "https://github.com/octocat/hello/releases/tag/v1", true),
		evidenceAt(project.EvidenceRepository, "octocat/hello", "https://github.com/octocat/hello", false),
		evidenceAt(project.EvidencePullRequest, "Fix the parser", "https://github.com/octocat/hello/pull/42", true),
	}
	page, err := buildProjectRequest(projectTestInput(t, projectTestSnapshot("en", "Ada", nil, schema.MmYyyy, false), 1, evidenceProject("engine", items...)))
	if err != nil {
		t.Fatal(err)
	}
	want := []directrender.ProjectEvidence{
		{Kind: "release", Title: "Release v1", URL: "https://github.com/octocat/hello/releases/tag/v1", RepositoryName: "octocat/hello", ItemDate: "2024-05-06T07:08:09Z", CheckedAt: "2026-11-02T08:00:00Z", Confirmed: true},
		{Kind: "repository", Title: "octocat/hello", URL: "https://github.com/octocat/hello", RepositoryName: "octocat/hello", ItemDate: "2024-05-06T07:08:09Z", CheckedAt: "2026-11-02T08:00:00Z"},
		{Kind: "pullRequest", Title: "Fix the parser", URL: "https://github.com/octocat/hello/pull/42", RepositoryName: "octocat/hello", ItemDate: "2024-05-06T07:08:09Z", CheckedAt: "2026-11-02T08:00:00Z", Confirmed: true},
	}
	if !reflect.DeepEqual(page.Evidence, want) {
		t.Fatalf("evidence = %+v, want %+v", page.Evidence, want)
	}
	encoded, isList := envelopeMap(t, page)["evidence"].([]any)
	first, isObject := encoded[0].(map[string]any)
	if !isList || !isObject {
		t.Fatal("the envelope has no evidence item")
	}
	keys := []string{}
	for key := range first {
		keys = append(keys, key)
	}
	if len(keys) != 7 {
		t.Fatalf("evidence keys = %v, want kind, title, url, repositoryName, itemDate, checkedAt and confirmed only", keys)
	}
}

func TestBuildProjectRequest_WritesEvidenceInstantsAsUTCSeconds(t *testing.T) {
	item := evidenceAt(project.EvidencePullRequest, "Fix", "https://github.com/octocat/hello/pull/1", true)
	plus7 := time.FixedZone("ICT", 7*60*60)
	item.ItemDate = time.Date(2026, time.January, 1, 6, 30, 0, 999_999_999, plus7)
	item.CheckedAt = time.Date(2026, time.March, 1, 0, 0, 1, 500_000_000, plus7)
	page, err := buildProjectRequest(projectTestInput(t, projectTestSnapshot("en", "Ada", nil, schema.MmYyyy, false), 1, evidenceProject("engine", item)))
	if err != nil {
		t.Fatal(err)
	}
	if got := page.Evidence[0]; got.ItemDate != "2025-12-31T23:30:00Z" || got.CheckedAt != "2026-02-28T17:00:01Z" {
		t.Fatalf("instants = %q, %q; want 2025-12-31T23:30:00Z and 2026-02-28T17:00:01Z", got.ItemDate, got.CheckedAt)
	}
}

func TestBuildProjectRequest_EmptyEvidenceEncodesAsAnEmptyArray(t *testing.T) {
	for name, evidence := range map[string][]publicresume.PublicEvidence{"none stored": {}, "capability off": nil} {
		available := evidenceProject("engine")
		available.Evidence = evidence
		page, err := buildProjectRequest(projectTestInput(t, projectTestSnapshot("en", "Ada", nil, schema.MmYyyy, false), 1, available))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(page)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `"evidence":[]`) {
			t.Fatalf("%s: envelope %s, want \"evidence\":[]", name, raw)
		}
	}
}

func TestBuildProjectRequest_TurnsUnsafeEvidenceCharactersIntoSpaces(t *testing.T) {
	item := evidenceAt(project.EvidenceRepository, "a\u0007b\u200bc\u2028d\u2029e\u00adf", "https://github.com/octocat/hello", true)
	item.RepositoryName = "octo\u0000cat\u2060/\u2028hello\u2029\u0085"
	page, err := buildProjectRequest(projectTestInput(t, projectTestSnapshot("en", "Ada", nil, schema.MmYyyy, false), 1, evidenceProject("engine", item)))
	if err != nil {
		t.Fatal(err)
	}
	if got := page.Evidence[0]; got.Title != "a b c d e f" || got.RepositoryName != "octo cat / hello  " {
		t.Fatalf("title %q, repository %q; want each Cc, Cf, Zl and Zp character as a space", got.Title, got.RepositoryName)
	}
	if broken := decodeProjectEnvelopeRules(t, page); len(broken) != 0 {
		t.Fatalf("the cleaned envelope breaks the decoder copy: %q", broken)
	}
}

func TestBuildProjectListRequest_CarriesNoEvidence(t *testing.T) {
	input := projectTestInput(t, projectTestSnapshot("en", "Ada", nil, schema.MmYyyy, false), 2,
		evidenceProject("engine", evidenceAt(project.EvidenceRepository, "Visible", "https://github.com/octocat/hello", true)), smallProject("other", 0, nil))
	list, err := buildProjectListRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "evidence") || strings.Contains(string(raw), "Visible") {
		t.Fatalf("list envelope %s carries evidence", raw)
	}
}

func TestBuildProjectRequest_EvidenceLeavesThePreviewUnchanged(t *testing.T) {
	snapshot := projectTestSnapshot("en", "Ada", nil, schema.MmYyyy, false)
	plain, err := buildProjectRequest(projectTestInput(t, snapshot, 1, evidenceProject("engine")))
	if err != nil {
		t.Fatal(err)
	}
	with, err := buildProjectRequest(projectTestInput(t, snapshot, 1, evidenceProject("engine",
		evidenceAt(project.EvidencePullRequest, "Evidence title", "https://github.com/octocat/hello/pull/9", true))))
	if err != nil {
		t.Fatal(err)
	}
	if with.Preview != plain.Preview || with.PageTitle != plain.PageTitle {
		t.Fatalf("preview with evidence = %+v %q, want %+v %q", with.Preview, with.PageTitle, plain.Preview, plain.PageTitle)
	}
}
