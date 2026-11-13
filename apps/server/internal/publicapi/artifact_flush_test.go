package publicapi

import "testing"

func TestServeLeasedArtifact_ResponseLeavesGoWhileLeased(t *testing.T) {
	assertLeasedResponseLeavesGoWhileLeased(t, serveLeasedArtifact)
}
