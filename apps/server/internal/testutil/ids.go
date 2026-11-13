package testutil

import (
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
)

func NewSeededRand(seed uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, seed)) //nolint:gosec // deliberately weak/reproducible: this is a fixture generator, never used for anything security-sensitive
}

func SeededUUID(seed uint64) string {
	r := NewSeededRand(seed)
	var b [16]byte
	for i := range b {
		b[i] = byte(r.IntN(256)) //nolint:gosec // r.IntN(256) is always in [0,256), so the byte conversion never overflows
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]))
}

func NamedID(label string) string {
	h := fnv.New64a()
	if _, err := h.Write([]byte(label)); err != nil {
		// hash.Hash.Write never returns an error (hash package docs).
		panic(fmt.Sprintf("testutil: unexpected error hashing NamedID label: %v", err))
	}
	return SeededUUID(h.Sum64())
}
