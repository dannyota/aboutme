package publicapi

import "testing"

// TestServeLeasedArtifact_ResponseLeavesGoWhileLeased proves over a real
// connection that an artifact's body, its buffered tail, a HEAD, and a 304
// all leave Go while the artifact's lease is held.
func TestServeLeasedArtifact_ResponseLeavesGoWhileLeased(t *testing.T) {
	assertLeasedResponseLeavesGoWhileLeased(t, serveLeasedArtifact)
}
