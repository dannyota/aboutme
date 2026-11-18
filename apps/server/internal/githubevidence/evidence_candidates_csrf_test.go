package githubevidence

import (
	"net/http"
	"testing"
)

func (eh *evidenceHarness) spentRequests() int {
	eh.t.Helper()
	limiter := eh.svc.client.requests
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	return len(limiter.events[eh.user.ID])
}

func (eh *evidenceHarness) rememberedCandidate(id string) bool {
	eh.t.Helper()
	live, ok := eh.svc.registry.current(eh.user.ID)
	if !ok {
		eh.t.Fatal("no live evidence session")
	}
	_, found := live.memory.candidate(id)
	return found
}

func (eh *evidenceHarness) candidatesWith(csrf string, header http.Header) int {
	eh.t.Helper()
	c := eh.as(http.MethodGet, eh.path("/candidates?kind=repository&page=1"))
	c.csrf, c.header = csrf, header
	return eh.do(c).Code
}

func TestEvidenceCandidates_ACrossSiteListingIsRefusedBeforeGitHub(t *testing.T) {
	eh := newEvidenceHarness(t)
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	eh.gh.readJSON("/user/repos", http.StatusOK, `[`+repositoryJSON+`]`)
	listed := eh.listed("kind=repository&page=1").Items[0].CandidateID
	reads, spent := len(eh.gh.readCalls()), eh.spentRequests()
	for name, header := range map[string]http.Header{
		"same-origin":           {"Sec-Fetch-Site": {"same-origin"}},
		"cross-site":            {"Sec-Fetch-Site": {"cross-site"}},
		"same-site":             {"Sec-Fetch-Site": {"same-site"}},
		"none":                  {"Sec-Fetch-Site": {"none"}},
		"absent":                nil,
		"same-origin twice":     {"Sec-Fetch-Site": {"same-origin", "same-origin"}},
		"same-origin in a list": {"Sec-Fetch-Site": {"same-origin, cross-site"}},
	} {
		t.Run(name, func(t *testing.T) {
			c := eh.as(http.MethodGet, eh.path("/candidates?kind=repository&page=1"))
			c.csrf, c.header = "", header
			requireError(t, eh.do(c), http.StatusForbidden, "csrf_rejected")
			c.csrf = "bm90LXRoZS10b2tlbg"
			requireError(t, eh.do(c), http.StatusForbidden, "csrf_rejected")
		})
	}
	if got := len(eh.gh.readCalls()); got != reads {
		t.Fatalf("refused listings read GitHub %d times", got-reads)
	}
	if got := eh.spentRequests(); got != spent {
		t.Fatalf("refused listings spent %d GitHub requests", got-spent)
	}
	if !eh.rememberedCandidate(listed) {
		t.Fatal("a refused listing replaced the open picker's candidates")
	}
}

func TestEvidenceCandidates_AValidTokenLists(t *testing.T) {
	eh := newEvidenceHarness(t)
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	eh.gh.readJSON("/user/repos", http.StatusOK, `[`+repositoryJSON+`]`)
	for name, tc := range map[string]struct {
		csrf   string
		header http.Header
	}{
		"token without fetch metadata":    {csrfFor(eh.session), nil},
		"token with same-origin metadata": {csrfFor(eh.session), http.Header{"Sec-Fetch-Site": {"same-origin"}}},
		"token with cross-site metadata":  {csrfFor(eh.session), http.Header{"Sec-Fetch-Site": {"cross-site"}}},
	} {
		t.Run(name, func(t *testing.T) {
			spent := eh.spentRequests()
			if status := eh.candidatesWith(tc.csrf, tc.header); status != http.StatusOK {
				t.Fatalf("listing = %d, want 200", status)
			}
			if eh.spentRequests() != spent+1 {
				t.Fatal("an admitted listing did not read GitHub once")
			}
		})
	}
}
