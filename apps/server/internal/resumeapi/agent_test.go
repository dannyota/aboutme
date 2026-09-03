package resumeapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/oauthsrv"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func newAgentPrincipalForTest(t *testing.T, h *resumeAPITestHarness) AgentPrincipal {
	t.Helper()
	now := time.Now().UTC()
	client, err := h.queries.CreateOAuthClient(h.ctx, store.CreateOAuthClientParams{
		ClientName: "Resume agent", RedirectURIs: json.RawMessage(`["https://agent.example/callback"]`), CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateOAuthClient: %v", err)
	}
	grant, err := h.queries.UpsertOAuthGrant(h.ctx, store.UpsertOAuthGrantParams{
		UserID: h.userID, ClientID: client.ID, Scopes: "resumes:read resumes:write", CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("UpsertOAuthGrant: %v", err)
	}
	entropy := sha256.Sum256([]byte(client.ID.String()))
	_, digest, err := oauthsrv.NewToken(oauthsrv.TokenKindAccess, bytes.NewReader(entropy[:]))
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	token, err := h.queries.CreateOAuthToken(h.ctx, store.CreateOAuthTokenParams{
		TokenDigest: digest[:], Kind: string(oauthsrv.TokenKindAccess), FamilyID: uuid.New(), ClientID: client.ID,
		UserID: h.userID, GrantID: grant.ID, CreatedAt: now, ExpiresAt: now.Add(time.Hour), FamilyExpiresAt: now.Add(30 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateOAuthToken: %v", err)
	}
	principal, err := NewAgentPrincipal(h.userID, grant.ID, token.ID, digest)
	if err != nil {
		t.Fatalf("NewAgentPrincipal: %v", err)
	}
	return principal
}

func TestExecuteAgent_RequiresCallerIdempotencyKey(t *testing.T) {
	h := newResumeAPITestHarness(t)
	principal := newAgentPrincipalForTest(t, h)
	response := h.service.ExecuteAgent(h.ctx, principal, AgentCall{
		Operation: AgentCreateResume,
		Payload:   json.RawMessage(`{"title":"missing key"}`),
	})
	if response.Status != http.StatusBadRequest || !bytes.Contains(response.Body, []byte(`"idempotency_key_required"`)) {
		t.Fatalf("missing key = %d %s", response.Status, response.Body)
	}
	if resumes := countResumeTestRows(t, h, "resumes"); resumes != 0 {
		t.Fatalf("missing key created %d resumes", resumes)
	}
	if records := countResumeTestRows(t, h, "idempotency_records"); records != 0 {
		t.Fatalf("missing key retained %d records", records)
	}
}

func TestExecuteAgent_ReplaysCallerIdempotencyKey(t *testing.T) {
	h := newResumeAPITestHarness(t)
	principal := newAgentPrincipalForTest(t, h)
	key := uuid.NewString()
	call := AgentCall{
		Operation: AgentCreateResume, IdempotencyKey: key,
		Payload: json.RawMessage(`{"title":"retryable create"}`),
	}
	first := h.service.ExecuteAgent(h.ctx, principal, call)
	second := h.service.ExecuteAgent(h.ctx, principal, call)
	if first.Status != http.StatusCreated || second.Status != http.StatusCreated {
		t.Fatalf("create statuses = %d/%d, bodies = %s/%s", first.Status, second.Status, first.Body, second.Body)
	}
	firstResource := decodeResumeResource(t, testHTTPResponse{status: first.Status, header: first.Header, body: first.Body})
	secondResource := decodeResumeResource(t, testHTTPResponse{status: second.Status, header: second.Header, body: second.Body})
	if firstResource.ID != secondResource.ID || firstResource.Revision != secondResource.Revision {
		t.Fatalf("replay resources = %#v / %#v", firstResource, secondResource)
	}
	if resumes := countResumeTestRows(t, h, "resumes"); resumes != 1 {
		t.Fatalf("replay created %d resumes", resumes)
	}
	if records := countResumeTestRows(t, h, "idempotency_records"); records != 1 {
		t.Fatalf("replay retained %d records", records)
	}
}

func TestExecuteAgent_ReplaysExistingMutation(t *testing.T) {
	h := newResumeAPITestHarness(t)
	principal := newAgentPrincipalForTest(t, h)
	created := h.createResume(t)
	key := uuid.NewString()
	call := AgentCall{
		Operation: AgentUpdateResumeMetadata, IdempotencyKey: key,
		ResumeID: created.ID.String(), Revision: "1", Payload: json.RawMessage(`{"title":"retryable update"}`),
	}
	first := h.service.ExecuteAgent(h.ctx, principal, call)
	second := h.service.ExecuteAgent(h.ctx, principal, call)
	if first.Status != http.StatusOK || second.Status != http.StatusOK {
		t.Fatalf("update statuses = %d/%d, bodies = %s/%s", first.Status, second.Status, first.Body, second.Body)
	}
	firstResource := decodeResumeResource(t, testHTTPResponse{status: first.Status, header: first.Header, body: first.Body})
	secondResource := decodeResumeResource(t, testHTTPResponse{status: second.Status, header: second.Header, body: second.Body})
	if firstResource.ID != secondResource.ID || firstResource.Revision != "2" || secondResource.Revision != "2" {
		t.Fatalf("replay resources = %#v / %#v", firstResource, secondResource)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Revision != 2 || stored.Title != "retryable update" {
		t.Fatalf("stored update = %q/%d", stored.Title, stored.Revision)
	}
}

func TestExecuteAgent_RejectsChangedIdempotencyFingerprint(t *testing.T) {
	h := newResumeAPITestHarness(t)
	principal := newAgentPrincipalForTest(t, h)
	created := h.createResume(t)
	key := uuid.NewString()
	first := h.service.ExecuteAgent(h.ctx, principal, AgentCall{
		Operation: AgentUpdateResumeMetadata, IdempotencyKey: key,
		ResumeID: created.ID.String(), Revision: "1", Payload: json.RawMessage(`{"title":"first intent"}`),
	})
	changed := h.service.ExecuteAgent(h.ctx, principal, AgentCall{
		Operation: AgentUpdateResumeMetadata, IdempotencyKey: key,
		ResumeID: created.ID.String(), Revision: "1", Payload: json.RawMessage(`{"title":"changed intent"}`),
	})
	if first.Status != http.StatusOK {
		t.Fatalf("first update = %d %s", first.Status, first.Body)
	}
	var retainedBefore []byte
	if err := h.pool.QueryRow(h.ctx, `SELECT response_body FROM idempotency_records WHERE user_id = $1 AND idempotency_key = $2`, h.userID, key).Scan(&retainedBefore); err != nil {
		t.Fatalf("read retained response before reuse: %v", err)
	}
	if changed.Status != http.StatusConflict || !bytes.Contains(changed.Body, []byte(`"idempotency_key_reuse"`)) {
		t.Fatalf("changed replay = %d %s", changed.Status, changed.Body)
	}
	var retainedAfter []byte
	if err := h.pool.QueryRow(h.ctx, `SELECT response_body FROM idempotency_records WHERE user_id = $1 AND idempotency_key = $2`, h.userID, key).Scan(&retainedAfter); err != nil {
		t.Fatalf("read retained response after reuse: %v", err)
	}
	if !bytes.Equal(retainedBefore, retainedAfter) {
		t.Fatalf("changed replay replaced retained response: before=%s after=%s", retainedBefore, retainedAfter)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Title != "first intent" || stored.Revision != 2 {
		t.Fatalf("changed replay mutated row = %q/%d", stored.Title, stored.Revision)
	}
	if records := countResumeTestRows(t, h, "idempotency_records"); records != 1 {
		t.Fatalf("changed replay retained %d records", records)
	}
}

func TestExecuteAgent_ReusesCanonicalMetadataMutation(t *testing.T) {
	h := newResumeAPITestHarness(t)
	principal := newAgentPrincipalForTest(t, h)
	created := h.createResume(t)

	response := h.service.ExecuteAgent(h.ctx, principal, AgentCall{
		Operation:      AgentUpdateResumeMetadata,
		IdempotencyKey: uuid.NewString(),
		ResumeID:       created.ID.String(),
		Revision:       "1",
		Payload:        json.RawMessage(`{"title":"Agent title"}`),
	})
	if response.Status != http.StatusOK {
		t.Fatalf("ExecuteAgent status = %d, body = %s", response.Status, response.Body)
	}
	resource := decodeResumeResource(t, testHTTPResponse{status: response.Status, header: response.Header, body: response.Body})
	if resource.Title != "Agent title" || resource.Revision != "2" || resource.Document == nil {
		t.Fatalf("resource = %#v", resource)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Title != resource.Title || stored.Revision != 2 {
		t.Fatalf("stored title/revision = %q/%d", stored.Title, stored.Revision)
	}
}

func TestExecuteAgent_InvalidPrincipalFailsClosedBeforeRead(t *testing.T) {
	h := newResumeAPITestHarness(t)
	response := h.service.ExecuteAgent(h.ctx, AgentPrincipal{}, AgentCall{Operation: AgentListResumes})
	if response.Status != http.StatusServiceUnavailable ||
		!bytes.Contains(response.Body, []byte(`"agent_access_unavailable"`)) {
		t.Fatalf("invalid principal read = %d %s", response.Status, response.Body)
	}
}

func TestExecuteAgent_IgnoresAmbientBrowserSessionContext(t *testing.T) {
	h := newResumeAPITestHarness(t)
	principal := newAgentPrincipalForTest(t, h)
	created := h.createResume(t)
	ctx := auth.ContextWithSession(h.ctx, store.Session{ID: uuid.New(), UserID: uuid.New()})

	listed := h.service.ExecuteAgent(ctx, principal, AgentCall{Operation: AgentListResumes})
	if listed.Status != http.StatusOK || !bytes.Contains(listed.Body, []byte(created.ID.String())) {
		t.Fatalf("agent list with ambient session = %d %s", listed.Status, listed.Body)
	}
	updated := h.service.ExecuteAgent(ctx, principal, AgentCall{
		Operation:      AgentUpdateResumeMetadata,
		IdempotencyKey: uuid.NewString(),
		ResumeID:       created.ID.String(),
		Revision:       "1",
		Payload:        json.RawMessage(`{"title":"agent authority"}`),
	})
	if updated.Status != http.StatusOK {
		t.Fatalf("agent mutation with ambient session = %d %s", updated.Status, updated.Body)
	}
}

func TestExecuteAgent_RevokedGrantCannotCommitMutation(t *testing.T) {
	h := newResumeAPITestHarness(t)
	principal := newAgentPrincipalForTest(t, h)
	created := h.createResume(t)
	if _, err := h.queries.RevokeOAuthGrant(context.Background(), store.RevokeOAuthGrantParams{
		ID: principal.GrantID(), RevokedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("RevokeOAuthGrant: %v", err)
	}

	response := h.service.ExecuteAgent(h.ctx, principal, AgentCall{
		Operation:      AgentUpdateResumeMetadata,
		IdempotencyKey: uuid.NewString(),
		ResumeID:       created.ID.String(),
		Revision:       "1",
		Payload:        json.RawMessage(`{"title":"must not commit"}`),
	})
	if response.Status != http.StatusServiceUnavailable || !bytes.Contains(response.Body, []byte(`"agent_access_unavailable"`)) {
		t.Fatalf("ExecuteAgent = %d %s", response.Status, response.Body)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Title != created.Title || stored.Revision != created.Revision {
		t.Fatalf("revoked mutation changed row to %q/%d", stored.Title, stored.Revision)
	}
}

func TestExecuteAgent_TokenRevokedAfterAdmissionCannotCommitMutation(t *testing.T) {
	h := newResumeAPITestHarness(t)
	principal := newAgentPrincipalForTest(t, h)
	created := h.createResume(t)
	reached := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	h.service.transactionOrderHook = func(step string) {
		if step == "resume" {
			once.Do(func() { close(reached) })
			<-release
		}
	}
	t.Cleanup(func() { h.service.transactionOrderHook = nil })
	result := make(chan AgentResponse, 1)
	go func() {
		result <- h.service.ExecuteAgent(h.ctx, principal, AgentCall{
			Operation:      AgentUpdateResumeMetadata,
			IdempotencyKey: uuid.NewString(),
			ResumeID:       created.ID.String(),
			Revision:       "1",
			Payload:        json.RawMessage(`{"title":"must not commit"}`),
		})
	}()
	<-reached
	if _, err := h.pool.Exec(context.Background(),
		`UPDATE oauth_tokens SET revoked_at = now() WHERE grant_id = $1 AND kind = 'access'`, principal.GrantID()); err != nil {
		close(release)
		t.Fatalf("revoke admitted token: %v", err)
	}
	close(release)
	response := <-result
	if response.Status != http.StatusServiceUnavailable ||
		!bytes.Contains(response.Body, []byte(`"agent_access_unavailable"`)) {
		t.Fatalf("revoked in-flight mutation = %d %s", response.Status, response.Body)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Title != created.Title || stored.Revision != created.Revision {
		t.Fatalf("revoked in-flight mutation changed row to %q/%d", stored.Title, stored.Revision)
	}
}

func TestExecuteAgent_ResumeLifecycleUsesCanonicalHandlers(t *testing.T) {
	h := newResumeAPITestHarness(t)
	principal := newAgentPrincipalForTest(t, h)
	existing := h.createResume(t)

	listed := h.service.ExecuteAgent(h.ctx, principal, AgentCall{Operation: AgentListResumes})
	if listed.Status != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listed.Status, listed.Body)
	}
	var listEnvelope struct {
		Data []resumeTestResource `json:"data"`
	}
	if err := json.Unmarshal(listed.Body, &listEnvelope); err != nil {
		t.Fatalf("decode list: %v (body=%s)", err, listed.Body)
	}
	if len(listEnvelope.Data) != 1 || listEnvelope.Data[0].ID != existing.ID || listEnvelope.Data[0].Revision != "1" {
		t.Fatalf("list data = %#v", listEnvelope.Data)
	}

	got := h.service.ExecuteAgent(h.ctx, principal, AgentCall{
		Operation: AgentGetResume,
		ResumeID:  existing.ID.String(),
	})
	if got.Status != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", got.Status, got.Body)
	}
	gotResource := decodeResumeResource(t, testHTTPResponse{status: got.Status, header: got.Header, body: got.Body})
	if gotResource.ID != existing.ID || gotResource.Revision != "1" || len(gotResource.Document) == 0 {
		t.Fatalf("get resource = %#v", gotResource)
	}

	createdResponse := h.service.ExecuteAgent(h.ctx, principal, AgentCall{
		Operation:      AgentCreateResume,
		IdempotencyKey: uuid.NewString(),
		Payload:        json.RawMessage(`{"title":"Created by agent","lng":"EN-us"}`),
	})
	if createdResponse.Status != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createdResponse.Status, createdResponse.Body)
	}
	created := decodeResumeResource(t, testHTTPResponse{
		status: createdResponse.Status, header: createdResponse.Header, body: createdResponse.Body,
	})
	if created.Title != "Created by agent" || created.Lng != "en-US" || created.Revision != "1" || len(created.Document) == 0 {
		t.Fatalf("created resource = %#v", created)
	}

	deleted := h.service.ExecuteAgent(h.ctx, principal, AgentCall{
		Operation:      AgentDeleteResume,
		IdempotencyKey: uuid.NewString(),
		ResumeID:       created.ID.String(),
		Revision:       created.Revision,
	})
	if deleted.Status != http.StatusNoContent || len(deleted.Body) != 0 {
		t.Fatalf("delete status = %d, body = %s", deleted.Status, deleted.Body)
	}
	missing := h.service.ExecuteAgent(h.ctx, principal, AgentCall{
		Operation: AgentGetResume,
		ResumeID:  created.ID.String(),
	})
	if missing.Status != http.StatusNotFound {
		t.Fatalf("get deleted status = %d, body = %s", missing.Status, missing.Body)
	}
}

func TestExecuteAgent_DeletePublishedResumePreservesPublicRevocation(t *testing.T) {
	h := newResumeAPITestHarness(t)
	principal := newAgentPrincipalForTest(t, h)
	created, err := h.resumes.Create(h.ctx, h.userID, "Published agent delete", publishCompleteDocument(t))
	if err != nil {
		t.Fatalf("create publishable resume: %v", err)
	}
	slug := "agent-delete-" + uuid.NewString()[:8]
	published := h.mutationRequest(t, http.MethodPost, apiResumePath+"/"+created.ID.String()+"/publish",
		strings.NewReader(`{"slug":"`+slug+`","live":true,"downloadEnabled":false,"seoGeoEnabled":false}`),
		created.Revision, uuid.NewString())
	if published.status != http.StatusOK {
		t.Fatalf("publish status = %d, body = %s", published.status, published.body)
	}
	before, err := h.queries.GetPublicState(h.ctx)
	if err != nil {
		t.Fatalf("get public state before delete: %v", err)
	}
	if _, execErr := h.pool.Exec(h.ctx,
		`UPDATE sessions SET reauthenticated_at = now() - interval '16 minutes' WHERE id = $1`, h.session.ID); execErr != nil {
		t.Fatalf("expire browser reauthentication: %v", execErr)
	}

	deleted := h.service.ExecuteAgent(h.ctx, principal, AgentCall{
		Operation:      AgentDeleteResume,
		IdempotencyKey: uuid.NewString(),
		ResumeID:       created.ID.String(),
		Revision:       "2",
	})
	if deleted.Status != http.StatusNoContent || len(deleted.Body) != 0 {
		t.Fatalf("delete published status = %d, body = %s", deleted.Status, deleted.Body)
	}
	if _, getErr := h.resumes.Get(h.ctx, h.userID, created.ID); !errors.Is(getErr, resume.ErrNotFound) {
		t.Fatalf("deleted published resume remains accessible: %v", getErr)
	}
	if _, tombstoneErr := h.queries.GetSlugTombstoneForUpdate(h.ctx, slug); tombstoneErr != nil {
		t.Fatalf("published delete omitted slug tombstone: %v", tombstoneErr)
	}
	after, err := h.queries.GetPublicState(h.ctx)
	if err != nil {
		t.Fatalf("get public state after delete: %v", err)
	}
	if after.DiscoveryGeneration != before.DiscoveryGeneration+1 {
		t.Fatalf("discovery generation = %d, want %d", after.DiscoveryGeneration, before.DiscoveryGeneration+1)
	}
	if err := h.service.coordinator.Ready(); err != nil {
		t.Fatalf("published delete left public fence closed: %v", err)
	}
}

func TestExecuteAgent_ContentOperationsUseCanonicalMutationKernel(t *testing.T) {
	h := newResumeAPITestHarness(t)
	principal := newAgentPrincipalForTest(t, h)
	created := createEntryContractResume(t, h)
	secondEntryID := "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61"

	upserted := h.service.ExecuteAgent(h.ctx, principal, AgentCall{
		Operation:      AgentUpsertEntry,
		IdempotencyKey: uuid.NewString(),
		ResumeID:       created.ID.String(),
		Revision:       "1",
		SectionKey:     "work",
		Payload:        json.RawMessage(`{"entry":{"id":"` + secondEntryID + `"}}`),
	})
	if upserted.Status != http.StatusOK || decodeResumeResource(t, testHTTPResponse{
		status: upserted.Status, header: upserted.Header, body: upserted.Body,
	}).Revision != "2" {
		t.Fatalf("upsert entry = %d %s", upserted.Status, upserted.Body)
	}

	deletedEntry := h.service.ExecuteAgent(h.ctx, principal, AgentCall{
		Operation:      AgentDeleteEntry,
		IdempotencyKey: uuid.NewString(),
		ResumeID:       created.ID.String(),
		Revision:       "2",
		SectionKey:     "work",
		EntryID:        "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60",
	})
	if deletedEntry.Status != http.StatusNoContent || deletedEntry.Header.Get("ETag") != `"r3"` {
		t.Fatalf("delete entry = %d headers=%v body=%s", deletedEntry.Status, deletedEntry.Header, deletedEntry.Body)
	}

	updatedSection := h.service.ExecuteAgent(h.ctx, principal, AgentCall{
		Operation:      AgentUpdateSection,
		IdempotencyKey: uuid.NewString(),
		ResumeID:       created.ID.String(),
		Revision:       "3",
		SectionKey:     "work",
		Payload:        json.RawMessage(`{"displayName":"Agent experience"}`),
	})
	if updatedSection.Status != http.StatusOK || decodeResumeResource(t, testHTTPResponse{
		status: updatedSection.Status, header: updatedSection.Header, body: updatedSection.Body,
	}).Revision != "4" {
		t.Fatalf("update section = %d %s", updatedSection.Status, updatedSection.Body)
	}

	updatedStructure := h.service.ExecuteAgent(h.ctx, principal, AgentCall{
		Operation:      AgentUpdateStructure,
		IdempotencyKey: uuid.NewString(),
		ResumeID:       created.ID.String(),
		Revision:       "4",
		Payload: json.RawMessage(`{"commands":[{"op":"createSection","key":"skills","sectionType":"skill",` +
			`"displayName":"Skills","column":"main","index":1}]}`),
	})
	if updatedStructure.Status != http.StatusOK || decodeResumeResource(t, testHTTPResponse{
		status: updatedStructure.Status, header: updatedStructure.Header, body: updatedStructure.Body,
	}).Revision != "5" {
		t.Fatalf("update structure = %d %s", updatedStructure.Status, updatedStructure.Body)
	}

	updatedDetails := h.service.ExecuteAgent(h.ctx, principal, AgentCall{
		Operation:      AgentUpdatePersonalDetails,
		IdempotencyKey: uuid.NewString(),
		ResumeID:       created.ID.String(),
		Revision:       "5",
		Payload:        json.RawMessage(`{"fullName":"Agent User","headline":"Writer","details":[]}`),
	})
	if updatedDetails.Status != http.StatusOK || decodeResumeResource(t, testHTTPResponse{
		status: updatedDetails.Status, header: updatedDetails.Header, body: updatedDetails.Body,
	}).Revision != "6" {
		t.Fatalf("update personal details = %d %s", updatedDetails.Status, updatedDetails.Body)
	}

	updatedCustomization := h.service.ExecuteAgent(h.ctx, principal, AgentCall{
		Operation:      AgentUpdateCustomization,
		IdempotencyKey: uuid.NewString(),
		ResumeID:       created.ID.String(),
		Revision:       "6",
		Payload:        json.RawMessage(`{"deltas":[{"op":"set","path":"colors.primary","value":"#112233"}]}`),
	})
	if updatedCustomization.Status != http.StatusOK {
		t.Fatalf("update customization = %d %s", updatedCustomization.Status, updatedCustomization.Body)
	}
	resource := decodeResumeResource(t, testHTTPResponse{
		status: updatedCustomization.Status, header: updatedCustomization.Header, body: updatedCustomization.Body,
	})
	if resource.Revision != "7" {
		t.Fatalf("final revision = %q", resource.Revision)
	}
	var document schema.Resume
	if err := json.Unmarshal(resource.Document, &document); err != nil {
		t.Fatalf("decode final document: %v", err)
	}
	work := document.Content["work"]
	if len(work.WorkEntries) != 1 || work.WorkEntries[0].ID != secondEntryID ||
		work.DisplayName == nil || *work.DisplayName != "Agent experience" {
		t.Fatalf("final work section = %#v", work)
	}
	if _, ok := document.Content["skills"]; !ok || document.PersonalDetails.FullName == nil ||
		*document.PersonalDetails.FullName != "Agent User" ||
		document.Customization.Colors.Primary != "#112233" {
		t.Fatalf("final document = %#v", document)
	}
}

func TestExecuteAgent_PhotoOperationsUseCanonicalMediaPath(t *testing.T) {
	h := newResumeAPITestHarness(t)
	principal := newAgentPrincipalForTest(t, h)
	created := h.createResume(t)

	uploaded := h.service.ExecuteAgent(h.ctx, principal, AgentCall{
		Operation:      AgentUploadPhoto,
		IdempotencyKey: uuid.NewString(),
		ResumeID:       created.ID.String(),
		Revision:       "1",
		File:           makePhotoPNG(t),
	})
	if uploaded.Status != http.StatusOK {
		t.Fatalf("upload photo = %d %s", uploaded.Status, uploaded.Body)
	}
	uploadedResource := decodeResumeResource(t, testHTTPResponse{
		status: uploaded.Status, header: uploaded.Header, body: uploaded.Body,
	})
	if uploadedResource.Revision != "2" {
		t.Fatalf("uploaded revision = %q", uploadedResource.Revision)
	}
	var uploadedDocument schema.Resume
	if err := json.Unmarshal(uploadedResource.Document, &uploadedDocument); err != nil || uploadedDocument.PersonalDetails.Photo == nil {
		t.Fatalf("uploaded photo metadata = %#v, error = %v", uploadedDocument.PersonalDetails.Photo, err)
	}

	got := h.service.ExecuteAgent(h.ctx, principal, AgentCall{
		Operation: AgentGetPhoto,
		ResumeID:  created.ID.String(),
	})
	if got.Status != http.StatusOK || !strings.HasPrefix(got.Header.Get("Content-Type"), "image/") || len(got.Body) == 0 {
		t.Fatalf("get photo = %d headers=%v bytes=%d body=%s", got.Status, got.Header, len(got.Body), got.Body)
	}

	cropped := h.service.ExecuteAgent(h.ctx, principal, AgentCall{
		Operation:      AgentUpdatePhotoCrop,
		IdempotencyKey: uuid.NewString(),
		ResumeID:       created.ID.String(),
		Revision:       "2",
		Payload:        json.RawMessage(`{"crop":{"x":0.1,"y":0.2,"width":0.7,"height":0.6}}`),
	})
	if cropped.Status != http.StatusOK || decodeResumeResource(t, testHTTPResponse{
		status: cropped.Status, header: cropped.Header, body: cropped.Body,
	}).Revision != "3" {
		t.Fatalf("update crop = %d %s", cropped.Status, cropped.Body)
	}

	deleted := h.service.ExecuteAgent(h.ctx, principal, AgentCall{
		Operation:      AgentDeletePhoto,
		IdempotencyKey: uuid.NewString(),
		ResumeID:       created.ID.String(),
		Revision:       "3",
	})
	if deleted.Status != http.StatusNoContent || deleted.Header.Get("ETag") != `"r4"` {
		t.Fatalf("delete photo = %d headers=%v body=%s", deleted.Status, deleted.Header, deleted.Body)
	}
	missing := h.service.ExecuteAgent(h.ctx, principal, AgentCall{
		Operation: AgentGetPhoto,
		ResumeID:  created.ID.String(),
	})
	if missing.Status != http.StatusNotFound {
		t.Fatalf("get deleted photo = %d %s", missing.Status, missing.Body)
	}
}
