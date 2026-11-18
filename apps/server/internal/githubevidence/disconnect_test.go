package githubevidence

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
	"danny.vn/aboutme/apps/server/internal/store"
)

type disconnectWrite struct {
	Generation       int64        `json:"generation"`
	ProjectRevisions []revisionOf `json:"projectRevisions"`
	ResumeRevisions  []revisionOf `json:"resumeRevisions"`
}

func (h *harness) currentGeneration() int64 {
	h.t.Helper()
	return h.connection(h.user.ID).generation
}

func (h *harness) disconnectAs(query, key string) *httptest.ResponseRecorder {
	h.t.Helper()
	c := h.as(http.MethodDelete, StatusPath+query)
	if key != "" {
		c.header = http.Header{"Idempotency-Key": {key}}
	}
	return h.do(c)
}

func disconnectQuery(keepLinks bool, generation int64) string {
	return "?keepLinks=" + strconv.FormatBool(keepLinks) + "&generation=" + strconv.FormatInt(generation, 10)
}

func (h *harness) disconnect(keepLinks bool) *httptest.ResponseRecorder {
	h.t.Helper()
	return h.disconnectAs(disconnectQuery(keepLinks, h.currentGeneration()), uuid.NewString())
}

func requireDisconnected(t *testing.T, rec *httptest.ResponseRecorder) disconnectWrite {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("disconnect = %d %s, want 200", rec.Code, rec.Body.String())
	}
	out := decodeData[disconnectWrite](t, rec)
	if out.ProjectRevisions == nil || out.ResumeRevisions == nil {
		t.Fatalf("disconnect body %s lacks a revision list", rec.Body.String())
	}
	return out
}

func (eh *evidenceHarness) projectRevision(id uuid.UUID) int64 {
	eh.t.Helper()
	var revision int64
	if err := eh.pool.QueryRow(eh.t.Context(), `SELECT revision FROM projects WHERE id = $1`, id).Scan(&revision); err != nil {
		eh.t.Fatal(err)
	}
	return revision
}

func (eh *evidenceHarness) accountEvidence() int {
	eh.t.Helper()
	var n int
	if err := eh.pool.QueryRow(eh.t.Context(), `SELECT count(*) FROM project_evidence e JOIN projects p ON p.id = e.project_id WHERE p.user_id = $1`,
		eh.user.ID).Scan(&n); err != nil {
		eh.t.Fatal(err)
	}
	return n
}

func (eh *evidenceHarness) withProject(slug string, items int) uuid.UUID {
	eh.t.Helper()
	current := eh.project
	eh.project = eh.createProject(slug)
	eh.seedEvidence(items)
	id := eh.project.ID
	eh.project = current
	return id
}

func (eh *evidenceHarness) linkingResumeOf(projectID uuid.UUID, live bool) resume.Resume {
	eh.t.Helper()
	current := eh.project
	eh.project.ID = projectID
	defer func() { eh.project = current }()
	return eh.linkingResume(live)
}

func revisionsOf(entries []revisionOf) map[string]string {
	out := make(map[string]string, len(entries))
	for _, entry := range entries {
		out[entry.ID] = entry.Revision
	}
	return out
}

func requireSortedIDs(t *testing.T, name string, entries []revisionOf) {
	t.Helper()
	ids := make([]string, len(entries))
	for i, entry := range entries {
		ids[i] = entry.ID
	}
	if !slices.IsSorted(ids) {
		t.Fatalf("%s = %v, want id order", name, ids)
	}
}

func TestEvidenceDisconnect_RemovingLinksDeletesEveryItemAndAdvancesRevisions(t *testing.T) {
	eh := newEvidenceHarness(t)
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	token := eh.liveToken()
	eh.seedEvidence(2)
	second := eh.withProject("second", 1)
	plain := eh.createProject("plain")
	live := eh.linkingResume(true)
	secondLive := eh.linkingResumeOf(second, true)
	unaffected := eh.linkingResumeOf(plain.ID, true)
	before := map[uuid.UUID]int64{eh.project.ID: eh.projectRevision(eh.project.ID), second: eh.projectRevision(second), plain.ID: eh.projectRevision(plain.ID)}
	held := eh.holdLease(live)
	out := requireDisconnected(t, eh.disconnect(false))
	if out.Generation != 2 || eh.currentGeneration() != 2 {
		t.Fatalf("generation = %d (stored %d), want 2", out.Generation, eh.currentGeneration())
	}
	if n := eh.accountEvidence(); n != 0 {
		t.Fatalf("evidence after removing links = %d, want 0", n)
	}
	wantProjects := map[string]string{
		eh.project.ID.String(): strconv.FormatInt(before[eh.project.ID]+1, 10),
		second.String():        strconv.FormatInt(before[second]+1, 10),
	}
	if got := revisionsOf(out.ProjectRevisions); len(got) != 2 || got[eh.project.ID.String()] != wantProjects[eh.project.ID.String()] ||
		got[second.String()] != wantProjects[second.String()] {
		t.Fatalf("project revisions = %+v, want %v", out.ProjectRevisions, wantProjects)
	}
	requireSortedIDs(t, "project revisions", out.ProjectRevisions)
	for id, revision := range before {
		want := revision + 1
		if id == plain.ID {
			want = revision
		}
		if got := eh.projectRevision(id); got != want {
			t.Fatalf("project %s revision = %d, want %d", id, got, want)
		}
	}
	wantResumes := map[string]string{
		live.ID.String():       strconv.FormatInt(live.Revision+1, 10),
		secondLive.ID.String(): strconv.FormatInt(secondLive.Revision+1, 10),
	}
	if got := revisionsOf(out.ResumeRevisions); len(got) != 2 || got[live.ID.String()] != wantResumes[live.ID.String()] ||
		got[secondLive.ID.String()] != wantResumes[secondLive.ID.String()] {
		t.Fatalf("resume revisions = %+v, want %v", out.ResumeRevisions, wantResumes)
	}
	requireSortedIDs(t, "resume revisions", out.ResumeRevisions)
	if eh.resumeRevision(live.ID) != live.Revision+1 || eh.resumeRevision(secondLive.ID) != secondLive.Revision+1 ||
		eh.resumeRevision(unaffected.ID) != unaffected.Revision {
		t.Fatal("disconnect advanced the wrong resumes")
	}
	if held.revoked.Load() {
		t.Fatal("disconnect drained an admitted request")
	}
	if row := eh.connection(eh.user.ID); row.githubUserID != nil {
		t.Fatalf("connection after disconnect = %+v", row)
	}
	eh.requireRevokedOnce(token)
}

func TestEvidenceDisconnect_KeepingLinksChangesNoProjectAndDropsEveryMark(t *testing.T) {
	eh := newEvidenceHarness(t)
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	eh.seedEvidence(2)
	live := eh.linkingResume(true)
	draft := eh.linkingResume(false)
	if items := eh.projectEvidence(); len(items) != 2 || !items[0].Confirmed || !items[1].Confirmed {
		t.Fatalf("before disconnect = %+v, want two confirmed items", items)
	}
	projectBefore, stored := eh.revision(), eh.stored()
	out := requireDisconnected(t, eh.disconnect(true))
	if len(out.ProjectRevisions) != 0 || eh.revision() != projectBefore {
		t.Fatalf("project revisions = %+v, stored %d; want none and %d", out.ProjectRevisions, eh.revision(), projectBefore)
	}
	if len(out.ResumeRevisions) != 1 || out.ResumeRevisions[0].ID != live.ID.String() ||
		out.ResumeRevisions[0].Revision != strconv.FormatInt(live.Revision+1, 10) || eh.resumeRevision(live.ID) != live.Revision+1 {
		t.Fatalf("resume revisions = %+v, want %s at %d", out.ResumeRevisions, live.ID, live.Revision+1)
	}
	if eh.resumeRevision(draft.ID) != draft.Revision {
		t.Fatal("disconnect advanced a draft resume")
	}
	after := eh.stored()
	for i := range after {
		if len(after) != len(stored) || after[i].ID != stored[i].ID || after[i].Position != stored[i].Position ||
			after[i].CheckedBinding != stored[i].CheckedBinding || after[i].RuleMet != stored[i].RuleMet || after[i].FlagReason != nil {
			t.Fatalf("stored evidence changed: %+v, want %+v", after, stored)
		}
	}
	for _, item := range eh.projectEvidence() {
		if item.Confirmed {
			t.Fatalf("after keeping links = %+v, want every item unconfirmed", item)
		}
	}
}

func TestEvidenceDisconnect_ReplayIsAnsweredBeforeTheGenerationCheck(t *testing.T) {
	h := newHarness(t)
	requireOutcome(t, h.connect(h.raw, h.session), outcomeConnected)
	key, query := uuid.NewString(), disconnectQuery(true, 1)
	first := h.disconnectAs(query, key)
	stored := requireDisconnected(t, first)
	requireOutcome(t, h.connect(h.raw, h.session), outcomeConnected)
	if h.currentGeneration() != 3 {
		t.Fatalf("generation after reconnecting = %d, want 3", h.currentGeneration())
	}
	replay := h.disconnectAs(query, key)
	if replay.Code != http.StatusOK || replay.Body.String() != first.Body.String() {
		t.Fatalf("replay = %d %s, want the stored %s", replay.Code, replay.Body.String(), first.Body.String())
	}
	if row := h.connection(h.user.ID); row.githubUserID == nil || row.generation != 3 || stored.Generation != 2 {
		t.Fatalf("after a replay connection = %+v, want connected at 3", row)
	}
	if h.liveToken() == "" {
		t.Fatal("a replay ended the new session")
	}
	requireError(t, h.disconnectAs(disconnectQuery(false, 1), key), http.StatusConflict, "idempotency_key_reuse")
	requireError(t, h.disconnectAs(disconnectQuery(true, 3), key), http.StatusConflict, "idempotency_key_reuse")
}

func TestEvidenceDisconnect_StaleGenerationChangesNothing(t *testing.T) {
	eh := newEvidenceHarness(t)
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	token := eh.liveToken()
	eh.seedEvidence(1)
	live := eh.linkingResume(true)
	project := eh.revision()
	for _, generation := range []int64{0, 2} {
		requireError(t, eh.disconnectAs(disconnectQuery(false, generation), uuid.NewString()), http.StatusConflict, "evidence_connection_changed")
	}
	if row := eh.connection(eh.user.ID); row.githubUserID == nil || row.generation != 1 {
		t.Fatalf("connection after a stale disconnect = %+v", row)
	}
	if eh.accountEvidence() != 1 || eh.revision() != project || eh.resumeRevision(live.ID) != live.Revision {
		t.Fatal("a stale disconnect changed evidence or revisions")
	}
	if eh.liveToken() != token {
		t.Fatal("a stale disconnect ended the session")
	}
	eh.drain()
	if _, revoked, _, _ := eh.gh.snapshot(); len(revoked) != 0 {
		t.Fatalf("a stale disconnect revoked %d tokens", len(revoked))
	}
}

func TestEvidenceDisconnect_WithoutEvidenceClearsTheConnection(t *testing.T) {
	h := newHarness(t)
	out := requireDisconnected(t, h.disconnect(true))
	if out.Generation != 1 || len(out.ProjectRevisions) != 0 || len(out.ResumeRevisions) != 0 {
		t.Fatalf("disconnect without a row = %+v, want generation 1 and no revisions", out)
	}
	requireOutcome(t, h.connect(h.raw, h.session), outcomeConnected)
	token := h.liveToken()
	out = requireDisconnected(t, h.disconnect(false))
	if out.Generation != 3 || len(out.ProjectRevisions) != 0 || len(out.ResumeRevisions) != 0 {
		t.Fatalf("disconnect = %+v, want generation 3 and no revisions", out)
	}
	if row := h.connection(h.user.ID); row.githubUserID != nil || row.login != nil || row.connectedAt != nil || row.generation != 3 {
		t.Fatalf("connection after disconnect = %+v", row)
	}
	h.requireRevokedOnce(token)
	if _, live := h.svc.registry.current(h.user.ID); live {
		t.Fatal("disconnect left a live session")
	}
}

type steeredLibrary struct {
	inner     Library
	authorize func(attempt int)
	apply     func(resume.StoredResponse) (resume.StoredResponse, error)
	attempts  int
}

func (l *steeredLibrary) DisconnectEvidence(ctx context.Context, d resumeapi.EvidenceDisconnect) (resume.ExecuteResult, error) {
	authorize, apply := d.Authorize, d.Apply
	d.Authorize = func(ctx context.Context, q *store.Queries) error {
		l.attempts++
		if l.authorize != nil {
			l.authorize(l.attempts)
		}
		return authorize(ctx, q)
	}
	d.Apply = func(ctx context.Context, q *store.Queries, changed []resumeapi.ChangedResume) (resume.StoredResponse, error) {
		response, err := apply(ctx, q, changed)
		if err != nil || l.apply == nil {
			return response, err
		}
		return l.apply(response)
	}
	return l.inner.DisconnectEvidence(ctx, d)
}

func TestEvidenceDisconnect_ThreeChangedPlansAreBusyAndChangeNothing(t *testing.T) {
	eh := newEvidenceHarness(t)
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	token := eh.liveToken()
	eh.seedEvidence(1)
	live := eh.linkingResume(true)
	late := make([]uuid.UUID, 2)
	lateResumes := make([]resume.Resume, 2)
	for i := range late {
		late[i] = eh.createProject("late-" + strconv.Itoa(i)).ID
		lateResumes[i] = eh.linkingResumeOf(late[i], true)
	}
	steered := &steeredLibrary{inner: eh.library, authorize: func(attempt int) {
		statement := `INSERT INTO project_evidence (project_id, position, kind, repository_id, item_id, url,
			repository_name, title, item_date, checked_at, checked_binding, rule_met)
			VALUES ($1, 0, 'repository', 700, 700, 'https://github.com/octocat/late', 'octocat/late', 'octocat/late', now(), now(), 1, true)`
		target := late[min(attempt, 2)-1]
		if attempt == 3 {
			statement = `DELETE FROM project_evidence WHERE project_id = $1`
		}
		if _, err := eh.pool.Exec(context.Background(), statement, target); err != nil {
			t.Errorf("evidence change during the disconnect: %v", err)
		}
	}}
	eh.svc.SetLibrary(steered)
	project := eh.revision()
	rec := eh.disconnect(false)
	requireError(t, rec, http.StatusServiceUnavailable, "public_state_busy")
	if rec.Header().Get("Retry-After") != "1" || steered.attempts != 3 {
		t.Fatalf("busy Retry-After = %q after %d attempts, want 1 after 3", rec.Header().Get("Retry-After"), steered.attempts)
	}
	if row := eh.connection(eh.user.ID); row.githubUserID == nil || row.generation != 1 {
		t.Fatalf("connection after a busy disconnect = %+v", row)
	}
	if eh.accountEvidence() != 2 || eh.revision() != project || eh.resumeRevision(live.ID) != live.Revision {
		t.Fatal("a busy disconnect changed evidence or revisions")
	}
	for _, item := range lateResumes {
		if eh.resumeRevision(item.ID) != item.Revision {
			t.Fatalf("a busy disconnect advanced %s", item.ID)
		}
	}
	if eh.liveToken() != token {
		t.Fatal("a busy disconnect ended the session")
	}
}

func TestEvidenceDisconnect_AFailureBeforeCommitKeepsEverything(t *testing.T) {
	failed := errors.New("failure after the writes")
	for _, tc := range []struct {
		name    string
		prepare func(eh *evidenceHarness, steered *steeredLibrary)
		status  int
		code    string
	}{
		{"apply failure", func(_ *evidenceHarness, steered *steeredLibrary) {
			steered.apply = func(resume.StoredResponse) (resume.StoredResponse, error) { return resume.StoredResponse{}, failed }
		}, http.StatusInternalServerError, "internal_error"},
		{"idempotency storage", func(eh *evidenceHarness, _ *steeredLibrary) {
			if _, err := eh.pool.Exec(eh.t.Context(), `INSERT INTO idempotency_usage (user_id, retained_records, stored_bytes) VALUES ($1, 0, $2)
				ON CONFLICT (user_id) DO UPDATE SET stored_bytes = EXCLUDED.stored_bytes`, eh.user.ID, int64(1<<30)); err != nil {
				eh.t.Fatal(err)
			}
		}, http.StatusTooManyRequests, "rate_limited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eh := newEvidenceHarness(t)
			requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
			token := eh.liveToken()
			eh.seedEvidence(1)
			live := eh.linkingResume(true)
			project := eh.revision()
			steered := &steeredLibrary{inner: eh.library}
			eh.svc.SetLibrary(steered)
			tc.prepare(eh, steered)
			requireError(t, eh.disconnect(false), tc.status, tc.code)
			if row := eh.connection(eh.user.ID); row.githubUserID == nil || row.generation != 1 {
				t.Fatalf("connection after a failed disconnect = %+v", row)
			}
			if eh.accountEvidence() != 1 || eh.revision() != project || eh.resumeRevision(live.ID) != live.Revision {
				t.Fatal("a failed disconnect changed evidence or revisions")
			}
			if eh.liveToken() != token {
				t.Fatal("a failed disconnect ended the session")
			}
			eh.drain()
			if _, revoked, _, _ := eh.gh.snapshot(); len(revoked) != 0 {
				t.Fatalf("a failed disconnect revoked %d tokens", len(revoked))
			}
			if _, err := eh.pool.Exec(t.Context(), `UPDATE idempotency_usage SET stored_bytes = 0 WHERE user_id = $1`, eh.user.ID); err != nil {
				t.Fatal(err)
			}
			eh.svc.SetLibrary(eh.library)
			requireDisconnected(t, eh.disconnect(false))
			eh.requireRevokedOnce(token)
			if eh.liveToken() != "" || eh.accountEvidence() != 0 {
				t.Fatal("the disconnect after the failure left a session or evidence")
			}
		})
	}
}
