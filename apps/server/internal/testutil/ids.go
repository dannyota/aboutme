package testutil

import (
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"math/rand/v2" // nosemgrep: go.lang.security.audit.crypto.math_random.math-random-used -- seeded test fixtures require reproducible, non-cryptographic randomness
)

// NewSeededRand returns a reproducible generator for test fixtures.
func NewSeededRand(seed uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, seed)) //nolint:gosec // deliberately weak/reproducible: this is a fixture generator, never used for anything security-sensitive
}

// SeededUUID derives a reproducible RFC 4122 version-4-shaped fixture ID.
func SeededUUID(seed uint64) string {
	r := NewSeededRand(seed)
	var b [16]byte
	for i := range b {
		b[i] = byte(r.IntN(256)) //nolint:gosec // r.IntN(256) is always in [0,256), so the byte conversion never overflows
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]))
}

// NamedID deterministically derives a stable, UUID-shaped fixture ID from a
// human-readable label (e.g. "user-alice"): the same label always yields
// the same ID, in this run and every future one, on any machine. This is
// the preferred way to mint fixture IDs — it reads better in test
// failures than a bare numeric seed while staying just as reproducible.
func NamedID(label string) string {
	h := fnv.New64a()
	if _, err := h.Write([]byte(label)); err != nil {
		// hash.Hash.Write is documented to never return an error; this
		// branch is unreachable, but the return value must still be
		// handled explicitly (errcheck, check-blank).
		panic(fmt.Sprintf("testutil: unexpected error hashing NamedID label: %v", err))
	}
	return SeededUUID(h.Sum64())
}
