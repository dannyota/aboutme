package githubevidence

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/project"
)

func (eh *evidenceHarness) measure(d project.Document) int {
	eh.t.Helper()
	prepared, err := project.Prepare(d)
	if err != nil {
		eh.t.Fatal(err)
	}
	raw, err := json.Marshal(prepared)
	if err != nil {
		eh.t.Fatal(err)
	}
	var n int
	if err = eh.pool.QueryRow(eh.t.Context(), "SELECT octet_length($1::jsonb::text)", raw).Scan(&n); err != nil {
		eh.t.Fatal(err)
	}
	return n
}

func (eh *evidenceHarness) sizedDocument(target int) project.Document {
	eh.t.Helper()
	d := project.Document{DefaultLanguage: "en", Text: map[string]project.Text{"en": {Name: "Atlas"}}}
	padding := []string{"vi", "ja", "fr"}
	for _, key := range padding {
		d.Text[key] = project.Text{Name: key, Description: "a"}
	}
	remaining := target - eh.measure(d)
	for _, key := range padding {
		share := min(remaining, 32766)
		text := d.Text[key]
		text.Description = strings.Repeat(`\`, share/2) + strings.Repeat("a", share%2+1)
		d.Text[key] = text
		remaining -= share
	}
	if got := eh.measure(d); remaining != 0 || got != target {
		eh.t.Fatalf("sized document = %d bytes, want %d", got, target)
	}
	return d
}

func (eh *evidenceHarness) putDocument(d project.Document) *httptest.ResponseRecorder {
	eh.t.Helper()
	body, err := json.Marshal(struct {
		Slug string `json:"slug"`
		project.Document
	}{Slug: eh.project.Slug, Document: d})
	if err != nil {
		eh.t.Fatal(err)
	}
	c := eh.as(http.MethodPut, evidenceRoot+eh.project.ID.String())
	c.body = string(body)
	c.header = http.Header{"Idempotency-Key": {uuid.NewString()}, "If-Match": {`"r` + strconv.FormatInt(eh.revision(), 10) + `"`}, "Content-Type": {"application/json"}}
	return eh.do(c)
}

func TestEvidenceSize_EachItemCountsAKibibyteOnAttachAndProjectWrites(t *testing.T) {
	eh := newEvidenceHarness(t)
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	full := eh.sizedDocument(project.MaxStoredBytes - project.EvidenceItemBytes)
	if rec := eh.putDocument(full); rec.Code != http.StatusOK {
		t.Fatalf("PUT a document with room for one item = %d %s", rec.Code, rec.Body.String())
	}
	eh.servePullRequest()
	requireWrite(t, eh.attach(pullRequestLink))
	eh.gh.readJSON("/repos/octocat/hello/releases/tags/v1.0.0", http.StatusOK, releaseJSON)
	before := eh.revision()
	requireError(t, eh.attach(`{"kind":"release","link":"https://github.com/octocat/hello/releases/tag/v1.0.0"}`), http.StatusUnprocessableEntity, "document_invalid")
	if len(eh.stored()) != 1 || eh.revision() != before {
		t.Fatal("an attach over the size bound changed the project")
	}
	if rec := eh.putDocument(full); rec.Code != http.StatusOK {
		t.Fatalf("PUT the same document with one item = %d %s", rec.Code, rec.Body.String())
	}
	eh.seedEvidence(1)
	requireError(t, eh.putDocument(full), http.StatusUnprocessableEntity, "document_invalid")
}
