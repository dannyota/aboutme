package accountapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
)

func TestExportResumeCheckpoints(t *testing.T) {
	h := newExportTestHarness(t)
	row := h.insertResume(t, "Current", true, exportTestPNG(t))
	cutoff := h.service.now().Add(-resume.CheckpointRetention)
	for _, fixture := range []struct {
		title   string
		started time.Time
		version int32
	}{{"First", cutoff.Add(time.Second), docmigrate.CurrentVersion},
		{"Second", cutoff.Add(2 * time.Second), docmigrate.CurrentVersion},
		{"Unknown", cutoff.Add(3 * time.Second), 9999},
		{"Expired", cutoff.Add(-time.Microsecond), docmigrate.CurrentVersion}} {
		if _, err := h.pool.Exec(h.ctx, `INSERT INTO resume_checkpoints
			(resume_id,started_at,cause,title,schema_version,personal_details,content,customization,byte_size)
			SELECT id,$2,'edit',$3,$4,personal_details - 'photo',content,customization,100
			FROM resumes WHERE id=$1`, row.ID, fixture.started, fixture.title, fixture.version); err != nil {
			t.Fatal(err)
		}
	}
	empty := h.insertResume(t, "No history", false, nil)
	response := h.request(t, http.MethodGet, ExportPath, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d: %s", response.Code, response.Body.String())
	}
	var got struct {
		Data struct {
			ExportVersion int `json:"exportVersion"`
			Resumes       []struct {
				ID          uuid.UUID `json:"id"`
				Checkpoints []struct {
					Title    string `json:"title"`
					Document struct {
						PersonalDetails map[string]json.RawMessage `json:"personalDetails"`
					} `json:"document"`
				} `json:"checkpoints"`
			} `json:"resumes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Data.ExportVersion != 3 || len(got.Data.Resumes) != 2 {
		t.Fatalf("version=%d, resumes=%d", got.Data.ExportVersion, len(got.Data.Resumes))
	}
	if got.Data.Resumes[0].ID != row.ID || got.Data.Resumes[1].ID != empty.ID {
		t.Fatal("unexpected resume order")
	}
	history := got.Data.Resumes[0].Checkpoints
	if len(history) != 2 || history[0].Title != "Second" || history[1].Title != "First" {
		t.Fatalf("checkpoint order or visibility: %+v", history)
	}
	for _, item := range history {
		if _, ok := item.Document.PersonalDetails["photo"]; ok {
			t.Fatal("checkpoint contained the current photo")
		}
	}
	if got.Data.Resumes[1].Checkpoints == nil || len(got.Data.Resumes[1].Checkpoints) != 0 {
		t.Fatal("empty history was not an empty array")
	}
}
