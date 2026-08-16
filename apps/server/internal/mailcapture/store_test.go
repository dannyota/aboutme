package mailcapture

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/authmail"
)

func sampleMessage() authmail.Message {
	return authmail.Message{
		Kind:     authmail.KindVerify,
		To:       "alice@example.com",
		Subject:  "Verify your email",
		TextBody: "Confirm by opening the link.",
		HTMLBody: "<p>Confirm by opening the link.</p>",
	}
}

func TestMailCaptureStoreAddRejectsOversize(t *testing.T) {
	s := NewStore()
	m := authmail.Message{
		Kind: authmail.KindVerify, To: "a", Subject: "b",
		TextBody: strings.Repeat("x", MaxMessageBytes+1),
	}
	if _, err := s.Add(m); !errors.Is(err, ErrOversize) {
		t.Fatalf("Add = %v, want ErrOversize", err)
	}
	if s.Count() != 0 {
		t.Fatalf("count = %d, want 0 (oversize message not stored)", s.Count())
	}
}

func TestMailCaptureStoreAddAcceptsExactlyAtCap(t *testing.T) {
	s := NewStore()
	m := authmail.Message{
		Kind: authmail.KindVerify, To: "a", Subject: "b",
		TextBody: strings.Repeat("x", MaxMessageBytes-len("verify")-len("a")-len("b")),
	}
	if _, err := s.Add(m); err != nil {
		t.Fatalf("Add at exactly the cap: %v", err)
	}
	if s.Count() != 1 {
		t.Fatalf("count = %d, want 1", s.Count())
	}
}

func TestMailCaptureStoreEvictsOldestByCount(t *testing.T) {
	s := NewStore()
	for i := 0; i < MaxMessages+1; i++ {
		m := sampleMessage()
		m.To = "u" + strings.Repeat("x", i) // distinct but tiny
		if _, err := s.Add(m); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}
	if s.Count() != MaxMessages {
		t.Fatalf("count = %d, want %d", s.Count(), MaxMessages)
	}
	msgs := s.Messages()
	if len(msgs) != MaxMessages {
		t.Fatalf("messages = %d, want %d", len(msgs), MaxMessages)
	}
	// The first added (ID 1) was evicted; the newest has ID MaxMessages+1.
	if msgs[0].ID != uint64(MaxMessages+1) {
		t.Errorf("newest ID = %d, want %d", msgs[0].ID, MaxMessages+1)
	}
	if msgs[len(msgs)-1].ID != uint64(2) {
		t.Errorf("oldest kept ID = %d, want 2", msgs[len(msgs)-1].ID)
	}
}

func TestMailCaptureStoreEvictsOldestByTotalBytes(t *testing.T) {
	s := NewStore()
	const body = 10 * 1024
	added := 30
	for i := 0; i < added; i++ {
		m := authmail.Message{
			Kind: authmail.KindVerify, To: "a", Subject: "b",
			TextBody: strings.Repeat("x", body),
		}
		if _, err := s.Add(m); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}
	msgs := s.Messages()
	if len(msgs) == 0 {
		t.Fatal("messages empty")
	}
	if msgs[0].ID != uint64(added) {
		t.Errorf("newest ID = %d, want %d (newest must never be evicted)", msgs[0].ID, added)
	}
	total := 0
	for _, m := range msgs {
		total += messageSizeStored(m)
	}
	if total > MaxTotalBytes {
		t.Errorf("total = %d, want <= %d", total, MaxTotalBytes)
	}
	if len(msgs) >= added {
		t.Errorf("kept %d of %d, want evictions", len(msgs), added)
	}
}

func TestMailCaptureStoreReset(t *testing.T) {
	s := NewStore()
	if _, err := s.Add(sampleMessage()); err != nil {
		t.Fatal(err)
	}
	s.Reset()
	if s.Count() != 0 {
		t.Fatalf("count after reset = %d, want 0", s.Count())
	}
	if msgs := s.Messages(); len(msgs) != 0 {
		t.Fatalf("messages after reset = %d, want 0", len(msgs))
	}
	// Restart semantics: IDs restart from 1 after reset.
	rec, err := s.Add(sampleMessage())
	if err != nil {
		t.Fatal(err)
	}
	if rec.ID != 1 {
		t.Fatalf("ID after reset = %d, want 1", rec.ID)
	}
}

func TestMailCaptureStoreMessagesNewestFirst(t *testing.T) {
	s := NewStore()
	for i := 1; i <= 3; i++ {
		m := sampleMessage()
		m.To = "alice" + strings.Repeat("x", i)
		rec, err := s.Add(m)
		if err != nil {
			t.Fatal(err)
		}
		if rec.ID != uint64(i) {
			t.Fatalf("ID = %d, want %d", rec.ID, i)
		}
	}
	msgs := s.Messages()
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3", len(msgs))
	}
	if msgs[0].ID != 3 || msgs[1].ID != 2 || msgs[2].ID != 1 {
		t.Fatalf("order = [%d %d %d], want [3 2 1]", msgs[0].ID, msgs[1].ID, msgs[2].ID)
	}
}

func TestMailCaptureStoreConcurrentAddAndRead(t *testing.T) {
	s := NewStore()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				if _, err := s.Add(sampleMessage()); err != nil {
					t.Errorf("Add: %v", err)
				}
				_ = s.Messages()
				_ = s.Count()
			}
		}()
	}
	wg.Wait()
	if s.Count() > MaxMessages {
		t.Fatalf("count = %d, want <= %d", s.Count(), MaxMessages)
	}
}
