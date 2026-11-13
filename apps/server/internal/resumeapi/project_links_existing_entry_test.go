package resumeapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/oauthsrv"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

// fullFixtureProjectEntryID names the full schema fixture's own project
// entry.
const fullFixtureProjectEntryID = "12949dfe-d9dc-4292-a17b-faeda92081e4"

// existingLinkFixture is a resume created through the REST create, so its
// stored text has passed the write sanitizer. Its project section holds an
// entry linking linked, the unlinked target, and the fixture's own entry;
// no entry links atlas.
type existingLinkFixture struct {
	resumeID      uuid.UUID
	revision      int64
	target        schema.ProjectEntry
	linkedEntryID string
	linked        uuid.UUID
	atlas         uuid.UUID
}

func newExistingLinkFixture(t *testing.T, h *resumeAPITestHarness) existingLinkFixture {
	t.Helper()
	linked := createLinkProject(t, h, h.userID, "beacon", linkProjectDocument("Beacon"))
	atlas := createLinkProject(t, h, h.userID, "atlas", linkProjectDocument("Atlas"))
	target := unlinkedProjectEntry()
	doc := existingEntryDocument(t, target, linked.ID)
	created := resumeRequest(t, h, http.MethodPost, apiResumePath,
		`{"title":"existing","lng":"en","document":`+mustDocumentJSON(t, doc)+`}`, 0, uuid.New(), "")
	if created.status != http.StatusCreated {
		t.Fatalf("create = %d %s", created.status, created.body)
	}
	resource := decodeResumeResource(t, created)
	revision, err := strconv.ParseInt(resource.Revision, 10, 64)
	if err != nil {
		t.Fatalf("created revision %q: %v", resource.Revision, err)
	}
	return existingLinkFixture{
		resumeID: resource.ID, revision: revision, target: target,
		linkedEntryID: doc.Content["project"].ProjectEntries[0].ID, linked: linked.ID, atlas: atlas.ID,
	}
}

func existingLinkBody(projectID uuid.UUID, entryID string) string {
	return `{"projectId":"` + projectID.String() + `","entryId":"` + entryID + `"}`
}

func linkExistingRequest(t *testing.T, h *resumeAPITestHarness, resumeID, projectID uuid.UUID, entryID string,
	revision int64, version string,
) testHTTPResponse {
	t.Helper()
	return linkRouteRequest(t, h, http.MethodPost, apiResumePath+"/"+resumeID.String()+"/project-links",
		existingLinkBody(projectID, entryID), revision, version)
}

func storedResume(t *testing.T, h *resumeAPITestHarness, id uuid.UUID) resume.Resume {
	t.Helper()
	stored, err := h.resumes.Get(h.ctx, h.userID, id)
	if err != nil {
		t.Fatalf("reload resume: %v", err)
	}
	return stored
}

// requireUnchanged fails unless the stored resume still has before's
// revision and document bytes.
func requireUnchanged(t *testing.T, h *resumeAPITestHarness, name string, before resume.Resume) {
	t.Helper()
	after := storedResume(t, h, before.ID)
	if after.Revision != before.Revision || !bytes.Equal(canonicalDocument(t, after.Doc), canonicalDocument(t, before.Doc)) {
		t.Fatalf("%s wrote the resume: revision %d, want %d unchanged with the same document", name, after.Revision, before.Revision)
	}
}

func projectEntryByID(t *testing.T, doc schema.Resume, entryID string) schema.ProjectEntry {
	t.Helper()
	for _, section := range doc.Content {
		for _, entry := range section.ProjectEntries {
			if entry.ID == entryID {
				return entry
			}
		}
	}
	t.Fatalf("document has no project entry %s", entryID)
	return schema.ProjectEntry{}
}

func decodedJSON(t *testing.T, raw []byte) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return value
}

func TestProjectLinks_ExistingEntryGainsOnlyTheLink(t *testing.T) {
	for name, version := range map[string]string{"current version": "", "version 5": "5"} {
		t.Run(name, func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			fixture := newExistingLinkFixture(t, h)
			before := storedResume(t, h, fixture.resumeID)
			response := linkExistingRequest(t, h, fixture.resumeID, fixture.atlas, fixture.target.ID, before.Revision, version)
			wantVersion := version
			if wantVersion == "" {
				wantVersion = wireVersionString(docmigrate.CurrentVersion)
			}
			if response.status != http.StatusOK || response.header.Get("ETag") != `"r`+formatRevision(before.Revision+1)+`"` ||
				response.header.Get(wireVersionHeader) != wantVersion {
				t.Fatalf("link = %d %v %s, want 200 at the next revision and version %s", response.status, response.header, response.body, wantVersion)
			}

			after := storedResume(t, h, fixture.resumeID)
			if after.Revision != before.Revision+1 {
				t.Fatalf("revision = %d, want %d", after.Revision, before.Revision+1)
			}
			wantEntry := projectEntryByID(t, before.Doc, fixture.target.ID)
			wantEntry.ProjectID = stringPointer(fixture.atlas.String())
			if got := projectEntryByID(t, after.Doc, fixture.target.ID); !reflect.DeepEqual(got, wantEntry) {
				t.Fatalf("entry = %+v, want %+v", got, wantEntry)
			}
			want := withEntryLink(t, canonicalDocument(t, before.Doc), fixture.target.ID, fixture.atlas)
			if got, wantBytes := canonicalDocument(t, after.Doc), canonicalDocument(t, want); !bytes.Equal(got, wantBytes) {
				t.Fatalf("document changed beyond the link:\n got=%s\nwant=%s", got, wantBytes)
			}

			read := resumeRequest(t, h, http.MethodGet, apiResumePath+"/"+fixture.resumeID.String(), "", 0, uuid.Nil, version)
			written, readBack := decodeResumeResource(t, response), decodeResumeResource(t, read)
			if written.Revision != readBack.Revision || !reflect.DeepEqual(decodedJSON(t, written.Document), decodedJSON(t, readBack.Document)) {
				t.Fatalf("response = r%s %s, want the resume as read: r%s %s", written.Revision, written.Document, readBack.Revision, readBack.Document)
			}
			if version == "5" && containsKey(decodedJSON(t, written.Document), "projectId") {
				t.Fatalf("version 5 response carries projectId: %s", written.Document)
			}
		})
	}
}

// Every refusal answers the same at version 5 as at the current version, so
// an older client cannot attach a link the current version refuses.
func TestProjectLinks_ExistingEntryRefusalsWriteNothing(t *testing.T) {
	h := newResumeAPITestHarness(t)
	fixture := newExistingLinkFixture(t, h)
	foreign := createLinkProject(t, h, createOtherAccount(t, h), "foreign", linkProjectDocument("Foreign"))
	otherEntry := unlinkedProjectEntry()
	other, err := h.resumes.Create(h.ctx, h.userID, "other", withProjectSection(loadMinimalDocument(t), "projects", otherEntry))
	if err != nil {
		t.Fatalf("create other resume: %v", err)
	}
	otherBefore := storedResume(t, h, other.ID)
	before := storedResume(t, h, fixture.resumeID)
	revision := before.Revision
	cases := []struct {
		name     string
		project  uuid.UUID
		entryID  string
		revision int64
		status   int
		code     string
	}{
		{"unknown entry", fixture.atlas, uuid.NewString(), revision, http.StatusNotFound, "entry_not_found"},
		{"entry of another resume", fixture.atlas, otherEntry.ID, revision, http.StatusNotFound, "entry_not_found"},
		{"entry linked to another project", fixture.atlas, fixture.linkedEntryID, revision, http.StatusConflict, "project_already_linked"},
		{"entry linked to the same project", fixture.linked, fixture.linkedEntryID, revision, http.StatusConflict, "project_already_linked"},
		{"work entry", fixture.atlas, fullFixtureWorkEntryID, revision, http.StatusUnprocessableEntity, "document_invalid"},
		{"custom entry", fixture.atlas, fullFixtureCustomEntryID, revision, http.StatusUnprocessableEntity, "document_invalid"},
		{"project linked by another entry", fixture.linked, fixture.target.ID, revision, http.StatusConflict, "project_already_linked"},
		{"another account's project", foreign.ID, fixture.target.ID, revision, http.StatusNotFound, "project_not_found"},
		{"missing project", uuid.New(), fixture.target.ID, revision, http.StatusNotFound, "project_not_found"},
		{"stale If-Match", fixture.atlas, fixture.target.ID, revision + 1, http.StatusPreconditionFailed, "revision_mismatch"},
		{"missing If-Match", fixture.atlas, fixture.target.ID, 0, http.StatusPreconditionRequired, "precondition_required"},
	}
	bodies := map[string][]byte{}
	for _, tc := range cases {
		for _, version := range []string{"", "5"} {
			response := linkExistingRequest(t, h, fixture.resumeID, tc.project, tc.entryID, tc.revision, version)
			if response.status != tc.status || errorCodeOf(t, response.body) != tc.code {
				t.Errorf("%s at version %q = %d %s, want %d %s", tc.name, version, response.status, response.body, tc.status, tc.code)
			}
			requireUnchanged(t, h, tc.name, before)
			if version == "" {
				bodies[tc.name] = response.body
			}
		}
	}
	requireUnchanged(t, h, "a link naming its entry", otherBefore)
	if foreignBody, missingBody := bodies["another account's project"], bodies["missing project"]; !bytes.Equal(foreignBody, missingBody) {
		t.Fatalf("foreign project = %s, missing project = %s; want identical answers", foreignBody, missingBody)
	}
}

// An omitted entryId inserts an entry, so an explicit null is refused before
// any write rather than read as an insert.
func TestProjectLinks_NullEntryIDWritesNothing(t *testing.T) {
	h := newResumeAPITestHarness(t)
	fixture := newExistingLinkFixture(t, h)
	before := storedResume(t, h, fixture.resumeID)
	for _, version := range []string{"", "5"} {
		response := linkRouteRequest(t, h, http.MethodPost, apiResumePath+"/"+fixture.resumeID.String()+"/project-links",
			`{"projectId":"`+fixture.atlas.String()+`","entryId":null}`, before.Revision, version)
		var envelope struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(response.body, &envelope); err != nil || response.status != http.StatusBadRequest ||
			envelope.Error.Code != "request_invalid" || !strings.Contains(envelope.Error.Message, `"entryId"`) {
			t.Fatalf("null entryId at version %q = %d %s, want 400 request_invalid naming entryId", version, response.status, response.body)
		}
		requireUnchanged(t, h, "a null entryId", before)
	}
}

// Two links of one project to two entries at one revision: the save's
// revision check lets one commit, and the loser's retry meets the link the
// winner made.
func TestProjectLinks_ExistingEntryConcurrentLinksOfOneProject(t *testing.T) {
	h := newResumeAPITestHarness(t)
	fixture := newExistingLinkFixture(t, h)
	entries := []string{fixture.target.ID, fullFixtureProjectEntryID}
	responses := make([]testHTTPResponse, len(entries))
	var wg sync.WaitGroup
	for index, entryID := range entries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			responses[index] = linkExistingRequest(t, h, fixture.resumeID, fixture.atlas, entryID, fixture.revision, "")
		}()
	}
	wg.Wait()
	statuses := []int{responses[0].status, responses[1].status}
	sort.Ints(statuses)
	if !reflect.DeepEqual(statuses, []int{http.StatusOK, http.StatusPreconditionFailed}) {
		t.Fatalf("concurrent links = %v: %s / %s", statuses, responses[0].body, responses[1].body)
	}
	winner, loser := 0, 1
	if responses[0].status != http.StatusOK {
		winner, loser = 1, 0
	}
	if code := errorCodeOf(t, responses[loser].body); code != "revision_mismatch" {
		t.Fatalf("loser code = %q, want revision_mismatch", code)
	}
	retry := linkExistingRequest(t, h, fixture.resumeID, fixture.atlas, entries[loser], fixture.revision+1, "")
	if retry.status != http.StatusConflict || errorCodeOf(t, retry.body) != "project_already_linked" {
		t.Fatalf("loser retry = %d %s, want 409 project_already_linked", retry.status, retry.body)
	}
	want := map[string]string{fixture.linkedEntryID: fixture.linked.String(), entries[winner]: fixture.atlas.String()}
	if links := storedLinks(t, h, fixture.resumeID); !reflect.DeepEqual(links, want) {
		t.Fatalf("stored links = %v, want %v", links, want)
	}
	if stored := storedResume(t, h, fixture.resumeID); stored.Revision != fixture.revision+1 {
		t.Fatalf("revision = %d, want %d after one committed link", stored.Revision, fixture.revision+1)
	}
}

func TestProjectLinks_ExistingEntryReplaysItsStoredResponse(t *testing.T) {
	h := newResumeAPITestHarness(t)
	fixture := newExistingLinkFixture(t, h)
	path := apiResumePath + "/" + fixture.resumeID.String() + "/project-links"
	body := existingLinkBody(fixture.atlas, fixture.target.ID)
	key := uuid.NewString()
	linked := keyedLinkRequest(t, h, path, body, fixture.revision, key)
	if linked.status != http.StatusOK {
		t.Fatalf("link = %d %s", linked.status, linked.body)
	}

	entry := projectEntryByID(t, storedResume(t, h, fixture.resumeID).Doc, fixture.target.ID)
	entry.Title = stringPointer("Note G, rewritten")
	edit := resumeRequest(t, h, http.MethodPatch, apiResumePath+"/"+fixture.resumeID.String()+"/entries/project",
		entryJSON(t, entry), fixture.revision+1, uuid.New(), "")
	if edit.status != http.StatusOK {
		t.Fatalf("edit after the link = %d %s", edit.status, edit.body)
	}
	edited := storedResume(t, h, fixture.resumeID)

	requireSameReplay(t, "existing-entry link", linked, keyedLinkRequest(t, h, path, body, fixture.revision, key))
	requireUnchanged(t, h, "the replay", edited)
	for name, changed := range map[string]string{
		"another entry":     existingLinkBody(fixture.atlas, fullFixtureProjectEntryID),
		"an inserting link": `{"projectId":"` + fixture.atlas.String() + `"}`,
	} {
		reused := keyedLinkRequest(t, h, path, changed, fixture.revision, key)
		if reused.status != http.StatusConflict || errorCodeOf(t, reused.body) != "idempotency_key_reuse" {
			t.Fatalf("%s under the same key = %d %s, want 409 idempotency_key_reuse", name, reused.status, reused.body)
		}
		requireUnchanged(t, h, name, edited)
	}
}

func TestProjectLinks_ExistingEntryOnALiveResumeGainsItsPublicLink(t *testing.T) {
	env := newShowcasePublishEnv(t)
	h := env.h
	owned := createLinkProject(t, h, h.userID, "atlas", linkProjectDocument("Atlas"))
	current := storedResume(t, h, env.id)
	target := linkedEntry(uuid.Nil, "Atlas")
	// Seed before publishing: a store write after publish would leave the
	// public fence at the published revision.
	var err error
	if env.revision, err = h.resumes.SaveDocument(h.ctx, h.userID, env.id,
		withProjectSection(current.Doc, "projects", target), current.Revision); err != nil {
		t.Fatalf("seed the entry: %v", err)
	}
	env.publishOK(t, publishBody(env.slug, `"showcaseEnabled":true,"showcaseRole":"backend"`))
	if links := readLive(t, h, env.slug).ProjectLinks; len(links) != 0 {
		t.Fatalf("public links before the link = %+v, want none", links)
	}

	response := linkExistingRequest(t, h, env.id, owned.ID, target.ID, env.revision, "")
	if response.status != http.StatusOK {
		t.Fatalf("link = %d %s", response.status, response.body)
	}
	live := readLive(t, h, env.slug)
	want := []publicresume.ProjectLink{{
		EntryID: target.ID, ProjectSlug: "atlas", URL: resumeAPITestOrigin + "/" + env.slug + "/projects/atlas",
	}}
	if live.Revision != env.revision+1 || !reflect.DeepEqual(live.ProjectLinks, want) {
		t.Fatalf("public resume = r%d links %+v, want r%d links %+v", live.Revision, live.ProjectLinks, env.revision+1, want)
	}
	listing, err := json.Marshal(findListing(t, h, env.slug))
	if err != nil || bytes.Contains(listing, []byte("atlas")) || bytes.Contains(listing, []byte("/projects/")) {
		t.Fatalf("listing %s (%v) carries a project slug or page address", listing, err)
	}
}

// newAgentBearerForTest stores an access token of a resumes:read and
// resumes:write grant for h's user and returns its raw value.
func newAgentBearerForTest(t *testing.T, h *resumeAPITestHarness) string {
	t.Helper()
	now := time.Now().UTC()
	client, err := h.queries.CreateOAuthClient(h.ctx, store.CreateOAuthClientParams{
		ClientName: "Bearer agent", RedirectURIs: json.RawMessage(`["https://agent.example/callback"]`), CreatedAt: now,
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
	entropy := sha256.Sum256([]byte("bearer " + client.ID.String()))
	raw, digest, err := oauthsrv.NewToken(oauthsrv.TokenKindAccess, bytes.NewReader(entropy[:]))
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	if _, err = h.queries.CreateOAuthToken(h.ctx, store.CreateOAuthTokenParams{
		TokenDigest: digest[:], Kind: string(oauthsrv.TokenKindAccess), FamilyID: uuid.New(), ClientID: client.ID,
		UserID: h.userID, GrantID: grant.ID, CreatedAt: now, ExpiresAt: now.Add(time.Hour), FamilyExpiresAt: now.Add(30 * 24 * time.Hour),
	}); err != nil {
		t.Fatalf("CreateOAuthToken: %v", err)
	}
	return raw
}

// The link route admits only a cookie session, and a bearer write that adds
// a link needs a scope no grant holds, so neither bearer path links an
// existing entry (docs/design/projects-contract.md "Agents").
func TestProjectLinks_ExistingEntryRefusesBearerCallers(t *testing.T) {
	h := newResumeAPITestHarness(t)
	fixture := newExistingLinkFixture(t, h)
	before := storedResume(t, h, fixture.resumeID)

	req, err := http.NewRequestWithContext(h.ctx, http.MethodPost,
		h.server.URL+apiResumePath+"/"+fixture.resumeID.String()+"/project-links",
		strings.NewReader(existingLinkBody(fixture.atlas, fixture.target.ID)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+newAgentBearerForTest(t, h))
	req.Header.Set("Origin", resumeAPITestOrigin)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", uuid.NewString())
	req.Header.Set("If-Match", `"r`+formatRevision(before.Revision)+`"`)
	httpResponse, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	bearer := snapshotHTTPResponse(t, httpResponse)
	if bearer.status != http.StatusUnauthorized || errorCodeOf(t, bearer.body) != "session_required" {
		t.Fatalf("bearer link = %d %s, want 401 session_required", bearer.status, bearer.body)
	}
	requireUnchanged(t, h, "the bearer link", before)

	entry := projectEntryByID(t, before.Doc, fixture.target.ID)
	entry.ProjectID = stringPointer(fixture.atlas.String())
	agent := h.service.ExecuteAgent(h.ctx, newAgentPrincipalForTest(t, h), AgentCall{
		Operation: AgentUpsertEntry, IdempotencyKey: uuid.NewString(), ResumeID: fixture.resumeID.String(),
		Revision: formatRevision(before.Revision), SectionKey: "project",
		Payload: json.RawMessage(entryJSON(t, entry)),
	})
	if agent.Status != http.StatusForbidden || errorCodeOf(t, agent.Body) != "scope_denied" {
		t.Fatalf("agent link of the entry = %d %s, want 403 scope_denied", agent.Status, agent.Body)
	}
	requireUnchanged(t, h, "the agent link", before)
}
