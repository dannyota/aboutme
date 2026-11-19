package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/store"
)

func createDescribedPublicStoreProject(ctx context.Context, t *testing.T, tx pgx.Tx, userID uuid.UUID, slug string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO projects (user_id, slug, document)
		VALUES ($1, $2, jsonb_build_object(
			'defaultLanguage', 'en',
			'technologies', jsonb_build_array('Go'),
			'text', jsonb_build_object(
				'en', jsonb_build_object('name', $2::text, 'summary', 'Summary', 'description', '<p>English</p>'),
				'vi', jsonb_build_object('name', $2::text, 'description', '<p>Tiếng Việt</p>'))))
		RETURNING id
	`, userID, slug).Scan(&id); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	return id
}

type publicProjectsFixture struct {
	slug     string
	ids      []uuid.UUID
	entryIDs map[string]string
}

func newPublicProjectsFixture(ctx context.Context, t *testing.T, tx pgx.Tx) publicProjectsFixture {
	t.Helper()
	owner := createPublicStoreUser(ctx, t, tx)
	other := createPublicStoreUser(ctx, t, tx)
	atlas := createDescribedPublicStoreProject(ctx, t, tx, owner, "atlas")
	quiet := createDescribedPublicStoreProject(ctx, t, tx, owner, "quiet")
	foreign := createDescribedPublicStoreProject(ctx, t, tx, other, "foreign")
	entries := map[string]string{}
	for _, name := range []string{"linked", "foreign", "hidden", "unlinked"} {
		entries[name] = uuid.NewString()
	}
	content := fmt.Sprintf(`{"projects": {"sectionType": "project", "entries": [
		{"id": %q, "projectId": %q},
		{"id": %q, "projectId": %q},
		{"id": %q, "projectId": %q, "isHidden": true},
		{"id": %q}
	]}}`, entries["linked"], atlas, entries["foreign"], foreign, entries["hidden"], quiet, entries["unlinked"])
	slug := "projects-" + uuid.NewString()[:8]
	createLinkedPublicStoreResume(ctx, t, tx, owner, slug, true, content)
	return publicProjectsFixture{slug: slug, ids: []uuid.UUID{atlas, quiet, foreign}, entryIDs: entries}
}

type publicProjectRow struct {
	EntryID     string          `json:"entryId"`
	ProjectSlug string          `json:"projectSlug"`
	Document    json.RawMessage `json:"document"`
}

func decodePublicProjects(t *testing.T, column json.RawMessage, ids []uuid.UUID) []publicProjectRow {
	t.Helper()
	var rows []publicProjectRow
	if err := json.Unmarshal(column, &rows); err != nil {
		t.Fatalf("decode projects %s: %v", column, err)
	}
	for _, id := range ids {
		if bytes.Contains(bytes.ToLower(column), []byte(id.String())) {
			t.Fatalf("projects column carries project id %s: %s", id, column)
		}
	}
	return rows
}

func TestGetPublicResumeProjectList_ReadsVisibleOwnedProjectsWithoutDescriptions(t *testing.T) {
	ctx, _, tx, queries := newPublicStoreTx(t)
	fixture := newPublicProjectsFixture(ctx, t, tx)

	row, err := queries.GetPublicResumeProjectList(ctx, fixture.slug)
	if err != nil {
		t.Fatalf("GetPublicResumeProjectList() error: %v", err)
	}
	rows := decodePublicProjects(t, row.Projects, fixture.ids)
	if len(rows) != 1 || rows[0].EntryID != fixture.entryIDs["linked"] || rows[0].ProjectSlug != "atlas" {
		t.Fatalf("projects = %s, want only the visible owned link", row.Projects)
	}
	requireWithoutDescriptions(t, rows[0].Document)
}

func requireWithoutDescriptions(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var document struct {
		DefaultLanguage string                       `json:"defaultLanguage"`
		Technologies    []string                     `json:"technologies"`
		Text            map[string]map[string]string `json:"text"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string]string{"en": {"name": "atlas", "summary": "Summary"}, "vi": {"name": "atlas"}}
	if !reflect.DeepEqual(document.Text, want) || document.DefaultLanguage != "en" || !reflect.DeepEqual(document.Technologies, []string{"Go"}) {
		t.Fatalf("document = %s, want every description removed and the rest kept", raw)
	}
}

func TestGetPublicResumeProject_ReadsOnlyTheRequestedVisibleOwnedProject(t *testing.T) {
	ctx, _, tx, queries := newPublicStoreTx(t)
	fixture := newPublicProjectsFixture(ctx, t, tx)

	row, err := queries.GetPublicResumeProject(ctx, store.GetPublicResumeProjectParams{Slug: fixture.slug, ProjectSlug: "atlas"})
	if err != nil {
		t.Fatalf("GetPublicResumeProject() error: %v", err)
	}
	rows := decodePublicProjects(t, row.Projects, fixture.ids)
	if len(rows) != 1 || rows[0].EntryID != fixture.entryIDs["linked"] || !bytes.Contains(rows[0].Document, []byte("<p>English</p>")) {
		t.Fatalf("projects = %s, want the linked project with its descriptions", row.Projects)
	}
	links := decodePublicProjects(t, row.ProjectLinks, fixture.ids)
	if len(links) != 1 || links[0].ProjectSlug != "atlas" || links[0].EntryID != fixture.entryIDs["linked"] {
		t.Fatalf("project links = %s, want the one visible owned link", row.ProjectLinks)
	}
	requireWithoutDescriptions(t, links[0].Document)

	for _, slug := range []string{"quiet", "foreign", "absent"} {
		row, err := queries.GetPublicResumeProject(ctx, store.GetPublicResumeProjectParams{Slug: fixture.slug, ProjectSlug: slug})
		if err != nil {
			t.Fatalf("GetPublicResumeProject(%s) error: %v", slug, err)
		}
		if string(row.Projects) != "[]" {
			t.Fatalf("projects for %s = %s, want []", slug, row.Projects)
		}
	}
}

func TestGetPublicResumeProjects_NotLiveReadsNoRow(t *testing.T) {
	ctx, _, tx, queries := newPublicStoreTx(t)
	owner := createPublicStoreUser(ctx, t, tx)
	atlas := createDescribedPublicStoreProject(ctx, t, tx, owner, "atlas")
	slug := "draft-" + uuid.NewString()[:8]
	createLinkedPublicStoreResume(ctx, t, tx, owner, slug, false,
		fmt.Sprintf(`{"projects": {"sectionType": "project", "entries": [{"id": %q, "projectId": %q}]}}`, uuid.NewString(), atlas))

	if _, err := queries.GetPublicResumeProjectList(ctx, slug); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("GetPublicResumeProjectList() error = %v, want pgx.ErrNoRows", err)
	}
	if _, err := queries.GetPublicResumeProject(ctx, store.GetPublicResumeProjectParams{Slug: slug, ProjectSlug: "atlas"}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("GetPublicResumeProject() error = %v, want pgx.ErrNoRows", err)
	}
}

type publicEvidenceRow struct {
	position  int
	kind      string
	url       string
	title     string
	ruleMet   bool
	binding   int64
	itemDate  time.Time
	checkedAt time.Time
}

func insertPublicStoreEvidence(ctx context.Context, t *testing.T, tx pgx.Tx, projectID uuid.UUID, rows ...publicEvidenceRow) {
	t.Helper()
	for i, row := range rows {
		var pull *int64
		var tag, flag *string
		switch row.kind {
		case "pullRequest":
			number := int64(i + 1)
			pull = &number
		case "release":
			value := "v" + strconv.Itoa(i+1)
			tag = &value
		}
		if !row.ruleMet {
			reason := "rule_failed"
			flag = &reason
		}
		item := int64(9000 + i)
		repository := item
		if row.kind != "repository" {
			repository = 8000
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO project_evidence (project_id, position, kind, repository_id, item_id, pull_number, release_tag, url,
				repository_name, title, item_date, checked_at, checked_binding, rule_met, flag_reason)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'octocat/hello', $9, $10, $11, $12, $13, $14)
		`, projectID, row.position, row.kind, repository, item, pull, tag, row.url, row.title, row.itemDate, row.checkedAt, row.binding, row.ruleMet, flag); err != nil {
			t.Fatalf("insert evidence %q: %v", row.title, err)
		}
	}
}

func evidenceInstant(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func requestedEvidence(ctx context.Context, t *testing.T, queries *store.Queries, slug string, withEvidence bool) []map[string]json.RawMessage {
	t.Helper()
	row, err := queries.GetPublicResumeProject(ctx, store.GetPublicResumeProjectParams{Slug: slug, ProjectSlug: "atlas", WithEvidence: withEvidence})
	if err != nil {
		t.Fatalf("GetPublicResumeProject() error: %v", err)
	}
	var rows []struct {
		Evidence []map[string]json.RawMessage `json:"evidence"`
	}
	if err := json.Unmarshal(row.Projects, &rows); err != nil || len(rows) != 1 || rows[0].Evidence == nil {
		t.Fatalf("projects = %s (%v), want one row with an evidence array", row.Projects, err)
	}
	return rows[0].Evidence
}

func TestGetPublicResumeProject_ReadsTheRequestedProjectEvidenceInOrder(t *testing.T) {
	ctx, _, tx, queries := newPublicStoreTx(t)
	fixture := newPublicProjectsFixture(ctx, t, tx)
	atlas, quiet, foreign := fixture.ids[0], fixture.ids[1], fixture.ids[2]
	month := evidenceInstant(t, "2025-12-31T23:30:00Z")
	checked := evidenceInstant(t, "2026-11-01T08:15:30Z")
	insertPublicStoreEvidence(ctx, t, tx, atlas,
		publicEvidenceRow{2, "release", "https://github.com/octocat/hello/releases/tag/v1", "Third", true, 1, month, checked},
		publicEvidenceRow{0, "repository", "https://github.com/octocat/hello", "First", true, 1, month, checked},
		publicEvidenceRow{1, "pullRequest", "https://github.com/octocat/hello/pull/2", "Second", false, 1, month, checked})
	insertPublicStoreEvidence(ctx, t, tx, quiet, publicEvidenceRow{0, "repository", "https://github.com/octocat/quiet", "Quiet evidence", true, 1, month, checked})
	insertPublicStoreEvidence(ctx, t, tx, foreign, publicEvidenceRow{0, "repository", "https://github.com/octocat/foreign", "Foreign evidence", true, 1, month, checked})

	items := requestedEvidence(ctx, t, queries, fixture.slug, true)
	keys := []string{"checkedAt", "confirmed", "itemDate", "kind", "repositoryName", "title", "url"}
	var titles []string
	for _, item := range items {
		got := make([]string, 0, len(item))
		for key := range item {
			got = append(got, key)
		}
		slices.Sort(got)
		if !slices.Equal(got, keys) {
			t.Fatalf("evidence keys = %v, want exactly %v", got, keys)
		}
		var title, itemDate, checkedAt string
		for key, target := range map[string]*string{"title": &title, "itemDate": &itemDate, "checkedAt": &checkedAt} {
			if err := json.Unmarshal(item[key], target); err != nil {
				t.Fatal(err)
			}
		}
		if !evidenceInstant(t, itemDate).Equal(month) || !evidenceInstant(t, checkedAt).Equal(checked) {
			t.Fatalf("instants = %s, %s; want %s and %s", itemDate, checkedAt, month, checked)
		}
		titles = append(titles, title)
	}
	if !slices.Equal(titles, []string{"First", "Second", "Third"}) {
		t.Fatalf("evidence titles = %v, want position order", titles)
	}
	row, err := queries.GetPublicResumeProject(ctx, store.GetPublicResumeProjectParams{Slug: fixture.slug, ProjectSlug: "atlas", WithEvidence: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, other := range []string{"Quiet evidence", "Foreign evidence"} {
		if bytes.Contains(row.Projects, []byte(other)) || bytes.Contains(row.ProjectLinks, []byte(other)) {
			t.Fatalf("the read carries another project's evidence %q", other)
		}
	}
}

func TestGetPublicResumeProject_ConfirmedEqualsTheOwnerReadInEveryState(t *testing.T) {
	ctx, _, tx, queries := newPublicStoreTx(t)
	fixture := newPublicProjectsFixture(ctx, t, tx)
	atlas := fixture.ids[0]
	var owner uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT user_id FROM projects WHERE id = $1`, atlas).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	at := evidenceInstant(t, "2026-01-02T03:04:05Z")
	insertPublicStoreEvidence(ctx, t, tx, atlas,
		publicEvidenceRow{0, "repository", "https://github.com/octocat/hello", "Met", true, 1, at, at},
		publicEvidenceRow{1, "pullRequest", "https://github.com/octocat/hello/pull/2", "Flagged", false, 1, at, at})
	install := func(githubUserID int64) {
		if _, err := queries.InstallGitHubConnection(ctx, store.InstallGitHubConnectionParams{UserID: owner, GithubUserID: githubUserID, Login: "octocat", ConnectedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	for _, step := range []struct {
		name   string
		change func()
		want   []bool
	}{
		{"no connection row", func() {}, []bool{false, false}},
		{"rule met under the live binding", func() { install(101) }, []bool{true, false}},
		{"stale binding after another account", func() { install(202) }, []bool{false, false}},
		{"checked again under the new binding", func() {
			if _, err := tx.Exec(ctx, `UPDATE project_evidence SET checked_binding = 2 WHERE project_id = $1`, atlas); err != nil {
				t.Fatal(err)
			}
		}, []bool{true, false}},
		{"after ClearGitHubConnection", func() {
			if _, err := queries.ClearGitHubConnection(ctx, owner); err != nil {
				t.Fatal(err)
			}
		}, []bool{false, false}},
	} {
		step.change()
		var public []bool
		for _, item := range requestedEvidence(ctx, t, queries, fixture.slug, true) {
			var confirmed bool
			if err := json.Unmarshal(item["confirmed"], &confirmed); err != nil {
				t.Fatalf("%s: confirmed %s: %v", step.name, item["confirmed"], err)
			}
			public = append(public, confirmed)
		}
		owned, err := queries.ListProjectEvidence(ctx, store.ListProjectEvidenceParams{UserID: owner, ProjectID: atlas})
		if err != nil {
			t.Fatal(err)
		}
		var listed []bool
		for _, item := range owned {
			listed = append(listed, item.Confirmed)
		}
		if !slices.Equal(public, step.want) || !slices.Equal(listed, public) {
			t.Fatalf("%s: public confirmed = %v, owner read %v; want both %v", step.name, public, listed, step.want)
		}
	}
}

func TestGetPublicResumeProject_WithoutEvidenceReadsAnEmptyArray(t *testing.T) {
	ctx, _, tx, queries := newPublicStoreTx(t)
	fixture := newPublicProjectsFixture(ctx, t, tx)
	at := evidenceInstant(t, "2026-01-02T03:04:05Z")
	insertPublicStoreEvidence(ctx, t, tx, fixture.ids[0], publicEvidenceRow{0, "repository", "https://github.com/octocat/hello", "Stored", true, 1, at, at})
	if items := requestedEvidence(ctx, t, queries, fixture.slug, false); len(items) != 0 {
		t.Fatalf("evidence with the capability off = %v, want []", items)
	}
	if items := requestedEvidence(ctx, t, queries, fixture.slug, true); len(items) != 1 {
		t.Fatalf("evidence with the capability on = %v, want the stored item", items)
	}
}

func TestPublicReads_OtherQueriesCarryNoEvidence(t *testing.T) {
	ctx, _, tx, queries := newPublicStoreTx(t)
	fixture := newPublicProjectsFixture(ctx, t, tx)
	at := evidenceInstant(t, "2026-01-02T03:04:05Z")
	insertPublicStoreEvidence(ctx, t, tx, fixture.ids[0], publicEvidenceRow{0, "repository", "https://github.com/octocat/hello", "Stored", true, 1, at, at})
	var resumeID, owner uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id, user_id FROM resumes WHERE slug = $1`, fixture.slug).Scan(&resumeID, &owner); err != nil {
		t.Fatal(err)
	}
	page, err := queries.GetPublicResumeProject(ctx, store.GetPublicResumeProjectParams{Slug: fixture.slug, ProjectSlug: "atlas", WithEvidence: true})
	if err != nil {
		t.Fatal(err)
	}
	list, err := queries.GetPublicResumeProjectList(ctx, fixture.slug)
	if err != nil {
		t.Fatal(err)
	}
	bySlug, err := queries.GetPublicResumeBySlug(ctx, fixture.slug)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := queries.GetResumeForUserWithProjectLinks(ctx, store.GetResumeForUserWithProjectLinksParams{ID: resumeID, UserID: owner})
	if err != nil {
		t.Fatal(err)
	}
	for name, column := range map[string]json.RawMessage{
		"project links of the page read": page.ProjectLinks, "project list": list.Projects,
		"resume by slug": bySlug.ProjectLinks, "owner resume": owned.ProjectLinks,
	} {
		if bytes.Contains(column, []byte(`"evidence"`)) || bytes.Contains(column, []byte("octocat/hello")) {
			t.Fatalf("%s carries evidence: %s", name, column)
		}
	}
}
