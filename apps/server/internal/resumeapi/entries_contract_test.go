package resumeapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
)

func (h *resumeAPITestHarness) mutationRequest(t *testing.T, method, path string, body io.Reader, revision int64, key string) testHTTPResponse {
	t.Helper()
	req, err := http.NewRequestWithContext(h.ctx, method, h.server.URL+path, body)
	if err != nil {
		t.Fatalf("build mutation request: %v", err)
	}
	req.AddCookie(h.cookie)
	req.Header.Set("Origin", resumeAPITestOrigin)
	req.Header.Set(auth.CSRFHeaderName, h.csrfToken)
	req.Header.Set("Idempotency-Key", key)
	req.Header.Set("If-Match", fmt.Sprintf(`"r%d"`, revision))
	if method != http.MethodDelete {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("perform mutation request: %v", err)
	}
	return snapshotHTTPResponse(t, response)
}

func createEntryContractResume(t *testing.T, h *resumeAPITestHarness) resume.Resume {
	t.Helper()
	doc := loadMinimalDocument(t)
	doc.Content = map[string]schema.Section{
		"work": schema.NewWorkSection(nil, nil, []schema.WorkEntry{{ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60"}}),
	}
	doc.Customization.Layout.Sections.Main = []string{"work"}
	doc.Customization.Layout.Sections.Sidebar = []string{}
	created, err := h.resumes.Create(h.ctx, h.userID, "Entry contract", doc)
	if err != nil {
		t.Fatalf("create entry contract resume: %v", err)
	}
	return created
}

func decodedWrittenDocument(t *testing.T, response testHTTPResponse) (string, schema.Resume) {
	t.Helper()
	var envelope struct {
		Data struct {
			Revision string        `json:"revision"`
			Document schema.Resume `json:"document"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.body, &envelope); err != nil {
		t.Fatalf("decode written response %q: %v", response.body, err)
	}
	return envelope.Data.Revision, envelope.Data.Document
}

func decodedRevisionMismatch(t *testing.T, response testHTTPResponse) (string, schema.Resume) {
	t.Helper()
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Revision string        `json:"revision"`
				Document schema.Resume `json:"document"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.body, &envelope); err != nil {
		t.Fatalf("decode mismatch response %q: %v", response.body, err)
	}
	if envelope.Error.Code != "revision_mismatch" {
		t.Fatalf("mismatch code = %q", envelope.Error.Code)
	}
	return envelope.Error.Details.Revision, envelope.Error.Details.Document
}

func TestEntryRoutesAreImplemented(t *testing.T) {
	t.Parallel()
	for _, route := range entryRoutes() {
		if route.Handler == nil {
			t.Errorf("%s %s has no handler", route.Method, route.Pattern)
		}
	}
}

func TestEntryUpsertDeleteContractAndReplay(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := createEntryContractResume(t, h)
	path := fmt.Sprintf("/api/v1/resumes/%s/entries/work", created.ID)
	upsertBody := []byte(`{"entry":{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61"}}`)
	upsert := h.mutationRequest(t, http.MethodPatch, path, bytes.NewReader(upsertBody), created.Revision, uuid.NewString())
	if upsert.status != http.StatusOK {
		t.Fatalf("upsert status = %d body=%s", upsert.status, upsert.body)
	}
	if upsert.header.Get("ETag") != `"r2"` || upsert.header.Get(wireVersionHeader) == "" {
		t.Fatalf("upsert headers = %v", upsert.header)
	}
	upsertResource := decodeResumeResource(t, upsert)
	freshResource := decodeResumeResource(t, resumeRequest(t, h, http.MethodGet,
		fmt.Sprintf("/api/v1/resumes/%s", created.ID), "", 0, uuid.Nil, wireVersionString(docmigrate.CurrentVersion)))
	if upsertResource.Lng != "und" || !upsertResource.UpdatedAt.Equal(freshResource.UpdatedAt) ||
		upsertResource.UpdatedAt.Equal(created.UpdatedAt) {
		t.Fatalf("upsert summary lng=%q updatedAt=%s; fresh GET updatedAt=%s; prior=%s",
			upsertResource.Lng, upsertResource.UpdatedAt, freshResource.UpdatedAt, created.UpdatedAt)
	}
	revision, document := decodedWrittenDocument(t, upsert)
	if revision != "2" || len(document.Content["work"].WorkEntries) != 2 {
		t.Fatalf("upsert response revision=%q entries=%#v", revision, document.Content["work"].WorkEntries)
	}
	beforeUntouched, err := json.Marshal(created.Doc.Content["work"].WorkEntries[0])
	if err != nil {
		t.Fatalf("marshal untouched entry before: %v", err)
	}
	afterUntouched, err := json.Marshal(document.Content["work"].WorkEntries[0])
	if err != nil {
		t.Fatalf("marshal untouched entry after: %v", err)
	}
	if !bytes.Equal(beforeUntouched, afterUntouched) {
		t.Fatalf("append changed existing entry bytes: before=%s after=%s", beforeUntouched, afterUntouched)
	}

	replaceBody := []byte(`{"entry":{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60","jobTitle":"replacement"}}`)
	replaced := h.mutationRequest(t, http.MethodPatch, path, bytes.NewReader(replaceBody), 2, uuid.NewString())
	if replaced.status != http.StatusOK {
		t.Fatalf("replace status = %d body=%s", replaced.status, replaced.body)
	}
	revision, document = decodedWrittenDocument(t, replaced)
	entries := document.Content["work"].WorkEntries
	if revision != "3" || len(entries) != 2 || entries[0].JobTitle == nil || *entries[0].JobTitle != "replacement" || entries[1].ID != "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61" {
		t.Fatalf("replace response revision=%q entries=%#v", revision, entries)
	}

	deletePath := path + "/01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61"
	deleteKey := uuid.NewString()
	deleted := h.mutationRequest(t, http.MethodDelete, deletePath, nil, 3, deleteKey)
	if deleted.status != http.StatusNoContent || len(deleted.body) != 0 || deleted.header.Get("ETag") != `"r4"` {
		t.Fatalf("delete = status %d headers %v body %q", deleted.status, deleted.header, deleted.body)
	}
	replayed := h.mutationRequest(t, http.MethodDelete, deletePath, nil, 3, deleteKey)
	if replayed.status != deleted.status || !bytes.Equal(replayed.body, deleted.body) ||
		replayed.header.Get("ETag") != deleted.header.Get("ETag") ||
		replayed.header.Get(wireVersionHeader) != deleted.header.Get(wireVersionHeader) {
		t.Fatalf("delete replay = %#v, want stored status/body/deterministic headers from %#v", replayed, deleted)
	}
}

func TestEntryDeleteLastPersistsPresentEmptySection(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := createEntryContractResume(t, h)
	path := fmt.Sprintf("/api/v1/resumes/%s/entries/work/01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60", created.ID)
	response := h.mutationRequest(t, http.MethodDelete, path, nil, created.Revision, uuid.NewString())
	if response.status != http.StatusNoContent {
		t.Fatalf("delete last status = %d body=%s", response.status, response.body)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("read emptied section: %v", err)
	}
	entries := stored.Doc.Content["work"].WorkEntries
	if entries == nil || len(entries) != 0 {
		t.Fatalf("stored emptied entries = %#v, want present empty slice", entries)
	}
}

func TestEntryHandlerRejectsWrongShapeCollisionAnd65thWithoutWrite(t *testing.T) {
	for _, test := range []struct {
		name string
		doc  func(*testing.T) schema.Resume
		body string
	}{
		{
			name: "wrong section shape",
			doc: func(t *testing.T) schema.Resume {
				return createEntryContractDocument(t)
			},
			body: `{"entry":{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e99","degree":"BSc"}}`,
		},
		{
			name: "whole resume collision",
			doc: func(t *testing.T) schema.Resume {
				doc := createEntryContractDocument(t)
				doc.Content["skill"] = schema.NewSkillSection(nil, nil, []schema.SkillEntry{{ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e62"}})
				doc.Customization.Layout.Sections.Sidebar = []string{"skill"}
				return doc
			},
			body: `{"entry":{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e62"}}`,
		},
		{
			name: "65th entry",
			doc: func(t *testing.T) schema.Resume {
				doc := createEntryContractDocument(t)
				entries := make([]schema.WorkEntry, 64)
				for i := range entries {
					entries[i].ID = fmt.Sprintf("10000000-0000-4000-8000-%012d", i)
				}
				doc.Content["work"] = schema.NewWorkSection(nil, nil, entries)
				return doc
			},
			body: `{"entry":{"id":"20000000-0000-4000-8000-000000000001"}}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			created, err := h.resumes.Create(h.ctx, h.userID, "Entry rejection", test.doc(t))
			if err != nil {
				t.Fatalf("create rejection resume: %v", err)
			}
			beforeRow := snapshotStoredStructureRow(t, h, created.ID)
			beforeRecords := h.snapshotUserTable(t, "idempotency_records")
			path := fmt.Sprintf("/api/v1/resumes/%s/entries/work", created.ID)
			response := h.mutationRequest(t, http.MethodPatch, path, strings.NewReader(test.body), created.Revision, uuid.NewString())
			if response.status != http.StatusUnprocessableEntity || !bytes.Contains(response.body, []byte(`"code":"document_invalid"`)) {
				t.Fatalf("response = status %d body=%s", response.status, response.body)
			}
			if afterRow := snapshotStoredStructureRow(t, h, created.ID); afterRow != beforeRow {
				t.Fatalf("rejection changed row:\nbefore %s\nafter  %s", beforeRow, afterRow)
			}
			if afterRecords := h.snapshotUserTable(t, "idempotency_records"); afterRecords != beforeRecords {
				t.Fatalf("rejection stored idempotency record: before=%q after=%q", beforeRecords, afterRecords)
			}
		})
	}
}

func createEntryContractDocument(t *testing.T) schema.Resume {
	t.Helper()
	doc := loadMinimalDocument(t)
	doc.Content = map[string]schema.Section{
		"work": schema.NewWorkSection(nil, nil, []schema.WorkEntry{{ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60"}}),
	}
	doc.Customization.Layout.Sections.Main = []string{"work"}
	doc.Customization.Layout.Sections.Sidebar = []string{}
	return doc
}

func TestEntryUnknownDeleteWritesNothing(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := createEntryContractResume(t, h)
	beforeRecords := h.snapshotUserTable(t, "idempotency_records")
	path := fmt.Sprintf("/api/v1/resumes/%s/entries/work/01890f47-7e8a-7b2a-8d70-9a1f2c3d4e99", created.ID)
	response := h.mutationRequest(t, http.MethodDelete, path, nil, created.Revision, uuid.NewString())
	if response.status != http.StatusNotFound || !bytes.Contains(response.body, []byte(`"code":"resume_not_found"`)) {
		t.Fatalf("unknown delete = status %d body %s", response.status, response.body)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("read after unknown delete: %v", err)
	}
	if stored.Revision != created.Revision {
		t.Fatalf("revision = %d, want unchanged %d", stored.Revision, created.Revision)
	}
	if after := h.snapshotUserTable(t, "idempotency_records"); after != beforeRecords {
		t.Fatalf("unknown delete stored idempotency row: before=%q after=%q", beforeRecords, after)
	}
}

func TestEntryDeleteNoOracleResponseIsIdenticalForUnknownAndForeign(t *testing.T) {
	owner := newResumeAPITestHarness(t)
	owned := createEntryContractResume(t, owner)
	foreignOwner := newResumeAPITestHarness(t)
	foreign := createEntryContractResume(t, foreignOwner)
	entryID := "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e99"
	unknownPath := fmt.Sprintf("/api/v1/resumes/%s/entries/work/%s", owned.ID, entryID)
	foreignPath := fmt.Sprintf("/api/v1/resumes/%s/entries/work/%s", foreign.ID, entryID)
	unknown := owner.mutationRequest(t, http.MethodDelete, unknownPath, nil, owned.Revision, uuid.NewString())
	foreignResponse := owner.mutationRequest(t, http.MethodDelete, foreignPath, nil, foreign.Revision, uuid.NewString())
	if unknown.status != http.StatusNotFound || foreignResponse.status != http.StatusNotFound || !bytes.Equal(unknown.body, foreignResponse.body) {
		t.Fatalf("unknown/foreign = (%d,%s) / (%d,%s), want identical 404", unknown.status, unknown.body, foreignResponse.status, foreignResponse.body)
	}
}

func TestEntryConcurrentUpsertsHaveOneWinnerAndRetry(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := createEntryContractResume(t, h)
	path := fmt.Sprintf("/api/v1/resumes/%s/entries/work", created.ID)
	bodies := [][]byte{
		[]byte(`{"entry":{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61"}}`),
		[]byte(`{"entry":{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e62"}}`),
	}
	responses := make([]testHTTPResponse, 2)
	var wg sync.WaitGroup
	for i := range bodies {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			responses[index] = h.mutationRequest(t, http.MethodPatch, path, bytes.NewReader(bodies[index]), created.Revision, uuid.NewString())
		}(i)
	}
	wg.Wait()
	statuses := []int{responses[0].status, responses[1].status}
	sort.Ints(statuses)
	if !reflect.DeepEqual(statuses, []int{http.StatusOK, http.StatusPreconditionFailed}) {
		t.Fatalf("concurrent statuses = %v bodies=%s / %s", statuses, responses[0].body, responses[1].body)
	}
	loser := 0
	if responses[0].status == http.StatusOK {
		loser = 1
	}
	mismatchRevision, winningDocument := decodedRevisionMismatch(t, responses[loser])
	if mismatchRevision != "2" || len(winningDocument.Content["work"].WorkEntries) != 2 {
		t.Fatalf("mismatch details revision=%q entries=%#v", mismatchRevision, winningDocument.Content["work"].WorkEntries)
	}
	retry := h.mutationRequest(t, http.MethodPatch, path, bytes.NewReader(bodies[loser]), created.Revision+1, uuid.NewString())
	if retry.status != http.StatusOK {
		t.Fatalf("loser retry = status %d body %s", retry.status, retry.body)
	}
	_, document := decodedWrittenDocument(t, retry)
	if len(document.Content["work"].WorkEntries) != 3 {
		t.Fatalf("retry entries = %#v, want original plus both contenders", document.Content["work"].WorkEntries)
	}
}
