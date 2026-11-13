package mailcapture

import (
	"errors"
	"sync"
	"time"

	"danny.vn/aboutme/apps/server/internal/authmail"
)

const (
	MaxMessages     = 50
	MaxTotalBytes   = 256 * 1024
	MaxMessageBytes = 16 * 1024
)

var (
	ErrSecret   = errors.New("mailcapture: invalid capture secret")
	ErrConfig   = errors.New("mailcapture: invalid capture configuration")
	ErrOversize = errors.New("mailcapture: message exceeds capture cap")
)

type StoredMessage struct {
	ID         uint64        `json:"id"`
	ReceivedAt time.Time     `json:"received_at"`
	Kind       authmail.Kind `json:"kind"`
	To         string        `json:"to"`
	Subject    string        `json:"subject"`
	TextBody   string        `json:"text_body"`
	HTMLBody   string        `json:"html_body"`
}

type Store struct {
	mu       sync.Mutex
	nextID   uint64
	messages []StoredMessage
	total    int
}

func NewStore() *Store {
	return &Store{}
}

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

func (s *Store) Messages() []StoredMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]StoredMessage, 0, len(s.messages))
	for i := len(s.messages) - 1; i >= 0; i-- {
		out = append(out, s.messages[i])
	}
	return out
}

func (s *Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = nil
	s.nextID = 0
	s.total = 0
}

func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}

func messageSize(m authmail.Message) int {
	return len(m.Kind) + len(m.To) + len(m.Subject) + len(m.TextBody) + len(m.HTMLBody)
}

func messageSizeStored(m StoredMessage) int {
	return len(m.Kind) + len(m.To) + len(m.Subject) + len(m.TextBody) + len(m.HTMLBody)
}
