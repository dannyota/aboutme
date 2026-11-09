package oauthsrv

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"sync"
	"testing"
)

// indexedEntropy returns chunks 32-byte blocks, each starting with its own
// big-endian index, so every secret drawn from it names the block it read.
func indexedEntropy(chunks int) *bytes.Reader {
	buf := make([]byte, chunks*secretEntropyBytes)
	for i := range chunks {
		binary.BigEndian.PutUint32(buf[i*secretEntropyBytes:], uint32(i))
	}
	return bytes.NewReader(buf)
}

// entropyIndex decodes the block index from a secret's trailing body.
func entropyIndex(t *testing.T, raw string) uint32 {
	t.Helper()
	if len(raw) < secretBodyChars {
		t.Fatalf("secret length = %d, want at least %d", len(raw), secretBodyChars)
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(raw[len(raw)-secretBodyChars:])
	if err != nil || len(decoded) != secretEntropyBytes {
		t.Fatalf("secret body does not decode to %d bytes: %v", secretEntropyBytes, err)
	}
	return binary.BigEndian.Uint32(decoded)
}

// TestServiceSerializesEntropyReads draws codes and token pairs from many
// goroutines through one Service whose reader is not safe for concurrent use.
// The race detector fails an unserialized read; every draw must also read its
// own block, and a token pair must read two adjacent blocks.
func TestServiceSerializesEntropyReads(t *testing.T) {
	const workers = 16
	s := &Service{entropy: &lockedEntropy{reader: indexedEntropy(workers * 3)}}

	var wg sync.WaitGroup
	codes := make(chan string, workers)
	pairs := make(chan tokenPair, workers)
	failures := make(chan error, 2*workers)
	for range workers {
		wg.Add(2)
		go func() {
			defer wg.Done()
			raw, _, err := s.newCode()
			if err != nil {
				failures <- err
				return
			}
			codes <- raw
		}()
		go func() {
			defer wg.Done()
			pair, err := s.newTokenPair()
			if err != nil {
				failures <- err
				return
			}
			pairs <- pair
		}()
	}
	wg.Wait()
	close(codes)
	close(pairs)
	close(failures)
	for err := range failures {
		t.Fatalf("entropy draw failed: %v", err)
	}

	seen := make(map[uint32]bool, workers*3)
	record := func(index uint32) {
		if seen[index] {
			t.Fatalf("entropy block %d was read twice", index)
		}
		seen[index] = true
	}
	for raw := range codes {
		record(entropyIndex(t, raw))
	}
	for pair := range pairs {
		access, refresh := entropyIndex(t, pair.access), entropyIndex(t, pair.refresh)
		if refresh != access+1 {
			t.Fatalf("token pair read blocks %d and %d, want adjacent blocks", access, refresh)
		}
		record(access)
		record(refresh)
	}
	if len(seen) != workers*3 {
		t.Fatalf("distinct entropy blocks = %d, want %d", len(seen), workers*3)
	}
}
