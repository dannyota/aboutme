package resumeapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
)

func createStructureContractResume(t *testing.T, h *resumeAPITestHarness) resume.Resume {
	t.Helper()
	var doc = decodeEntryTestDocument(t, structureTestDocument(t))
	created, err := h.resumes.Create(h.ctx, h.userID, "Structure contract", doc)
	if err != nil {
		t.Fatalf("create structure contract resume: %v", err)
	}
	return created
}

func TestStructureRouteIsImplemented(t *testing.T) {
	t.Parallel()
	for _, route := range structureRoutes() {
		if route.Handler == nil {
			t.Errorf("%s %s has no handler", route.Method, route.Pattern)
		}
	}
}

func TestStructureContractSequentialAtomicAndBounds(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := createStructureContractResume(t, h)
	path := fmt.Sprintf("/api/v1/resumes/%s/structure", created.ID)
	body := `{"commands":[{"op":"createSection","key":"d","sectionType":"education","column":"sidebar","index":0},{"op":"moveSection","key":"d","column":"main","index":3},{"op":"reorderColumn","column":"main","keys":["d","a","b","c"]}]}`
	commands := []structureCommand{
		{Op: "createSection", Key: "d", SectionType: "education", Column: "sidebar", Index: 0, HasIndex: true},
		{Op: "moveSection", Key: "d", Column: "main", Index: 3, HasIndex: true},
		{Op: "reorderColumn", Column: "main", Keys: []string{"d", "a", "b", "c"}},
	}
	if _, err := h.service.applyAtWireVersion(created.Doc, 2, func(document json.RawMessage) (json.RawMessage, error) {
		return applyStructureCommands(document, commands)
	}); err != nil {
		t.Fatalf("structure pipeline before HTTP: %T: %v", err, err)
	}
	response := h.mutationRequest(t, http.MethodPatch, path, strings.NewReader(body), created.Revision, uuid.NewString())
	if response.status != http.StatusOK {
		t.Fatalf("structure write status = %d body=%s", response.status, response.body)
	}
	_, document := decodedWrittenDocument(t, response)
	if got := strings.Join(document.Customization.Layout.Sections.Main, ","); got != "d,a,b,c" {
		t.Fatalf("main order = %s", got)
	}

	storedBefore := snapshotStoredStructureRow(t, h, created.ID)
	recordsBefore := h.snapshotUserTable(t, "idempotency_records")
	badBody := `{"commands":[{"op":"createSection","key":"e","sectionType":"work","column":"main","index":0},{"op":"deleteSection","key":"missing"}]}`
	rejected := h.mutationRequest(t, http.MethodPatch, path, strings.NewReader(badBody), 2, uuid.NewString())
	if rejected.status != http.StatusUnprocessableEntity || !bytes.Contains(rejected.body, []byte(`"code":"document_invalid"`)) {
		t.Fatalf("rejected batch = status %d body=%s", rejected.status, rejected.body)
	}
	storedAfter := snapshotStoredStructureRow(t, h, created.ID)
	if storedAfter != storedBefore {
		t.Fatalf("rejected batch changed stored row bytes:\nbefore %s\nafter  %s", storedBefore, storedAfter)
	}
	if recordsAfter := h.snapshotUserTable(t, "idempotency_records"); recordsAfter != recordsBefore {
		t.Fatalf("rejected batch stored idempotency row: before=%q after=%q", recordsBefore, recordsAfter)
	}
}

func snapshotStoredStructureRow(t *testing.T, h *resumeAPITestHarness, resumeID uuid.UUID) string {
	t.Helper()
	var content, customization string
	var revision int64
	if err := h.pool.QueryRow(h.ctx,
		"SELECT content::text, customization::text, revision FROM resumes WHERE id = $1 AND user_id = $2",
		resumeID, h.userID,
	).Scan(&content, &customization, &revision); err != nil {
		t.Fatalf("snapshot stored structure row: %v", err)
	}
	return fmt.Sprintf("revision=%d content=%s customization=%s", revision, content, customization)
}

func TestStructureContractRejectsNonIntegerAnd101CommandsBeforeApplication(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := createStructureContractResume(t, h)
	path := fmt.Sprintf("/api/v1/resumes/%s/structure", created.ID)
	nonInteger := h.mutationRequest(t, http.MethodPatch, path,
		strings.NewReader(`{"commands":[{"op":"moveSection","key":"a","column":"main","index":0.5}]}`),
		created.Revision, uuid.NewString())
	if nonInteger.status != http.StatusBadRequest || !bytes.Contains(nonInteger.body, []byte(`"code":"request_invalid"`)) {
		t.Fatalf("non-integer = status %d body=%s", nonInteger.status, nonInteger.body)
	}
	hugeInteger := h.mutationRequest(t, http.MethodPatch, path,
		strings.NewReader(`{"commands":[{"op":"moveSection","key":"a","column":"main","index":999999999999999999999999999999999999}]}`),
		created.Revision, uuid.NewString())
	if hugeInteger.status != http.StatusUnprocessableEntity || !bytes.Contains(hugeInteger.body, []byte(`"code":"document_invalid"`)) {
		t.Fatalf("huge integer = status %d body=%s, want semantic 422", hugeInteger.status, hugeInteger.body)
	}
	admittedCommands := make([]map[string]any, 100)
	for i := range admittedCommands {
		admittedCommands[i] = map[string]any{"op": "reorderColumn", "column": "main", "keys": []string{"a", "b", "c"}}
	}
	admittedRaw, err := json.Marshal(map[string]any{"commands": admittedCommands})
	if err != nil {
		t.Fatalf("marshal 100 commands: %v", err)
	}
	admitted := h.mutationRequest(t, http.MethodPatch, path, bytes.NewReader(admittedRaw), created.Revision, uuid.NewString())
	if admitted.status != http.StatusOK {
		t.Fatalf("100 commands = status %d body=%s", admitted.status, admitted.body)
	}
	commands := make([]map[string]any, 101)
	for i := range commands {
		commands[i] = map[string]any{"op": "reorderColumn", "column": "main", "keys": []string{"a", "b", "c"}}
	}
	raw, err := json.Marshal(map[string]any{"commands": commands})
	if err != nil {
		t.Fatalf("marshal 101 commands: %v", err)
	}
	over := h.mutationRequest(t, http.MethodPatch, path, bytes.NewReader(raw), created.Revision+1, uuid.NewString())
	if over.status != http.StatusUnprocessableEntity || !bytes.Contains(over.body, []byte(`"code":"document_invalid"`)) {
		t.Fatalf("101 commands = status %d body=%s, want semantic 422", over.status, over.body)
	}
}

func TestStructureHandlerRejects25thSectionWithoutWrite(t *testing.T) {
	h := newResumeAPITestHarness(t)
	doc := loadMinimalDocument(t)
	doc.Content = make(map[string]schema.Section, 24)
	doc.Customization.Layout.Sections.Main = make([]string, 24)
	doc.Customization.Layout.Sections.Sidebar = []string{}
	for i := 0; i < 24; i++ {
		key := fmt.Sprintf("%c", 'a'+i)
		doc.Content[key] = schema.NewWorkSection(nil, nil, nil)
		doc.Customization.Layout.Sections.Main[i] = key
	}
	created, err := h.resumes.Create(h.ctx, h.userID, "Section bound", doc)
	if err != nil {
		t.Fatalf("create 24-section resume: %v", err)
	}
	beforeRow := snapshotStoredStructureRow(t, h, created.ID)
	beforeRecords := h.snapshotUserTable(t, "idempotency_records")
	path := fmt.Sprintf("/api/v1/resumes/%s/structure", created.ID)
	body := `{"commands":[{"op":"createSection","key":"y","sectionType":"work","column":"main","index":24}]}`
	response := h.mutationRequest(t, http.MethodPatch, path, strings.NewReader(body), created.Revision, uuid.NewString())
	if response.status != http.StatusUnprocessableEntity || !bytes.Contains(response.body, []byte(`"code":"document_invalid"`)) {
		t.Fatalf("25th section = status %d body=%s", response.status, response.body)
	}
	if afterRow := snapshotStoredStructureRow(t, h, created.ID); afterRow != beforeRow {
		t.Fatalf("25th section changed row:\nbefore %s\nafter  %s", beforeRow, afterRow)
	}
	if afterRecords := h.snapshotUserTable(t, "idempotency_records"); afterRecords != beforeRecords {
		t.Fatalf("25th section stored idempotency record: before=%q after=%q", beforeRecords, afterRecords)
	}
}

func TestStructureSemanticRejectionsWriteNothing(t *testing.T) {
	for _, test := range []struct {
		name    string
		command string
	}{
		{"create existing key", `{"op":"createSection","key":"a","sectionType":"work","column":"main","index":0}`},
		{"delete unknown key", `{"op":"deleteSection","key":"missing"}`},
		{"negative index", `{"op":"moveSection","key":"a","column":"main","index":-1}`},
		{"above remove-first bound", `{"op":"moveSection","key":"a","column":"main","index":3}`},
		{"reorder drops member", `{"op":"reorderColumn","column":"main","keys":["a","b"]}`},
		{"reorder adds member", `{"op":"reorderColumn","column":"main","keys":["a","b","c","d"]}`},
		{"reorder duplicates member", `{"op":"reorderColumn","column":"main","keys":["a","a","c"]}`},
		{"unknown section type", `{"op":"createSection","key":"d","sectionType":"unknown","column":"main","index":0}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			created := createStructureContractResume(t, h)
			beforeRow := snapshotStoredStructureRow(t, h, created.ID)
			beforeRecords := h.snapshotUserTable(t, "idempotency_records")
			path := fmt.Sprintf("/api/v1/resumes/%s/structure", created.ID)
			body := `{"commands":[` + test.command + `]}`
			response := h.mutationRequest(t, http.MethodPatch, path, strings.NewReader(body), created.Revision, uuid.NewString())
			if response.status != http.StatusUnprocessableEntity || !bytes.Contains(response.body, []byte(`"code":"document_invalid"`)) {
				t.Fatalf("semantic rejection = status %d body=%s", response.status, response.body)
			}
			if afterRow := snapshotStoredStructureRow(t, h, created.ID); afterRow != beforeRow {
				t.Fatalf("semantic rejection changed row:\nbefore %s\nafter  %s", beforeRow, afterRow)
			}
			if afterRecords := h.snapshotUserTable(t, "idempotency_records"); afterRecords != beforeRecords {
				t.Fatalf("semantic rejection stored idempotency record: before=%q after=%q", beforeRecords, afterRecords)
			}
		})
	}
}

func TestStructureConcurrentWithEntryUpsertHasOneWholeDocumentWinner(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := createEntryContractResume(t, h)
	entryPath := fmt.Sprintf("/api/v1/resumes/%s/entries/work", created.ID)
	structurePath := fmt.Sprintf("/api/v1/resumes/%s/structure", created.ID)
	requests := []struct {
		path string
		body []byte
	}{
		{entryPath, []byte(`{"entry":{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61"}}`)},
		{structurePath, []byte(`{"commands":[{"op":"createSection","key":"skills","sectionType":"skill","column":"sidebar","index":0}]}`)},
	}
	responses := make([]testHTTPResponse, 2)
	var wg sync.WaitGroup
	for i := range requests {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			responses[index] = h.mutationRequest(t, http.MethodPatch, requests[index].path, bytes.NewReader(requests[index].body), created.Revision, uuid.NewString())
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
	if mismatchRevision != "2" {
		t.Fatalf("mismatch revision = %q", mismatchRevision)
	}
	if err := resume.ValidateForStore(winningDocument); err != nil {
		t.Fatalf("mismatch details document is a half-state: %v", err)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("read concurrent winner: %v", err)
	}
	if err := resume.ValidateForStore(stored.Doc); err != nil {
		t.Fatalf("concurrent winner is a half-state: %v", err)
	}
	if stored.Revision != created.Revision+1 {
		t.Fatalf("winner revision = %d, want %d", stored.Revision, created.Revision+1)
	}
}
