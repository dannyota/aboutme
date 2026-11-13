package viewcount

import (
	"crypto/hmac"
	"crypto/sha256"
	"net/netip"
	"time"

	"github.com/google/uuid"
)

type fifo[K comparable] struct {
	limit   int
	entries map[K]time.Time
	order   []K
	head    int
}

func newFIFO[K comparable](limit int) *fifo[K] {
	return &fifo[K]{limit: limit, entries: make(map[K]time.Time)}
}

func (f *fifo[K]) expire(now time.Time) {
	for f.head < len(f.order) {
		key := f.order[f.head]
		expiry, ok := f.entries[key]
		if ok && expiry.After(now) {
			break
		}
		if ok {
			delete(f.entries, key)
		}
		f.head++
	}
	f.compact()
}

func (f *fifo[K]) compact() {
	if f.head > 0 && f.head*2 >= len(f.order) {
		f.order = append([]K(nil), f.order[f.head:]...)
		f.head = 0
	}
}

func (f *fifo[K]) has(key K) bool {
	_, ok := f.entries[key]
	return ok
}

func (f *fifo[K]) full() bool {
	return len(f.entries) >= f.limit
}

func (f *fifo[K]) add(key K, expiry time.Time) bool {
	if f.full() {
		return false
	}
	f.entries[key] = expiry
	f.order = append(f.order, key)
	return true
}

func (f *fifo[K]) evictOldest() (K, bool) {
	defer f.compact()
	for f.head < len(f.order) {
		key := f.order[f.head]
		f.head++
		if _, ok := f.entries[key]; ok {
			delete(f.entries, key)
			return key, true
		}
	}
	var zero K
	return zero, false
}

type networkFlags uint8

const (
	flagCounted networkFlags = 1 << iota
	flagBot
	flagDatacenter
	flagAnomaly
	flagInvalid
)

type networkKey [16]byte

func networkBytes(addr netip.Addr) []byte {
	addr = addr.Unmap()
	if addr.Is4() {
		b := addr.As4()
		return b[:]
	}
	b := addr.As16()
	return b[:8]
}

func deriveNetworkKey(dayKey []byte, addr netip.Addr, resumeID uuid.UUID) networkKey {
	mac := hmac.New(sha256.New, dayKey)
	mac.Write(networkBytes(addr))
	mac.Write(resumeID[:])
	var key networkKey
	copy(key[:], mac.Sum(nil))
	return key
}

var vietnam = time.FixedZone("Asia/Ho_Chi_Minh", 7*60*60)

func Day(t time.Time) time.Time {
	y, m, d := t.In(vietnam).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func hourOf(t time.Time) time.Time {
	return t.In(vietnam).Truncate(time.Hour)
}
