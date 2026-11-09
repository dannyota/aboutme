package app

import (
	"context"
	"testing"
)

// With PREVIEW_CARD_ENABLED off, nothing of the card path is wired: the
// public service gets a nil boundary and keeps og.png, the hub has no
// observer, and no scheduler runs.
func TestPreviewCardsOffWiresNothing(t *testing.T) {
	t.Parallel()
	cards, err := NewPreviewCards(false, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewPreviewCards(false) error = %v", err)
	}
	if cards.Public != nil || cards.Scheduler != nil || cards.Observe() != nil {
		t.Fatalf("disabled preview cards = %+v", cards)
	}
	select {
	case <-cards.Run(context.Background(), nil):
	default:
		t.Fatal("disabled scheduler did not report stopped")
	}
}

func TestPreviewCardsOnRequiresItsDependencies(t *testing.T) {
	t.Parallel()
	if _, err := NewPreviewCards(true, nil, nil, nil, nil); err == nil {
		t.Fatal("NewPreviewCards(true) without a pool and reader succeeded")
	}
}
