package resumeapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"
)

func loadFullDocument(t *testing.T) schema.Resume {
	t.Helper()
	raw, err := os.ReadFile("../../../../packages/schema/fixtures/full.json")
	if err != nil {
		t.Fatalf("read full schema fixture: %v", err)
	}
	var doc schema.Resume
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode full schema fixture: %v", err)
	}
	return doc
}

type linkWriter func(t *testing.T, h *resumeAPITestHarness, projectID uuid.UUID) (int, []byte)

func restCreateWriter(t *testing.T, h *resumeAPITestHarness, projectID uuid.UUID) (int, []byte) {
	doc := withProjectSection(loadMinimalDocument(t), "projects", linkedEntry(projectID, "Atlas"))
	response := resumeRequest(t, h, http.MethodPost, apiResumePath,
		`{"title":"linked","document":`+mustDocumentJSON(t, doc)+`}`, 0, uuid.New(), "")
	return response.status, response.body
}

func importWriter(t *testing.T, h *resumeAPITestHarness, projectID uuid.UUID) (int, []byte) {
	doc := loadFullDocument(t)
	doc.PersonalDetails.Photo = nil
	section := doc.Content["project"]
	section.ProjectEntries[0].ProjectID = stringPointer(projectID.String())
	doc.Content["project"] = section
	response := resumeRequest(t, h, http.MethodPost, apiResumePath,
		`{"title":"imported","lng":"en","document":`+mustDocumentJSON(t, doc)+`}`, 0, uuid.New(), "")
	return response.status, response.body
}

func entryUpsertWriter(t *testing.T, h *resumeAPITestHarness, projectID uuid.UUID) (int, []byte) {
	created := h.createResume(t)
	doc := withProjectSection(created.Doc, "projects")
	revision, err := h.resumes.SaveDocument(h.ctx, h.userID, created.ID, doc, created.Revision)
	if err != nil {
		t.Fatalf("seed project section: %v", err)
	}
	response := resumeRequest(t, h, http.MethodPatch, apiResumePath+"/"+created.ID.String()+"/entries/projects",
		entryJSON(t, linkedEntry(projectID, "Atlas")), revision, uuid.New(), "")
	return response.status, response.body
}

func storeSaveWriter(t *testing.T, h *resumeAPITestHarness, projectID uuid.UUID) (int, []byte) {
	created := h.createResume(t)
	doc := withProjectSection(created.Doc, "projects", linkedEntry(projectID, "Atlas"))
	_, err := h.resumes.SaveDocument(h.ctx, h.userID, created.ID, doc, created.Revision)
	if err == nil {
		return http.StatusOK, nil
	}
	return 0, []byte(err.Error())
}

func agentCreateWriter(t *testing.T, h *resumeAPITestHarness, projectID uuid.UUID) (int, []byte) {
	doc := withProjectSection(loadMinimalDocument(t), "projects", linkedEntry(projectID, "Atlas"))
	response := h.service.ExecuteAgent(h.ctx, newAgentPrincipalForTest(t, h), AgentCall{
		Operation: AgentCreateResume, IdempotencyKey: uuid.NewString(),
		Payload: json.RawMessage(`{"title":"agent","document":` + mustDocumentJSON(t, doc) + `}`),
	})
	return response.Status, response.Body
}

func agentUpsertWriter(t *testing.T, h *resumeAPITestHarness, projectID uuid.UUID) (int, []byte) {
	created := h.createResume(t)
	doc := withProjectSection(created.Doc, "projects")
	revision, err := h.resumes.SaveDocument(h.ctx, h.userID, created.ID, doc, created.Revision)
	if err != nil {
		t.Fatalf("seed project section: %v", err)
	}
	response := h.service.ExecuteAgent(h.ctx, newAgentPrincipalForTest(t, h), AgentCall{
		Operation: AgentUpsertEntry, IdempotencyKey: uuid.NewString(), ResumeID: created.ID.String(),
		Revision: formatRevision(revision), SectionKey: "projects",
		Payload: json.RawMessage(entryJSON(t, linkedEntry(projectID, "Atlas"))),
	})
	return response.Status, response.Body
}

func TestProjectLinks_ForeignAndMissingProjectsAnswerIdenticallyOnEveryWriter(t *testing.T) {
	for _, tc := range []struct {
		name   string
		write  linkWriter
		status int
		code   string
	}{
		{"rest create", restCreateWriter, http.StatusNotFound, "project_not_found"},
		{"import", importWriter, http.StatusNotFound, "project_not_found"},
		{"entry upsert", entryUpsertWriter, http.StatusNotFound, "project_not_found"},
		{"store save", storeSaveWriter, 0, ""},
		{"agent create_resume", agentCreateWriter, http.StatusForbidden, "scope_denied"},
		{"agent upsert_entry", agentUpsertWriter, http.StatusForbidden, "scope_denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			foreign := createLinkProject(t, h, createOtherAccount(t, h), "foreign", linkProjectDocument("Foreign"))
			foreignStatus, foreignBody := tc.write(t, h, foreign.ID)
			missingStatus, missingBody := tc.write(t, h, uuid.New())
			if foreignStatus != missingStatus || !bytes.Equal(foreignBody, missingBody) {
				t.Fatalf("foreign = %d %s, missing = %d %s; want identical answers",
					foreignStatus, foreignBody, missingStatus, missingBody)
			}
			if foreignStatus != tc.status || foreignStatus == http.StatusOK || foreignStatus == http.StatusCreated {
				t.Fatalf("status = %d %s, want %d", foreignStatus, foreignBody, tc.status)
			}
			if tc.code != "" {
				if code := errorCodeOf(t, foreignBody); code != tc.code {
					t.Fatalf("code = %q, want %q", code, tc.code)
				}
			}
			if resumes := countResumeTestRows(t, h, "resumes"); resumes > 2 {
				t.Fatalf("refused writes left %d resumes", resumes)
			}
		})
	}
}

func TestProjectLinks_OwnProjectLinksOnCreateAndTwinCopyKeepsLinks(t *testing.T) {
	h := newResumeAPITestHarness(t)
	owned := createLinkProject(t, h, h.userID, "atlas", linkProjectDocument("Atlas"))
	status, body := importWriter(t, h, owned.ID)
	if status != http.StatusCreated {
		t.Fatalf("own link create = %d %s", status, body)
	}
	source := decodeResumeResource(t, testHTTPResponse{status: status, body: body})
	twin := resumeRequest(t, h, http.MethodPost, apiResumePath,
		`{"title":"imported · Tiếng Việt","lng":"vi","document":`+string(source.Document)+`}`, 0, uuid.New(), "")
	if twin.status != http.StatusCreated {
		t.Fatalf("twin copy = %d %s", twin.status, twin.body)
	}
	twinID := decodeResumeResource(t, twin).ID
	links := storedLinks(t, h, twinID)
	if len(links) != 1 {
		t.Fatalf("twin links = %v, want the source link", links)
	}
	for _, linked := range links {
		if linked != owned.ID.String() {
			t.Fatalf("twin link = %s, want %s", linked, owned.ID)
		}
	}
}

func TestProjectLinks_DuplicateLinkIsRefused(t *testing.T) {
	h := newResumeAPITestHarness(t)
	owned := createLinkProject(t, h, h.userID, "atlas", linkProjectDocument("Atlas"))
	doc := withProjectSection(loadMinimalDocument(t), "projects", linkedEntry(owned.ID, "One"), linkedEntry(owned.ID, "Two"))
	created := resumeRequest(t, h, http.MethodPost, apiResumePath,
		`{"title":"dup","document":`+mustDocumentJSON(t, doc)+`}`, 0, uuid.New(), "")
	if created.status != http.StatusConflict || errorCodeOf(t, created.body) != "project_already_linked" {
		t.Fatalf("duplicate create = %d %s, want 409 project_already_linked", created.status, created.body)
	}

	base := h.createResume(t)
	revision, err := h.resumes.SaveDocument(h.ctx, h.userID, base.ID,
		withProjectSection(base.Doc, "projects", linkedEntry(owned.ID, "One")), base.Revision)
	if err != nil {
		t.Fatalf("seed one link: %v", err)
	}
	upsert := resumeRequest(t, h, http.MethodPatch, apiResumePath+"/"+base.ID.String()+"/entries/projects",
		entryJSON(t, linkedEntry(owned.ID, "Two")), revision, uuid.New(), "")
	if upsert.status != http.StatusConflict || errorCodeOf(t, upsert.body) != "project_already_linked" {
		t.Fatalf("duplicate upsert = %d %s, want 409 project_already_linked", upsert.status, upsert.body)
	}
	if links := storedLinks(t, h, base.ID); len(links) != 1 {
		t.Fatalf("stored links = %v after a refused duplicate", links)
	}
}
