package accountapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func exportedGitHubConnection(t *testing.T, h *exportTestHarness) (map[string]json.RawMessage, bool, string) {
	t.Helper()
	response := h.request(t, http.MethodGet, ExportPath, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", response.Code, response.Body.String())
	}
	var bundle struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &bundle); err != nil {
		t.Fatal(err)
	}
	raw, ok := bundle.Data["githubConnection"]
	if !ok {
		return nil, false, response.Body.String()
	}
	var connection map[string]json.RawMessage
	if err := json.Unmarshal(raw, &connection); err != nil {
		t.Fatalf("githubConnection = %s: %v", raw, err)
	}
	return connection, true, response.Body.String()
}

func TestExport_GitHubConnectionHoldsIdLoginAndTimeOnly(t *testing.T) {
	h := newExportTestHarness(t)
	connectedAt := time.Date(2026, 11, 15, 8, 30, 0, 0, time.UTC)
	if _, err := h.pool.Exec(h.ctx, `INSERT INTO github_connections (user_id, github_user_id, login, connected_at, generation) VALUES ($1, 583231, 'octocat', $2, 3)`, h.user.ID, connectedAt); err != nil {
		t.Fatal(err)
	}
	connection, ok, body := exportedGitHubConnection(t, h)
	if !ok {
		t.Fatalf("export has no githubConnection: %s", body)
	}
	keys := make([]string, 0, len(connection))
	for key := range connection {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"connectedAt", "githubUserId", "login"}) {
		t.Fatalf("githubConnection keys = %v, want exactly connectedAt, githubUserId, login", keys)
	}
	if string(connection["githubUserId"]) != "583231" || string(connection["login"]) != `"octocat"` ||
		string(connection["connectedAt"]) != `"2026-11-15T08:30:00Z"` {
		t.Fatalf("githubConnection = %v", connection)
	}
	for _, forbidden := range []string{"token", "generation", "refresh", "secret"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("export mentions %q: %s", forbidden, body)
		}
	}
}

func TestExport_GitHubConnectionAbsentWithoutAConnection(t *testing.T) {
	h := newExportTestHarness(t)
	if _, ok, body := exportedGitHubConnection(t, h); ok {
		t.Fatalf("export without a connection row has githubConnection: %s", body)
	}
	if _, err := h.pool.Exec(h.ctx, `INSERT INTO github_connections (user_id, generation) VALUES ($1, 2)`, h.user.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, body := exportedGitHubConnection(t, h); ok {
		t.Fatalf("export after a disconnect has githubConnection: %s", body)
	}
}

type recordedEndings struct {
	mu    sync.Mutex
	users []uuid.UUID
}

func (r *recordedEndings) AccountSessionsEnded(userID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.users = append(r.users, userID)
}

func TestDeleteHandlerEndsTheAccountsEvidenceSession(t *testing.T) {
	env := newDeletionEnvironment(t)
	endings := &recordedEndings{}
	env.service.sessions.SetEndListener(endings)
	if _, err := env.service.pool.Exec(env.ctx, `INSERT INTO github_connections (user_id, github_user_id, login, connected_at, generation) VALUES ($1, 9, 'octocat', now(), 1)`, env.user.ID); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	env.service.DeleteHandler().ServeHTTP(recorder, authenticatedDeleteRequest(env))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("DELETE /me = %d %s", recorder.Code, recorder.Body.String())
	}
	if !slices.Equal(endings.users, []uuid.UUID{env.user.ID}) {
		t.Fatalf("ended accounts = %v, want exactly %v", endings.users, env.user.ID)
	}
	var rows int
	if err := env.service.pool.QueryRow(env.ctx, `SELECT count(*) FROM github_connections WHERE user_id = $1`, env.user.ID).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("github_connections rows after deletion = %d, %v; want 0 by cascade", rows, err)
	}
}
