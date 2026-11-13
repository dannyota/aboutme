package resumeapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/store"
)

func linkProjectDocument(name string) project.Document {
	return project.Document{DefaultLanguage: "en", Text: map[string]project.Text{"en": {Name: name}}}
}

func createLinkProject(t *testing.T, h *resumeAPITestHarness, owner uuid.UUID, slug string, d project.Document) project.Project {
	t.Helper()
	created, err := project.NewStore().CreateTx(h.ctx, h.queries, owner, slug, d, time.Now(), nil)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return created
}

func createOtherAccount(t *testing.T, h *resumeAPITestHarness) uuid.UUID {
	t.Helper()
	user, err := h.queries.CreateUser(h.ctx, store.CreateUserParams{
		Email: "linkother-" + uuid.NewString() + "@example.test", Name: "other account",
	})
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}
	return user.ID
}

func formatRevision(revision int64) string { return strconv.FormatInt(revision, 10) }

func withProjectSection(doc schema.Resume, key string, entries ...schema.ProjectEntry) schema.Resume {
	if doc.Content == nil {
		doc.Content = map[string]schema.Section{}
	}
	doc.Content[key] = schema.Section{SectionType: schema.Project, ProjectEntries: append([]schema.ProjectEntry{}, entries...)}
	doc.Customization.Layout.Sections.Main = append(append([]string(nil), doc.Customization.Layout.Sections.Main...), key)
	return doc
}

func linkedEntry(projectID uuid.UUID, title string) schema.ProjectEntry {
	entry := schema.ProjectEntry{ID: uuid.NewString(), Title: stringPointer(title)}
	if projectID != uuid.Nil {
		entry.ProjectID = stringPointer(projectID.String())
	}
	return entry
}

func storedLinks(t *testing.T, h *resumeAPITestHarness, resumeID uuid.UUID) map[string]string {
	t.Helper()
	stored, err := h.resumes.Get(h.ctx, h.userID, resumeID)
	if err != nil {
		t.Fatalf("reload resume: %v", err)
	}
	links := map[string]string{}
	for _, section := range stored.Doc.Content {
		for _, entry := range section.ProjectEntries {
			if entry.ProjectID != nil {
				links[entry.ID] = *entry.ProjectID
			}
		}
	}
	return links
}

func errorCodeOf(t *testing.T, body []byte) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode error body %s: %v", body, err)
	}
	return envelope.Error.Code
}

func linkRouteRequest(t *testing.T, h *resumeAPITestHarness, method, path, body string, revision int64, version string) testHTTPResponse {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(h.ctx, method, h.server.URL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.AddCookie(h.cookie)
	req.Header.Set("Origin", resumeAPITestOrigin)
	req.Header.Set(auth.CSRFHeaderName, h.csrfToken)
	req.Header.Set("Idempotency-Key", uuid.NewString())
	if revision > 0 {
		req.Header.Set("If-Match", fmt.Sprintf(`"r%d"`, revision))
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if version != "" {
		req.Header.Set(wireVersionHeader, version)
	}
	response, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	return snapshotHTTPResponse(t, response)
}

func mustDocumentJSON(t *testing.T, doc schema.Resume) string {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode document: %v", err)
	}
	return string(raw)
}

func entryJSON(t *testing.T, entry schema.ProjectEntry) string {
	t.Helper()
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("encode entry: %v", err)
	}
	return `{"entry":` + string(raw) + `}`
}
