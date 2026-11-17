package githubevidence

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/store"
)

func (h *harness) newProject(owner uuid.UUID) uuid.UUID {
	h.t.Helper()
	var id uuid.UUID
	slug := "p-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	if err := h.pool.QueryRow(h.t.Context(), `INSERT INTO projects (user_id, slug, document) VALUES ($1, $2, '{}') RETURNING id`, owner, slug).Scan(&id); err != nil {
		h.t.Fatal(err)
	}
	return id
}

func (h *harness) attachEvidence(project uuid.UUID) {
	h.t.Helper()
	if _, err := h.pool.Exec(h.t.Context(), `
		INSERT INTO project_evidence (project_id, position, kind, repository_id, item_id, url, repository_name, title, item_date, checked_at, checked_binding, rule_met)
		VALUES ($1, 0, 'repository', 101, 101, 'https://github.com/octocat/hello', 'octocat/hello', 'octocat/hello', now(), now(), 1, true)
	`, project); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) binding(userID uuid.UUID) int64 {
	h.t.Helper()
	var binding int64
	if err := h.pool.QueryRow(h.t.Context(), `SELECT binding FROM github_connections WHERE user_id = $1`, userID).Scan(&binding); err != nil {
		h.t.Fatal(err)
	}
	return binding
}

func (h *harness) startWith(body string) (*httptest.ResponseRecorder, started) {
	h.t.Helper()
	c := h.as(http.MethodPost, StartPath)
	c.body = body
	if body != "" {
		c.header = http.Header{"Content-Type": {"application/json"}}
	}
	rec := h.do(c)
	if rec.Code != http.StatusOK {
		return rec, started{}
	}
	var decoded struct {
		Data struct {
			AuthorizeURL string `json:"authorizeUrl"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		h.t.Fatal(err)
	}
	link, err := url.Parse(decoded.Data.AuthorizeURL)
	if err != nil {
		h.t.Fatal(err)
	}
	s := started{state: link.Query().Get("state"), challenge: link.Query().Get("code_challenge"), link: link}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == auth.OAuthTxCookieName {
			s.txCookie = cookie
		}
	}
	return rec, s
}

func (h *harness) connectReturning(body string) *httptest.ResponseRecorder {
	h.t.Helper()
	rec, s := h.startWith(body)
	if rec.Code != http.StatusOK {
		h.t.Fatalf("start = %d %s", rec.Code, rec.Body.String())
	}
	code := "code-" + uuid.NewString()
	h.gh.expect(code, s.challenge)
	return h.callback(s, "code="+code+"&state="+url.QueryEscape(s.state), h.raw)
}

func (h *harness) latestTransactionProject() (*uuid.UUID, int) {
	h.t.Helper()
	var project *uuid.UUID
	var count int
	err := h.pool.QueryRow(h.t.Context(), `
		SELECT (SELECT evidence_project_id FROM oauth_transactions WHERE linking_user_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1),
		       (SELECT count(*) FROM oauth_transactions WHERE linking_user_id = $1)
	`, h.user.ID).Scan(&project, &count)
	if err != nil {
		h.t.Fatal(err)
	}
	return project, count
}

func requireOutcomeAt(t *testing.T, rec *httptest.ResponseRecorder, path, outcome string) {
	t.Helper()
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != testOrigin+path+"?evidence="+outcome {
		t.Fatalf("callback = %d Location=%q, want 302 to %s?evidence=%s", rec.Code, rec.Header().Get("Location"), path, outcome)
	}
}

func TestEvidence_StartStoresAnOwnedReturnProject(t *testing.T) {
	h := newHarness(t)
	own := h.newProject(h.user.ID)
	other, _, _ := h.newAccount()
	foreign := h.newProject(other.ID)
	for _, tc := range []struct {
		name string
		body string
		want uuid.UUID
	}{
		{"no body", "", uuid.Nil},
		{"empty object", `{}`, uuid.Nil},
		{"own project", `{"returnTo":"/app/projects/` + own.String() + `"}`, own},
		{"foreign project", `{"returnTo":"/app/projects/` + foreign.String() + `"}`, uuid.Nil},
		{"missing project", `{"returnTo":"/app/projects/` + uuid.NewString() + `"}`, uuid.Nil},
		{"not a uuid", `{"returnTo":"/app/projects/not-a-uuid"}`, uuid.Nil},
		{"upper-case uuid", `{"returnTo":"/app/projects/` + strings.ToUpper(own.String()) + `"}`, uuid.Nil},
		{"braced uuid", `{"returnTo":"/app/projects/{` + own.String() + `}"}`, uuid.Nil},
		{"trailing slash", `{"returnTo":"/app/projects/` + own.String() + `/"}`, uuid.Nil},
		{"with a query", `{"returnTo":"/app/projects/` + own.String() + `?tab=evidence"}`, uuid.Nil},
		{"absolute URL", `{"returnTo":"` + testOrigin + `/app/projects/` + own.String() + `"}`, uuid.Nil},
		{"another app page", `{"returnTo":"/app/settings/sessions"}`, uuid.Nil},
		{"empty string", `{"returnTo":""}`, uuid.Nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h.svc.starts = newWindowLimiter(startLimit, limitWindow)
			rec, _ := h.startWith(tc.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("start = %d %s, want 200", rec.Code, rec.Body.String())
			}
			project, _ := h.latestTransactionProject()
			got := uuid.Nil
			if project != nil {
				got = *project
			}
			if got != tc.want {
				t.Fatalf("stored return project = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEvidence_StartRefusesAMalformedBody(t *testing.T) {
	h := newHarness(t)
	own := h.newProject(h.user.ID).String()
	for _, body := range []string{
		`[]`, `"/app/projects/` + own + `"`, `null`, `1`, ` `, `not json`,
		`{"returnTo":1}`, `{"returnTo":null}`, `{"returnTo":["/app/projects/` + own + `"]}`,
		`{"returnto":"/app/projects/` + own + `"}`, `{"other":"x"}`, `{"returnTo":"/app/projects/` + own + `","other":1}`,
		`{"returnTo":"/app/projects/` + own + `","returnTo":"/app/projects/` + own + `"}`,
		`{"returnTo":"/app/projects/` + own + `"} {}`, `{"returnTo":"` + strings.Repeat("a", 5000) + `"}`,
	} {
		rec, _ := h.startWith(body)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), requestInvalidCode) {
			t.Errorf("start with %.60q = %d %s, want 400 %s", body, rec.Code, rec.Body.String(), requestInvalidCode)
		}
	}
	if _, count := h.latestTransactionProject(); count != 0 {
		t.Fatalf("refused starts created %d transactions, want 0", count)
	}
}

func TestEvidence_CallbackReturnsToTheStartingProject(t *testing.T) {
	h := newHarness(t)
	project := h.newProject(h.user.ID)
	body := `{"returnTo":"/app/projects/` + project.String() + `"}`
	requireOutcomeAt(t, h.connectReturning(body), "/app/projects/"+project.String(), outcomeConnected)

	rec, s := h.startWith(body)
	if rec.Code != http.StatusOK {
		t.Fatalf("start = %d", rec.Code)
	}
	requireOutcomeAt(t, h.callback(s, "code=c&state=wrong", h.raw), "/app/projects/"+project.String(), outcomeFailed)

	_, otherRaw, _ := h.newAccount()
	rec, s = h.startWith(body)
	if rec.Code != http.StatusOK {
		t.Fatalf("start = %d", rec.Code)
	}
	requireOutcome(t, h.callback(s, "code=c&state="+url.QueryEscape(s.state), otherRaw), outcomeFailed)

	rec, s = h.startWith(body)
	if rec.Code != http.StatusOK {
		t.Fatalf("start = %d", rec.Code)
	}
	if _, err := h.pool.Exec(t.Context(), `DELETE FROM projects WHERE id = $1`, project); err != nil {
		t.Fatal(err)
	}
	code := "code-" + uuid.NewString()
	h.gh.expect(code, s.challenge)
	requireOutcome(t, h.callback(s, "code="+code+"&state="+url.QueryEscape(s.state), h.raw), outcomeConnected)
}

func TestEvidence_ContinueWithTheSameAccountKeepsTheBinding(t *testing.T) {
	h := newHarness(t)
	h.attachEvidence(h.newProject(h.user.ID))
	requireOutcome(t, h.connect(h.raw, h.session), outcomeConnected)
	requireOutcome(t, h.connect(h.raw, h.session), outcomeConnected)
	if row := h.connection(h.user.ID); row.generation != 2 || *row.githubUserID != 583231 || h.binding(h.user.ID) != 1 {
		t.Fatalf("after Continue: connection = %+v binding = %d, want generation 2 and binding 1", row, h.binding(h.user.ID))
	}
}

func TestEvidence_DifferentAccountWithoutEvidenceReplacesTheConnection(t *testing.T) {
	h := newHarness(t)
	h.newProject(h.user.ID)
	requireOutcome(t, h.connect(h.raw, h.session), outcomeConnected)
	h.gh.set(func(gh *stubGitHub) { gh.userID, gh.login = 777001, "someone-else" })
	requireOutcome(t, h.connect(h.raw, h.session), outcomeConnected)
	row := h.connection(h.user.ID)
	if *row.githubUserID != 777001 || *row.login != "someone-else" || row.generation != 2 || h.binding(h.user.ID) != 2 {
		t.Fatalf("after a different account: connection = %+v binding = %d, want the new id at generation 2 and binding 2", row, h.binding(h.user.ID))
	}
	tokens, _, _, _ := h.gh.snapshot()
	h.requireRevokedOnce(tokens[0])
}

func TestEvidence_DifferentAccountWithEvidenceIsRefused(t *testing.T) {
	h := newHarness(t)
	project := h.newProject(h.user.ID)
	requireOutcome(t, h.connect(h.raw, h.session), outcomeConnected)
	h.attachEvidence(project)
	h.gh.set(func(gh *stubGitHub) { gh.userID, gh.login = 777001, "someone-else" })
	requireOutcomeAt(t, h.connectReturning(`{"returnTo":"/app/projects/`+project.String()+`"}`), "/app/projects/"+project.String(), outcomeDifferentAccount)
	requireOutcome(t, h.connect(h.raw, h.session), outcomeDifferentAccount)
	row := h.connection(h.user.ID)
	if *row.githubUserID != 583231 || *row.login != "octocat" || row.generation != 1 || h.binding(h.user.ID) != 1 {
		t.Fatalf("after a refused account: connection = %+v binding = %d, want it unchanged", row, h.binding(h.user.ID))
	}
	tokens, _, _, _ := h.gh.snapshot()
	if len(tokens) != 3 {
		t.Fatalf("tokens = %d, want 3", len(tokens))
	}
	h.requireRevokedOnce(tokens[1], tokens[2])
	if h.liveToken() != tokens[0] {
		t.Fatal("a refused account replaced the live session")
	}
	if !strings.Contains(h.logs.String(), `"reason":"different_account"`) {
		t.Fatalf("logs = %s, want the different_account reason", h.logs.String())
	}
}

func TestEvidence_ConnectingAfterADisconnectAdvancesTheBinding(t *testing.T) {
	h := newHarness(t)
	h.attachEvidence(h.newProject(h.user.ID))
	requireOutcome(t, h.connect(h.raw, h.session), outcomeConnected)
	if rec := h.do(h.as(http.MethodDelete, StatusPath+"?keepLinks=true")); rec.Code != http.StatusNoContent {
		t.Fatalf("disconnect = %d %s", rec.Code, rec.Body.String())
	}
	if got := h.binding(h.user.ID); got != 2 {
		t.Fatalf("binding after disconnect = %d, want 2", got)
	}
	h.gh.set(func(gh *stubGitHub) { gh.userID, gh.login = 777001, "someone-else" })
	requireOutcome(t, h.connect(h.raw, h.session), outcomeConnected)
	if row := h.connection(h.user.ID); *row.githubUserID != 777001 || h.binding(h.user.ID) != 3 {
		t.Fatalf("after connecting from a cleared row: connection = %+v binding = %d, want the new id and binding 3", row, h.binding(h.user.ID))
	}
}

func TestEvidence_DisconnectWithoutARowStartsTheBindingAtOne(t *testing.T) {
	h := newHarness(t)
	if _, err := store.New(h.pool).ClearGitHubConnection(t.Context(), h.user.ID); err != nil {
		t.Fatal(err)
	}
	if got := h.binding(h.user.ID); got != 1 {
		t.Fatalf("binding of a new cleared row = %d, want 1", got)
	}
}
