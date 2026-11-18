package githubevidence

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/testutil"
)

func renamedPullRequestJSON() string {
	return strings.Replace(pullRequestJSON, `"title":"Fix the thing"`, `"title":"Fix the thing, read during the race"`, 1)
}

func TestEvidenceRecheck_ADisconnectDuringTheReadStoresNothing(t *testing.T) {
	eh := newEvidenceHarness(t)
	id := eh.attachedPullRequest()
	var disconnected int
	eh.gh.read("/repos/octocat/hello/pulls/42", func(w http.ResponseWriter, r *http.Request) {
		c := eh.as(http.MethodDelete, StatusPath+disconnectQuery(true, 1))
		c.header = http.Header{"Idempotency-Key": {uuid.NewString()}}
		disconnected = eh.doWith(r.Context(), c).Code
		w.Header().Set("Content-Type", "application/json")
		eh.gh.write(w, renamedPullRequestJSON())
	})
	before, revision := eh.stored(), eh.revision()
	requireError(t, eh.recheck(id), http.StatusConflict, "evidence_session_required")
	if disconnected != http.StatusOK {
		t.Fatalf("disconnect during the read = %d", disconnected)
	}
	if after := eh.stored(); !reflect.DeepEqual(after, before) || eh.revision() != revision {
		t.Fatalf("a recheck racing a disconnect changed %+v into %+v", before, after)
	}
	if row := eh.connection(eh.user.ID); row.githubUserID != nil || row.generation != 2 {
		t.Fatalf("connection after the race = %+v, want cleared at generation 2", row)
	}
}

func TestEvidenceRecheck_AContinueDuringTheReadNeedsTheNewSession(t *testing.T) {
	eh := newEvidenceHarness(t)
	id := eh.attachedPullRequest()
	first := eh.liveToken()
	reading, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	eh.gh.read("/repos/octocat/hello/pulls/42", func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(reading) })
		<-resume
		w.Header().Set("Content-Type", "application/json")
		eh.gh.write(w, renamedPullRequestJSON())
	})
	before, revision := eh.stored(), eh.revision()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- eh.write(http.MethodPost, "/"+id+"/recheck", "", uuid.NewString(), revision) }()
	select {
	case <-reading:
	case <-time.After(raceWait):
		close(resume)
		t.Fatal("check again did not read the pull request")
	}
	continued := eh.connect(eh.raw, eh.session)
	close(resume)
	requireError(t, <-done, http.StatusConflict, "evidence_session_required")
	requireOutcome(t, continued, outcomeConnected)
	if after := eh.stored(); !reflect.DeepEqual(after, before) || eh.revision() != revision {
		t.Fatalf("a recheck racing a Continue changed %+v into %+v", before, after)
	}
	second := eh.liveToken()
	if second == "" || second == first || eh.currentGeneration() != 2 || eh.binding(eh.user.ID) != 1 {
		t.Fatalf("after the Continue: new session %t, generation %d, binding %d; want a new session at generation 2 and binding 1",
			second != "" && second != first, eh.currentGeneration(), eh.binding(eh.user.ID))
	}
	if items := eh.projectEvidence(); len(items) != 1 || !items[0].Confirmed {
		t.Fatalf("after the Continue the item = %+v, want it still confirmed", items)
	}
	eh.gh.readJSON("/repos/octocat/hello/pulls/42", http.StatusOK, renamedPullRequestJSON())
	item := requireWrite(t, eh.recheck(id)).Evidence[0]
	if !item.Confirmed || item.Title != "Fix the thing, read during the race" || eh.liveToken() != second {
		t.Fatalf("check again under the new session = %+v", item)
	}
}

func (eh *evidenceHarness) attachWhileReading(change func()) {
	eh.t.Helper()
	requireOutcome(eh.t, eh.connect(eh.raw, eh.session), outcomeConnected)
	eh.gh.readJSON("/repos/octocat/hello", http.StatusOK, repositoryJSON)
	eh.gh.read("/repos/octocat/hello/pulls/42", func(w http.ResponseWriter, _ *http.Request) {
		change()
		w.Header().Set("Content-Type", "application/json")
		eh.gh.write(w, pullRequestJSON)
	})
	token, revision := eh.liveToken(), eh.revision()
	requireError(eh.t, eh.attach(pullRequestLink), http.StatusConflict, "evidence_session_required")
	if rows := eh.stored(); len(rows) != 0 || eh.revision() != revision {
		eh.t.Fatalf("a refused confirmation stored %d items at revision %d, want none at %d", len(rows), eh.revision(), revision)
	}
	if eh.liveToken() != token {
		eh.t.Fatal("a refused confirmation changed the evidence session")
	}
}

func TestEvidenceConfirm_AGenerationChangeAloneRefuses(t *testing.T) {
	eh := newEvidenceHarness(t)
	eh.attachWhileReading(func() {
		if _, err := eh.pool.Exec(context.Background(), `UPDATE github_connections SET generation = generation + 1 WHERE user_id = $1`, eh.user.ID); err != nil {
			t.Error(err)
		}
	})
	if row := eh.connection(eh.user.ID); row.githubUserID == nil || *row.githubUserID != 583231 || row.generation != 2 {
		t.Fatalf("connection = %+v, want the same GitHub user at generation 2", row)
	}
}

func TestEvidenceConfirm_AGitHubUserChangeAloneRefuses(t *testing.T) {
	dsn, _ := testutil.NewMigratedTestDatabase(t)
	t.Setenv("TEST_DATABASE_URL", dsn)
	eh := newEvidenceHarness(t)
	if _, err := eh.pool.Exec(t.Context(), `DROP TRIGGER github_connections_generation_grows ON github_connections`); err != nil {
		t.Fatalf("drop the generation trigger in this test's database: %v", err)
	}
	eh.attachWhileReading(func() {
		if _, err := eh.pool.Exec(context.Background(), `UPDATE github_connections SET github_user_id = 777 WHERE user_id = $1`, eh.user.ID); err != nil {
			t.Error(err)
		}
	})
	if row := eh.connection(eh.user.ID); row.githubUserID == nil || *row.githubUserID != 777 || row.generation != 1 {
		t.Fatalf("connection = %+v, want GitHub user 777 at generation 1", row)
	}
}
