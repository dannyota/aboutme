package viewcount

import (
	"bytes"
	"encoding/json"
	"testing"
)

// browserVector is one challenge this package issues, signed with the
// secrets newProver derives from 64 bytes of 0x5a and bound to the view ID
// 0x01..0x10, with the solution the public page's solver returns for it:
// altcha-lib's solveChallenge with its Web Crypto PBKDF2 deriveKey, as
// apps/web/app/public/viewBeacon.ts calls them. The web test
// apps/web/test/public/viewBeacon.test.ts solves the same challenge and
// expects the same solution, so a change on either side of the wire fails
// one of the two tests.
const (
	browserVectorChallenge = `{"parameters":{"algorithm":"PBKDF2/SHA-256",` +
		`"nonce":"51fbdcb225ff905fd95f74004f1d8bf3",` +
		`"salt":"e9bab6504c9be5f5505966b5d465eda0","cost":1000,"keyLength":32,` +
		`"keyPrefix":"10af46ffb1c0c5a2b152bc0a46f02db9",` +
		`"keySignature":"0ba76e0d148fc3792b7db085a69a85d024600285a8ec994cd17271a654e9d502",` +
		`"data":{"view":"AQIDBAUGBwgJCgsMDQ4PEA"}},` +
		`"signature":"7d447670d8367e6e84361c00482b3cfe96dc87d68c31ec81dea1431ae69244eb"}`
	browserVectorCounter    = 437
	browserVectorDerivedKey = "10af46ffb1c0c5a2b152bc0a46f02db92ddcfff3d20793675c70f45b8861ce73"
)

func TestVerifyAcceptsTheBrowserSolverSolution(t *testing.T) {
	p, err := newProver(bytes.NewReader(bytes.Repeat([]byte{0x5a}, 64)))
	if err != nil {
		t.Fatalf("newProver: %v", err)
	}
	var challenge Challenge
	if err := json.Unmarshal([]byte(browserVectorChallenge), &challenge); err != nil {
		t.Fatalf("decode challenge: %v", err)
	}
	var id viewID
	for i := range id {
		id[i] = byte(i + 1)
	}
	solution := Solution{Counter: browserVectorCounter, DerivedKey: browserVectorDerivedKey}
	if !p.verify(challenge, solution, id) {
		t.Fatal("verify rejected the browser solver's solution")
	}

	tests := []struct {
		name     string
		solution Solution
		id       viewID
	}{
		{"other key", Solution{Counter: browserVectorCounter, DerivedKey: browserVectorDerivedKey[:62] + "00"}, id},
		{"other view", solution, viewID{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if p.verify(challenge, tt.solution, tt.id) {
				t.Fatal("verify accepted a solution that does not solve this challenge")
			}
		})
	}
}
