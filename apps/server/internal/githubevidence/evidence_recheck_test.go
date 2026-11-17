package githubevidence

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func (eh *evidenceHarness) attachedPullRequest() string {
	eh.t.Helper()
	requireOutcome(eh.t, eh.connect(eh.raw, eh.session), outcomeConnected)
	eh.servePullRequest()
	return requireWrite(eh.t, eh.attach(pullRequestLink)).Evidence[0].ID
}

func (eh *evidenceHarness) recheck(id string) *httptest.ResponseRecorder {
	eh.t.Helper()
	return eh.write(http.MethodPost, "/"+id+"/recheck", "", uuid.NewString(), eh.revision())
}

func flagOf(item evidenceItem) string {
	if item.FlagReason == nil {
		return ""
	}
	return *item.FlagReason
}

func TestEvidenceRecheck_RuleFailedThenPassClearsTheFlag(t *testing.T) {
	eh := newEvidenceHarness(t)
	id := eh.attachedPullRequest()
	eh.gh.readJSON("/repos/octocat/hello/pulls/42", http.StatusOK, `{"id":9001,"number":42,"title":"Someone else's","merged":true,"merged_at":"2026-01-02T03:04:05Z","user":{"id":7},"html_url":"https://github.com/octocat/hello/pull/42"}`)
	failed := requireWrite(t, eh.recheck(id)).Evidence[0]
	if flagOf(failed) != "rule_failed" || failed.Confirmed || failed.Title != "Someone else's" {
		t.Fatalf("rule failure = %+v", failed)
	}
	if rows := eh.stored(); rows[0].RuleMet || rows[0].FlagReason == nil || *rows[0].FlagReason != "rule_failed" {
		t.Fatalf("stored = %+v", rows[0])
	}
	eh.gh.readJSON("/repos/octocat/hello/pulls/42", http.StatusOK, `{"id":9001,"number":42,"title":"Fix the thing, renamed","merged":true,"merged_at":"2026-01-02T03:04:05Z","user":{"id":583231},"html_url":"https://github.com/octocat/hello/pull/42"}`)
	passed := requireWrite(t, eh.recheck(id)).Evidence[0]
	if passed.FlagReason != nil || !passed.Confirmed || passed.Title != "Fix the thing, renamed" || passed.ID != id {
		t.Fatalf("pass = %+v", passed)
	}
}

func TestEvidenceRecheck_GoneByNotFoundKeepsTheStoredText(t *testing.T) {
	eh := newEvidenceHarness(t)
	id := eh.attachedPullRequest()
	eh.gh.readJSON("/repos/octocat/hello/pulls/42", http.StatusNotFound, `{"message":"Not Found"}`)
	gone := requireWrite(t, eh.recheck(id)).Evidence[0]
	if flagOf(gone) != "gone" || gone.Confirmed || gone.Title != "Fix the thing" || gone.URL != "https://github.com/octocat/hello/pull/42" {
		t.Fatalf("gone = %+v", gone)
	}
}

func TestEvidenceRecheck_GoneByAnotherRepositoryID(t *testing.T) {
	eh := newEvidenceHarness(t)
	id := eh.attachedPullRequest()
	eh.gh.readJSON("/repos/octocat/hello", http.StatusOK, `{"id":202,"full_name":"octocat/hello","owner":{"id":583231},"private":false,"fork":false,"created_at":"2024-01-02T03:04:05Z"}`)
	gone := requireWrite(t, eh.recheck(id)).Evidence[0]
	if flagOf(gone) != "gone" || gone.Confirmed {
		t.Fatalf("recheck after the name moved to another repository = %+v", gone)
	}
	if rows := eh.stored(); rows[0].RepositoryID != 101 || rows[0].RuleMet {
		t.Fatalf("stored = %+v", rows[0])
	}
}

func TestEvidenceRecheck_TransientFailureChangesNothing(t *testing.T) {
	for name, serve := range map[string]func(eh *evidenceHarness){
		"upstream": func(eh *evidenceHarness) {
			eh.gh.readJSON("/repos/octocat/hello/pulls/42", http.StatusInternalServerError, `{"message":"boom"}`)
		},
		"rate limit": func(eh *evidenceHarness) {
			eh.gh.read("/repos/octocat/hello/pulls/42", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "30")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				eh.gh.write(w, `{"message":"slow down"}`)
			})
		},
		"session ended": func(eh *evidenceHarness) {
			eh.do(eh.as(http.MethodPost, SessionEndPath))
		},
	} {
		t.Run(name, func(t *testing.T) {
			eh := newEvidenceHarness(t)
			id := eh.attachedPullRequest()
			before, revision := eh.stored(), eh.revision()
			serve(eh)
			rec := eh.recheck(id)
			switch name {
			case "upstream":
				requireError(t, rec, http.StatusBadGateway, "evidence_upstream")
			case "rate limit":
				requireError(t, rec, http.StatusTooManyRequests, "rate_limited")
				if rec.Header().Get("Retry-After") != "30" {
					t.Fatalf("Retry-After = %q", rec.Header().Get("Retry-After"))
				}
			default:
				requireError(t, rec, http.StatusConflict, "evidence_session_required")
			}
			if after := eh.stored(); !reflect.DeepEqual(after, before) || eh.revision() != revision {
				t.Fatalf("a transient failure changed %+v into %+v", before, after)
			}
		})
	}
}

func TestEvidenceRecheck_UnknownItem(t *testing.T) {
	eh := newEvidenceHarness(t)
	eh.attachedPullRequest()
	requireError(t, eh.recheck(uuid.NewString()), http.StatusNotFound, "evidence_not_found")
}

func TestEvidenceRecheck_FollowsARenameOfTheStoredRepository(t *testing.T) {
	eh := newEvidenceHarness(t)
	id := eh.attachedPullRequest()
	moved := func(location string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", location)
			w.WriteHeader(http.StatusMovedPermanently)
		}
	}
	eh.gh.read("/repos/octocat/hello", moved("https://api.github.com/repositories/202"))
	before := eh.stored()
	requireError(t, eh.recheck(id), http.StatusBadGateway, "evidence_upstream")
	if after := eh.stored(); !reflect.DeepEqual(after, before) {
		t.Fatalf("a rename to another repository id changed %+v into %+v", before, after)
	}
	eh.gh.read("/repos/octocat/hello", moved("https://api.github.com/repositories/101"))
	eh.gh.readJSON("/repositories/101", http.StatusOK, `{"id":101,"full_name":"octocat/renamed","owner":{"id":583231},"private":false,"fork":false,"created_at":"2020-01-02T03:04:05Z"}`)
	eh.gh.readJSON("/repos/octocat/renamed/pulls/42", http.StatusOK, pullRequestJSON)
	item := requireWrite(t, eh.recheck(id)).Evidence[0]
	if item.URL != "https://github.com/octocat/renamed/pull/42" || item.RepositoryName != "octocat/renamed" || !item.Confirmed || item.FlagReason != nil {
		t.Fatalf("after the rename = %+v", item)
	}
}
