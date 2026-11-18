package githubevidence

import (
	"net/http"
	"reflect"
	"strconv"
	"testing"

	"github.com/google/uuid"
)

func (eh *evidenceHarness) idempotencyRecords() int {
	eh.t.Helper()
	var n int
	if err := eh.pool.QueryRow(eh.t.Context(), `SELECT count(*) FROM idempotency_records WHERE user_id = $1`, eh.user.ID).Scan(&n); err != nil {
		eh.t.Fatal(err)
	}
	return n
}

func TestEvidenceOrder_ANoOpOrderAdvancesNothingAndReplays(t *testing.T) {
	for name, items := range map[string]int{"empty order without evidence": 0, "the current order": 3} {
		t.Run(name, func(t *testing.T) {
			eh := newEvidenceHarness(t)
			eh.seedEvidence(items)
			live := eh.linkingResume(true)
			body := `{"order":[]}`
			if items > 0 {
				body = orderBody(eh.storedIDs()...)
			}
			revision, before, records := eh.revision(), eh.stored(), eh.idempotencyRecords()
			key := uuid.NewString()
			first := eh.write(http.MethodPut, "/order", body, key, revision)
			out := requireWrite(t, first)
			if out.ProjectRevision != strconv.FormatInt(revision, 10) || out.ResumeRevisions == nil || len(out.ResumeRevisions) != 0 ||
				len(out.Evidence) != items || first.Header().Get("ETag") != `"r`+strconv.FormatInt(revision, 10)+`"` {
				t.Fatalf("no-op order = %+v ETag %s, want the current state at r%d", out, first.Header().Get("ETag"), revision)
			}
			for i, id := range eh.storedIDs() {
				if out.Evidence[i].ID != id {
					t.Fatalf("no-op order answered %+v, want the stored order", out.Evidence)
				}
			}
			if eh.revision() != revision || eh.resumeRevision(live.ID) != live.Revision || !reflect.DeepEqual(eh.stored(), before) {
				t.Fatalf("a no-op order changed the project (r%d), the live resume (r%d) or the evidence", eh.revision(), eh.resumeRevision(live.ID))
			}
			if got := eh.idempotencyRecords(); got != records+1 {
				t.Fatalf("idempotency records = %d, want %d", got, records+1)
			}
			again := eh.write(http.MethodPut, "/order", body, key, revision)
			if again.Code != first.Code || again.Body.String() != first.Body.String() || again.Header().Get("ETag") != first.Header().Get("ETag") {
				t.Fatalf("replay = %d %s, want %d %s", again.Code, again.Body.String(), first.Code, first.Body.String())
			}
			if got := eh.idempotencyRecords(); got != records+1 || eh.revision() != revision {
				t.Fatalf("after the replay records = %d at r%d, want %d at r%d", got, eh.revision(), records+1, revision)
			}
		})
	}
}

func TestEvidenceOrder_AChangedOrderStillAdvances(t *testing.T) {
	eh := newEvidenceHarness(t)
	eh.seedEvidence(2)
	live := eh.linkingResume(true)
	ids, revision := eh.storedIDs(), eh.revision()
	out := requireWrite(t, eh.reorder(ids[1], ids[0]))
	if out.ProjectRevision != strconv.FormatInt(revision+1, 10) || len(out.ResumeRevisions) != 1 || eh.resumeRevision(live.ID) != live.Revision+1 {
		t.Fatalf("changed order = %+v, want the project and the live resume advanced", out)
	}
}
