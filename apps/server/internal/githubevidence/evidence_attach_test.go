package githubevidence

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestEvidenceAttach_PullRequestLinkStoresTheServerBuiltItem(t *testing.T) {
	eh := newEvidenceHarness(t)
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	eh.servePullRequest()
	before := eh.revision()
	out := requireWrite(t, eh.attach(`{"kind":"pullRequest","link":" https://github.com/octocat/hello/pull/42/ "}`))
	if out.ProjectRevision != strconv.FormatInt(before+1, 10) || len(out.ResumeRevisions) != 0 || len(out.Evidence) != 1 {
		t.Fatalf("attach = %+v, want one item at project revision %d and no resume", out, before+1)
	}
	item := out.Evidence[0]
	merged := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if item.Kind != "pullRequest" || item.URL != "https://github.com/octocat/hello/pull/42" || item.RepositoryName != "octocat/hello" ||
		item.Title != "Fix the thing" || !item.ItemDate.Equal(merged) || !item.Confirmed || item.FlagReason != nil {
		t.Fatalf("attached item = %+v", item)
	}
	rows := eh.stored()
	if len(rows) != 1 || rows[0].ItemID != 9001 || rows[0].RepositoryID != 101 || rows[0].PullNumber == nil || *rows[0].PullNumber != 42 ||
		rows[0].CheckedBinding != 1 || !rows[0].RuleMet || rows[0].FlagReason != nil || rows[0].ID.String() != item.ID {
		t.Fatalf("stored = %+v", rows)
	}
	if calls := eh.gh.readCalls(); len(calls) != 2 {
		t.Fatalf("GitHub reads = %v, want the repository and the pull request", calls)
	}
}

func TestEvidenceAttach_DisconnectBetweenTheReadAndTheTransactionAttachesNothing(t *testing.T) {
	eh := newEvidenceHarness(t)
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	eh.gh.readJSON("/repos/octocat/hello", http.StatusOK, repositoryJSON)
	var disconnected int
	eh.gh.read("/repos/octocat/hello/pulls/42", func(w http.ResponseWriter, r *http.Request) {
		c := eh.as(http.MethodDelete, StatusPath+disconnectQuery(true, 1))
		c.header = http.Header{"Idempotency-Key": {uuid.NewString()}}
		disconnected = eh.doWith(r.Context(), c).Code
		w.Header().Set("Content-Type", "application/json")
		eh.gh.write(w, pullRequestJSON)
	})
	before := eh.revision()
	requireError(t, eh.attach(pullRequestLink), http.StatusConflict, "evidence_session_required")
	if disconnected != http.StatusOK {
		t.Fatalf("disconnect during the read = %d", disconnected)
	}
	if rows := eh.stored(); len(rows) != 0 || eh.revision() != before {
		t.Fatalf("after a disconnect the attach stored %d items at revision %d, want none at %d", len(rows), eh.revision(), before)
	}
}

func TestEvidenceAttach_GitHub401EndsTheSessionAndStoresNothing(t *testing.T) {
	eh := newEvidenceHarness(t)
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	token := eh.liveToken()
	eh.gh.readJSON("/repos/octocat/hello", http.StatusUnauthorized, `{"message":"Bad credentials"}`)
	before := eh.revision()
	requireError(t, eh.attach(pullRequestLink), http.StatusConflict, "evidence_session_required")
	if rows := eh.stored(); len(rows) != 0 || eh.revision() != before {
		t.Fatalf("a 401 stored %d items at revision %d", len(rows), eh.revision())
	}
	if _, live := eh.svc.registry.current(eh.user.ID); live {
		t.Fatal("the evidence session outlived GitHub's 401")
	}
	eh.requireRevokedOnce(token)
}

func TestEvidenceAttach_ReplayAnswersWithoutGitHubAndRefusesAnotherBody(t *testing.T) {
	eh := newEvidenceHarness(t)
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	eh.servePullRequest()
	key, revision := uuid.NewString(), eh.revision()
	first := eh.write(http.MethodPost, "", pullRequestLink, key, revision)
	requireWrite(t, first)
	reads := len(eh.gh.readCalls())
	again := eh.write(http.MethodPost, "", pullRequestLink, key, revision)
	if again.Code != first.Code || again.Body.String() != first.Body.String() {
		t.Fatalf("replay = %d %s, want %d %s", again.Code, again.Body.String(), first.Code, first.Body.String())
	}
	if calls := eh.gh.readCalls(); len(calls) != reads {
		t.Fatalf("replay read GitHub again: %v", calls[reads:])
	}
	other := `{"kind":"repository","link":"https://github.com/octocat/hello"}`
	requireError(t, eh.write(http.MethodPost, "", other, key, revision), http.StatusConflict, "idempotency_key_reuse")
	if len(eh.stored()) != 1 {
		t.Fatalf("stored = %+v, want only the first attach", eh.stored())
	}
}

func TestEvidenceAttach_SameItemTwiceIsADuplicate(t *testing.T) {
	eh := newEvidenceHarness(t)
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	eh.servePullRequest()
	requireWrite(t, eh.attach(pullRequestLink))
	before := eh.revision()
	requireError(t, eh.attach(pullRequestLink), http.StatusConflict, "evidence_duplicate")
	if len(eh.stored()) != 1 || eh.revision() != before {
		t.Fatal("a duplicate attach changed the project")
	}
}

func TestEvidenceAttach_EleventhItemIsRefused(t *testing.T) {
	eh := newEvidenceHarness(t)
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	eh.servePullRequest()
	eh.seedEvidence(10)
	requireError(t, eh.attach(pullRequestLink), http.StatusUnprocessableEntity, "evidence_limit")
	if len(eh.stored()) != 10 {
		t.Fatalf("stored %d items, want 10", len(eh.stored()))
	}
}

func TestEvidenceAttach_RuleFailureAndGoneAreNotConfirmed(t *testing.T) {
	for name, serve := range map[string]func(eh *evidenceHarness){
		"author differs": func(eh *evidenceHarness) {
			eh.gh.readJSON("/repos/octocat/hello", http.StatusOK, repositoryJSON)
			eh.gh.readJSON("/repos/octocat/hello/pulls/42", http.StatusOK, `{"id":9001,"number":42,"title":"x","merged":true,"merged_at":"2026-01-02T03:04:05Z","user":{"id":7},"html_url":"https://github.com/octocat/hello/pull/42"}`)
		},
		"not merged": func(eh *evidenceHarness) {
			eh.gh.readJSON("/repos/octocat/hello", http.StatusOK, repositoryJSON)
			eh.gh.readJSON("/repos/octocat/hello/pulls/42", http.StatusOK, `{"id":9001,"number":42,"title":"x","merged":false,"merged_at":null,"user":{"id":583231},"html_url":"https://github.com/octocat/hello/pull/42"}`)
		},
		"repository gone": func(eh *evidenceHarness) {
			eh.gh.readJSON("/repos/octocat/hello", http.StatusNotFound, `{"message":"Not Found"}`)
		},
		"pull request gone": func(eh *evidenceHarness) {
			eh.gh.readJSON("/repos/octocat/hello", http.StatusOK, repositoryJSON)
			eh.gh.readJSON("/repos/octocat/hello/pulls/42", http.StatusGone, `{"message":"Gone"}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			eh := newEvidenceHarness(t)
			requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
			serve(eh)
			requireError(t, eh.attach(pullRequestLink), http.StatusUnprocessableEntity, "evidence_not_confirmed")
			if len(eh.stored()) != 0 {
				t.Fatal("an unconfirmed item was stored")
			}
		})
	}
}

func TestEvidenceAttach_RequestShapes(t *testing.T) {
	eh := newEvidenceHarness(t)
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	for name, tc := range map[string]struct {
		body   string
		status int
		code   string
	}{
		"both link and candidate": {`{"kind":"repository","link":"https://github.com/octocat/hello","candidateId":"x"}`, 400, "request_invalid"},
		"neither":                 {`{"kind":"repository"}`, 400, "request_invalid"},
		"unknown field":           {`{"kind":"repository","link":"https://github.com/octocat/hello","evidence":1}`, 400, "request_invalid"},
		"unknown kind":            {`{"kind":"gist","link":"https://github.com/octocat/hello"}`, 400, "request_invalid"},
		"kind disagrees":          {`{"kind":"release","link":"https://github.com/octocat/hello"}`, 422, "evidence_unsupported_link"},
		"another host":            {`{"kind":"repository","link":"https://gitlab.com/octocat/hello"}`, 422, "evidence_unsupported_link"},
		"unknown candidate":       {`{"kind":"repository","candidateId":"not-listed"}`, 409, "evidence_session_required"},
	} {
		t.Run(name, func(t *testing.T) {
			requireError(t, eh.attach(tc.body), tc.status, tc.code)
		})
	}
	if calls := eh.gh.readCalls(); len(calls) != 0 {
		t.Fatalf("refused requests read GitHub: %v", calls)
	}
}

func TestEvidenceAttach_RefusesABearerTokenAndAMissingHeader(t *testing.T) {
	eh := newEvidenceHarness(t)
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	bearer := eh.as(http.MethodPost, eh.path(""))
	bearer.cookies = nil
	bearer.body = pullRequestLink
	bearer.header = http.Header{"Authorization": {"Bearer " + sentinelToken}, "Idempotency-Key": {uuid.NewString()}, "If-Match": {`"r1"`}, "Content-Type": {"application/json"}}
	requireError(t, eh.do(bearer), http.StatusUnauthorized, "session_required")
	withBoth := eh.as(http.MethodPost, eh.path(""))
	withBoth.body = pullRequestLink
	withBoth.header = http.Header{"Authorization": {"Bearer " + sentinelToken}, "Idempotency-Key": {uuid.NewString()}, "If-Match": {`"r1"`}, "Content-Type": {"application/json"}}
	requireError(t, eh.do(withBoth), http.StatusBadRequest, "request_invalid")
	noMatch := eh.as(http.MethodPost, eh.path(""))
	noMatch.body = pullRequestLink
	noMatch.header = http.Header{"Idempotency-Key": {uuid.NewString()}, "Content-Type": {"application/json"}}
	requireError(t, eh.do(noMatch), http.StatusPreconditionRequired, "precondition_required")
	noKey := eh.as(http.MethodPost, eh.path(""))
	noKey.body = pullRequestLink
	noKey.header = http.Header{"If-Match": {`"r1"`}, "Content-Type": {"application/json"}}
	requireError(t, eh.do(noKey), http.StatusBadRequest, "idempotency_key_required")
	stale := eh.write(http.MethodPost, "", pullRequestLink, uuid.NewString(), eh.revision()+5)
	requireError(t, stale, http.StatusPreconditionFailed, "revision_mismatch")
	if calls := eh.gh.readCalls(); len(calls) != 0 {
		t.Fatalf("refused requests read GitHub: %v", calls)
	}
}
