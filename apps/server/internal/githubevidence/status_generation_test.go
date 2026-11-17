package githubevidence

import (
	"encoding/json"
	"net/http"
	"testing"
)

func (h *harness) statusGeneration() int64 {
	h.t.Helper()
	rec := h.do(h.as(http.MethodGet, StatusPath))
	if rec.Code != http.StatusOK {
		h.t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		h.t.Fatal(err)
	}
	raw, present := body.Data["generation"]
	if !present {
		h.t.Fatalf("status %s carries no generation", rec.Body.String())
	}
	var generation int64
	if err := json.Unmarshal(raw, &generation); err != nil {
		h.t.Fatalf("status generation %s is not an integer: %v", raw, err)
	}
	return generation
}

func TestEvidenceStatus_ReportsTheConnectionGeneration(t *testing.T) {
	h := newHarness(t)
	if got := h.statusGeneration(); got != 0 {
		t.Fatalf("generation without a connection row = %d, want 0", got)
	}
	requireOutcome(t, h.connect(h.raw, h.session), outcomeConnected)
	if got := h.statusGeneration(); got != 1 {
		t.Fatalf("generation after connecting = %d, want 1", got)
	}
	if _, err := h.q.ClearGitHubConnection(t.Context(), h.user.ID); err != nil {
		t.Fatal(err)
	}
	if got := h.statusGeneration(); got != 2 || h.connection(h.user.ID).generation != 2 {
		t.Fatalf("generation of a cleared row = %d, want 2", got)
	}
}
