package resumeapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	schema "danny.vn/aboutme/packages/schema/gen/go"
	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
)

func TestCheckpointRoutesRejectAgentPrincipal(t *testing.T) {
	t.Parallel()
	principal, err := NewAgentPrincipal(uuid.New(), uuid.New(), uuid.New(), [32]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, route := range registeredRoutes() {
		if route.Operation != "listResumeCheckpoints" && route.Operation != "getResumeCheckpoint" &&
			route.Operation != "restoreResumeCheckpoint" && route.Operation != "deleteResumeCheckpoint" &&
			route.Operation != "clearResumeCheckpoints" {
			continue
		}
		count++
		t.Run(route.Operation, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), route.Method, concreteRoutePath(route.Pattern), nil)
			r = r.WithContext(contextWithAgentPrincipal(r.Context(), principal))
			w := httptest.NewRecorder()
			route.Handler(&Service{}, w, r)
			assertRouteError(t, testHTTPResponse{status: w.Code, body: w.Body.Bytes()}, http.StatusNotFound, "resume_not_found")
		})
	}
	if count != 5 {
		t.Fatalf("registered %d checkpoint routes, want 5", count)
	}
}

func TestCheckpointRoutesListRestoreAndClear(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	revision := created.Revision + 1
	changed := resumeRequest(t, h, http.MethodPatch, apiResumePath+"/"+created.ID.String()+"/personal-details",
		`{"fullName":"After","details":[]}`, created.Revision, uuid.New(), "6")
	if changed.status != http.StatusOK {
		t.Fatalf("first REST write: %d %s", changed.status, changed.body)
	}
	var checkpointID uuid.UUID
	if err := h.pool.QueryRow(h.ctx, `SELECT id FROM resume_checkpoints WHERE resume_id = $1 AND cause = 'edit'`, created.ID).Scan(&checkpointID); err != nil {
		t.Fatal(err)
	}

	collection := apiResumePath + "/" + created.ID.String() + "/checkpoints"
	item := collection + "/" + checkpointID.String()
	for _, path := range []string{collection, item} {
		res := h.request(t, http.MethodGet, path, nil, true, false)
		if res.status != http.StatusOK || res.header.Get(wireVersionHeader) != "" || res.header.Get("ETag") != "" {
			t.Fatalf("GET checkpoint status=%d headers=%v: %s", res.status, res.header, res.body)
		}
	}

	assertRouteError(t, checkpointWriteRequest(t, h, http.MethodPost, item+"/restore", uuid.NewString(), created.Revision), http.StatusPreconditionFailed, "revision_mismatch")
	key := uuid.NewString()
	first := checkpointWriteRequest(t, h, http.MethodPost, item+"/restore", key, revision)
	if first.status != http.StatusOK || first.header.Get(wireVersionHeader) == "" {
		t.Fatalf("restore status=%d: %s", first.status, first.body)
	}
	replay := checkpointWriteRequest(t, h, http.MethodPost, item+"/restore", key, revision)
	if replay.status != first.status || string(replay.body) != string(first.body) {
		t.Fatalf("restore replay status=%d: %s", replay.status, replay.body)
	}
	var count int
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM resume_checkpoints WHERE resume_id = $1`, created.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("restore and replay stored %d checkpoints: %v", count, err)
	}
	var title, name string
	if err := h.pool.QueryRow(h.ctx, `SELECT title, personal_details->>'fullName' FROM resume_checkpoints
        WHERE resume_id=$1 AND cause='restore' AND actor_grant_id IS NULL`, created.ID).Scan(&title, &name); err != nil || title != created.Title || name != "After" {
		t.Fatalf("restore checkpoint title=%q name=%q error=%v", title, name, err)
	}
	if got := checkpointWriteRequest(t, h, http.MethodDelete, item, uuid.NewString(), 0); got.status != http.StatusNoContent {
		t.Fatalf("delete status=%d: %s", got.status, got.body)
	}
	if got := checkpointWriteRequest(t, h, http.MethodDelete, collection, uuid.NewString(), 0); got.status != http.StatusNoContent {
		t.Fatalf("clear status=%d: %s", got.status, got.body)
	}
	res := h.request(t, http.MethodGet, collection, nil, true, false)
	if res.status != http.StatusOK || string(res.body) != `{"data":[]}` {
		t.Fatalf("list after clear status=%d: %s", res.status, res.body)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil || stored.Revision != revision+1 {
		t.Fatalf("revision=%d after restore and deletes, want %d: %v", stored.Revision, revision+1, err)
	}
}

func seedCheckpointForTest(t *testing.T, h *resumeAPITestHarness, id uuid.UUID) uuid.UUID {
	t.Helper()
	var checkpoint uuid.UUID
	if err := h.pool.QueryRow(h.ctx, `INSERT INTO resume_checkpoints
    (resume_id, started_at, cause, title, lng, schema_version, personal_details, content, customization, byte_size)
    SELECT id, $2, 'edit', title, lng, schema_version, personal_details - 'photo', content, customization, 100
    FROM resumes WHERE id = $1 RETURNING id`, id, h.service.clock()).Scan(&checkpoint); err != nil {
		t.Fatal(err)
	}
	return checkpoint
}

func checkpointDeletionOperation(operation string) bool {
	return operation == "deleteResumeCheckpoint" || operation == "clearResumeCheckpoints"
}

func checkpointWriteRequest(t *testing.T, h *resumeAPITestHarness, method, path, key string, match int64, versions ...string) testHTTPResponse {
	t.Helper()
	req, err := http.NewRequestWithContext(h.ctx, method, h.server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(h.cookie)
	req.Header.Set("Origin", resumeAPITestOrigin)
	req.Header.Set(auth.CSRFHeaderName, h.csrfToken)
	req.Header.Set("Idempotency-Key", key)
	for _, version := range versions {
		req.Header.Add(wireVersionHeader, version)
	}
	if match > 0 {
		req.Header.Set("If-Match", fmt.Sprintf(`"r%d"`, match))
	}
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return snapshotHTTPResponse(t, resp)
}

func TestCheckpointScopeAndVisibility(t *testing.T) {
	h := newResumeAPITestHarness(t)
	other := newResumeAPITestHarness(t)
	first := h.createResume(t)
	second := h.createResume(t)
	foreign := other.createResume(t)
	firstCheckpoint := seedCheckpointForTest(t, h, first.ID)
	foreignCheckpoint := seedCheckpointForTest(t, other, foreign.ID)
	unknownCheckpoint := seedCheckpointForTest(t, h, second.ID)
	expiredCheckpoint := seedCheckpointForTest(t, h, second.ID)
	if _, err := h.pool.Exec(h.ctx, `UPDATE resume_checkpoints SET schema_version=9999 WHERE id=$1`, unknownCheckpoint); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(h.ctx, `UPDATE resume_checkpoints SET started_at=$2 WHERE id=$1`, expiredCheckpoint, h.service.clock().Add(-31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	collection := func(id uuid.UUID) string { return apiResumePath + "/" + id.String() + "/checkpoints" }
	for _, tc := range []struct {
		name              string
		owner, checkpoint uuid.UUID
	}{
		{"different resume", second.ID, firstCheckpoint},
		{"different account", foreign.ID, foreignCheckpoint},
		{"unknown version", second.ID, unknownCheckpoint},
		{"expired", second.ID, expiredCheckpoint},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := collection(tc.owner) + "/" + tc.checkpoint.String()
			assertRouteError(t, h.request(t, http.MethodGet, path, nil, true, false), 404, "resume_not_found")
			assertRouteError(t, checkpointWriteRequest(t, h, http.MethodPost, path+"/restore", uuid.NewString(), 1), 404, "resume_not_found")
			assertRouteError(t, checkpointWriteRequest(t, h, http.MethodDelete, path, uuid.NewString(), 0), 404, "resume_not_found")
		})
	}
	assertRouteError(t, h.request(t, http.MethodGet, collection(foreign.ID), nil, true, false), 404, "resume_not_found")
	assertRouteError(t, checkpointWriteRequest(t, h, http.MethodDelete, collection(foreign.ID), uuid.NewString(), 0), 404, "resume_not_found")
	got := h.request(t, http.MethodGet, collection(second.ID), nil, true, false)
	if got.status != 200 || string(got.body) != `{"data":[]}` {
		t.Fatalf("hidden checkpoint list: %d %s", got.status, got.body)
	}
}

func TestRestoreCheckpointKeepsPhotoAndPublishingAndCleansDocument(t *testing.T) {
	h := newResumeAPITestHarness(t)
	current := h.createResume(t)
	key := fmt.Sprintf("resumes/%s/photo-0123456789abcdef0123456789abcdef.jpg", current.ID)
	photo := &schema.Photo{Key: key, Crop: &schema.PhotoCrop{X: 0.1, Y: 0.1, Width: 0.8, Height: 0.8}}
	doc := current.Doc
	doc.PersonalDetails.Photo = photo
	rev, err := h.resumes.SaveDocument(h.ctx, h.userID, current.ID, doc, current.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.pool.Exec(h.ctx, `UPDATE resumes SET slug=$2, live=true, download_enabled=false, seo_geo_enabled=true,
  sign_in_to_view=false, public_title='Public title', favicon_emoji='☕' WHERE id=$1`, current.ID, "checkpoint-"+uuid.NewString()[:8]); err != nil {
		t.Fatal(err)
	}
	entry := linkedEntry(uuid.New(), "Keep project text")
	entry.Description = new("<script>alert(1)</script><p>safe</p>")
	historical := withProjectSection(loadMinimalDocument(t), "projects", entry)
	checkpoint := insertCheckpointDocumentFixture(t, h, current.ID, historical, docmigrate.CurrentVersion)
	path := apiResumePath + "/" + current.ID.String() + "/checkpoints/" + checkpoint.String() + "/restore"
	res := checkpointWriteRequest(t, h, http.MethodPost, path, uuid.NewString(), rev)
	if res.status != 200 {
		t.Fatalf("restore=%d %s", res.status, res.body)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, current.ID)
	if err != nil {
		t.Fatal(err)
	}
	savedPhoto := stored.Doc.PersonalDetails.Photo
	if savedPhoto == nil || savedPhoto.Key != key || savedPhoto.Crop == nil || *savedPhoto.Crop != *photo.Crop ||
		stored.Slug == nil || !stored.Live || stored.DownloadEnabled || !stored.SEOGeoEnabled || stored.SignInToView ||
		stored.PublicTitle == nil || *stored.PublicTitle != "Public title" || stored.FaviconEmoji == nil || *stored.FaviconEmoji != "☕" ||
		stored.Title != "Historic title" || stored.Lng == nil || *stored.Lng != "vi" {
		t.Fatalf("restore did not preserve settings or restore title/language: %+v", stored)
	}
	savedEntry := stored.Doc.Content["projects"].ProjectEntries[0]
	if savedEntry.ProjectID != nil || savedEntry.Title == nil || *savedEntry.Title != "Keep project text" || savedEntry.Description == nil || *savedEntry.Description != "<p>safe</p>" {
		t.Fatalf("restore did not clean and unlink the entry: %+v", savedEntry)
	}
}

func TestRestoreCheckpointOlderInvalidAndBusy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version int32
		status  int
		code    string
	}{
		{"older", 1, http.StatusOK, ""},
		{"invalid", docmigrate.CurrentVersion, http.StatusUnprocessableEntity, "document_invalid"},
		{"busy", docmigrate.CurrentVersion, http.StatusServiceUnavailable, "public_state_busy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			current := h.createResume(t)
			doc := loadMinimalDocument(t)
			doc.SchemaVersion = int64(tc.version)
			if tc.name == "invalid" {
				doc.Customization.Layout.Sections.Main = []string{"absent"}
			}
			if tc.name == "busy" {
				h.service.idempotency = checkpointBusyRecheck{h.service.idempotency}
			}
			var checkpoint uuid.UUID
			if tc.name == "older" {
				raw, readErr := os.ReadFile("../../../../packages/schema/fixtures/v1/minimal.json")
				if readErr != nil {
					t.Fatal(readErr)
				}
				checkpoint = insertCheckpointRawFixture(t, h, current.ID, raw, tc.version)
			} else {
				checkpoint = insertCheckpointDocumentFixture(t, h, current.ID, doc, tc.version)
			}
			path := apiResumePath + "/" + current.ID.String() + "/checkpoints/" + checkpoint.String() + "/restore"
			response := checkpointWriteRequest(t, h, http.MethodPost, path, uuid.NewString(), 1)
			if tc.code != "" {
				assertRouteError(t, response, tc.status, tc.code)
			} else if response.status != tc.status {
				t.Fatalf("restore=%d %s", response.status, response.body)
			}
			got, err := h.resumes.Get(h.ctx, h.userID, current.ID)
			want := current.Revision
			if tc.status == http.StatusOK {
				want++
			}
			if err != nil || got.Revision != want || got.StoredSchemaVersion != docmigrate.CurrentVersion {
				t.Fatalf("revision=%d version=%d error=%v, want revision %d and current schema", got.Revision, got.StoredSchemaVersion, err, want)
			}
			if tc.name == "older" && got.Doc.Customization.Font.Family != "inter" {
				t.Fatalf("stored v1 Inter family was not converted: %q", got.Doc.Customization.Font.Family)
			}
		})
	}
}

func insertCheckpointDocumentFixture(t *testing.T, h *resumeAPITestHarness, id uuid.UUID, doc schema.Resume, version int32) uuid.UUID {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return insertCheckpointRawFixture(t, h, id, raw, version)
}

func insertCheckpointRawFixture(t *testing.T, h *resumeAPITestHarness, id uuid.UUID, raw []byte, version int32) uuid.UUID {
	t.Helper()
	var checkpoint uuid.UUID
	if err := h.pool.QueryRow(h.ctx, `INSERT INTO resume_checkpoints
  (resume_id,started_at,cause,title,lng,schema_version,personal_details,content,customization,byte_size)
  SELECT $1,$2,'edit','Historic title','vi',$3,doc->'personalDetails',doc->'content',doc->'customization',100
  FROM (SELECT $4::jsonb AS doc) AS fixture RETURNING id`, id, h.service.clock(), version, raw).Scan(&checkpoint); err != nil {
		t.Fatal(err)
	}
	return checkpoint
}

type checkpointBusyRecheck struct{ idempotencyBoundary }

func (b checkpointBusyRecheck) Recheck(context.Context, uuid.UUID, string, uuid.UUID, [32]byte) (resume.RecheckResult, error) {
	return resume.RecheckResult{}, &publicstate.DrainTimeoutError{}
}

func TestCheckpointReadProjectsStoredLanguage(t *testing.T) {
	h := newResumeAPITestHarness(t)
	row := h.createResume(t)
	for _, tc := range []struct {
		name string
		lng  *string
	}{{"null", nil}, {"empty", new("")}} {
		t.Run(tc.name, func(t *testing.T) {
			checkpoint := seedCheckpointForTest(t, h, row.ID)
			if _, err := h.pool.Exec(h.ctx, `UPDATE resume_checkpoints SET lng=$2 WHERE id=$1`, checkpoint, tc.lng); err != nil {
				t.Fatal(err)
			}
			path := apiResumePath + "/" + row.ID.String() + "/checkpoints/" + checkpoint.String()
			result := h.request(t, http.MethodGet, path, nil, true, false)
			var payload struct {
				Data struct {
					Lng string `json:"lng"`
				} `json:"data"`
			}
			if err := json.Unmarshal(result.body, &payload); err != nil || result.status != http.StatusOK || payload.Data.Lng != "und" {
				t.Fatalf("GET projected language=%q status=%d error=%v", payload.Data.Lng, result.status, err)
			}
		})
	}
}

func TestCheckpointDeletesIgnoreSchemaVersionHeader(t *testing.T) {
	h := newResumeAPITestHarness(t)
	row := h.createResume(t)
	collection := apiResumePath + "/" + row.ID.String() + "/checkpoints"
	for _, operation := range []struct {
		name string
		one  bool
	}{{"delete one", true}, {"clear all", false}} {
		t.Run(operation.name, func(t *testing.T) {
			for _, tc := range []struct {
				name     string
				versions []string
			}{{"absent", nil}, {"present", []string{"2"}}, {"unsupported and repeated", []string{"future", "2"}}} {
				t.Run(tc.name, func(t *testing.T) {
					id := seedCheckpointForTest(t, h, row.ID)
					path := collection
					if operation.one {
						path += "/" + id.String()
					}
					key := uuid.NewString()
					result := checkpointWriteRequest(t, h, http.MethodDelete, path, key, 0, tc.versions...)
					if result.status != http.StatusNoContent {
						t.Fatalf("status=%d body=%s", result.status, result.body)
					}
					replay := checkpointWriteRequest(t, h, http.MethodDelete, path, key, 0, "some other value")
					if replay.status != http.StatusNoContent {
						t.Fatalf("replay status=%d body=%s", replay.status, replay.body)
					}
					var exists bool
					if err := h.pool.QueryRow(h.ctx, `SELECT EXISTS(SELECT 1 FROM resume_checkpoints WHERE id=$1)`, id).Scan(&exists); err != nil || exists {
						t.Fatalf("checkpoint remains: %v, %v", exists, err)
					}
				})
			}
		})
	}
}
