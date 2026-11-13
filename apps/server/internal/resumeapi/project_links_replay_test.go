package resumeapi

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/project"
)

func keyedLinkRequest(t *testing.T, h *resumeAPITestHarness, path, body string, revision int64, key string) testHTTPResponse {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(h.ctx, http.MethodPost, h.server.URL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.AddCookie(h.cookie)
	req.Header.Set("Origin", resumeAPITestOrigin)
	req.Header.Set(auth.CSRFHeaderName, h.csrfToken)
	req.Header.Set("Idempotency-Key", key)
	req.Header.Set("If-Match", fmt.Sprintf(`"r%d"`, revision))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	return snapshotHTTPResponse(t, response)
}

func renameLibraryText(t *testing.T, h *resumeAPITestHarness, id uuid.UUID, name string) {
	t.Helper()
	store := project.NewStore()
	current, err := store.GetTx(h.ctx, h.queries, h.userID, id)
	if err != nil {
		t.Fatalf("read project: %v", err)
	}
	edited := current.Document
	edited.Text = map[string]project.Text{}
	for key, value := range current.Text {
		value.Name = name
		edited.Text[key] = value
	}
	if _, err = store.ReplaceTx(h.ctx, h.queries, h.userID, id, current.Revision, current.Slug, edited, time.Now(), nil); err != nil {
		t.Fatalf("edit project: %v", err)
	}
}

func requireSameReplay(t *testing.T, name string, first, replay testHTTPResponse) {
	t.Helper()
	if replay.status != first.status || !bytes.Equal(replay.body, first.body) || replay.header.Get("ETag") != first.header.Get("ETag") {
		t.Fatalf("%s replay = %d %s %s, want %d %s %s", name, replay.status, replay.header.Get("ETag"), replay.body,
			first.status, first.header.Get("ETag"), first.body)
	}
}

func TestProjectLinks_ReplayAfterALibraryEditReturnsTheOriginalResponse(t *testing.T) {
	h := newResumeAPITestHarness(t)
	owned := createLinkProject(t, h, h.userID, "atlas", bilingualProject())
	created := createResumeWithLanguage(t, h, nil, loadMinimalDocument(t))
	linkPath := apiResumePath + "/" + created.ID.String() + "/project-links"
	linkBody := `{"projectId":"` + owned.ID.String() + `"}`
	linkKey := uuid.NewString()
	linked := keyedLinkRequest(t, h, linkPath, linkBody, created.Revision, linkKey)
	if linked.status != http.StatusOK {
		t.Fatalf("link = %d %s", linked.status, linked.body)
	}
	renameLibraryText(t, h, owned.ID, "Atlas renamed")
	requireSameReplay(t, "link", linked, keyedLinkRequest(t, h, linkPath, linkBody, created.Revision, linkKey))
	entry := onlyProjectEntry(t, h, created.ID, "projects")
	if stored, err := h.resumes.Get(h.ctx, h.userID, created.ID); err != nil || stored.Revision != 2 || *entry.Title != "Atlas" {
		t.Fatalf("after link replay: revision %d title %q (%v), want 2 and the original prefill", stored.Revision, *entry.Title, err)
	}

	refreshPath := apiResumePath + "/" + created.ID.String() + "/entries/" + entry.ID + "/project-refresh"
	refreshKey := uuid.NewString()
	refreshed := keyedLinkRequest(t, h, refreshPath, "", 2, refreshKey)
	if refreshed.status != http.StatusOK {
		t.Fatalf("refresh = %d %s", refreshed.status, refreshed.body)
	}
	renameLibraryText(t, h, owned.ID, "Atlas third")
	requireSameReplay(t, "refresh", refreshed, keyedLinkRequest(t, h, refreshPath, "", 2, refreshKey))
	entry = onlyProjectEntry(t, h, created.ID, "projects")
	if stored, err := h.resumes.Get(h.ctx, h.userID, created.ID); err != nil || stored.Revision != 3 || *entry.Title != "Atlas renamed" {
		t.Fatalf("after refresh replay: revision %d title %q (%v), want 3 and the first refresh", stored.Revision, *entry.Title, err)
	}
}
