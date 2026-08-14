package resumeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

type resumeTestEnvelope struct {
	Data json.RawMessage `json:"data"`
}

type resumeTestResource struct {
	ID              uuid.UUID       `json:"id"`
	Title           string          `json:"title"`
	Lng             string          `json:"lng"`
	Revision        string          `json:"revision"`
	Live            bool            `json:"live"`
	Slug            *string         `json:"slug"`
	DownloadEnabled bool            `json:"downloadEnabled"`
	SEOGeoEnabled   bool            `json:"seoGeoEnabled"`
	SchemaVersion   int32           `json:"schemaVersion"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
	Document        json.RawMessage `json:"document"`
}

func resumeRequest(t *testing.T, h *resumeAPITestHarness, method, path, body string, revision int64,
	key uuid.UUID, version string,
) testHTTPResponse {
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
	if method == http.MethodPost || method == http.MethodPatch || method == http.MethodDelete {
		req.Header.Set("Origin", resumeAPITestOrigin)
		req.Header.Set(auth.CSRFHeaderName, h.csrfToken)
		req.Header.Set("Idempotency-Key", key.String())
		if method != http.MethodPost {
			req.Header.Set("If-Match", fmt.Sprintf(`"r%d"`, revision))
		}
		if method != http.MethodDelete {
			req.Header.Set("Content-Type", "application/json")
		}
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

func decodeResumeResource(t *testing.T, response testHTTPResponse) resumeTestResource {
	t.Helper()
	var envelope resumeTestEnvelope
	if err := json.Unmarshal(response.body, &envelope); err != nil {
		t.Fatalf("decode success envelope: %v (body=%s)", err, response.body)
	}
	var resource resumeTestResource
	if err := json.Unmarshal(envelope.Data, &resource); err != nil {
		t.Fatalf("decode resume resource: %v (data=%s)", err, envelope.Data)
	}
	return resource
}

func TestResumeCRUD_LifecycleAndWriteEnvelope(t *testing.T) {
	h := newResumeAPITestHarness(t)

	listed := resumeRequest(t, h, http.MethodGet, apiResumePath, "", 0, uuid.Nil, "2")
	if listed.status != http.StatusOK {
		t.Fatalf("empty list status = %d, want 200 (body=%s)", listed.status, listed.body)
	}
	if listed.header.Get(wireVersionHeader) != "2" || listed.header.Get("ETag") != "" {
		t.Fatalf("empty list headers schema=%q etag=%q, want 2 and absent",
			listed.header.Get(wireVersionHeader), listed.header.Get("ETag"))
	}
	var listEnvelope struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(listed.body, &listEnvelope); err != nil || len(listEnvelope.Data) != 0 {
		t.Fatalf("empty list = %s, decode error %v; want {data:[]}", listed.body, err)
	}

	createKey := uuid.New()
	createdResponse := resumeRequest(t, h, http.MethodPost, apiResumePath,
		`{"title":"","lng":"EN-us"}`, 0, createKey, "2")
	if createdResponse.status != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 (body=%s)", createdResponse.status, createdResponse.body)
	}
	created := decodeResumeResource(t, createdResponse)
	if created.Title != "" || created.Lng != "en-US" || created.Revision != "1" || created.SchemaVersion != 2 {
		t.Fatalf("created resource = %+v, want cleared title, canonical en-US, revision 1, schema 2", created)
	}
	if len(created.Document) == 0 {
		t.Fatal("created response omitted the default draft document")
	}
	wantLocation := apiResumePath + "/" + created.ID.String()
	if createdResponse.header.Get("Location") != wantLocation || createdResponse.header.Get("ETag") != `"r1"` ||
		createdResponse.header.Get(wireVersionHeader) != "2" {
		t.Fatalf("create headers location=%q etag=%q schema=%q",
			createdResponse.header.Get("Location"), createdResponse.header.Get("ETag"), createdResponse.header.Get(wireVersionHeader))
	}

	replay := resumeRequest(t, h, http.MethodPost, apiResumePath,
		`{"title":"","lng":"EN-us"}`, 0, createKey, "2")
	if replay.status != http.StatusCreated || !bytes.Equal(replay.body, createdResponse.body) ||
		!reflect.DeepEqual(replay.header.Values("Location"), createdResponse.header.Values("Location")) {
		t.Fatalf("create replay differs: first=%d %s replay=%d %s", createdResponse.status, createdResponse.body, replay.status, replay.body)
	}
	reuse := resumeRequest(t, h, http.MethodPost, apiResumePath,
		`{"title":"different"}`, 0, createKey, "2")
	assertResumeTestError(t, reuse, http.StatusConflict, "idempotency_key_reuse")

	path := apiResumePath + "/" + created.ID.String()
	gotResponse := resumeRequest(t, h, http.MethodGet, path, "", 0, uuid.Nil, "2")
	if gotResponse.status != http.StatusOK || gotResponse.header.Get("ETag") != `"r1"` {
		t.Fatalf("get = status %d etag %q body=%s", gotResponse.status, gotResponse.header.Get("ETag"), gotResponse.body)
	}
	got := decodeResumeResource(t, gotResponse)
	var createdDocument, gotDocument any
	if err := json.Unmarshal(created.Document, &createdDocument); err != nil {
		t.Fatalf("decode created document: %v", err)
	}
	if err := json.Unmarshal(got.Document, &gotDocument); err != nil {
		t.Fatalf("decode reloaded document: %v", err)
	}
	if !reflect.DeepEqual(gotDocument, createdDocument) {
		t.Fatalf("reloaded document changed:\ncreate=%s\nget=%s", created.Document, got.Document)
	}

	patchKey := uuid.New()
	patchedResponse := resumeRequest(t, h, http.MethodPatch, path, `{"title":"renamed","lng":""}`, 1, patchKey, "2")
	if patchedResponse.status != http.StatusOK {
		t.Fatalf("patch status = %d, want 200 (body=%s)", patchedResponse.status, patchedResponse.body)
	}
	patched := decodeResumeResource(t, patchedResponse)
	if patched.Title != "renamed" || patched.Lng != "und" || patched.Revision != "2" {
		t.Fatalf("patched resource = %+v, want title renamed, lng und, revision 2", patched)
	}
	updatedGet := decodeResumeResource(t, resumeRequest(t, h, http.MethodGet, path, "", 0, uuid.Nil, "2"))
	if !patched.UpdatedAt.Equal(updatedGet.UpdatedAt) || patched.UpdatedAt.Equal(created.UpdatedAt) {
		t.Fatalf("patch updatedAt = %s, fresh GET = %s, create = %s", patched.UpdatedAt, updatedGet.UpdatedAt, created.UpdatedAt)
	}
	patchReplay := resumeRequest(t, h, http.MethodPatch, path, `{"title":"renamed","lng":""}`, 1, patchKey, "2")
	if patchReplay.status != http.StatusOK || !bytes.Equal(patchReplay.body, patchedResponse.body) {
		t.Fatalf("patch replay = %d %s, want stored 200 %s", patchReplay.status, patchReplay.body, patchedResponse.body)
	}

	stale := resumeRequest(t, h, http.MethodPatch, path, `{"title":"loser"}`, 1, uuid.New(), "2")
	assertResumeTestError(t, stale, http.StatusPreconditionFailed, "revision_mismatch")
	var staleEnvelope struct {
		Error struct {
			Details struct {
				Revision string          `json:"revision"`
				Document json.RawMessage `json:"document"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stale.body, &staleEnvelope); err != nil || staleEnvelope.Error.Details.Revision != "2" ||
		len(staleEnvelope.Error.Details.Document) == 0 {
		t.Fatalf("stale details = %s, decode error %v", stale.body, err)
	}

	deleteKey := uuid.New()
	deleted := resumeRequest(t, h, http.MethodDelete, path, "", 2, deleteKey, "2")
	if deleted.status != http.StatusNoContent || len(deleted.body) != 0 || deleted.header.Get("Content-Type") != "" ||
		deleted.header.Get("ETag") != "" || deleted.header.Get(wireVersionHeader) != "" {
		t.Fatalf("delete = status %d headers=%v body=%q, want empty 204", deleted.status, deleted.header, deleted.body)
	}
	deleteReplay := resumeRequest(t, h, http.MethodDelete, path, "", 2, deleteKey, "2")
	if deleteReplay.status != http.StatusNoContent || len(deleteReplay.body) != 0 {
		t.Fatalf("delete replay = %d %q, want empty 204", deleteReplay.status, deleteReplay.body)
	}
	missingDelete := resumeRequest(t, h, http.MethodDelete, path, "", 2, uuid.New(), "2")
	assertResumeTestError(t, missingDelete, http.StatusNotFound, "resume_not_found")
}

func TestPublishTransitionStoresFlagsAndReplaysExactResponse(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created, err := h.resumes.Create(h.ctx, h.userID, "Published", publishCompleteDocument(t))
	if err != nil {
		t.Fatalf("create publishable resume: %v", err)
	}
	path := apiResumePath + "/" + created.ID.String() + "/publish"
	slug := "publish-" + uuid.NewString()[:8]
	key := uuid.NewString()
	body := `{"slug":"` + slug + `","live":true,"downloadEnabled":true,"seoGeoEnabled":true}`
	first := h.mutationRequest(t, http.MethodPost, path, strings.NewReader(body), created.Revision, key)
	if first.status != http.StatusOK || first.header.Get("ETag") != `"r2"` {
		t.Fatalf("publish = status %d etag %q body=%s, want 200 r2", first.status, first.header.Get("ETag"), first.body)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("read published resume: %v", err)
	}
	if stored.Slug == nil || *stored.Slug != slug || !stored.Live || !stored.DownloadEnabled || !stored.SEOGeoEnabled {
		t.Fatalf("stored publish state = %+v, want live claimed flags", stored)
	}
	replay := h.mutationRequest(t, http.MethodPost, path, strings.NewReader(body), created.Revision, key)
	if replay.status != first.status || !bytes.Equal(replay.body, first.body) || replay.header.Get("ETag") != first.header.Get("ETag") {
		t.Fatalf("publish replay differs: first=%d %s replay=%d %s", first.status, first.body, replay.status, replay.body)
	}
}

func TestPublishChangedBodyRejectsIdempotencyKeyReuse(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created, err := h.resumes.Create(h.ctx, h.userID, "Published", publishCompleteDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	path := apiResumePath + "/" + created.ID.String() + "/publish"
	slug := "replay-" + uuid.NewString()[:8]
	key := uuid.NewString()
	first := h.mutationRequest(t, http.MethodPost, path, strings.NewReader(`{"slug":"`+slug+`","live":true,"downloadEnabled":false,"seoGeoEnabled":false}`), created.Revision, key)
	if first.status != http.StatusOK {
		t.Fatalf("first publish = %d %s", first.status, first.body)
	}
	changed := h.mutationRequest(t, http.MethodPost, path, strings.NewReader(`{"slug":"`+slug+`","live":true,"downloadEnabled":true,"seoGeoEnabled":false}`), created.Revision, key)
	assertResumeTestError(t, changed, http.StatusConflict, "idempotency_key_reuse")
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Revision != created.Revision+1 || stored.DownloadEnabled {
		t.Fatalf("changed-body replay mutated stored publish state: %+v", stored)
	}
}

func TestDeleteReplayPrecedesExpiredReauthAndFence(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created, err := h.resumes.Create(h.ctx, h.userID, "Delete replay", publishCompleteDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	slug := "delete-" + uuid.NewString()[:8]
	published := h.mutationRequest(t, http.MethodPost, apiResumePath+"/"+created.ID.String()+"/publish", strings.NewReader(`{"slug":"`+slug+`","live":true,"downloadEnabled":false,"seoGeoEnabled":false}`), created.Revision, uuid.NewString())
	if published.status != http.StatusOK {
		t.Fatalf("publish = %d %s", published.status, published.body)
	}
	key := uuid.NewString()
	deleted := h.mutationRequest(t, http.MethodDelete, apiResumePath+"/"+created.ID.String(), nil, created.Revision+1, key)
	if deleted.status != http.StatusNoContent {
		t.Fatalf("delete = %d %s", deleted.status, deleted.body)
	}
	if _, err := h.pool.Exec(h.ctx, `UPDATE sessions SET reauthenticated_at = now() - interval '16 minutes' WHERE id = $1`, h.session.ID); err != nil {
		t.Fatal(err)
	}
	replay := h.mutationRequest(t, http.MethodDelete, apiResumePath+"/"+created.ID.String(), nil, created.Revision+1, key)
	if replay.status != http.StatusNoContent || len(replay.body) != 0 {
		t.Fatalf("expired-reauth replay = %d %s, want exact 204", replay.status, replay.body)
	}
	if err := h.service.coordinator.Ready(); err != nil {
		t.Fatalf("replay left fence closed: %v", err)
	}
}

func TestContentContendersSerializeThenLoserGetsFresh412(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	path := apiResumePath + "/" + created.ID.String()
	start := make(chan struct{})
	responses := make(chan testHTTPResponse, 2)
	for _, title := range []string{"winner-a", "winner-b"} {
		title := title
		go func() {
			<-start
			responses <- resumeRequest(t, h, http.MethodPatch, path, `{"title":"`+title+`"}`, created.Revision, uuid.New(), "2")
		}()
	}
	close(start)
	first, second := <-responses, <-responses
	var stale testHTTPResponse
	if first.status == http.StatusOK && second.status == http.StatusPreconditionFailed {
		stale = second
	} else if second.status == http.StatusOK && first.status == http.StatusPreconditionFailed {
		stale = first
	} else {
		t.Fatalf("content contenders = (%d,%s), (%d,%s); want 200 and fresh 412", first.status, first.body, second.status, second.body)
	}
	var envelope struct {
		Error struct {
			Details struct {
				Revision string `json:"revision"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stale.body, &envelope); err != nil || envelope.Error.Details.Revision != "2" {
		t.Fatalf("stale contender details = %s err=%v, want winner revision 2", stale.body, err)
	}
}

func assertResumeTestError(t *testing.T, response testHTTPResponse, status int, code string) {
	t.Helper()
	if response.status != status {
		t.Fatalf("status = %d, want %d (body=%s)", response.status, status, response.body)
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.body, &envelope); err != nil {
		t.Fatalf("decode error response: %v (body=%s)", err, response.body)
	}
	if envelope.Error.Code != code {
		t.Fatalf("error code = %q, want %q (body=%s)", envelope.Error.Code, code, response.body)
	}
}

func TestResumeCreate_CapAndConcurrentHTTPEnforcement(t *testing.T) {
	h := newResumeAPITestHarness(t)
	const attempts = 20
	statuses := make(chan int, attempts)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			response := resumeRequest(t, h, http.MethodPost, apiResumePath,
				fmt.Sprintf(`{"title":"resume %02d"}`, i), 0, uuid.New(), "2")
			statuses <- response.status
		}(i)
	}
	close(start)
	wg.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[http.StatusCreated] != 3 || counts[http.StatusConflict] != 17 || len(counts) != 2 {
		t.Fatalf("concurrent create statuses = %v, want 3x201 and 17x409", counts)
	}
	var rows, records int
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM resumes WHERE user_id = $1`, h.userID).Scan(&rows); err != nil {
		t.Fatalf("count resumes: %v", err)
	}
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM idempotency_records WHERE user_id = $1`, h.userID).Scan(&records); err != nil {
		t.Fatalf("count idempotency records: %v", err)
	}
	if rows != 3 || records != 3 {
		t.Fatalf("after cap race rows=%d records=%d, want 3 and 3", rows, records)
	}
}

func TestResumeAuthorization_NoExistenceOracle(t *testing.T) {
	owner := newResumeAPITestHarness(t)
	attacker := newResumeAPITestHarness(t)
	owned := owner.createResume(t)
	missing := uuid.New()

	type probe struct {
		method   string
		body     string
		revision int64
	}
	for _, operation := range []probe{
		{method: http.MethodGet},
		{method: http.MethodPatch, body: `{"title":"probe"}`, revision: owned.Revision},
		{method: http.MethodDelete, revision: owned.Revision},
	} {
		realResponse := resumeRequest(t, attacker, operation.method,
			apiResumePath+"/"+owned.ID.String(), operation.body, operation.revision, uuid.New(), "2")
		missingResponse := resumeRequest(t, attacker, operation.method,
			apiResumePath+"/"+missing.String(), operation.body, operation.revision, uuid.New(), "2")
		if realResponse.status != missingResponse.status || !bytes.Equal(realResponse.body, missingResponse.body) {
			t.Fatalf("%s wrong-owner response differs from missing:\nreal=%d %s\nmissing=%d %s",
				operation.method, realResponse.status, realResponse.body, missingResponse.status, missingResponse.body)
		}
		if realResponse.status != http.StatusNotFound {
			t.Fatalf("%s probe status = %d, want 404", operation.method, realResponse.status)
		}
		if stable := stableResumeHeaders(realResponse.header); !reflect.DeepEqual(stable, stableResumeHeaders(missingResponse.header)) {
			t.Fatalf("%s stable headers differ: real=%v missing=%v", operation.method, stable, stableResumeHeaders(missingResponse.header))
		}
		realRequestID := realResponse.header.Get("X-Request-Id")
		missingRequestID := missingResponse.header.Get("X-Request-Id")
		if _, err := uuid.Parse(realRequestID); err != nil || realRequestID == missingRequestID {
			t.Fatalf("request IDs real=%q missing=%q, want valid distinct UUIDs", realRequestID, missingRequestID)
		}
		if _, err := uuid.Parse(missingRequestID); err != nil {
			t.Fatalf("missing request ID %q is invalid: %v", missingRequestID, err)
		}
	}
}

func TestResumeAuthorization_SessionAndCSRFRequired(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	for _, request := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: apiResumePath},
		{method: http.MethodPost, path: apiResumePath, body: `{"title":"new"}`},
		{method: http.MethodGet, path: apiResumePath + "/" + created.ID.String()},
		{method: http.MethodPatch, path: apiResumePath + "/" + created.ID.String(), body: `{"title":"new"}`},
		{method: http.MethodDelete, path: apiResumePath + "/" + created.ID.String()},
	} {
		var body io.Reader
		if request.body != "" {
			body = strings.NewReader(request.body)
		}
		unauthenticated := h.request(t, request.method, request.path, body, false, false)
		assertResumeTestError(t, unauthenticated, http.StatusUnauthorized, "session_required")
		if request.method == http.MethodGet {
			continue
		}
		body = nil
		if request.body != "" {
			body = strings.NewReader(request.body)
		}
		missingCSRF := h.request(t, request.method, request.path, body, true, false)
		assertResumeTestError(t, missingCSRF, http.StatusForbidden, "csrf_rejected")
	}
}

func stableResumeHeaders(header http.Header) [][2]string {
	var stable [][2]string
	for name, values := range header {
		if strings.EqualFold(name, "Date") || strings.EqualFold(name, "X-Request-Id") ||
			strings.EqualFold(name, "Content-Length") {
			continue
		}
		for _, value := range values {
			stable = append(stable, [2]string{http.CanonicalHeaderKey(name), value})
		}
	}
	sort.Slice(stable, func(i, j int) bool {
		if stable[i][0] == stable[j][0] {
			return stable[i][1] < stable[j][1]
		}
		return stable[i][0] < stable[j][0]
	})
	return stable
}

type enqueueFailureStore struct {
	*resume.Store
	err error
}

func (s enqueueFailureStore) EnqueueMediaDeletionTx(context.Context, *store.Queries, uuid.UUID, string) error {
	return s.err
}

func TestResumeDelete_ExactPhotoCleanupIsTransactional(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	key := "resumes/" + created.ID.String() + "/photo-0123456789abcdef0123456789abcdef.jpg"
	unrelatedKey := "resumes/" + created.ID.String() + "/photo-abcdefabcdefabcdefabcdefabcdefab.jpg"
	for _, objectKey := range []string{key, unrelatedKey} {
		outcome, putErr := h.service.blobs.Put(h.ctx, objectKey, "image/jpeg", strings.NewReader("private"), 7)
		if putErr != nil || outcome != media.PutCreated {
			t.Fatalf("put private object %q = (%v, %v)", objectKey, outcome, putErr)
		}
	}
	doc := created.Doc
	doc.PersonalDetails.Photo = &schema.Photo{Key: key}
	revision, err := h.resumes.SaveDocument(h.ctx, h.userID, created.ID, doc, created.Revision)
	if err != nil {
		t.Fatalf("attach photo: %v", err)
	}

	path := apiResumePath + "/" + created.ID.String()
	response := resumeRequest(t, h, http.MethodDelete, path, "", revision, uuid.New(), "2")
	if response.status != http.StatusNoContent {
		t.Fatalf("delete with photo = %d %s, want 204", response.status, response.body)
	}
	var gotKey string
	if queryErr := h.pool.QueryRow(h.ctx,
		`SELECT object_key FROM media_deletion_jobs WHERE resume_id = $1`, created.ID).Scan(&gotKey); queryErr != nil {
		t.Fatalf("load deletion job: %v", queryErr)
	}
	if gotKey != key {
		t.Fatalf("deletion job key = %q, want %q", gotKey, key)
	}
	if _, _, getErr := h.service.blobs.Get(h.ctx, key); getErr != nil {
		t.Fatalf("queued object was physically deleted before worker: %v", getErr)
	}
	if _, _, unrelatedErr := h.service.blobs.Get(h.ctx, unrelatedKey); unrelatedErr != nil {
		t.Fatalf("unrelated same-prefix object changed: %v", unrelatedErr)
	}
	if _, getErr := h.resumes.Get(h.ctx, h.userID, created.ID); !errors.Is(getErr, resume.ErrNotFound) {
		t.Fatalf("deleted resume remains reference-accessible: %v", getErr)
	}

	readRace := h.createResume(t)
	priorKey := "resumes/" + readRace.ID.String() + "/photo-11111111111111111111111111111111.jpg"
	winnerKey := "resumes/" + readRace.ID.String() + "/photo-22222222222222222222222222222222.png"
	priorDoc := readRace.Doc
	priorDoc.PersonalDetails.Photo = &schema.Photo{Key: priorKey}
	priorRevision, err := h.resumes.SaveDocument(h.ctx, h.userID, readRace.ID, priorDoc, readRace.Revision)
	if err != nil {
		t.Fatalf("attach prior race photo: %v", err)
	}
	preflight, err := h.resumes.Get(h.ctx, h.userID, readRace.ID)
	if err != nil || preflight.Doc.PersonalDetails.Photo == nil || preflight.Doc.PersonalDetails.Photo.Key != priorKey {
		t.Fatalf("preflight photo = %#v error=%v", preflight.Doc.PersonalDetails.Photo, err)
	}
	winnerDoc := preflight.Doc
	winnerDoc.PersonalDetails.Photo = &schema.Photo{Key: winnerKey}
	winnerRevision, err := h.resumes.SaveDocument(h.ctx, h.userID, readRace.ID, winnerDoc, priorRevision)
	if err != nil {
		t.Fatalf("land transaction-time photo winner: %v", err)
	}
	racedDelete := resumeRequest(t, h, http.MethodDelete, apiResumePath+"/"+readRace.ID.String(), "",
		winnerRevision, uuid.New(), "2")
	if racedDelete.status != http.StatusNoContent {
		t.Fatalf("delete after photo race = %d %s", racedDelete.status, racedDelete.body)
	}
	var racedKeys []string
	rows, err := h.pool.Query(h.ctx,
		`SELECT object_key FROM media_deletion_jobs WHERE resume_id = $1 ORDER BY object_key`, readRace.ID)
	if err != nil {
		t.Fatalf("query raced deletion jobs: %v", err)
	}
	for rows.Next() {
		var objectKey string
		if scanErr := rows.Scan(&objectKey); scanErr != nil {
			rows.Close()
			t.Fatalf("scan raced deletion job: %v", scanErr)
		}
		racedKeys = append(racedKeys, objectKey)
	}
	rows.Close()
	if !reflect.DeepEqual(racedKeys, []string{winnerKey}) {
		t.Fatalf("raced deletion jobs = %v, want only transaction-time %q", racedKeys, winnerKey)
	}

	rollback := h.createResume(t)
	rollbackKey := "resumes/" + rollback.ID.String() + "/photo-abcdefabcdefabcdefabcdefabcdefab.png"
	rollbackDoc := rollback.Doc
	rollbackDoc.PersonalDetails.Photo = &schema.Photo{Key: rollbackKey}
	rollbackRevision, err := h.resumes.SaveDocument(h.ctx, h.userID, rollback.ID, rollbackDoc, rollback.Revision)
	if err != nil {
		t.Fatalf("attach rollback photo: %v", err)
	}
	h.service.resumes = enqueueFailureStore{Store: h.resumes, err: fmt.Errorf("injected queue failure")}
	failed := resumeRequest(t, h, http.MethodDelete, apiResumePath+"/"+rollback.ID.String(), "",
		rollbackRevision, uuid.New(), "2")
	if failed.status != http.StatusInternalServerError || bytes.Contains(failed.body, []byte(rollbackKey)) {
		t.Fatalf("queue failure response = %d %s, want opaque 500", failed.status, failed.body)
	}
	if _, err := h.resumes.Get(h.ctx, h.userID, rollback.ID); err != nil {
		t.Fatalf("queue failure deleted row: %v", err)
	}
	var jobs int
	if err := h.pool.QueryRow(h.ctx,
		`SELECT count(*) FROM media_deletion_jobs WHERE resume_id = $1`, rollback.ID).Scan(&jobs); err != nil {
		t.Fatalf("count rollback jobs: %v", err)
	}
	if jobs != 0 {
		t.Fatalf("queue failure committed %d jobs, want 0", jobs)
	}
}

func TestResumeCreate_SeedVersionsPhotoRejectionAndBounds(t *testing.T) {
	h := newResumeAPITestHarness(t)
	v1, err := os.ReadFile("../../../../packages/schema/fixtures/v1/minimal.json")
	if err != nil {
		t.Fatalf("read released v1 fixture: %v", err)
	}
	seedBody, err := json.Marshal(map[string]any{
		"title": "v1 seed", "document": json.RawMessage(v1),
	})
	if err != nil {
		t.Fatalf("marshal v1 create: %v", err)
	}
	seeded := resumeRequest(t, h, http.MethodPost, apiResumePath, string(seedBody), 0, uuid.New(), "1")
	if seeded.status != http.StatusCreated || seeded.header.Get(wireVersionHeader) != "1" {
		t.Fatalf("v1 seed create = %d schema=%q body=%s, want 201/version 1",
			seeded.status, seeded.header.Get(wireVersionHeader), seeded.body)
	}
	seededResource := decodeResumeResource(t, seeded)
	var seededDocument map[string]any
	if decodeErr := json.Unmarshal(seededResource.Document, &seededDocument); decodeErr != nil {
		t.Fatalf("decode v1 emitted seed: %v", decodeErr)
	}
	if seededDocument["schemaVersion"] != float64(1) {
		t.Fatalf("seed response schemaVersion = %v, want 1", seededDocument["schemaVersion"])
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, seededResource.ID)
	if err != nil {
		t.Fatalf("reload v1 seed: %v", err)
	}
	if stored.StoredSchemaVersion != 2 || stored.Doc.SchemaVersion != 2 {
		t.Fatalf("v1 seed stored versions = row %d doc %d, want current 2", stored.StoredSchemaVersion, stored.Doc.SchemaVersion)
	}

	for _, version := range []string{"1", "2"} {
		var document map[string]any
		fixture := v1
		if version == "2" {
			fixture, err = os.ReadFile("../../../../packages/schema/fixtures/minimal.json")
			if err != nil {
				t.Fatalf("read current fixture: %v", err)
			}
		}
		if err := json.Unmarshal(fixture, &document); err != nil {
			t.Fatalf("decode version %s fixture: %v", version, err)
		}
		personal, ok := document["personalDetails"].(map[string]any)
		if !ok {
			t.Fatalf("version %s personalDetails = %T, want object", version, document["personalDetails"])
		}
		personal["photo"] = map[string]any{
			"key": "resumes/01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f/photo-0123456789abcdef0123456789abcdef.jpg",
		}
		body, marshalErr := json.Marshal(map[string]any{"title": "photo seed", "document": document})
		if marshalErr != nil {
			t.Fatalf("marshal version %s photo seed: %v", version, marshalErr)
		}
		beforeRows := countResumeTestRows(t, h, "resumes")
		beforeRecords := countResumeTestRows(t, h, "idempotency_records")
		beforeObjects := snapshotObjectKeys(t, h)
		response := resumeRequest(t, h, http.MethodPost, apiResumePath, string(body), 0, uuid.New(), version)
		assertResumeTestError(t, response, http.StatusUnprocessableEntity, "document_invalid")
		if rows := countResumeTestRows(t, h, "resumes"); rows != beforeRows {
			t.Fatalf("version %s photo seed changed row count from %d to %d", version, beforeRows, rows)
		}
		if records := countResumeTestRows(t, h, "idempotency_records"); records != beforeRecords {
			t.Fatalf("version %s photo seed changed record count from %d to %d", version, beforeRecords, records)
		}
		if objects := snapshotObjectKeys(t, h); !reflect.DeepEqual(objects, beforeObjects) {
			t.Fatalf("version %s photo seed changed objects from %v to %v", version, beforeObjects, objects)
		}
	}

	boundaryHarness := newResumeAPITestHarness(t)
	title160 := strings.Repeat("😀", 160)
	title161 := title160 + "😀"
	body160 := mustResumeTestJSON(t, map[string]any{"title": title160})
	accepted := resumeRequest(t, boundaryHarness, http.MethodPost, apiResumePath, string(body160), 0, uuid.New(), "2")
	if accepted.status != http.StatusCreated {
		t.Fatalf("160-code-point title = %d %s, want 201", accepted.status, accepted.body)
	}
	body161 := mustResumeTestJSON(t, map[string]any{"title": title161})
	rejected := resumeRequest(t, boundaryHarness, http.MethodPost, apiResumePath, string(body161), 0, uuid.New(), "2")
	assertResumeTestError(t, rejected, http.StatusUnprocessableEntity, "document_invalid")
	if rows := countResumeTestRows(t, boundaryHarness, "resumes"); rows != 1 {
		t.Fatalf("161-code-point rejection left %d rows, want 1", rows)
	}
	if records := countResumeTestRows(t, boundaryHarness, "idempotency_records"); records != 1 {
		t.Fatalf("161-code-point rejection left %d records, want 1", records)
	}
}

func countResumeTestRows(t *testing.T, h *resumeAPITestHarness, table string) int {
	t.Helper()
	var count int
	query := "SELECT count(*) FROM " + table + " WHERE user_id = $1"
	if err := h.pool.QueryRow(h.ctx, query, h.userID).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func TestResumeMetadata_LanguageProjectionAndCompleteLegacyUpgrade(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	path := apiResumePath + "/" + created.ID.String()

	canonicalized := resumeRequest(t, h, http.MethodPatch, path, `{"lng":"EN-us"}`,
		created.Revision, uuid.New(), "2")
	if canonicalized.status != http.StatusOK || decodeResumeResource(t, canonicalized).Lng != "en-US" {
		t.Fatalf("language canonicalization = %d %s, want en-US", canonicalized.status, canonicalized.body)
	}
	valid35 := "en-x-12345678-12345678-12345678-abc"
	valid35Body := mustResumeTestJSON(t, map[string]any{"lng": valid35})
	validBoundary := resumeRequest(t, h, http.MethodPatch, path, string(valid35Body), 2, uuid.New(), "2")
	if validBoundary.status != http.StatusOK || decodeResumeResource(t, validBoundary).Lng != valid35 {
		t.Fatalf("35-character canonical language = %d %s, want accepted %q", validBoundary.status, validBoundary.body, valid35)
	}
	revision := int64(3)
	canonicalizedOverlong := "sh-u-ca-gregory-co-phonebk-hc-h24"
	overlongBody := mustResumeTestJSON(t, map[string]any{"lng": canonicalizedOverlong})
	overlong := resumeRequest(t, h, http.MethodPatch, path, string(overlongBody), revision, uuid.New(), "2")
	assertResumeTestError(t, overlong, http.StatusUnprocessableEntity, "document_invalid")
	for _, invalid := range []string{"en--US", "x"} {
		body := mustResumeTestJSON(t, map[string]any{"lng": invalid})
		response := resumeRequest(t, h, http.MethodPatch, path, string(body), revision, uuid.New(), "2")
		assertResumeTestError(t, response, http.StatusUnprocessableEntity, "document_invalid")
	}

	legacyCases := []struct {
		name string
		lng  *string
		want string
	}{
		{name: "null", lng: nil, want: "und"},
		{name: "empty", lng: stringPointer(""), want: "und"},
		{name: "invalid", lng: stringPointer("en--US"), want: "und"},
		{name: "canonicalized overlong", lng: stringPointer(canonicalizedOverlong), want: "und"},
		{name: "canonical", lng: stringPointer("EN-us"), want: "en-US"},
	}
	for _, test := range legacyCases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := h.pool.Exec(h.ctx, `UPDATE resumes SET lng = $2 WHERE id = $1`, created.ID, test.lng); err != nil {
				t.Fatalf("seed legacy lng: %v", err)
			}
			response := resumeRequest(t, h, http.MethodGet, path, "", 0, uuid.Nil, "2")
			if response.status != http.StatusOK {
				t.Fatalf("legacy lng GET = %d %s", response.status, response.body)
			}
			if got := decodeResumeResource(t, response).Lng; got != test.want {
				t.Fatalf("projected lng = %q, want %q", got, test.want)
			}
		})
	}

	v1, err := os.ReadFile("../../../../packages/schema/fixtures/v1/minimal.json")
	if err != nil {
		t.Fatalf("read v1 fixture: %v", err)
	}
	var legacy map[string]any
	if decodeErr := json.Unmarshal(v1, &legacy); decodeErr != nil {
		t.Fatalf("decode v1 fixture: %v", decodeErr)
	}
	legacy["content"] = map[string]any{
		"profile": map[string]any{
			"sectionType": "profile",
			"entries": []any{map[string]any{
				"id":   "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60",
				"text": `<script>alert(1)</script><p>safe</p>`,
			}},
		},
	}
	customization, ok := legacy["customization"].(map[string]any)
	if !ok {
		t.Fatalf("legacy customization = %T, want object", legacy["customization"])
	}
	layout, ok := customization["layout"].(map[string]any)
	if !ok {
		t.Fatalf("legacy layout = %T, want object", customization["layout"])
	}
	sections, ok := layout["sections"].(map[string]any)
	if !ok {
		t.Fatalf("legacy sections = %T, want object", layout["sections"])
	}
	sections["main"] = []any{"profile"}
	parts := []any{legacy["personalDetails"], legacy["content"], legacy["customization"]}
	if _, execErr := h.pool.Exec(h.ctx, `
		UPDATE resumes
		SET personal_details = $2::jsonb, content = $3::jsonb,
		    customization = $4::jsonb, schema_version = 1
		WHERE id = $1`, created.ID, mustResumeTestJSON(t, parts[0]), mustResumeTestJSON(t, parts[1]), mustResumeTestJSON(t, parts[2])); execErr != nil {
		t.Fatalf("seed stored v1 row: %v", execErr)
	}
	var beforeRevision int64
	if queryErr := h.pool.QueryRow(h.ctx, `SELECT revision FROM resumes WHERE id = $1`, created.ID).Scan(&beforeRevision); queryErr != nil {
		t.Fatalf("load legacy revision: %v", queryErr)
	}
	upgraded := resumeRequest(t, h, http.MethodPatch, path, `{"title":"upgraded"}`,
		beforeRevision, uuid.New(), "1")
	if upgraded.status != http.StatusOK {
		t.Fatalf("legacy metadata patch = %d %s, want 200", upgraded.status, upgraded.body)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("reload upgraded resume: %v", err)
	}
	if stored.StoredSchemaVersion != 2 || stored.Revision != beforeRevision+1 {
		t.Fatalf("upgraded row schema=%d revision=%d, want 2 and %d",
			stored.StoredSchemaVersion, stored.Revision, beforeRevision+1)
	}
	profile := stored.Doc.Content["profile"].ProfileEntries[0]
	if profile.Text == nil || *profile.Text != "<p>safe</p>" {
		t.Fatalf("upgraded profile text = %v, want sanitized <p>safe</p>", profile.Text)
	}
}

func mustResumeTestJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal test JSON: %v", err)
	}
	return raw
}

func stringPointer(value string) *string { return &value }

func TestResumeDelete_InvalidAndCrossResumePhotoKeysRollBack(t *testing.T) {
	h := newResumeAPITestHarness(t)
	neighbor := h.createResume(t)
	for _, test := range []struct {
		name string
		key  func(uuid.UUID) string
	}{
		{name: "malformed", key: func(uuid.UUID) string { return "not-a-photo-key" }},
		{name: "cross-resume", key: func(uuid.UUID) string {
			return "resumes/" + neighbor.ID.String() + "/photo-0123456789abcdef0123456789abcdef.jpg"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			created := h.createResume(t)
			photo := mustResumeTestJSON(t, map[string]any{"key": test.key(created.ID)})
			if _, err := h.pool.Exec(h.ctx,
				`UPDATE resumes SET personal_details = jsonb_set(personal_details, '{photo}', $2::jsonb) WHERE id = $1`,
				created.ID, photo); err != nil {
				t.Fatalf("seed invalid photo: %v", err)
			}
			response := resumeRequest(t, h, http.MethodDelete, apiResumePath+"/"+created.ID.String(), "",
				created.Revision, uuid.New(), "2")
			if response.status != http.StatusInternalServerError || bytes.Contains(response.body, photo) {
				t.Fatalf("delete invalid key = %d %s, want opaque 500", response.status, response.body)
			}
			var rows, jobs int
			if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM resumes WHERE id = $1`, created.ID).Scan(&rows); err != nil {
				t.Fatalf("count retained resume: %v", err)
			}
			if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM media_deletion_jobs WHERE resume_id = $1`, created.ID).Scan(&jobs); err != nil {
				t.Fatalf("count jobs: %v", err)
			}
			if rows != 1 || jobs != 0 {
				t.Fatalf("invalid key result rows=%d jobs=%d, want 1 and 0", rows, jobs)
			}
		})
	}
}
