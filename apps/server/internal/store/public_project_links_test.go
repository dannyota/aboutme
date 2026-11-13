package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func createPublicStoreProject(ctx context.Context, t *testing.T, tx pgx.Tx, userID uuid.UUID, slug string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO projects (user_id, slug, document)
		VALUES ($1, $2, '{"defaultLanguage":"en","text":{"en":{"name":"Project"}}}'::jsonb)
		RETURNING id
	`, userID, slug).Scan(&id); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	return id
}

func createLinkedPublicStoreResume(ctx context.Context, t *testing.T, tx pgx.Tx, userID uuid.UUID, slug string, live bool, content string) {
	t.Helper()
	if _, err := tx.Exec(ctx, `
		INSERT INTO resumes (
			user_id, title, slug, live, download_enabled, seo_geo_enabled,
			schema_version, personal_details, content, customization
		) VALUES ($1, 'Linked Resume', $2, $3, true, false, 6, '{}'::jsonb, $4::jsonb, '{}'::jsonb)
	`, userID, slug, live, content); err != nil {
		t.Fatalf("insert resume: %v", err)
	}
}

func TestGetPublicResumeBySlug_ReadsTheVisibleOwnedProjectLinks(t *testing.T) {
	ctx, _, tx, queries := newPublicStoreTx(t)
	owner := createPublicStoreUser(ctx, t, tx)
	other := createPublicStoreUser(ctx, t, tx)
	atlas := createPublicStoreProject(ctx, t, tx, owner, "atlas")
	orbit := createPublicStoreProject(ctx, t, tx, owner, "orbit")
	shown := createPublicStoreProject(ctx, t, tx, owner, "shown")
	quiet := createPublicStoreProject(ctx, t, tx, owner, "quiet")
	foreign := createPublicStoreProject(ctx, t, tx, other, "foreign")
	entries := map[string]string{}
	for _, name := range []string{"linked", "upper", "unhidden", "unlinked", "hidden", "foreign", "work"} {
		entries[name] = uuid.NewString()
	}
	content := fmt.Sprintf(`{
		"projects": {"sectionType": "project", "entries": [
			{"id": %q, "projectId": %q},
			{"id": %q, "projectId": %q},
			{"id": %q, "projectId": %q, "isHidden": false},
			{"id": %q},
			{"id": %q, "projectId": %q, "isHidden": true},
			{"id": %q, "projectId": %q}
		]},
		"jobs": {"sectionType": "work", "entries": [{"id": %q}]}
	}`, entries["linked"], atlas, entries["upper"], strings.ToUpper(orbit.String()), entries["unhidden"], shown,
		entries["unlinked"], entries["hidden"], quiet, entries["foreign"], foreign, entries["work"])
	slug := "links-" + uuid.NewString()[:8]
	createLinkedPublicStoreResume(ctx, t, tx, owner, slug, true, content)

	row, err := queries.GetPublicResumeBySlug(ctx, slug)
	if err != nil {
		t.Fatalf("GetPublicResumeBySlug() error: %v", err)
	}
	var links []map[string]json.RawMessage
	if err = json.Unmarshal(row.ProjectLinks, &links); err != nil {
		t.Fatalf("decode project links %s: %v", row.ProjectLinks, err)
	}
	got := map[string]string{}
	for _, link := range links {
		var entryID, projectSlug string
		if len(link) != 3 || link["document"] == nil || json.Unmarshal(link["entryId"], &entryID) != nil || json.Unmarshal(link["projectSlug"], &projectSlug) != nil {
			t.Fatalf("link %s, want exactly entryId, projectSlug and document", link)
		}
		got[entryID] = projectSlug
	}
	want := map[string]string{entries["linked"]: "atlas", entries["upper"]: "orbit", entries["unhidden"]: "shown"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("project links = %v, want %v", got, want)
	}
	for _, id := range []uuid.UUID{atlas, orbit, shown, quiet, foreign} {
		if bytes.Contains(bytes.ToLower(row.ProjectLinks), []byte(id.String())) {
			t.Fatalf("project links carry project id %s: %s", id, row.ProjectLinks)
		}
	}
}

func TestGetPublicResumeBySlug_WithoutLinksReadsAnEmptyList(t *testing.T) {
	ctx, _, tx, queries := newPublicStoreTx(t)
	owner := createPublicStoreUser(ctx, t, tx)
	slug := "nolinks-" + uuid.NewString()[:8]
	createLinkedPublicStoreResume(ctx, t, tx, owner, slug, true, `{"jobs": {"sectionType": "work", "entries": [{"id": "`+uuid.NewString()+`"}]}}`)
	row, err := queries.GetPublicResumeBySlug(ctx, slug)
	if err != nil {
		t.Fatalf("GetPublicResumeBySlug() error: %v", err)
	}
	if string(row.ProjectLinks) != "[]" {
		t.Fatalf("project links = %s, want []", row.ProjectLinks)
	}
}
