package api_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/api"
)

func TestWriteData_Envelope(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	api.WriteData(rec, 200, map[string]string{"status": "ok"})

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json; charset=utf-8", ct)
	}

	var body struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v (body=%s)", err, rec.Body.String())
	}
	if body.Data.Status != "ok" {
		t.Errorf("data.status = %q, want %q", body.Data.Status, "ok")
	}
}

func TestWriteError_Envelope(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	api.WriteError(rec, 503, "not_ready", "database is unreachable")

	if rec.Code != 503 {
		t.Errorf("status = %d, want 503", rec.Code)
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v (body=%s)", err, rec.Body.String())
	}
	if body.Error.Code != "not_ready" {
		t.Errorf("error.code = %q, want %q", body.Error.Code, "not_ready")
	}
	if body.Error.Message != "database is unreachable" {
		t.Errorf("error.message = %q, want %q", body.Error.Message, "database is unreachable")
	}
}

// TestWriteData_RejectsNoOtherTopLevelKeys guards the envelope shape: a
// success body must contain exactly the "data" key, never "error", and vice
// versa, since every API consumer branches on which key is present.
func TestWriteData_RejectsNoOtherTopLevelKeys(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	api.WriteData(rec, 200, map[string]int{"n": 1})

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if len(raw) != 1 {
		t.Fatalf("top-level keys = %v, want exactly one key", raw)
	}
	if _, ok := raw["data"]; !ok {
		t.Errorf("top-level keys = %v, want %q", raw, "data")
	}
}
