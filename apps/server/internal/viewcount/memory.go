package viewcount

import (
	"crypto/hmac"
	"crypto/sha256"
	"net/netip"
	"time"

	"github.com/google/uuid"
)

// fifo is a bounded set whose entries leave in insertion order: when full,
// or once an entry's expiry has passed. Callers insert with expiries that
// rise with insertion, give or take a few milliseconds, so expire stops at
// the first live entry and a late neighbor leaves on a later call.
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

// add inserts key and reports false when the set is full.
func (f *fifo[K]) add(key K, expiry time.Time) bool {
	if f.full() {
		return false
	}
	f.entries[key] = expiry
	f.order = append(f.order, key)
	return true
}

// evictOldest removes the oldest entry to make room and returns it.
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

// networkFlags records what one network already did for one resume today.
type networkFlags uint8

const (
	flagCounted networkFlags = 1 << iota
	flagBot
	flagDatacenter
	flagAnomaly
	flagInvalid
)

// networkKey is HMAC-SHA-256(dayKey, network || resumeID), truncated. The
// day key is random, lives in memory for one Asia/Ho_Chi_Minh day, and is
// never written anywhere (docs/design/viewer-analytics/counting.md, "Layer
// 7").
type networkKey [16]byte

// networkBytes is the IPv4 address or the IPv6 /64.
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

// vietnam is Asia/Ho_Chi_Minh, which has kept UTC+7 without daylight saving
// since 1975. A fixed zone needs no time zone database in the image.
var vietnam = time.FixedZone("Asia/Ho_Chi_Minh", 7*60*60)

// Day returns the Asia/Ho_Chi_Minh calendar day of t at midnight UTC, the
// form the store's date columns take.
func Day(t time.Time) time.Time {
	y, m, d := t.In(vietnam).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func hourOf(t time.Time) time.Time {
	return t.In(vietnam).Truncate(time.Hour)
}
