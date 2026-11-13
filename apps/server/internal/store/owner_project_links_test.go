package store_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/store"
)

func TestGetPublicResumeBySlug_ReadsLinkedDocumentsWithoutDescriptions(t *testing.T) {
	ctx, _, tx, queries := newPublicStoreTx(t)
	fixture := newPublicProjectsFixture(ctx, t, tx)

	row, err := queries.GetPublicResumeBySlug(ctx, fixture.slug)
	if err != nil {
		t.Fatalf("GetPublicResumeBySlug() error: %v", err)
	}
	links := decodePublicProjects(t, row.ProjectLinks, fixture.ids)
	if len(links) != 1 || links[0].EntryID != fixture.entryIDs["linked"] || links[0].ProjectSlug != "atlas" {
		t.Fatalf("project links = %s, want only the visible owned link", row.ProjectLinks)
	}
	requireWithoutDescriptions(t, links[0].Document)
}

func TestGetResumeForUserWithProjectLinks_ReadsTheOwnersLinkContext(t *testing.T) {
	ctx, _, tx, queries := newPublicStoreTx(t)
	owner := createPublicStoreUser(ctx, t, tx)
	other := createPublicStoreUser(ctx, t, tx)
	atlas := createDescribedPublicStoreProject(ctx, t, tx, owner, "atlas")
	quiet := createDescribedPublicStoreProject(ctx, t, tx, owner, "quiet")
	foreign := createDescribedPublicStoreProject(ctx, t, tx, other, "foreign")
	linked, hidden, foreignEntry := uuid.NewString(), uuid.NewString(), uuid.NewString()
	content := fmt.Sprintf(`{"projects": {"sectionType": "project", "entries": [
		{"id": %q, "projectId": %q},
		{"id": %q, "projectId": %q, "isHidden": true},
		{"id": %q, "projectId": %q}
	]}}`, linked, atlas, hidden, quiet, foreignEntry, foreign)
	var resumeID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO resumes (user_id, title, schema_version, personal_details, content, customization)
		VALUES ($1, 'Draft', 6, '{}'::jsonb, $2::jsonb, '{}'::jsonb)
		RETURNING id
	`, owner, content).Scan(&resumeID); err != nil {
		t.Fatalf("insert resume: %v", err)
	}

	row, err := queries.GetResumeForUserWithProjectLinks(ctx, store.GetResumeForUserWithProjectLinksParams{ID: resumeID, UserID: owner})
	if err != nil {
		t.Fatalf("GetResumeForUserWithProjectLinks() error: %v", err)
	}
	if row.Resume.ID != resumeID || row.Resume.Live {
		t.Fatalf("resume = %+v, want the owner's unpublished resume", row.Resume)
	}
	links := decodePublicProjects(t, row.ProjectLinks, []uuid.UUID{atlas, quiet, foreign})
	if len(links) != 1 || links[0].EntryID != linked || links[0].ProjectSlug != "atlas" {
		t.Fatalf("project links = %s, want only the visible owned link", row.ProjectLinks)
	}
	requireWithoutDescriptions(t, links[0].Document)

	_, err = queries.GetResumeForUserWithProjectLinks(ctx, store.GetResumeForUserWithProjectLinksParams{ID: resumeID, UserID: other})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("another account's read error = %v, want pgx.ErrNoRows", err)
	}
}
