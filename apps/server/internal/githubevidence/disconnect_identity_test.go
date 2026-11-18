package githubevidence

import (
	"net/http"
	"testing"

	"danny.vn/aboutme/apps/server/internal/resumeapi"
)

func TestEvidenceDisconnect_IdentityUsesTheSharedIdempotencyTuples(t *testing.T) {
	for _, tc := range []struct {
		keepLinks  bool
		generation int64
		payload    string
	}{
		{true, 0, "keepLinks=true&generation=0"},
		{false, 41, "keepLinks=false&generation=41"},
	} {
		operation, fingerprint := disconnectIdentity(tc.keepLinks, tc.generation)
		wantOperation, wantFingerprint := resumeapi.AccountWriteIdentity(http.MethodDelete, "deleteGitHubEvidence", []byte(tc.payload))
		if operation != wantOperation || fingerprint != wantFingerprint {
			t.Errorf("disconnectIdentity(%t, %d) = (%s, %x), want (%s, %x)", tc.keepLinks, tc.generation, operation, fingerprint, wantOperation, wantFingerprint)
		}
	}
}
