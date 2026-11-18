package githubevidence

import (
	"reflect"
	"slices"
	"testing"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/store"
)

type accountState struct {
	evidence   []store.ProjectEvidence
	project    int64
	resume     int64
	connection connectionRow
	binding    int64
}

func (eh *evidenceHarness) asAccount(user store.User, p project.Project, run func()) {
	eh.t.Helper()
	keptUser, keptProject := eh.user, eh.project
	eh.user, eh.project = user, p
	defer func() { eh.user, eh.project = keptUser, keptProject }()
	run()
}

func (eh *evidenceHarness) stateOf(user store.User, p project.Project, live resume.Resume) accountState {
	eh.t.Helper()
	var state accountState
	eh.asAccount(user, p, func() {
		state = accountState{evidence: eh.stored(), project: eh.revision(), resume: eh.resumeRevision(live.ID),
			connection: eh.connection(user.ID), binding: eh.binding(user.ID)}
	})
	return state
}

func TestEvidenceDisconnect_LeavesAnotherAccountAndItsEvidenceUnchanged(t *testing.T) {
	eh := newEvidenceHarness(t)
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	eh.seedEvidence(2)
	eh.linkingResume(true)

	other, otherRaw, otherSession := eh.newAccount()
	requireOutcome(t, eh.connect(otherRaw, otherSession), outcomeConnected)
	eh.gh.set(func(gh *stubGitHub) { gh.userID, gh.login = 777, "someone-else" })
	requireOutcome(t, eh.connect(otherRaw, otherSession), outcomeConnected)
	if row := eh.connection(other.ID); row.githubUserID == nil || *row.githubUserID != 777 || eh.binding(other.ID) != 2 {
		t.Fatalf("the other account's connection = %+v binding %d, want GitHub user 777 at binding 2", row, eh.binding(other.ID))
	}
	if eh.accountEvidence() != 2 {
		t.Fatal("the first account lost evidence when the other account changed GitHub account")
	}

	var otherProject project.Project
	var otherLive resume.Resume
	eh.asAccount(other, eh.project, func() {
		otherProject = eh.createProject("other-atlas")
		eh.project = otherProject
		eh.seedEvidence(2)
		otherLive = eh.linkingResume(true)
	})
	before := eh.stateOf(other, otherProject, otherLive)
	otherToken := eh.tokenOf(other.ID)
	if otherToken == "" {
		t.Fatal("the other account has no live evidence session")
	}

	requireDisconnected(t, eh.disconnect(false))
	if eh.accountEvidence() != 0 {
		t.Fatalf("the disconnected account kept %d evidence items", eh.accountEvidence())
	}
	if after := eh.stateOf(other, otherProject, otherLive); !reflect.DeepEqual(after, before) || len(after.evidence) != 2 {
		t.Fatalf("the other account changed from %+v to %+v", before, after)
	}
	if eh.tokenOf(other.ID) != otherToken {
		t.Fatal("the disconnect ended the other account's evidence session")
	}
	eh.drain()
	if _, revoked, _, _ := eh.gh.snapshot(); slices.Contains(revoked, otherToken) {
		t.Fatal("the disconnect revoked the other account's token")
	}
}

func (eh *evidenceHarness) tokenOf(userID uuid.UUID) string {
	eh.t.Helper()
	live, ok := eh.svc.registry.current(userID)
	if !ok {
		return ""
	}
	value, held := live.token.peek()
	if !held {
		return ""
	}
	return value
}
