package password

import (
	"bytes"
	_ "embed" // required by the //go:embed directive below
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
)

// blocklistMagic is the ASCII magic prefix of the generated digests.bin.
const blocklistMagic = "ABMEBL01"

//go:embed blocklist/digests.bin
var blocklistDigests []byte

// Blocklist is the in-memory set of common-password SHA-256 digests, loaded
// from the embedded generated artifact and kept sorted for binary search.
type Blocklist struct {
	digests [][32]byte
}

// LoadBlocklist parses the embedded generated digest artifact.
func LoadBlocklist() (*Blocklist, error) {
	return NewBlocklist(blocklistDigests)
}

// NewBlocklist parses a digests.bin artifact: ASCII magic, big-endian uint32
// count, then that many strictly-increasing 32-byte SHA-256 digests. Any
// structural corruption fails closed before any lookup happens.
func NewBlocklist(data []byte) (*Blocklist, error) {
	if len(data) < 12 {
		return nil, errors.New("password: blocklist too short")
	}
	if string(data[:8]) != blocklistMagic {
		return nil, errors.New("password: blocklist magic mismatch")
	}
	count := binary.BigEndian.Uint32(data[8:12])
	if uint64(len(data)) != 12+uint64(count)*32 {
		return nil, fmt.Errorf("password: blocklist has %d bytes for %d digests", len(data), count)
	}

	digests := make([][32]byte, int(count))
	for i := range digests {
		off := 12 + i*32
		copy(digests[i][:], data[off:off+32])
	}
	for i := 1; i < len(digests); i++ {
		if bytes.Compare(digests[i-1][:], digests[i][:]) >= 0 {
			return nil, errors.New("password: blocklist digests are not strictly increasing")
		}
	}
	return &Blocklist{digests: digests}, nil
}

// Contains reports whether digest is in the blocklist.
func (b *Blocklist) Contains(digest [32]byte) bool {
	i := sort.Search(len(b.digests), func(i int) bool {
		return bytes.Compare(b.digests[i][:], digest[:]) >= 0
	})
	return i < len(b.digests) && b.digests[i] == digest
}

// Len returns the number of digests in the blocklist.
func (b *Blocklist) Len() int { return len(b.digests) }
