package githubevidence

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const releaseLink = `{"kind":"release","link":"https://github.com/octocat/hello/releases/tag/v1.0.0"}`

func releaseTagged(tag string) string {
	return strings.Replace(releaseJSON, `"tag_name":"v1.0.0"`, `"tag_name":"`+tag+`"`, 1)
}

func TestEvidenceAttach_AnInvalidReleaseTagFromGitHubIsUnsupported(t *testing.T) {
	for name, tag := range map[string]string{"format character": `v1.0.0\u202e`, "space": "v1 0", "double dot": "v1..0"} {
		t.Run(name, func(t *testing.T) {
			eh := newEvidenceHarness(t)
			requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
			eh.gh.readJSON("/repos/octocat/hello", http.StatusOK, repositoryJSON)
			eh.gh.readJSON("/repos/octocat/hello/releases/tags/v1.0.0", http.StatusOK, releaseTagged(tag))
			before := eh.revision()
			requireError(t, eh.attach(releaseLink), http.StatusUnprocessableEntity, "evidence_unsupported_link")
			if len(eh.stored()) != 0 || eh.revision() != before {
				t.Fatalf("an invalid tag stored %d items at revision %d, want none at %d", len(eh.stored()), eh.revision(), before)
			}
		})
	}
}

func TestEvidenceRecheck_AnInvalidReleaseTagFromGitHubChangesNothing(t *testing.T) {
	for name, tag := range map[string]string{"format character": `v1.0.0\u2066`, "space": "v1 0", "lock suffix": "v1.lock"} {
		t.Run(name, func(t *testing.T) {
			eh := newEvidenceHarness(t)
			requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
			eh.gh.readJSON("/repos/octocat/hello", http.StatusOK, repositoryJSON)
			eh.gh.readJSON("/repos/octocat/hello/releases/tags/v1.0.0", http.StatusOK, releaseJSON)
			id := requireWrite(t, eh.attach(releaseLink)).Evidence[0].ID
			eh.gh.readJSON("/repos/octocat/hello/releases/7001", http.StatusOK, releaseTagged(tag))
			before, revision := eh.stored(), eh.revision()
			requireError(t, eh.recheck(id), http.StatusBadGateway, "evidence_upstream")
			if after := eh.stored(); !reflect.DeepEqual(after, before) || eh.revision() != revision {
				t.Fatalf("an invalid tag on check again changed %+v into %+v", before, after)
			}
		})
	}
}
