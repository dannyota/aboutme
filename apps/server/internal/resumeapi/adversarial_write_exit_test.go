package resumeapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
)

type adversarialExitResult struct {
	response testHTTPResponse
	err      error
}

func TestConcurrentSameRevision_OneWinner(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := createEntryContractResume(t, h)
	before := snapshotStoredResumeRow(t, h, created.ID)
	requests := []struct {
		name string
		path string
		body string
	}{
		{
			name: "entry",
			path: fmt.Sprintf("/api/v1/resumes/%s/entries/work", created.ID),
			body: `{"entry":{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61","jobTitle":"race entry"}}`,
		},
		{
			name: "section",
			path: fmt.Sprintf("/api/v1/resumes/%s/sections/work", created.ID),
			body: `{"displayName":"Race section"}`,
		},
		{
			name: "structure",
			path: fmt.Sprintf("/api/v1/resumes/%s/structure", created.ID),
			body: `{"commands":[{"op":"createSection","key":"skills","sectionType":"skill","column":"sidebar","index":0}]}`,
		},
		{
			name: "customization",
			path: fmt.Sprintf("/api/v1/resumes/%s/customization", created.ID),
			body: `{"deltas":[{"op":"set","path":"font.baseSizePx","value":18}]}`,
		},
	}

	httpRequests := make([]*http.Request, len(requests))
	for i, request := range requests {
		httpRequests[i] = newAdversarialExitMutationRequest(
			t, h, http.MethodPatch, request.path, []byte(request.body), created.Revision, uuid.NewString(), "2", "application/json",
		)
	}
	results := runAdversarialExitRequestsTogether(h.client, httpRequests)
	winner := -1
	for i, result := range results {
		if result.err != nil {
			t.Fatalf("%s request failed: %v", requests[i].name, result.err)
		}
		switch result.response.status {
		case http.StatusOK:
			if winner >= 0 {
				t.Fatalf("both %s and %s won at revision %d", requests[winner].name, requests[i].name, created.Revision)
			}
			winner = i
		case http.StatusPreconditionFailed:
			assertResumeTestError(t, result.response, http.StatusPreconditionFailed, "revision_mismatch")
		default:
			t.Fatalf("%s status = %d, want 200 or 412 (body=%s)", requests[i].name, result.response.status, result.response.body)
		}
	}
	if winner < 0 {
		t.Fatalf("mixed-route race had no winner: %#v", results)
	}

	winnerResource := decodeResumeResource(t, results[winner].response)
	if winnerResource.Revision != fmt.Sprint(created.Revision+1) {
		t.Fatalf("winner revision = %q, want %d", winnerResource.Revision, created.Revision+1)
	}
	var winnerDocument schema.Resume
	if err := json.Unmarshal(winnerResource.Document, &winnerDocument); err != nil {
		t.Fatalf("decode winning document: %v", err)
	}
	assertAdversarialRaceWinner(t, winnerDocument, winner)
	assertStoredPartsEqualDocument(t, h, created.ID, winnerDocument)

	after := snapshotStoredResumeRow(t, h, created.ID)
	if after.Revision != before.Revision+1 || after.UpdatedAt == before.UpdatedAt {
		t.Fatalf("stored row revision/updated_at = %d/%q, before %d/%q", after.Revision, after.UpdatedAt, before.Revision, before.UpdatedAt)
	}
	fresh := resumeRequest(t, h, http.MethodGet, apiResumePath+"/"+created.ID.String(), "", 0, uuid.Nil, "2")
	if fresh.status != http.StatusOK {
		t.Fatalf("fresh GET status = %d body=%s", fresh.status, fresh.body)
	}
	freshState := adversarialExitSuccessState(t, fresh)
	if freshState.Revision != winnerResource.Revision || !adversarialExitJSONEqual(t, freshState.Document, winnerResource.Document) {
		t.Fatalf("stored winner differs from GET:\nwinner revision=%s document=%s\nGET revision=%s document=%s",
			winnerResource.Revision, winnerResource.Document, freshState.Revision, freshState.Document)
	}
	for i, result := range results {
		if i == winner {
			continue
		}
		loserState := adversarialExitMismatchState(t, result.response)
		if loserState.Revision != freshState.Revision || !bytes.Equal(loserState.Document, freshState.Document) {
			t.Fatalf("%s 412 does not carry the whole winner:\n412 revision=%s document=%s\nGET revision=%s document=%s",
				requests[i].name, loserState.Revision, loserState.Document, freshState.Revision, freshState.Document)
		}
	}
}

func TestIdempotency_ScopedByUserAndOperation(t *testing.T) {
	t.Run("different users", func(t *testing.T) {
		first := newResumeAPITestHarness(t)
		second := newResumeAPITestHarness(t)
		key := uuid.New()
		body := `{"title":"same key user scope"}`
		firstResponse := resumeRequest(t, first, http.MethodPost, apiResumePath, body, 0, key, "2")
		secondResponse := resumeRequest(t, second, http.MethodPost, apiResumePath, body, 0, key, "2")
		if firstResponse.status != http.StatusCreated || secondResponse.status != http.StatusCreated {
			t.Fatalf("same-key creates for different users = %d/%d, bodies=%s / %s",
				firstResponse.status, secondResponse.status, firstResponse.body, secondResponse.body)
		}
		if decodeResumeResource(t, firstResponse).ID == decodeResumeResource(t, secondResponse).ID {
			t.Fatal("different users received one replayed resume")
		}
	})

	t.Run("different canonical operations", func(t *testing.T) {
		h := newResumeAPITestHarness(t)
		created := createEntryContractResume(t, h)
		key := uuid.NewString()
		entry := h.mutationRequest(t, http.MethodPatch,
			fmt.Sprintf("/api/v1/resumes/%s/entries/work", created.ID),
			strings.NewReader(`{"entry":{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61"}}`),
			created.Revision, key)
		section := h.mutationRequest(t, http.MethodPatch,
			fmt.Sprintf("/api/v1/resumes/%s/sections/work", created.ID),
			strings.NewReader(`{"displayName":"operation scoped"}`),
			created.Revision+1, key)
		if entry.status != http.StatusOK || section.status != http.StatusOK {
			t.Fatalf("same key across operations = %d/%d, bodies=%s / %s", entry.status, section.status, entry.body, section.body)
		}
		stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
		if err != nil {
			t.Fatalf("read operation-scoped writes: %v", err)
		}
		work := stored.Doc.Content["work"]
		if stored.Revision != created.Revision+2 || len(work.WorkEntries) != 2 || work.DisplayName == nil || *work.DisplayName != "operation scoped" {
			t.Fatalf("operation-scoped result revision=%d work=%#v", stored.Revision, work)
		}
	})

	t.Run("different concrete targets", func(t *testing.T) {
		h := newResumeAPITestHarness(t)
		first := h.createResume(t)
		second := h.createResume(t)
		key := uuid.New()
		firstResponse := resumeRequest(t, h, http.MethodPatch, apiResumePath+"/"+first.ID.String(),
			`{"title":"first target"}`, first.Revision, key, "2")
		secondResponse := resumeRequest(t, h, http.MethodPatch, apiResumePath+"/"+second.ID.String(),
			`{"title":"second target"}`, second.Revision, key, "2")
		if firstResponse.status != http.StatusOK || secondResponse.status != http.StatusOK {
			t.Fatalf("same key across targets = %d/%d, bodies=%s / %s",
				firstResponse.status, secondResponse.status, firstResponse.body, secondResponse.body)
		}
		if decodeResumeResource(t, firstResponse).Title != "first target" || decodeResumeResource(t, secondResponse).Title != "second target" {
			t.Fatalf("cross-target response was replayed: first=%s second=%s", firstResponse.body, secondResponse.body)
		}
	})
}

func TestIdempotency_ChangedFingerprintRejected(t *testing.T) {
	tests := []struct {
		name            string
		baselineBody    string
		changedBody     string
		baselineVersion string
		changedVersion  string
		changedRevision int64
	}{
		{
			name:            "resolved wire version",
			baselineBody:    `{"title":"fingerprint baseline"}`,
			changedBody:     `{"title":"fingerprint baseline"}`,
			baselineVersion: "2",
			changedVersion:  "1",
			changedRevision: 1,
		},
		{
			name:            "parsed precondition",
			baselineBody:    `{"title":"fingerprint baseline"}`,
			changedBody:     `{"title":"fingerprint baseline"}`,
			baselineVersion: "2",
			changedVersion:  "2",
			changedRevision: 2,
		},
		{
			name:            "bounded JSON payload bytes",
			baselineBody:    `{"title":"fingerprint baseline"}`,
			changedBody:     `{"title": "fingerprint baseline"}`,
			baselineVersion: "2",
			changedVersion:  "2",
			changedRevision: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			created := h.createResume(t)
			path := apiResumePath + "/" + created.ID.String()
			key := uuid.NewString()
			baseline := performAdversarialExitMutation(t, h, http.MethodPatch, path,
				[]byte(test.baselineBody), created.Revision, key, test.baselineVersion, "application/json")
			if baseline.status != http.StatusOK {
				t.Fatalf("baseline status = %d body=%s", baseline.status, baseline.body)
			}
			beforeConflict := snapshotStoredResumeRow(t, h, created.ID)
			beforeRecords := h.snapshotUserTable(t, "idempotency_records")

			changed := performAdversarialExitMutation(t, h, http.MethodPatch, path,
				[]byte(test.changedBody), test.changedRevision, key, test.changedVersion, "application/json")
			assertResumeTestError(t, changed, http.StatusConflict, "idempotency_key_reuse")
			assertAdversarialFailureUnreserved(t, h, created.ID, beforeConflict, beforeRecords)
		})
	}
}

func TestIdempotency_ConcurrentSameKey(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	path := apiResumePath + "/" + created.ID.String()
	key := uuid.NewString()
	body := []byte(`{"title":"concurrent same key"}`)
	const contenders = 8
	requests := make([]*http.Request, contenders)
	for i := range requests {
		requests[i] = newAdversarialExitMutationRequest(t, h, http.MethodPatch, path, body,
			created.Revision, key, "2", "application/json")
	}

	results := runAdversarialExitRequestsTogether(h.client, requests)
	want := results[0].response
	requestIDs := make(map[string]struct{}, contenders)
	for i, result := range results {
		if result.err != nil {
			t.Fatalf("contender %d failed: %v", i, result.err)
		}
		if result.response.status != http.StatusOK {
			t.Fatalf("contender %d status = %d body=%s", i, result.response.status, result.response.body)
		}
		if result.response.status != want.status || !bytes.Equal(result.response.body, want.body) ||
			!reflect.DeepEqual(stableResumeHeaders(result.response.header), stableResumeHeaders(want.header)) {
			t.Fatalf("contender %d did not replay the stored response:\nwant=%d %v %s\ngot=%d %v %s",
				i, want.status, stableResumeHeaders(want.header), want.body,
				result.response.status, stableResumeHeaders(result.response.header), result.response.body)
		}
		requestID := result.response.header.Get("X-Request-Id")
		if _, err := uuid.Parse(requestID); err != nil {
			t.Fatalf("contender %d request ID %q is invalid: %v", i, requestID, err)
		}
		if _, duplicate := requestIDs[requestID]; duplicate {
			t.Fatalf("contender %d reused request ID %q", i, requestID)
		}
		requestIDs[requestID] = struct{}{}
	}

	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("read concurrent same-key result: %v", err)
	}
	if stored.Revision != created.Revision+1 || stored.Title != "concurrent same key" {
		t.Fatalf("same-key race mutated %d times: revision=%d title=%q", stored.Revision-created.Revision, stored.Revision, stored.Title)
	}
	var recordCount int
	var storedStatus int
	var storedBody, storedHeaders []byte
	if err := h.pool.QueryRow(h.ctx, `
		SELECT count(*) OVER (), response_status, response_body, response_headers
		FROM idempotency_records
		WHERE user_id = $1 AND idempotency_key = $2`, h.userID, key).Scan(
		&recordCount, &storedStatus, &storedBody, &storedHeaders,
	); err != nil {
		t.Fatalf("read concurrent idempotency record: %v", err)
	}
	if recordCount != 1 || storedStatus != want.status || !bytes.Equal(storedBody, want.body) {
		t.Fatalf("stored response = count %d status %d body %s, want 1/%d/%s",
			recordCount, storedStatus, storedBody, want.status, want.body)
	}
	var deterministicHeaders map[string]string
	if err := json.Unmarshal(storedHeaders, &deterministicHeaders); err != nil {
		t.Fatalf("decode stored deterministic headers: %v (headers=%s)", err, storedHeaders)
	}
	if deterministicHeaders["ETag"] != `"r2"` || deterministicHeaders[wireVersionHeader] != "2" {
		t.Fatalf("stored deterministic headers = %v, want ETag r2 and schema version 2", deterministicHeaders)
	}
	for name, value := range deterministicHeaders {
		if got := want.header.Get(name); got != value {
			t.Fatalf("stored header %s=%q, HTTP response has %q", name, value, got)
		}
	}
}

func TestIdempotency_FailureLeavesNoRecord(t *testing.T) {
	t.Run("validation rejection", func(t *testing.T) {
		h := newResumeAPITestHarness(t)
		created := h.createResume(t)
		path := fmt.Sprintf("/api/v1/resumes/%s/customization", created.ID)
		key := uuid.NewString()
		beforeRow := snapshotStoredResumeRow(t, h, created.ID)
		beforeRecords := h.snapshotUserTable(t, "idempotency_records")

		rejected := h.mutationRequest(t, http.MethodPatch, path,
			strings.NewReader(`{"deltas":[{"op":"set","path":"font.baseSizePx","value":99}]}`),
			created.Revision, key)
		assertResumeTestError(t, rejected, http.StatusUnprocessableEntity, "document_invalid")
		assertAdversarialFailureUnreserved(t, h, created.ID, beforeRow, beforeRecords)
		assertAdversarialCorrectedCustomization(t, h, created, path, key, created.Revision, beforeRecords)
	})

	t.Run("bounds rejection", func(t *testing.T) {
		h := newResumeAPITestHarness(t)
		created := h.createResume(t)
		path := fmt.Sprintf("/api/v1/resumes/%s/customization", created.ID)
		key := uuid.NewString()
		beforeRow := snapshotStoredResumeRow(t, h, created.ID)
		beforeRecords := h.snapshotUserTable(t, "idempotency_records")
		deltas := make([]string, 101)
		for i := range deltas {
			deltas[i] = `{"op":"set","path":"font.baseSizePx","value":18}`
		}

		rejected := h.mutationRequest(t, http.MethodPatch, path,
			strings.NewReader(`{"deltas":[`+strings.Join(deltas, ",")+`]}`),
			created.Revision, key)
		assertResumeTestError(t, rejected, http.StatusUnprocessableEntity, "document_invalid")
		assertAdversarialFailureUnreserved(t, h, created.ID, beforeRow, beforeRecords)
		assertAdversarialCorrectedCustomization(t, h, created, path, key, created.Revision, beforeRecords)
	})

	t.Run("stale CAS rejection", func(t *testing.T) {
		h := newResumeAPITestHarness(t)
		created := h.createResume(t)
		winner := resumeRequest(t, h, http.MethodPatch, apiResumePath+"/"+created.ID.String(),
			`{"title":"CAS winner"}`, created.Revision, uuid.New(), "2")
		if winner.status != http.StatusOK {
			t.Fatalf("seed CAS winner = %d %s", winner.status, winner.body)
		}
		path := fmt.Sprintf("/api/v1/resumes/%s/customization", created.ID)
		key := uuid.NewString()
		beforeRow := snapshotStoredResumeRow(t, h, created.ID)
		beforeRecords := h.snapshotUserTable(t, "idempotency_records")

		stale := h.mutationRequest(t, http.MethodPatch, path,
			strings.NewReader(`{"deltas":[{"op":"set","path":"font.baseSizePx","value":18}]}`),
			created.Revision, key)
		assertResumeTestError(t, stale, http.StatusPreconditionFailed, "revision_mismatch")
		assertAdversarialFailureUnreserved(t, h, created.ID, beforeRow, beforeRecords)
		assertAdversarialCorrectedCustomization(t, h, created, path, key, created.Revision+1, beforeRecords)
	})
}

func TestReadsNeverWrite(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	raw, err := os.ReadFile("../../../../packages/schema/fixtures/v1/minimal.json")
	if err != nil {
		t.Fatalf("read immutable v1 fixture: %v", err)
	}
	var legacy struct {
		PersonalDetails json.RawMessage `json:"personalDetails"`
		Content         json.RawMessage `json:"content"`
		Customization   json.RawMessage `json:"customization"`
	}
	if err := json.Unmarshal(raw, &legacy); err != nil {
		t.Fatalf("decode immutable v1 fixture: %v", err)
	}
	if _, err := h.pool.Exec(h.ctx, `
		UPDATE resumes
		SET personal_details = $2::jsonb, content = $3::jsonb,
		    customization = $4::jsonb, schema_version = 1
		WHERE id = $1 AND user_id = $5`,
		created.ID, legacy.PersonalDetails, legacy.Content, legacy.Customization, h.userID); err != nil {
		t.Fatalf("seed below-current stored row: %v", err)
	}
	before := snapshotStoredResumeRow(t, h, created.ID)
	if before.SchemaVersion != 1 {
		t.Fatalf("seeded schema version = %d, want 1", before.SchemaVersion)
	}

	const readers = 24
	requests := make([]*http.Request, readers)
	for i := range requests {
		requests[i] = newAdversarialExitReadRequest(t, h, apiResumePath+"/"+created.ID.String(), "2")
	}
	results := runAdversarialExitRequestsTogether(h.client, requests)
	for i, result := range results {
		if result.err != nil {
			t.Fatalf("GET %d failed: %v", i, result.err)
		}
		if result.response.status != http.StatusOK {
			t.Fatalf("GET %d status = %d body=%s", i, result.response.status, result.response.body)
		}
		resource := decodeResumeResource(t, result.response)
		if resource.SchemaVersion != 2 || resource.Revision != fmt.Sprint(created.Revision) {
			t.Fatalf("GET %d projected schema/revision = %d/%q", i, resource.SchemaVersion, resource.Revision)
		}
	}
	after := snapshotStoredResumeRow(t, h, created.ID)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("concurrent below-current GETs wrote the row:\nbefore=%+v\nafter=%+v", before, after)
	}
}

func TestIdempotency_BodylessReplay(t *testing.T) {
	t.Run("resume delete", func(t *testing.T) {
		h := newResumeAPITestHarness(t)
		created := h.createResume(t)
		path := apiResumePath + "/" + created.ID.String()
		fresh, replay := adversarialExitBodylessReplay(t, h, path, created.Revision)
		assertAdversarialBodylessResponse(t, fresh, "", "")
		assertAdversarialBodylessResponse(t, replay, "", "")
	})

	t.Run("entry delete", func(t *testing.T) {
		h := newResumeAPITestHarness(t)
		created := createEntryContractResume(t, h)
		path := fmt.Sprintf("/api/v1/resumes/%s/entries/work/01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60", created.ID)
		fresh, replay := adversarialExitBodylessReplay(t, h, path, created.Revision)
		assertAdversarialBodylessResponse(t, fresh, `"r2"`, "2")
		assertAdversarialBodylessResponse(t, replay, fresh.header.Get("ETag"), fresh.header.Get(wireVersionHeader))
	})

	t.Run("photo delete", func(t *testing.T) {
		h := newResumeAPITestHarness(t)
		created := h.createResume(t)
		uploaded := h.uploadPhotoRequest(t, created.ID, created.Revision, uuid.NewString(), "photo.png", makePhotoPNG(t))
		if uploaded.status != http.StatusOK {
			t.Fatalf("seed photo upload = %d %s", uploaded.status, uploaded.body)
		}
		path := fmt.Sprintf("/api/v1/resumes/%s/photo", created.ID)
		fresh, replay := adversarialExitBodylessReplay(t, h, path, created.Revision+1)
		assertAdversarialBodylessResponse(t, fresh, `"r3"`, "2")
		assertAdversarialBodylessResponse(t, replay, fresh.header.Get("ETag"), fresh.header.Get(wireVersionHeader))
	})
}

func Test412CarriesWinningState(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, *resumeAPITestHarness) (resume.Resume, string, string, string)
	}{
		{
			name: "resume metadata",
			setup: func(t *testing.T, h *resumeAPITestHarness) (resume.Resume, string, string, string) {
				created := h.createResume(t)
				return created, apiResumePath + "/" + created.ID.String(), `{"title":"winner"}`, `{"title":"loser"}`
			},
		},
		{
			name: "entry",
			setup: func(t *testing.T, h *resumeAPITestHarness) (resume.Resume, string, string, string) {
				created := createEntryContractResume(t, h)
				path := fmt.Sprintf("/api/v1/resumes/%s/entries/work", created.ID)
				return created, path,
					`{"entry":{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61"}}`,
					`{"entry":{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e62"}}`
			},
		},
		{
			name: "section",
			setup: func(t *testing.T, h *resumeAPITestHarness) (resume.Resume, string, string, string) {
				created := createEntryContractResume(t, h)
				return created, fmt.Sprintf("/api/v1/resumes/%s/sections/work", created.ID),
					`{"displayName":"winner"}`, `{"displayName":"loser"}`
			},
		},
		{
			name: "structure",
			setup: func(t *testing.T, h *resumeAPITestHarness) (resume.Resume, string, string, string) {
				created := createEntryContractResume(t, h)
				return created, fmt.Sprintf("/api/v1/resumes/%s/structure", created.ID),
					`{"commands":[{"op":"createSection","key":"skills","sectionType":"skill","column":"sidebar","index":0}]}`,
					`{"commands":[{"op":"createSection","key":"education","sectionType":"education","column":"sidebar","index":0}]}`
			},
		},
		{
			name: "personal details",
			setup: func(t *testing.T, h *resumeAPITestHarness) (resume.Resume, string, string, string) {
				created := h.createResume(t)
				return created, fmt.Sprintf("/api/v1/resumes/%s/personal-details", created.ID),
					`{"fullName":"Winner","details":[]}`, `{"fullName":"Loser","details":[]}`
			},
		},
		{
			name: "customization",
			setup: func(t *testing.T, h *resumeAPITestHarness) (resume.Resume, string, string, string) {
				created := h.createResume(t)
				return created, fmt.Sprintf("/api/v1/resumes/%s/customization", created.ID),
					`{"deltas":[{"op":"set","path":"font.baseSizePx","value":18}]}`,
					`{"deltas":[{"op":"set","path":"font.baseSizePx","value":19}]}`
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			created, path, winnerBody, loserBody := test.setup(t, h)
			winner := h.mutationRequest(t, http.MethodPatch, path, strings.NewReader(winnerBody), created.Revision, uuid.NewString())
			if winner.status != http.StatusOK {
				t.Fatalf("winner status = %d body=%s", winner.status, winner.body)
			}
			stale := h.mutationRequest(t, http.MethodPatch, path, strings.NewReader(loserBody), created.Revision, uuid.NewString())
			assertResumeTestError(t, stale, http.StatusPreconditionFailed, "revision_mismatch")
			assertAdversarial412MatchesImmediateGET(t, h, created.ID, stale)
		})
	}

	remaining := []struct {
		name    string
		execute func(*testing.T, *resumeAPITestHarness) (uuid.UUID, testHTTPResponse)
	}{
		{
			name: "resume delete",
			execute: func(t *testing.T, h *resumeAPITestHarness) (uuid.UUID, testHTTPResponse) {
				created := h.createResume(t)
				winner := resumeRequest(t, h, http.MethodPatch, apiResumePath+"/"+created.ID.String(),
					`{"title":"delete winner"}`, created.Revision, uuid.New(), "2")
				assertAdversarialWinnerResponse(t, winner)
				stale := h.mutationRequest(t, http.MethodDelete, apiResumePath+"/"+created.ID.String(), nil,
					created.Revision, uuid.NewString())
				return created.ID, stale
			},
		},
		{
			name: "entry delete",
			execute: func(t *testing.T, h *resumeAPITestHarness) (uuid.UUID, testHTTPResponse) {
				created := createEntryContractResume(t, h)
				winner := h.mutationRequest(t, http.MethodPatch,
					fmt.Sprintf("/api/v1/resumes/%s/sections/work", created.ID),
					strings.NewReader(`{"displayName":"delete winner"}`), created.Revision, uuid.NewString())
				assertAdversarialWinnerResponse(t, winner)
				stale := h.mutationRequest(t, http.MethodDelete,
					fmt.Sprintf("/api/v1/resumes/%s/entries/work/01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60", created.ID),
					nil, created.Revision, uuid.NewString())
				return created.ID, stale
			},
		},
		{
			name: "photo upload",
			execute: func(t *testing.T, h *resumeAPITestHarness) (uuid.UUID, testHTTPResponse) {
				created := h.createResume(t)
				winner := resumeRequest(t, h, http.MethodPatch, apiResumePath+"/"+created.ID.String(),
					`{"title":"upload winner"}`, created.Revision, uuid.New(), "2")
				assertAdversarialWinnerResponse(t, winner)
				beforeObjects := snapshotObjectKeys(t, h)
				stale := h.uploadPhotoRequest(t, created.ID, created.Revision, uuid.NewString(), "stale.png", makePhotoPNG(t))
				if afterObjects := snapshotObjectKeys(t, h); !reflect.DeepEqual(afterObjects, beforeObjects) {
					t.Fatalf("stale upload left an object: before=%v after=%v", beforeObjects, afterObjects)
				}
				return created.ID, stale
			},
		},
		{
			name: "photo crop",
			execute: func(t *testing.T, h *resumeAPITestHarness) (uuid.UUID, testHTTPResponse) {
				created := h.createResume(t)
				uploaded := h.uploadPhotoRequest(t, created.ID, created.Revision, uuid.NewString(), "photo.png", makePhotoPNG(t))
				assertAdversarialWinnerResponse(t, uploaded)
				winner := resumeRequest(t, h, http.MethodPatch, apiResumePath+"/"+created.ID.String(),
					`{"title":"crop winner"}`, created.Revision+1, uuid.New(), "2")
				assertAdversarialWinnerResponse(t, winner)
				stale := h.mutationRequest(t, http.MethodPatch, fmt.Sprintf("/api/v1/resumes/%s/photo", created.ID),
					strings.NewReader(`{"crop":{"x":0.1,"y":0.1,"width":0.8,"height":0.8}}`),
					created.Revision+1, uuid.NewString())
				return created.ID, stale
			},
		},
		{
			name: "photo delete",
			execute: func(t *testing.T, h *resumeAPITestHarness) (uuid.UUID, testHTTPResponse) {
				created := h.createResume(t)
				uploaded := h.uploadPhotoRequest(t, created.ID, created.Revision, uuid.NewString(), "photo.png", makePhotoPNG(t))
				assertAdversarialWinnerResponse(t, uploaded)
				winner := resumeRequest(t, h, http.MethodPatch, apiResumePath+"/"+created.ID.String(),
					`{"title":"photo delete winner"}`, created.Revision+1, uuid.New(), "2")
				assertAdversarialWinnerResponse(t, winner)
				stale := h.mutationRequest(t, http.MethodDelete, fmt.Sprintf("/api/v1/resumes/%s/photo", created.ID),
					nil, created.Revision+1, uuid.NewString())
				return created.ID, stale
			},
		},
	}
	for _, test := range remaining {
		t.Run(test.name, func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			resumeID, stale := test.execute(t, h)
			assertResumeTestError(t, stale, http.StatusPreconditionFailed, "revision_mismatch")
			assertAdversarial412MatchesImmediateGET(t, h, resumeID, stale)
		})
	}
}

func assertAdversarialFailureUnreserved(t *testing.T, h *resumeAPITestHarness, resumeID uuid.UUID,
	wantRow wireStoredRow, wantRecords string,
) {
	t.Helper()
	if gotRow := snapshotStoredResumeRow(t, h, resumeID); !reflect.DeepEqual(gotRow, wantRow) {
		t.Fatalf("rejected mutation changed stored state:\nwant=%+v\ngot=%+v", wantRow, gotRow)
	}
	if gotRecords := h.snapshotUserTable(t, "idempotency_records"); gotRecords != wantRecords {
		t.Fatalf("rejected mutation reserved idempotency state: before=%q after=%q", wantRecords, gotRecords)
	}
}

func assertAdversarialCorrectedCustomization(t *testing.T, h *resumeAPITestHarness, created resume.Resume,
	path, key string, revision int64, beforeRecords string,
) {
	t.Helper()
	corrected := h.mutationRequest(t, http.MethodPatch, path,
		strings.NewReader(`{"deltas":[{"op":"set","path":"font.baseSizePx","value":18}]}`),
		revision, key)
	if corrected.status != http.StatusOK {
		t.Fatalf("corrected same-key request = %d %s, want 200", corrected.status, corrected.body)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("read corrected mutation: %v", err)
	}
	if stored.Revision != revision+1 || stored.Doc.Customization.Font.BaseSizePx != 18 {
		t.Fatalf("corrected mutation revision/baseSizePx = %d/%d, want %d/18",
			stored.Revision, stored.Doc.Customization.Font.BaseSizePx, revision+1)
	}
	if afterSuccess := h.snapshotUserTable(t, "idempotency_records"); afterSuccess == beforeRecords {
		t.Fatal("corrected successful mutation did not store its idempotency result")
	}
}

func assertAdversarialWinnerResponse(t *testing.T, response testHTTPResponse) {
	t.Helper()
	if response.status != http.StatusOK {
		t.Fatalf("winner status = %d body=%s", response.status, response.body)
	}
}

func assertAdversarial412MatchesImmediateGET(t *testing.T, h *resumeAPITestHarness, resumeID uuid.UUID,
	mismatch testHTTPResponse,
) {
	t.Helper()
	fresh := resumeRequest(t, h, http.MethodGet, apiResumePath+"/"+resumeID.String(), "", 0, uuid.Nil, "2")
	if fresh.status != http.StatusOK {
		t.Fatalf("fresh GET status = %d body=%s", fresh.status, fresh.body)
	}
	mismatchState := adversarialExitMismatchState(t, mismatch)
	freshState := adversarialExitSuccessState(t, fresh)
	if mismatchState.Revision != freshState.Revision || !bytes.Equal(mismatchState.Document, freshState.Document) {
		t.Fatalf("412 winning state differs from GET:\n412 revision=%s document=%s\nGET revision=%s document=%s",
			mismatchState.Revision, mismatchState.Document, freshState.Revision, freshState.Document)
	}
}

func performAdversarialExitMutation(t *testing.T, h *resumeAPITestHarness, method, path string, body []byte,
	revision int64, key, version, contentType string,
) testHTTPResponse {
	t.Helper()
	request := newAdversarialExitMutationRequest(t, h, method, path, body, revision, key, version, contentType)
	response, err := performAdversarialExitRequest(h.client, request)
	if err != nil {
		t.Fatalf("perform mutation request: %v", err)
	}
	return response
}

func newAdversarialExitMutationRequest(t *testing.T, h *resumeAPITestHarness, method, path string, body []byte,
	revision int64, key, version, contentType string,
) *http.Request {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(h.ctx, method, h.server.URL+path, reader)
	if err != nil {
		t.Fatalf("build mutation request: %v", err)
	}
	request.AddCookie(h.cookie)
	request.Header.Set("Origin", resumeAPITestOrigin)
	request.Header.Set(auth.CSRFHeaderName, h.csrfToken)
	request.Header.Set("Idempotency-Key", key)
	request.Header.Set("If-Match", fmt.Sprintf(`"r%d"`, revision))
	if version != "" {
		request.Header.Set(wireVersionHeader, version)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	return request
}

func newAdversarialExitReadRequest(t *testing.T, h *resumeAPITestHarness, path, version string) *http.Request {
	t.Helper()
	request, err := http.NewRequestWithContext(h.ctx, http.MethodGet, h.server.URL+path, nil)
	if err != nil {
		t.Fatalf("build read request: %v", err)
	}
	request.AddCookie(h.cookie)
	request.Header.Set(wireVersionHeader, version)
	return request
}

func runAdversarialExitRequestsTogether(client *http.Client, requests []*http.Request) []adversarialExitResult {
	results := make([]adversarialExitResult, len(requests))
	start := make(chan struct{})
	var wait sync.WaitGroup
	for i := range requests {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			results[index].response, results[index].err = performAdversarialExitRequest(client, requests[index])
		}(i)
	}
	close(start)
	wait.Wait()
	return results
}

func performAdversarialExitRequest(client *http.Client, request *http.Request) (testHTTPResponse, error) {
	response, err := client.Do(request)
	if err != nil {
		return testHTTPResponse{}, err
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil {
		return testHTTPResponse{}, readErr
	}
	if closeErr != nil {
		return testHTTPResponse{}, closeErr
	}
	return testHTTPResponse{status: response.StatusCode, header: response.Header.Clone(), body: body}, nil
}

func adversarialExitJSONEqual(t *testing.T, first, second []byte) bool {
	t.Helper()
	var firstValue, secondValue any
	if err := json.Unmarshal(first, &firstValue); err != nil {
		t.Fatalf("decode first JSON value: %v (value=%s)", err, first)
	}
	if err := json.Unmarshal(second, &secondValue); err != nil {
		t.Fatalf("decode second JSON value: %v (value=%s)", err, second)
	}
	return reflect.DeepEqual(firstValue, secondValue)
}

func assertAdversarialRaceWinner(t *testing.T, document schema.Resume, winner int) {
	t.Helper()
	work := document.Content["work"]
	_, hasSkills := document.Content["skills"]
	wantEntries, wantDisplay, wantSkills, wantFont := 1, "", false, int64(14)
	switch winner {
	case 0:
		wantEntries = 2
	case 1:
		wantDisplay = "Race section"
	case 2:
		wantSkills = true
	case 3:
		wantFont = 18
	default:
		t.Fatalf("unknown winner index %d", winner)
	}
	gotDisplay := ""
	if work.DisplayName != nil {
		gotDisplay = *work.DisplayName
	}
	if len(work.WorkEntries) != wantEntries || gotDisplay != wantDisplay || hasSkills != wantSkills ||
		document.Customization.Font.BaseSizePx != wantFont {
		t.Fatalf("winning document contains a mixed state: entries=%d display=%q skills=%t font=%d; want %d/%q/%t/%d",
			len(work.WorkEntries), gotDisplay, hasSkills, document.Customization.Font.BaseSizePx,
			wantEntries, wantDisplay, wantSkills, wantFont)
	}
}

type adversarialExitRevisionDocument struct {
	Revision string
	Document json.RawMessage
}

func adversarialExitSuccessState(t *testing.T, response testHTTPResponse) adversarialExitRevisionDocument {
	t.Helper()
	var envelope struct {
		Data struct {
			Revision string          `json:"revision"`
			Document json.RawMessage `json:"document"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.body, &envelope); err != nil {
		t.Fatalf("decode success state: %v (body=%s)", err, response.body)
	}
	return adversarialExitRevisionDocument{Revision: envelope.Data.Revision, Document: envelope.Data.Document}
}

func adversarialExitMismatchState(t *testing.T, response testHTTPResponse) adversarialExitRevisionDocument {
	t.Helper()
	var envelope struct {
		Error struct {
			Details struct {
				Revision string          `json:"revision"`
				Document json.RawMessage `json:"document"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.body, &envelope); err != nil {
		t.Fatalf("decode mismatch state: %v (body=%s)", err, response.body)
	}
	return adversarialExitRevisionDocument{Revision: envelope.Error.Details.Revision, Document: envelope.Error.Details.Document}
}

func adversarialExitBodylessReplay(t *testing.T, h *resumeAPITestHarness, path string, revision int64) (testHTTPResponse, testHTTPResponse) {
	t.Helper()
	key := uuid.NewString()
	freshRequest := newAdversarialExitMutationRequest(t, h, http.MethodDelete, path, nil, revision, key, "2", "")
	fresh, err := performAdversarialExitRequest(h.client, freshRequest)
	if err != nil {
		t.Fatalf("perform fresh bodyless delete: %v", err)
	}
	replayRequest := newAdversarialExitMutationRequest(t, h, http.MethodDelete, path, nil, revision, key, "2", "application/json")
	replay, err := performAdversarialExitRequest(h.client, replayRequest)
	if err != nil {
		t.Fatalf("perform replayed bodyless delete: %v", err)
	}
	return fresh, replay
}

func assertAdversarialBodylessResponse(t *testing.T, response testHTTPResponse, etag, version string) {
	t.Helper()
	if response.status != http.StatusNoContent || len(response.body) != 0 || response.header.Get("Content-Type") != "" ||
		response.header.Get("ETag") != etag || response.header.Get(wireVersionHeader) != version {
		t.Fatalf("bodyless response = status %d content-type=%q etag=%q version=%q body=%q",
			response.status, response.header.Get("Content-Type"), response.header.Get("ETag"),
			response.header.Get(wireVersionHeader), response.body)
	}
}
