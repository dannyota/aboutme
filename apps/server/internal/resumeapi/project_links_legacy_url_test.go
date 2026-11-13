package resumeapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func insertLegacyProject(t *testing.T, h *resumeAPITestHarness, slug, repositoryURL string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.pool.QueryRow(h.ctx, `INSERT INTO projects (user_id, slug, document) VALUES ($1, $2, jsonb_build_object(
		'defaultLanguage', 'en', 'text', jsonb_build_object('en', jsonb_build_object('name', 'Legacy')), 'repositoryUrl', $3::text))
		RETURNING id`, h.userID, slug, repositoryURL).Scan(&id); err != nil {
		t.Fatalf("insert legacy project: %v", err)
	}
	return id
}

func setLegacyRepository(t *testing.T, h *resumeAPITestHarness, id uuid.UUID, repositoryURL string) {
	t.Helper()
	if _, err := h.pool.Exec(h.ctx, `UPDATE projects SET document = jsonb_set(document, '{repositoryUrl}', to_jsonb($2::text)) WHERE id = $1`,
		id, repositoryURL); err != nil {
		t.Fatalf("update legacy project: %v", err)
	}
}

func requireEntryLink(t *testing.T, h *resumeAPITestHarness, resumeID uuid.UUID, want *string) {
	t.Helper()
	got := onlyProjectEntry(t, h, resumeID, "projects").Link
	switch {
	case want == nil && got != nil:
		t.Fatalf("entry link = %q, want none", *got)
	case want != nil && (got == nil || *got != *want):
		t.Fatalf("entry link = %v, want %q", got, *want)
	}
}

func TestProjectLinks_LegacyLibraryLinksPrefillWithinTheEntryContract(t *testing.T) {
	h := newResumeAPITestHarness(t)
	tooLong := "https://example.com/" + strings.Repeat("a", 2049-len("https://example.com/"))
	upper := insertLegacyProject(t, h, "legacy-upper", "HTTPS://Example.COM/atlas")
	long := insertLegacyProject(t, h, "legacy-long", tooLong)

	first := createResumeWithLanguage(t, h, nil, loadMinimalDocument(t))
	if response := linkProject(t, h, first.ID, upper, first.Revision); response.status != http.StatusOK {
		t.Fatalf("link uppercase scheme = %d %s, want 200", response.status, response.body)
	}
	requireEntryLink(t, h, first.ID, stringPointer("https://Example.COM/atlas"))

	second := createResumeWithLanguage(t, h, nil, loadMinimalDocument(t))
	if response := linkProject(t, h, second.ID, long, second.Revision); response.status != http.StatusOK {
		t.Fatalf("link 2049 character URL = %d %s, want 200", response.status, response.body)
	}
	requireEntryLink(t, h, second.ID, nil)

}

func TestProjectLinks_LegacyLibraryLinksRefreshWithinTheEntryContract(t *testing.T) {
	h := newResumeAPITestHarness(t)
	tooLong := "https://example.com/" + strings.Repeat("a", 2049-len("https://example.com/"))
	legacy := insertLegacyProject(t, h, "legacy-refresh", "https://example.com/atlas")
	created := createResumeWithLanguage(t, h, nil, loadMinimalDocument(t))
	if response := linkProject(t, h, created.ID, legacy, created.Revision); response.status != http.StatusOK {
		t.Fatalf("link = %d %s, want 200", response.status, response.body)
	}
	entry := onlyProjectEntry(t, h, created.ID, "projects")
	refresh := func(revision int64) {
		t.Helper()
		response := linkRouteRequest(t, h, http.MethodPost,
			apiResumePath+"/"+created.ID.String()+"/entries/"+entry.ID+"/project-refresh", "", revision, "")
		if response.status != http.StatusOK {
			t.Fatalf("refresh = %d %s, want 200", response.status, response.body)
		}
	}
	setLegacyRepository(t, h, legacy, tooLong)
	refresh(2)
	requireEntryLink(t, h, created.ID, nil)
	setLegacyRepository(t, h, legacy, "HTTPS://Example.COM/renamed")
	refresh(3)
	requireEntryLink(t, h, created.ID, stringPointer("https://Example.COM/renamed"))
}

func TestProjectLinks_SavedHostKeepsItsBytesAndLinks(t *testing.T) {
	h := newResumeAPITestHarness(t)
	for _, tc := range []struct {
		slug, given string
		want        *string
	}{
		{"idn-host", "https://İ.example/Atlas", stringPointer("https://İ.example/Atlas")},
		{"upper-host", "HTTPS://Example.COM/Path?Q=A", stringPointer("https://Example.COM/Path?Q=A")},
	} {
		document := linkProjectDocument(tc.slug)
		document.RepositoryURL = tc.given
		saved := createLinkProject(t, h, h.userID, tc.slug, document)
		created := createResumeWithLanguage(t, h, nil, loadMinimalDocument(t))
		if response := linkProject(t, h, created.ID, saved.ID, created.Revision); response.status != http.StatusOK {
			t.Fatalf("link %s = %d %s, want 200", tc.given, response.status, response.body)
		}
		got := onlyProjectEntry(t, h, created.ID, "projects").Link
		if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
			t.Fatalf("link %s prefilled entry link %v, want %v", tc.given, got, tc.want)
		}
	}
}
