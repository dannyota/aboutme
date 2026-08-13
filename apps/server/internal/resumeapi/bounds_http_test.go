package resumeapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
)

func TestRevisionSerializedAsString(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	const revision = int64(9_007_199_254_740_993)
	if _, err := h.pool.Exec(h.ctx, `UPDATE resumes SET revision = $1 WHERE id = $2`, revision, created.ID); err != nil {
		t.Fatalf("seed large revision: %v", err)
	}
	get := resumeRequest(t, h, http.MethodGet, apiResumePath+"/"+created.ID.String(), "", 0, uuid.Nil, "2")
	if get.status != http.StatusOK || responseRevision(t, get.body) != fmt.Sprint(revision) {
		t.Fatalf("GET large revision = %d %s", get.status, get.body)
	}
	list := resumeRequest(t, h, http.MethodGet, apiResumePath, "", 0, uuid.Nil, "2")
	if list.status != http.StatusOK || listResponseRevision(t, list.body) != fmt.Sprint(revision) {
		t.Fatalf("list large revision = %d %s", list.status, list.body)
	}
	stale := resumeRequest(t, h, http.MethodPatch, apiResumePath+"/"+created.ID.String(), `{"title":"stale"}`, revision-1, uuid.New(), "2")
	if stale.status != http.StatusPreconditionFailed || errorResponseRevision(t, stale.body) != fmt.Sprint(revision) {
		t.Fatalf("412 large revision = %d %s", stale.status, stale.body)
	}
	written := resumeRequest(t, h, http.MethodPatch, apiResumePath+"/"+created.ID.String(), `{"title":"exact"}`, revision, uuid.New(), "2")
	if written.status != http.StatusOK || responseRevision(t, written.body) != fmt.Sprint(revision+1) ||
		written.header.Get("ETag") != fmt.Sprintf(`"r%d"`, revision+1) {
		t.Fatalf("write large revision = %d headers=%v body=%s", written.status, written.header, written.body)
	}
}

func TestByteVsCodePointBoundsThroughHTTP(t *testing.T) {
	h := newResumeAPITestHarness(t)
	doc := loadMinimalDocument(t)
	doc.Content = map[string]schema.Section{"profile": schema.NewProfileSection(nil, nil, []schema.ProfileEntry{})}
	doc.Customization.Layout.Sections.Main = []string{"profile"}
	doc.Customization.Layout.Sections.Sidebar = []string{}
	created, err := h.resumes.Create(h.ctx, h.userID, "UTF-8 bound", doc)
	if err != nil {
		t.Fatalf("create UTF-8 fixture: %v", err)
	}
	entryID := "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60"
	bodyFor := func(value string) string {
		raw, marshalErr := json.Marshal(map[string]any{"entry": map[string]any{"id": entryID, "text": value}})
		if marshalErr != nil {
			t.Fatalf("marshal UTF-8 entry: %v", marshalErr)
		}
		return string(raw)
	}
	atLimit := strings.Repeat("é", 8_192)
	accepted := resumeRequest(t, h, http.MethodPatch,
		apiResumePath+"/"+created.ID.String()+"/entries/profile", bodyFor(atLimit), created.Revision, uuid.New(), "2")
	if accepted.status != http.StatusOK {
		t.Fatalf("16 KiB UTF-8 rich text = %d %s, want 200", accepted.status, accepted.body)
	}
	before := snapshotStoredResumeRow(t, h, created.ID)
	over := resumeRequest(t, h, http.MethodPatch,
		apiResumePath+"/"+created.ID.String()+"/entries/profile", bodyFor(atLimit+"é"), created.Revision+1, uuid.New(), "2")
	assertRouteError(t, over, http.StatusUnprocessableEntity, "document_invalid")
	if after := snapshotStoredResumeRow(t, h, created.ID); !reflect.DeepEqual(after, before) {
		t.Fatalf("rich-text limit+1 wrote state: before=%+v after=%+v", before, after)
	}
}

func TestEveryBound_LimitAndLimitPlusOne(t *testing.T) {
	t.Run("ordinary request bytes", func(t *testing.T) {
		h := newResumeAPITestHarness(t)
		created := h.createResume(t)
		before := snapshotStoredResumeRow(t, h, created.ID)
		const requestLimit = 256 * 1024
		prefix, suffix := `{"title":"`, `"}`
		atLimitBody := prefix + strings.Repeat("a", requestLimit-len(prefix)-len(suffix)) + suffix
		if len(atLimitBody) != requestLimit {
			t.Fatalf("request fixture = %d bytes, want %d", len(atLimitBody), requestLimit)
		}
		atLimit := resumeRequest(t, h, http.MethodPatch, apiResumePath+"/"+created.ID.String(),
			atLimitBody, created.Revision, uuid.New(), "2")
		assertRouteError(t, atLimit, http.StatusUnprocessableEntity, "document_invalid")
		over := resumeRequest(t, h, http.MethodPatch, apiResumePath+"/"+created.ID.String(),
			prefix+strings.Repeat("a", requestLimit-len(prefix)-len(suffix)+1)+suffix,
			created.Revision, uuid.New(), "2")
		assertRouteError(t, over, http.StatusRequestEntityTooLarge, "body_too_large")
		if after := snapshotStoredResumeRow(t, h, created.ID); !reflect.DeepEqual(after, before) {
			t.Fatalf("request-body boundaries wrote state: before=%+v after=%+v", before, after)
		}
	})

	t.Run("sections", func(t *testing.T) {
		h := newResumeAPITestHarness(t)
		doc := loadMinimalDocument(t)
		doc.Content = make(map[string]schema.Section, 23)
		doc.Customization.Layout.Sections.Main = make([]string, 23)
		doc.Customization.Layout.Sections.Sidebar = []string{}
		for index := range 23 {
			key := fmt.Sprintf("%c", 'a'+index)
			doc.Content[key] = schema.NewWorkSection(nil, nil, []schema.WorkEntry{})
			doc.Customization.Layout.Sections.Main[index] = key
		}
		created, err := h.resumes.Create(h.ctx, h.userID, "section bound", doc)
		if err != nil {
			t.Fatalf("create 23-section fixture: %v", err)
		}
		path := apiResumePath + "/" + created.ID.String() + "/structure"
		atLimit := resumeRequest(t, h, http.MethodPatch, path,
			`{"commands":[{"op":"createSection","key":"x","sectionType":"work","column":"main","index":23}]}`,
			created.Revision, uuid.New(), "2")
		if atLimit.status != http.StatusOK {
			t.Fatalf("24th section = %d %s, want 200", atLimit.status, atLimit.body)
		}
		before := snapshotStoredResumeRow(t, h, created.ID)
		over := resumeRequest(t, h, http.MethodPatch, path,
			`{"commands":[{"op":"createSection","key":"y","sectionType":"work","column":"main","index":24}]}`,
			created.Revision+1, uuid.New(), "2")
		assertRouteError(t, over, http.StatusUnprocessableEntity, "document_invalid")
		if after := snapshotStoredResumeRow(t, h, created.ID); !reflect.DeepEqual(after, before) {
			t.Fatalf("25th section wrote state: before=%+v after=%+v", before, after)
		}
	})

	t.Run("entries", func(t *testing.T) {
		h := newResumeAPITestHarness(t)
		doc := loadMinimalDocument(t)
		entries := make([]schema.WorkEntry, 63)
		for index := range entries {
			entries[index].ID = fmt.Sprintf("10000000-0000-4000-8000-%012d", index)
		}
		doc.Content = map[string]schema.Section{"work": schema.NewWorkSection(nil, nil, entries)}
		doc.Customization.Layout.Sections.Main = []string{"work"}
		doc.Customization.Layout.Sections.Sidebar = []string{}
		created, err := h.resumes.Create(h.ctx, h.userID, "entry bound", doc)
		if err != nil {
			t.Fatalf("create 63-entry fixture: %v", err)
		}
		path := apiResumePath + "/" + created.ID.String() + "/entries/work"
		atLimit := resumeRequest(t, h, http.MethodPatch, path,
			`{"entry":{"id":"20000000-0000-4000-8000-000000000064"}}`, created.Revision, uuid.New(), "2")
		if atLimit.status != http.StatusOK {
			t.Fatalf("64th entry = %d %s, want 200", atLimit.status, atLimit.body)
		}
		before := snapshotStoredResumeRow(t, h, created.ID)
		over := resumeRequest(t, h, http.MethodPatch, path,
			`{"entry":{"id":"20000000-0000-4000-8000-000000000065"}}`, created.Revision+1, uuid.New(), "2")
		assertRouteError(t, over, http.StatusUnprocessableEntity, "document_invalid")
		if after := snapshotStoredResumeRow(t, h, created.ID); !reflect.DeepEqual(after, before) {
			t.Fatalf("65th entry wrote state: before=%+v after=%+v", before, after)
		}
	})

	t.Run("personal details", func(t *testing.T) {
		h := newResumeAPITestHarness(t)
		created := h.createResume(t)
		path := apiResumePath + "/" + created.ID.String() + "/personal-details"
		details := make([]map[string]any, 17)
		for index := range details {
			details[index] = map[string]any{
				"id": fmt.Sprintf("30000000-0000-4000-8000-%012d", index), "type": "email", "value": "", "isHidden": false,
			}
		}
		bodyFor := func(count int) string {
			raw, err := json.Marshal(map[string]any{"details": details[:count]})
			if err != nil {
				t.Fatalf("marshal %d details: %v", count, err)
			}
			return string(raw)
		}
		atLimit := resumeRequest(t, h, http.MethodPatch, path, bodyFor(16), created.Revision, uuid.New(), "2")
		if atLimit.status != http.StatusOK {
			t.Fatalf("16 details = %d %s, want 200", atLimit.status, atLimit.body)
		}
		before := snapshotStoredResumeRow(t, h, created.ID)
		over := resumeRequest(t, h, http.MethodPatch, path, bodyFor(17), created.Revision+1, uuid.New(), "2")
		assertRouteError(t, over, http.StatusUnprocessableEntity, "document_invalid")
		if after := snapshotStoredResumeRow(t, h, created.ID); !reflect.DeepEqual(after, before) {
			t.Fatalf("17 details wrote state: before=%+v after=%+v", before, after)
		}
	})
}

func TestAggregateDocumentBoundThroughHTTP(t *testing.T) {
	h := newResumeAPITestHarness(t)
	doc := exactCanonicalSizeDocument(t, resume.MaxDocumentBytes)
	created, err := h.resumes.Create(h.ctx, h.userID, "aggregate bound", doc)
	if err != nil {
		t.Fatalf("create exact aggregate fixture: %v", err)
	}
	atLimit := resumeRequest(t, h, http.MethodPatch, apiResumePath+"/"+created.ID.String(),
		`{"title":"aggregate bound accepted"}`, created.Revision, uuid.New(), "2")
	if atLimit.status != http.StatusOK {
		t.Fatalf("exact 512 KiB aggregate metadata write = %d %s, want 200", atLimit.status, atLimit.body)
	}
	before := snapshotStoredResumeRow(t, h, created.ID)
	response := resumeRequest(t, h, http.MethodPatch,
		apiResumePath+"/"+created.ID.String()+"/entries/work",
		`{"entry":{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4fff"}}`, created.Revision+1, uuid.New(), "2")
	assertRouteError(t, response, http.StatusUnprocessableEntity, "document_invalid")
	if after := snapshotStoredResumeRow(t, h, created.ID); !reflect.DeepEqual(after, before) {
		t.Fatalf("aggregate limit+1 wrote state: before=%+v after=%+v", before, after)
	}
}

func TestNoUnboundedWork(t *testing.T) {
	h := newResumeAPITestHarness(t)
	doc := loadMinimalDocument(t)
	doc.Content = map[string]schema.Section{"work": schema.NewWorkSection(nil, nil, []schema.WorkEntry{})}
	doc.Customization.Layout.Sections.Main = []string{"work"}
	doc.Customization.Layout.Sections.Sidebar = []string{}
	created, err := h.resumes.Create(h.ctx, h.userID, "unbounded work", doc)
	if err != nil {
		t.Fatalf("create unbounded-work fixture: %v", err)
	}
	before := snapshotStoredResumeRow(t, h, created.ID)
	beforeRecords := h.snapshotUserTable(t, "idempotency_records")

	deltas := strings.Repeat(`{"op":"set","path":"font.baseSizePx","value":16},`, 10_000)
	deltas = strings.TrimSuffix(deltas, ",")
	commands := strings.Repeat(`{"op":"reorderColumn","column":"main","keys":[]},`, 10_000)
	commands = strings.TrimSuffix(commands, ",")
	arrayMembers := strings.TrimSuffix(strings.Repeat("0,", 10_000), ",")
	requests := []struct{ path, body string }{
		{apiResumePath + "/" + created.ID.String() + "/customization", `{"deltas":[` + deltas + `]}`},
		{apiResumePath + "/" + created.ID.String() + "/structure", `{"commands":[` + commands + `]}`},
		{apiResumePath + "/" + created.ID.String() + "/entries/work", `{"entry":{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60","extra":[` + arrayMembers + `]}}`},
	}
	for index, request := range requests {
		response := resumeRequest(t, h, http.MethodPatch, request.path, request.body, created.Revision, uuid.New(), "2")
		if response.status != http.StatusBadRequest && response.status != http.StatusRequestEntityTooLarge && response.status != http.StatusUnprocessableEntity {
			t.Fatalf("unbounded request %d = %d %s", index, response.status, response.body)
		}
	}
	if after := snapshotStoredResumeRow(t, h, created.ID); !reflect.DeepEqual(after, before) {
		t.Fatalf("unbounded inputs wrote state: before=%+v after=%+v", before, after)
	}
	if after := h.snapshotUserTable(t, "idempotency_records"); after != beforeRecords {
		t.Fatalf("unbounded inputs wrote idempotency rows: before=%q after=%q", beforeRecords, after)
	}
}

func responseRevision(t *testing.T, body []byte) string {
	t.Helper()
	var envelope struct {
		Data struct {
			Revision string `json:"revision"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode revision response: %v", err)
	}
	return envelope.Data.Revision
}

func listResponseRevision(t *testing.T, body []byte) string {
	t.Helper()
	var envelope struct {
		Data []struct {
			Revision string `json:"revision"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Data) != 1 {
		t.Fatalf("decode revision list: len=%d err=%v", len(envelope.Data), err)
	}
	return envelope.Data[0].Revision
}

func errorResponseRevision(t *testing.T, body []byte) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Details struct {
				Revision string `json:"revision"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode revision error: %v", err)
	}
	return envelope.Error.Details.Revision
}

func exactCanonicalSizeDocument(t *testing.T, target int) schema.Resume {
	t.Helper()
	build := func(last int) schema.Resume {
		doc := loadMinimalDocument(t)
		entries := make([]schema.WorkEntry, 33)
		for index := range entries {
			textLength := 16_000
			if index == len(entries)-1 {
				textLength = last
			}
			text := strings.Repeat("a", textLength)
			entries[index] = schema.WorkEntry{ID: fmt.Sprintf("00000000-0000-4000-8000-%012x", index+1), Description: &text}
		}
		doc.Content = map[string]schema.Section{"work": schema.NewWorkSection(nil, nil, entries)}
		doc.Customization.Layout.Sections.Main = []string{"work"}
		doc.Customization.Layout.Sections.Sidebar = []string{}
		return doc
	}
	doc := build(0)
	raw, err := resume.AssembleCanonical(doc)
	if err != nil {
		t.Fatalf("assemble base aggregate: %v", err)
	}
	last := target - len(raw)
	if last < 0 || last > 16_384 {
		t.Fatalf("aggregate padding = %d, want [0,16384]", last)
	}
	doc = build(last)
	raw, err = resume.AssembleCanonical(doc)
	if err != nil || len(raw) != target {
		t.Fatalf("aggregate size = %d err=%v, want %d", len(raw), err, target)
	}
	return doc
}
