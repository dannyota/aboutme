// Package mailcapture implements the local, loopback-only authentication-email
// capture server (D7). It holds at most 50 messages and 256 KiB total, rejects
// an individual message over 16 KiB, evicts oldest accepted messages, resets on
// restart, and never logs the bearer secret.
package mailcapture

import (
	"errors"
	"sync"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/authmail"
)

// Capture bounds (D7).
const (
	MaxMessages     = 50
	MaxTotalBytes   = 256 * 1024
	MaxMessageBytes = 16 * 1024
)

// Closed sentinels. None carries a message, recipient, secret, or raw error.
var (
	ErrSecret   = errors.New("mailcapture: invalid capture secret")
	ErrConfig   = errors.New("mailcapture: invalid capture configuration")
	ErrOversize = errors.New("mailcapture: message exceeds capture cap")
)

// StoredMessage is the immutable record returned by the capture API.
type StoredMessage struct {
	ID         uint64        `json:"id"`
	ReceivedAt time.Time     `json:"received_at"`
	Kind       authmail.Kind `json:"kind"`
	To         string        `json:"to"`
	Subject    string        `json:"subject"`
	TextBody   string        `json:"text_body"`
	HTMLBody   string        `json:"html_body"`
}

// Store is an in-memory, restart-reset bounded message store. All methods are
// safe for concurrent use.
type Store struct {
	mu       sync.Mutex
	nextID   uint64
	messages []StoredMessage // oldest first; eviction removes from index 0
	total    int
}

// NewStore returns an empty capture store.
func NewStore() *Store {
	return &Store{}
}

// Add validates the per-message cap, then stores the message, evicting oldest
// accepted messages while over the 50-message or 256 KiB total caps. It returns
// the stored record or ErrOversize for a message over 16 KiB.
func (s *Store) Add(m authmail.Message) (StoredMessage, error) {
	if messageSize(m) > MaxMessageBytes {
		return StoredMessage{}, ErrOversize
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	rec := StoredMessage{
		ID:         s.nextID,
		ReceivedAt: now,
		Kind:       m.Kind,
		To:         m.To,
		Subject:    m.Subject,
		TextBody:   m.TextBody,
		HTMLBody:   m.HTMLBody,
	}
	s.messages = append(s.messages, rec)
	s.total += messageSize(m)
	for len(s.messages) > MaxMessages || s.total > MaxTotalBytes {
		evicted := s.messages[0]
		s.messages = s.messages[1:]
		s.total -= messageSizeStored(evicted)
	}
	return rec, nil
}

// Messages returns a newest-first snapshot of the stored messages.
func (s *Store) Messages() []StoredMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]StoredMessage, 0, len(s.messages))
	for i := len(s.messages) - 1; i >= 0; i-- {
		out = append(out, s.messages[i])
	}
	return out
}

// Reset clears every stored message.
func (s *Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = nil
	s.nextID = 0
	s.total = 0
}

// Count returns the number of stored messages.
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}

// messageSize is the capture byte accounting for one message: the sum of its
// field byte lengths, matching the 16 KiB per-message and 256 KiB total caps.
func messageSize(m authmail.Message) int {
	return len(m.Kind) + len(m.To) + len(m.Subject) + len(m.TextBody) + len(m.HTMLBody)
}

func messageSizeStored(m StoredMessage) int {
	return len(m.Kind) + len(m.To) + len(m.Subject) + len(m.TextBody) + len(m.HTMLBody)
}
