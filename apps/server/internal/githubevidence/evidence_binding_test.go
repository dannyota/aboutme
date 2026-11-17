package githubevidence

import (
	"net/http"
	"testing"
)

func TestEvidenceBinding_ContinueKeepsTheMarkAndANewBindingDropsIt(t *testing.T) {
	eh := newEvidenceHarness(t)
	id := eh.attachedPullRequest()
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	if items := eh.projectEvidence(); len(items) != 1 || !items[0].Confirmed {
		t.Fatalf("after Continue with the same account = %+v, want confirmed", items)
	}
	if rec := eh.do(eh.as(http.MethodDelete, StatusPath+"?keepLinks=true")); rec.Code != http.StatusNoContent {
		t.Fatalf("disconnect = %d %s", rec.Code, rec.Body.String())
	}
	if items := eh.projectEvidence(); len(items) != 1 || items[0].Confirmed {
		t.Fatalf("after disconnect = %+v, want a plain link", items)
	}
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	if items := eh.projectEvidence(); items[0].Confirmed || items[0].FlagReason != nil {
		t.Fatalf("under a new binding = %+v, want unconfirmed and unflagged", items)
	}
	rechecked := requireWrite(t, eh.recheck(id)).Evidence[0]
	if !rechecked.Confirmed {
		t.Fatalf("recheck under the new binding = %+v, want confirmed", rechecked)
	}
	var binding int64
	if err := eh.pool.QueryRow(t.Context(), `SELECT binding FROM github_connections WHERE user_id = $1`, eh.user.ID).Scan(&binding); err != nil {
		t.Fatal(err)
	}
	if rows := eh.stored(); binding <= 1 || rows[0].CheckedBinding != binding {
		t.Fatalf("checked binding = %d, want the current binding %d", rows[0].CheckedBinding, binding)
	}
}
