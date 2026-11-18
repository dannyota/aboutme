package githubevidence

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
	"danny.vn/aboutme/apps/server/internal/store"
)

type steeredLinks struct {
	*resumeapi.Service
	authorize func(attempt int)
	attempts  int
}

func (l *steeredLinks) EditProjectEvidence(ctx context.Context, edit resumeapi.ProjectEvidenceEdit) (resume.ExecuteResult, error) {
	authorize := edit.Authorize
	edit.Authorize = func(ctx context.Context, q *store.Queries) error {
		if l.authorize != nil {
			l.attempts++
			l.authorize(l.attempts)
		}
		return authorize(ctx, q)
	}
	return l.Service.EditProjectEvidence(ctx, edit)
}

func TestEvidenceWrites_ThreeChangedPlansAreBusyAndChangeNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		prepare func(eh *evidenceHarness) []string
		write   func(eh *evidenceHarness, ids []string) *httptest.ResponseRecorder
	}{
		"attach": {
			prepare: func(eh *evidenceHarness) []string {
				eh.servePullRequest()
				return nil
			},
			write: func(eh *evidenceHarness, _ []string) *httptest.ResponseRecorder { return eh.attach(pullRequestLink) },
		},
		"recheck": {
			prepare: func(eh *evidenceHarness) []string { return []string{eh.attachedPullRequest()} },
			write:   func(eh *evidenceHarness, ids []string) *httptest.ResponseRecorder { return eh.recheck(ids[0]) },
		},
		"order": {
			prepare: func(eh *evidenceHarness) []string {
				eh.seedEvidence(2)
				return eh.storedIDs()
			},
			write: func(eh *evidenceHarness, ids []string) *httptest.ResponseRecorder { return eh.reorder(ids[1], ids[0]) },
		},
		"remove": {
			prepare: func(eh *evidenceHarness) []string {
				eh.seedEvidence(1)
				return eh.storedIDs()
			},
			write: func(eh *evidenceHarness, ids []string) *httptest.ResponseRecorder {
				return eh.write(http.MethodDelete, "/"+ids[0], "", uuid.NewString(), eh.revision())
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			steered := &steeredLinks{Service: h.library}
			eh := wireEvidenceWith(t, h, h.svc, steered)
			eh.project = eh.createProject("atlas")
			if name == "attach" {
				requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
			}
			ids := tc.prepare(eh)
			live := eh.linkingResume(true)
			late := []resume.Resume{eh.linkingResume(false), eh.linkingResume(false)}
			steered.authorize = func(attempt int) {
				var err error
				if attempt <= len(late) {
					_, err = eh.pool.Exec(context.Background(), `UPDATE resumes SET slug = $2, live = true, revision = revision + 1 WHERE id = $1`,
						late[attempt-1].ID, "late-"+uuid.NewString()[:8])
				} else {
					_, err = eh.pool.Exec(context.Background(), `UPDATE resumes SET live = false, revision = revision + 1 WHERE id = $1`, late[0].ID)
				}
				if err != nil {
					t.Errorf("change the linking set during the write: %v", err)
				}
			}
			before, revision, reads := eh.stored(), eh.revision(), len(eh.gh.readCalls())
			rec := tc.write(eh, ids)
			requireError(t, rec, http.StatusServiceUnavailable, "public_state_busy")
			if rec.Header().Get("Retry-After") != "1" || steered.attempts != 3 {
				t.Fatalf("busy Retry-After = %q after %d attempts, want 1 after 3", rec.Header().Get("Retry-After"), steered.attempts)
			}
			if after := eh.stored(); !reflect.DeepEqual(after, before) || eh.revision() != revision {
				t.Fatalf("a busy write changed %+v at r%d into %+v at r%d", before, revision, after, eh.revision())
			}
			if got := eh.resumeRevision(live.ID); got != live.Revision {
				t.Fatalf("live resume revision = %d, want %d unchanged", got, live.Revision)
			}
			if eh.resumeRevision(late[0].ID) != late[0].Revision+2 || eh.resumeRevision(late[1].ID) != late[1].Revision+1 {
				t.Fatal("a busy write advanced a late resume beyond the changes that made it busy")
			}
			if name == "attach" || name == "recheck" {
				if got := len(eh.gh.readCalls()) - reads; got != 2 {
					t.Fatalf("a busy %s made %d GitHub reads, want the repository and the item once before planning", name, got)
				}
			}
		})
	}
}
