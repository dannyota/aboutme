package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

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
