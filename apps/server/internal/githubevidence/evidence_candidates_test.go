package githubevidence

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const (
	forkJSON     = `{"id":102,"full_name":"octocat/fork","owner":{"id":583231},"private":false,"fork":true,"created_at":"2020-01-02T03:04:05Z"}`
	privateJSON  = `{"id":103,"full_name":"octocat/secret","owner":{"id":583231},"private":true,"fork":false,"created_at":"2020-01-02T03:04:05Z"}`
	searchJSON   = `{"incomplete_results":true,"items":[{"repository_url":"https://api.github.com/repos/octocat/hello","number":42,"title":"Fix the thing","closed_at":"2026-01-02T03:04:05Z"}]}`
	releasesJSON = `[` + releaseJSON + `,{"id":7002,"tag_name":"v2","name":null,"draft":true,"prerelease":false,"published_at":null,"author":{"id":583231,"type":"User"}},` +
		`{"id":7003,"tag_name":"v3","name":"Theirs","draft":false,"prerelease":false,"published_at":"2026-02-03T04:05:06Z","author":{"id":9,"type":"User"}}]`
)

type candidateRepositoryBody struct {
	ID       *int64 `json:"id"`
	FullName string `json:"fullName"`
}

type candidateBody struct {
	CandidateID string                  `json:"candidateId"`
	Kind        string                  `json:"kind"`
	Title       string                  `json:"title"`
	Repository  candidateRepositoryBody `json:"repository"`
	Date        time.Time               `json:"date"`
	Attached    bool                    `json:"attached"`
}

type candidatesBody struct {
	Items      []candidateBody `json:"items"`
	NextPage   *int            `json:"nextPage"`
	Incomplete bool            `json:"incomplete"`
}

func (eh *evidenceHarness) candidates(query string) *httptest.ResponseRecorder {
	eh.t.Helper()
	return eh.do(eh.as(http.MethodGet, eh.path("/candidates?"+query)))
}

func (eh *evidenceHarness) listed(query string) candidatesBody {
	eh.t.Helper()
	rec := eh.candidates(query)
	if rec.Code != http.StatusOK {
		eh.t.Fatalf("candidates %s = %d %s", query, rec.Code, rec.Body.String())
	}
	return decodeData[candidatesBody](eh.t, rec)
}

func TestEvidenceCandidates_RepositoriesDropForksAndPrivateAndMarkAttached(t *testing.T) {
	eh := newEvidenceHarness(t)
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	eh.gh.readJSON("/user/repos", http.StatusOK, `[`+repositoryJSON+`,`+forkJSON+`,`+privateJSON+`]`)
	eh.gh.readJSON("/repos/octocat/hello", http.StatusOK, repositoryJSON)
	page := eh.listed("kind=repository&page=1")
	if len(page.Items) != 1 || page.NextPage != nil || page.Incomplete {
		t.Fatalf("repositories = %+v, want only the public source repository", page)
	}
	item := page.Items[0]
	if item.CandidateID == "" || item.Kind != "repository" || item.Title != "octocat/hello" || item.Repository.ID == nil ||
		*item.Repository.ID != 101 || item.Repository.FullName != "octocat/hello" || item.Attached ||
		!item.Date.Equal(time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("candidate = %+v", item)
	}
	out := requireWrite(t, eh.attach(`{"kind":"repository","candidateId":"`+item.CandidateID+`"}`))
	if len(out.Evidence) != 1 || out.Evidence[0].URL != "https://github.com/octocat/hello" || out.Evidence[0].Title != "octocat/hello" {
		t.Fatalf("attached = %+v", out.Evidence)
	}
	if again := eh.listed("kind=repository&page=1"); len(again.Items) != 1 || !again.Items[0].Attached || again.Items[0].CandidateID == item.CandidateID {
		t.Fatalf("listing after attach = %+v, want the repository marked attached under a new candidate id", again)
	}
}

func TestEvidenceCandidates_PullRequestsCarryNoRepositoryID(t *testing.T) {
	eh := newEvidenceHarness(t)
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	eh.gh.readJSON("/search/issues", http.StatusOK, searchJSON)
	eh.servePullRequest()
	rec := eh.candidates("kind=pullRequest&page=2")
	var raw struct {
		Data struct {
			Items []struct {
				Repository map[string]json.RawMessage `json:"repository"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil || rec.Code != http.StatusOK || len(raw.Data.Items) != 1 ||
		string(raw.Data.Items[0].Repository["id"]) != "null" {
		t.Fatalf("pull request candidates = %d %s, want repository.id null", rec.Code, rec.Body.String())
	}
	page := decodeData[candidatesBody](t, rec)
	item := page.Items[0]
	if !page.Incomplete || item.Title != "Fix the thing" || item.Repository.FullName != "octocat/hello" || item.Attached {
		t.Fatalf("pull request page = %+v", page)
	}
	requireWrite(t, eh.attach(`{"kind":"pullRequest","candidateId":"`+item.CandidateID+`"}`))
	if rows := eh.stored(); len(rows) != 1 || rows[0].ItemID != 9001 || !rows[0].ItemDate.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("stored = %+v, want the pull request id and its merge date", rows)
	}
	if again := eh.listed("kind=pullRequest&page=2"); !again.Items[0].Attached {
		t.Fatalf("listing after attach = %+v, want it attached by repository and number", again)
	}
	want := "/search/issues?order=desc&page=2&per_page=30&q=is%3Apr+is%3Amerged+is%3Apublic+author%3Aoctocat&sort=updated"
	if calls := eh.gh.readCalls(); calls[0] != want {
		t.Fatalf("search = %s, want %s", calls[0], want)
	}
}

func TestEvidenceCandidates_ReleasesNeedAListedRepository(t *testing.T) {
	eh := newEvidenceHarness(t)
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	eh.gh.readJSON("/user/repos", http.StatusOK, `[`+repositoryJSON+`,`+forkJSON+`]`)
	eh.gh.readJSON("/repos/octocat/hello/releases", http.StatusOK, releasesJSON)
	requireError(t, eh.candidates("kind=release&page=1&repository=101"), http.StatusConflict, "evidence_session_required")
	eh.listed("kind=repository&page=1")
	requireError(t, eh.candidates("kind=release&page=1&repository=102"), http.StatusConflict, "evidence_session_required")
	page := eh.listed("kind=release&page=1&repository=101")
	if len(page.Items) != 1 || page.Items[0].Title != "First" || page.Items[0].Repository.ID == nil || *page.Items[0].Repository.ID != 101 {
		t.Fatalf("releases = %+v, want only the published release by the owner", page)
	}
	eh.gh.readJSON("/repos/octocat/hello", http.StatusOK, repositoryJSON)
	eh.gh.readJSON("/repos/octocat/hello/releases/7001", http.StatusOK, releaseJSON)
	out := requireWrite(t, eh.attach(`{"kind":"release","candidateId":"`+page.Items[0].CandidateID+`"}`))
	if out.Evidence[0].URL != "https://github.com/octocat/hello/releases/tag/v1.0.0" || out.Evidence[0].Title != "First" {
		t.Fatalf("attached release = %+v", out.Evidence[0])
	}
}

func TestEvidenceCandidates_AStaleOrForeignCandidateNeedsASession(t *testing.T) {
	eh := newEvidenceHarness(t)
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	eh.gh.readJSON("/user/repos", http.StatusOK, `[`+repositoryJSON+`]`)
	eh.gh.readJSON("/repos/octocat/hello", http.StatusOK, repositoryJSON)
	stale := eh.listed("kind=repository&page=1").Items[0].CandidateID
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	requireError(t, eh.attach(`{"kind":"repository","candidateId":"`+stale+`"}`), http.StatusConflict, "evidence_session_required")

	_, otherRaw, otherSession := eh.newAccount()
	call := eh.as(http.MethodGet, evidenceRoot+eh.project.ID.String()+"/evidence/candidates?kind=repository&page=1")
	call.cookies, call.csrf = []*http.Cookie{sessionCookie(otherRaw)}, csrfFor(otherSession)
	requireError(t, eh.do(call), http.StatusNotFound, "project_not_found")
	fresh := eh.listed("kind=repository&page=1").Items[0].CandidateID
	if len(eh.stored()) != 0 || fresh == stale {
		t.Fatal("a stale candidate attached or was reissued")
	}
}

func TestEvidenceCandidates_QueryValidation(t *testing.T) {
	eh := newEvidenceHarness(t)
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	for _, query := range []string{
		"", "kind=repository", "kind=gist&page=1", "kind=repository&page=0", "kind=repository&page=11", "kind=repository&page=01",
		"kind=repository&page=1&repository=101", "kind=release&page=1", "kind=release&page=1&repository=0",
		"kind=repository&page=1&page=2", "kind=repository&page=1&extra=1",
	} {
		requireError(t, eh.candidates(query), http.StatusBadRequest, "request_invalid")
	}
	if calls := eh.gh.readCalls(); len(calls) != 0 {
		t.Fatalf("invalid queries read GitHub: %v", calls)
	}
}

func TestEvidenceCandidates_WithoutASessionNeedsOne(t *testing.T) {
	eh := newEvidenceHarness(t)
	requireError(t, eh.candidates("kind=repository&page=1"), http.StatusConflict, "evidence_session_required")
}
