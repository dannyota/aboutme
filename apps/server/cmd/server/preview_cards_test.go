package main

import (
	"context"
	"testing"
)

// With PREVIEW_CARD_ENABLED off, nothing of the card path is wired: the
// public service gets a nil boundary and keeps og.png, the hub has no
// observer, and no scheduler runs.
func TestPreviewCardsOffWiresNothing(t *testing.T) {
	t.Parallel()
	cards, err := newPreviewCards(false, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("newPreviewCards(false) error = %v", err)
	}
	if cards.public != nil || cards.scheduler != nil || cards.observe() != nil {
		t.Fatalf("disabled preview cards = %+v", cards)
	}
	select {
	case <-cards.run(context.Background(), nil):
	default:
		t.Fatal("disabled scheduler did not report stopped")
	}
}

func TestPreviewCardsOnRequiresItsDependencies(t *testing.T) {
	t.Parallel()
	if _, err := newPreviewCards(true, nil, nil, nil, nil); err == nil {
		t.Fatal("newPreviewCards(true) without a pool and reader succeeded")
	}
}
