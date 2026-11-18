package projectapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type enabledEvidence struct {
	EvidenceSource
}

func (enabledEvidence) Enabled() bool { return true }

func TestProjects_ReadTakesTheETagAndTheEvidenceFromOneSnapshot(t *testing.T) {
	h := newHarness(t)
	h.svc.evidence = enabledEvidence{}
	id := uuid.MustParse(idOf(t, h.create(t)))
	writes := 0
	h.svc.readHook = func() {
		writes++
		if writes > 1 {
			return
		}
		ctx := context.Background()
		tx, err := h.pool.Begin(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() {
			if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				t.Error(rollbackErr)
			}
		}()
		if _, err = tx.Exec(ctx, `INSERT INTO project_evidence (project_id, position, kind, repository_id, item_id, url,
			repository_name, title, item_date, checked_at, checked_binding, rule_met)
			VALUES ($1, 0, 'repository', 101, 101, 'https://github.com/octocat/hello', 'octocat/hello', 'octocat/hello', now(), now(), 1, true)`, id); err != nil {
			t.Error(err)
			return
		}
		if _, err = tx.Exec(ctx, `UPDATE projects SET revision = revision + 1 WHERE id = $1`, id); err != nil {
			t.Error(err)
			return
		}
		if err = tx.Commit(ctx); err != nil {
			t.Error(err)
		}
	}
	read := h.request(t, "GET", "/api/v1/projects/"+id.String(), "", "", "")
	if read.Code != 200 || writes != 1 {
		t.Fatalf("read = %d %s after %d concurrent writes, want 200 after 1", read.Code, read.Body.String(), writes)
	}
	var body struct {
		Data struct {
			Revision string            `json:"revision"`
			Evidence []json.RawMessage `json:"evidence"`
		} `json:"data"`
	}
	if err := json.Unmarshal(read.Body.Bytes(), &body); err != nil || body.Data.Evidence == nil {
		t.Fatalf("read body %s: %v", read.Body.String(), err)
	}
	etag := read.Header().Get("ETag")
	consistent := (etag == `"r1"` && body.Data.Revision == "1" && len(body.Data.Evidence) == 0) ||
		(etag == `"r2"` && body.Data.Revision == "2" && len(body.Data.Evidence) == 1)
	if !consistent {
		t.Fatalf("read ETag %s with revision %s and %d evidence items, want one committed state", etag, body.Data.Revision, len(body.Data.Evidence))
	}
	if again := h.request(t, "GET", "/api/v1/projects/"+id.String(), "", "", ""); again.Header().Get("ETag") != `"r2"` ||
		!json.Valid(again.Body.Bytes()) {
		t.Fatalf("read after the write = %s, want r2", again.Header().Get("ETag"))
	}
}
