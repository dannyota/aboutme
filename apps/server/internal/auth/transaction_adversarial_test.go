package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

const txTTL = 10 * time.Minute

type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func handleHash(handle string) [sha256.Size]byte {
	return sha256.Sum256([]byte(handle))
}

func rowState(ctx context.Context, t *testing.T, db rowQuerier, handle string) (consumedAt *time.Time, count int) {
	t.Helper()

	sum := handleHash(handle)
	err := db.QueryRow(ctx,
		`SELECT count(*), max(consumed_at) FROM oauth_transactions WHERE handle_hash = $1`,
		sum[:],
	).Scan(&count, &consumedAt)
	if err != nil {
		t.Fatalf("rowState query error: %v", err)
	}
	return consumedAt, count
}

func newRowInspectorPool(t *testing.T) *store.Pool {
	t.Helper()

	dsn := testutil.RequireTestDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() error: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	return pool
}

func randomHandle(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("rand.Read() error: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func assertTxEqual(t *testing.T, got, want auth.Transaction) {
	t.Helper()
	if got != want {
		t.Errorf("Transaction mismatch:\n got  = %+v\n want = %+v", got, want)
	}
}

func TestOAuthTxCookieName_MatchesHostPrefixContract(t *testing.T) {
	const want = "__Host-oauth-tx"
	if auth.OAuthTxCookieName != want {
		t.Errorf("OAuthTxCookieName = %q, want %q", auth.OAuthTxCookieName, want)
	}
}

func TestConsume_RejectsReplay(t *testing.T) {
	q := newTestQueries(t)
	ts := auth.NewTransactionStore(q)
	ctx := context.Background()
	inspector := newRowInspectorPool(t)

	handle, began, err := ts.Begin(ctx, auth.ProviderGoogle, auth.PurposeLogin, uuid.Nil,
		"https://aboutme.vn/api/v1/auth/google/callback")
	if err != nil {
		t.Fatalf("Begin() error: %v", err)
	}

	got1, err := ts.Consume(ctx, handle, auth.ProviderGoogle)
	if err != nil {
		t.Fatalf("first Consume() error: %v, want nil", err)
	}
	assertTxEqual(t, got1, began)

	consumedAt1, count1 := rowState(ctx, t, inspector, handle)
	if consumedAt1 == nil {
		t.Fatal("after first Consume(): consumed_at is NULL, want non-NULL")
	}
	if count1 != 1 {
		t.Fatalf("after first Consume(): %d rows for handle, want 1", count1)
	}

	if _, err := ts.Consume(ctx, handle, auth.ProviderGoogle); !errors.Is(err, auth.ErrTransactionInvalid) {
		t.Fatalf("second (replayed) Consume() error = %v, want ErrTransactionInvalid", err)
	}

	consumedAt2, count2 := rowState(ctx, t, inspector, handle)
	if count2 != 1 {
		t.Errorf("after replayed Consume(): %d rows for handle, want 1 (no new row created)", count2)
	}
	if consumedAt2 == nil || !consumedAt2.Equal(*consumedAt1) {
		t.Errorf("after replayed Consume(): consumed_at = %v, want unchanged %v (no second-row mutation)", consumedAt2, consumedAt1)
	}
}

func TestConsume_RejectsExpired(t *testing.T) {
	q := newTestQueries(t)
	clk := testutil.NewClockAtEpoch()
	ts := auth.NewTransactionStoreForTest(q, clk.Now)
	ctx := context.Background()

	handle, _, err := ts.Begin(ctx, auth.ProviderGoogle, auth.PurposeLogin, uuid.Nil,
		"https://aboutme.vn/api/v1/auth/google/callback")
	if err != nil {
		t.Fatalf("Begin() error: %v", err)
	}

	clk.Advance(txTTL + time.Second)

	if _, err := ts.Consume(ctx, handle, auth.ProviderGoogle); !errors.Is(err, auth.ErrTransactionInvalid) {
		t.Fatalf("Consume() after TTL error = %v, want ErrTransactionInvalid", err)
	}

	if _, err := ts.Consume(ctx, handle, auth.ProviderGoogle); !errors.Is(err, auth.ErrTransactionInvalid) {
		t.Errorf("second Consume() after TTL error = %v, want ErrTransactionInvalid", err)
	}
}

func TestConsume_RejectsProviderMismatch(t *testing.T) {
	tests := []struct {
		name      string
		begin     auth.Provider
		consumeAs auth.Provider
	}{
		{name: "github begin, google consume", begin: auth.ProviderGitHub, consumeAs: auth.ProviderGoogle},
		{name: "google begin, github consume", begin: auth.ProviderGoogle, consumeAs: auth.ProviderGitHub},
		{name: "google begin, linkedin consume", begin: auth.ProviderGoogle, consumeAs: auth.ProviderLinkedIn},
		{name: "linkedin begin, google consume", begin: auth.ProviderLinkedIn, consumeAs: auth.ProviderGoogle},
		{name: "github begin, linkedin consume", begin: auth.ProviderGitHub, consumeAs: auth.ProviderLinkedIn},
		{name: "linkedin begin, github consume", begin: auth.ProviderLinkedIn, consumeAs: auth.ProviderGitHub},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := newTestQueries(t)
			ts := auth.NewTransactionStore(q)
			ctx := context.Background()

			handle, _, err := ts.Begin(ctx, tt.begin, auth.PurposeLogin, uuid.Nil,
				fmt.Sprintf("https://aboutme.vn/api/v1/auth/%s/callback", tt.begin))
			if err != nil {
				t.Fatalf("Begin() error: %v", err)
			}

			if _, err := ts.Consume(ctx, handle, tt.consumeAs); !errors.Is(err, auth.ErrTransactionInvalid) {
				t.Fatalf("Consume(as %s) error = %v, want ErrTransactionInvalid", tt.consumeAs, err)
			}

			if _, err := ts.Consume(ctx, handle, tt.begin); !errors.Is(err, auth.ErrTransactionInvalid) {
				t.Errorf("Consume(as %s, the correct provider) after a mismatched attempt error = %v, want ErrTransactionInvalid (single-attempt fail-closed)", tt.begin, err)
			}
		})
	}
}

func TestConsume_UnknownHandle(t *testing.T) {
	q := newTestQueries(t)
	ts := auth.NewTransactionStore(q)
	ctx := context.Background()

	handle := randomHandle(t)

	if _, err := ts.Consume(ctx, handle, auth.ProviderGoogle); !errors.Is(err, auth.ErrTransactionInvalid) {
		t.Fatalf("Consume(unknown handle) error = %v, want ErrTransactionInvalid", err)
	}
}

func TestConsume_NoOracleAcrossFailureModes(t *testing.T) {
	errs := make(map[string]error, 4)

	{
		q := newTestQueries(t)
		ts := auth.NewTransactionStore(q)
		ctx := context.Background()
		handle, _, err := ts.Begin(ctx, auth.ProviderGoogle, auth.PurposeLogin, uuid.Nil,
			"https://aboutme.vn/api/v1/auth/google/callback")
		if err != nil {
			t.Fatalf("replay setup: Begin() error: %v", err)
		}
		if _, firstErr := ts.Consume(ctx, handle, auth.ProviderGoogle); firstErr != nil {
			t.Fatalf("replay setup: first Consume() error: %v, want nil", firstErr)
		}
		_, err = ts.Consume(ctx, handle, auth.ProviderGoogle)
		errs["replay"] = err
	}

	{
		q := newTestQueries(t)
		clk := testutil.NewClockAtEpoch()
		ts := auth.NewTransactionStoreForTest(q, clk.Now)
		ctx := context.Background()
		handle, _, err := ts.Begin(ctx, auth.ProviderGoogle, auth.PurposeLogin, uuid.Nil,
			"https://aboutme.vn/api/v1/auth/google/callback")
		if err != nil {
			t.Fatalf("expired setup: Begin() error: %v", err)
		}
		clk.Advance(txTTL + time.Second)
		_, err = ts.Consume(ctx, handle, auth.ProviderGoogle)
		errs["expired"] = err
	}

	{
		q := newTestQueries(t)
		ts := auth.NewTransactionStore(q)
		ctx := context.Background()
		handle, _, err := ts.Begin(ctx, auth.ProviderGitHub, auth.PurposeLogin, uuid.Nil,
			"https://aboutme.vn/api/v1/auth/github/callback")
		if err != nil {
			t.Fatalf("mismatch setup: Begin() error: %v", err)
		}
		_, err = ts.Consume(ctx, handle, auth.ProviderGoogle)
		errs["mismatch"] = err
	}

	{
		q := newTestQueries(t)
		ts := auth.NewTransactionStore(q)
		ctx := context.Background()
		_, err := ts.Consume(ctx, randomHandle(t), auth.ProviderGoogle)
		errs["unknown"] = err
	}

	for name, err := range errs {
		if !errors.Is(err, auth.ErrTransactionInvalid) {
			t.Errorf("%s: error = %v, want ErrTransactionInvalid", name, err)
		}
	}

	want := auth.ErrTransactionInvalid.Error()
	for name, err := range errs {
		if err == nil {
			continue
		}
		if got := err.Error(); got != want {
			t.Errorf("%s: error text = %q, want %q (identical text across failure modes -- no oracle)", name, got, want)
		}
	}
}

func TestConsume_ConcurrentDoubleConsume_ExactlyOneSucceeds(t *testing.T) {
	q := newTestQueries(t)
	ts := auth.NewTransactionStore(q)
	ctx := context.Background()

	handle, began, err := ts.Begin(ctx, auth.ProviderGoogle, auth.PurposeLogin, uuid.Nil,
		"https://aboutme.vn/api/v1/auth/google/callback")
	if err != nil {
		t.Fatalf("Begin() error: %v", err)
	}

	const workers = 10
	results := make([]struct {
		tx  auth.Transaction
		err error
	}, workers)

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := range results {
		go func(i int) {
			defer wg.Done()
			results[i].tx, results[i].err = ts.Consume(ctx, handle, auth.ProviderGoogle)
		}(i)
	}
	wg.Wait()

	var successes, invalidFailures, otherFailures int
	var winner auth.Transaction
	for _, r := range results {
		switch {
		case r.err == nil:
			successes++
			winner = r.tx
		case errors.Is(r.err, auth.ErrTransactionInvalid):
			invalidFailures++
		default:
			otherFailures++
			t.Errorf("unexpected error from concurrent Consume(): %v", r.err)
		}
	}

	if successes != 1 {
		t.Errorf("successes = %d, want exactly 1 (atomic single-consume guarantee)", successes)
	}
	if invalidFailures != workers-1 {
		t.Errorf("invalidFailures = %d, want %d", invalidFailures, workers-1)
	}
	if otherFailures != 0 {
		t.Errorf("otherFailures = %d, want 0", otherFailures)
	}
	if successes == 1 {
		assertTxEqual(t, winner, began)
	}
}
