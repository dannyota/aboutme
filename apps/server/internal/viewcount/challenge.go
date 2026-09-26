package viewcount

import (
	"encoding/hex"
	"fmt"
	"io"
	"math/big"

	altcha "github.com/altcha-org/altcha-lib-go/v2"
)

// Proof-of-work parameters (docs/design/viewer-analytics/counting.md,
// "Layers 5 and 6"; docs/design/budgets.md, "Viewer analytics"). The server
// picks the counter, so the client derives on average about 600 PBKDF2 keys
// of 1,000 iterations each, while verification is one HMAC.
const (
	challengeAlgorithm  = "PBKDF2/SHA-256"
	challengeCost       = 1_000
	challengeKeyLength  = 32
	challengeCounterMin = 200
	challengeCounterMax = 1_000
	// challengeDataKey names the token's view ID inside the signed challenge.
	challengeDataKey = "view"
)

// Challenge is the ALTCHA v2 challenge wire shape.
type Challenge = altcha.Challenge

// Solution is the ALTCHA v2 solution wire shape.
type Solution = altcha.Solution

type prover struct {
	signatureSecret string
	keySecret       string
	rand            io.Reader
	derive          altcha.DeriveKeyFunc
}

func newProver(random io.Reader) (*prover, error) {
	secrets := make([]byte, 64)
	if _, err := io.ReadFull(random, secrets); err != nil {
		return nil, fmt.Errorf("viewcount: challenge keys: %w", err)
	}
	return &prover{
		signatureSecret: hex.EncodeToString(secrets[:32]),
		keySecret:       hex.EncodeToString(secrets[32:]),
		rand:            random,
		derive:          altcha.DeriveKeyPBKDF2(),
	}, nil
}

// challenge creates a signed challenge bound to one view ID.
func (p *prover) challenge(id viewID) (Challenge, error) {
	span := big.NewInt(challengeCounterMax - challengeCounterMin)
	offset, err := randInt(p.rand, span)
	if err != nil {
		return Challenge{}, err
	}
	counter := challengeCounterMin + offset
	return altcha.CreateChallenge(altcha.CreateChallengeOptions{
		Algorithm:              challengeAlgorithm,
		Cost:                   challengeCost,
		KeyLength:              challengeKeyLength,
		Counter:                &counter,
		DeriveKey:              p.derive,
		Data:                   map[string]interface{}{challengeDataKey: id.text()},
		HMACSignatureSecret:    p.signatureSecret,
		HMACKeySignatureSecret: p.keySecret,
	})
}

// verify reports whether solution solves challenge, the challenge carries
// this process's signature, and it is bound to id. It never trusts a
// challenge without a key signature, which the library would otherwise
// accept on its signature alone.
func (p *prover) verify(challenge Challenge, solution Solution, id viewID) bool {
	params := challenge.Parameters
	if params.KeySignature == "" || params.Algorithm != challengeAlgorithm ||
		params.Cost != challengeCost || params.KeyLength != challengeKeyLength {
		return false
	}
	bound, ok := params.Data[challengeDataKey].(string)
	if !ok || len(params.Data) != 1 || bound != id.text() {
		return false
	}
	result, err := altcha.VerifySolution(altcha.VerifySolutionOptions{
		Challenge:              challenge,
		Solution:               solution,
		DeriveKey:              p.derive,
		HMACSignatureSecret:    p.signatureSecret,
		HMACKeySignatureSecret: p.keySecret,
	})
	return err == nil && result.Verified && !result.Expired
}

func randInt(random io.Reader, span *big.Int) (int, error) {
	buf := make([]byte, 8)
	if _, err := io.ReadFull(random, buf); err != nil {
		return 0, fmt.Errorf("viewcount: random: %w", err)
	}
	n := new(big.Int).SetBytes(buf)
	return int(n.Mod(n, span).Int64()), nil
}
