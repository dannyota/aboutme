package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeWriteTx struct {
	pgx.Tx
	events              *[]string
	entryErr            error
	callbackErr         error
	commitErr           error
	rollbackErr         error
	entryCtx            context.Context
	rollbackErrAtCall   error
	rollbackDeadline    time.Time
	rollbackHasDeadline bool
	commitCalls         int
	rollbacks           int
}

type callerContextKey struct{}

func (tx *fakeWriteTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	if sql == "SELECT public.runtime_enter_write()" && len(arguments) == 0 {
		*tx.events = append(*tx.events, "entry")
		tx.entryCtx = ctx
		return pgconn.CommandTag{}, tx.entryErr
	}
	*tx.events = append(*tx.events, "callback-query")
	return pgconn.CommandTag{}, tx.callbackErr
}

func (tx *fakeWriteTx) Commit(context.Context) error {
	*tx.events = append(*tx.events, "commit")
	tx.commitCalls++
	return tx.commitErr
}

func (tx *fakeWriteTx) Rollback(ctx context.Context) error {
	*tx.events = append(*tx.events, "rollback")
	tx.rollbacks++
	tx.rollbackErrAtCall = ctx.Err()
	tx.rollbackDeadline, tx.rollbackHasDeadline = ctx.Deadline()
	return tx.rollbackErr
}

type fakeWriteStarter struct {
	tx         pgx.Tx
	err        error
	events     *[]string
	gotCtx     context.Context
	gotOptions pgx.TxOptions
	beginCalls int
}

func (s *fakeWriteStarter) BeginWrite(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	*s.events = append(*s.events, "begin")
	s.gotCtx = ctx
	s.gotOptions = options
	s.beginCalls++
	return s.tx, s.err
}

func TestPoolWriteTxStarterBeginsThenEntersBeforeReturning(t *testing.T) {
	ctx := context.WithValue(context.Background(), callerContextKey{}, "caller")
	options := pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: pgx.ReadWrite}
	events := []string{}
	tx := &fakeWriteTx{events: &events}
	var gotCtx context.Context
	var gotOptions pgx.TxOptions
	starter := &poolWriteTxStarter{begin: func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
		events = append(events, "pool-begin")
		gotCtx, gotOptions = ctx, options
		return tx, nil
	}}

	got, err := starter.BeginWrite(ctx, options)
	if err != nil {
		t.Fatalf("BeginWrite() error = %v", err)
	}
	if got != tx {
		t.Fatalf("BeginWrite() tx = %T, want fake transaction", got)
	}
	if gotCtx != ctx || tx.entryCtx != ctx {
		t.Fatal("BeginWrite() did not forward the caller context")
	}
	if gotOptions != options {
		t.Fatalf("BeginWrite() options = %#v, want %#v", gotOptions, options)
	}
	assertEvents(t, events, "pool-begin", "entry")
}

func TestPoolWriteTxStarterBeginFailureExposesNoTransaction(t *testing.T) {
	wantErr := errors.New("begin failed")
	events := []string{}
	starter := &poolWriteTxStarter{begin: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
		events = append(events, "pool-begin")
		return nil, wantErr
	}}

	tx, err := starter.BeginWrite(context.Background(), pgx.TxOptions{})
	if tx != nil || !errors.Is(err, wantErr) {
		t.Fatalf("BeginWrite() = (%v, %v), want (nil, %v)", tx, err, wantErr)
	}
	assertEvents(t, events, "pool-begin")
}

func TestPoolWriteTxStarterEntryFailureRollsBackWithLiveBoundedContext(t *testing.T) {
	caller, cancel := context.WithCancel(context.Background())
	cancel()
	wantErr := errors.New("entry failed")
	events := []string{}
	tx := &fakeWriteTx{events: &events, entryErr: wantErr, rollbackErr: errors.New("cleanup failed")}
	starter := &poolWriteTxStarter{begin: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }}

	got, err := starter.BeginWrite(caller, pgx.TxOptions{})
	if got != nil || !errors.Is(err, wantErr) {
		t.Fatalf("BeginWrite() = (%v, %v), want (nil, %v)", got, err, wantErr)
	}
	assertLiveBoundedCleanup(t, tx)
	assertEvents(t, events, "entry", "rollback")
}

func TestWithWriteTxBindsQueriesAndCommitsOnce(t *testing.T) {
	ctx := context.Background()
	options := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, DeferrableMode: pgx.Deferrable}
	events := []string{}
	tx := &fakeWriteTx{events: &events}
	starter := &fakeWriteStarter{tx: tx, events: &events}

	err := WithWriteTx(ctx, starter, options, func(q *Queries) error {
		events = append(events, "callback")
		_, queryErr := q.db.Exec(ctx, "UPDATE public.example SET value = 1")
		return queryErr
	})
	if err != nil {
		t.Fatalf("WithWriteTx() error = %v", err)
	}
	if starter.gotCtx != ctx || starter.gotOptions != options {
		t.Fatalf("BeginWrite arguments = (%v, %#v), want caller context and %#v", starter.gotCtx, starter.gotOptions, options)
	}
	if tx.commitCalls != 1 || tx.rollbacks != 0 {
		t.Fatalf("commit calls = %d, rollbacks = %d, want 1, 0", tx.commitCalls, tx.rollbacks)
	}
	assertEvents(t, events, "begin", "callback", "callback-query", "commit")
}

func TestWithWriteTxBeginFailureDoesNotRunCallback(t *testing.T) {
	wantErr := errors.New("begin failed")
	events := []string{}
	starter := &fakeWriteStarter{err: wantErr, events: &events}

	err := WithWriteTx(context.Background(), starter, pgx.TxOptions{}, func(*Queries) error {
		t.Fatal("callback ran after begin failure")
		return nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("WithWriteTx() error = %v, want %v", err, wantErr)
	}
	assertEvents(t, events, "begin")
}

func TestWithWriteTxCallbackFailurePreservesErrorAndRollsBack(t *testing.T) {
	caller, cancel := context.WithCancel(context.Background())
	cancel()
	wantErr := errors.New("callback failed")
	events := []string{}
	tx := &fakeWriteTx{events: &events, rollbackErr: errors.New("cleanup failed")}
	starter := &fakeWriteStarter{tx: tx, events: &events}

	err := WithWriteTx(caller, starter, pgx.TxOptions{}, func(*Queries) error {
		events = append(events, "callback")
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("WithWriteTx() error = %v, want %v", err, wantErr)
	}
	assertLiveBoundedCleanup(t, tx)
	assertEvents(t, events, "begin", "callback", "rollback")
}

func TestWithWriteTxPanicRollsBackAndRethrows(t *testing.T) {
	panicValue := &struct{ message string }{"callback panic"}
	events := []string{}
	tx := &fakeWriteTx{events: &events, rollbackErr: pgx.ErrTxClosed}
	starter := &fakeWriteStarter{tx: tx, events: &events}

	func() {
		defer func() {
			if got := recover(); got != panicValue {
				t.Fatalf("recovered panic = %#v, want original %#v", got, panicValue)
			}
		}()
		if err := WithWriteTx(context.Background(), starter, pgx.TxOptions{}, func(*Queries) error {
			events = append(events, "callback")
			panic(panicValue)
		}); err != nil {
			t.Fatalf("WithWriteTx() returned after panic: %v", err)
		}
	}()

	if tx.commitCalls != 0 || tx.rollbacks != 1 {
		t.Fatalf("commit calls = %d, rollbacks = %d, want 0, 1", tx.commitCalls, tx.rollbacks)
	}
	assertEvents(t, events, "begin", "callback", "rollback")
}

func TestWithWriteTxCommitFailureIsReturnedWithoutRetry(t *testing.T) {
	wantErr := errors.New("commit outcome unknown")
	events := []string{}
	tx := &fakeWriteTx{events: &events, commitErr: wantErr}
	starter := &fakeWriteStarter{tx: tx, events: &events}
	callbacks := 0

	err := WithWriteTx(context.Background(), starter, pgx.TxOptions{}, func(*Queries) error {
		callbacks++
		events = append(events, "callback")
		return nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("WithWriteTx() error = %v, want %v", err, wantErr)
	}
	if starter.beginCalls != 1 || callbacks != 1 || tx.commitCalls != 1 || tx.rollbacks != 0 {
		t.Fatalf("calls: begin=%d callback=%d commit=%d rollback=%d, want 1,1,1,0", starter.beginCalls, callbacks, tx.commitCalls, tx.rollbacks)
	}
	assertEvents(t, events, "begin", "callback", "commit")
}

func TestExecWriteUsesDefaultOptions(t *testing.T) {
	events := []string{}
	tx := &fakeWriteTx{events: &events}
	starter := &fakeWriteStarter{tx: tx, events: &events}

	if err := ExecWrite(context.Background(), starter, func(*Queries) error { return nil }); err != nil {
		t.Fatalf("ExecWrite() error = %v", err)
	}
	if starter.gotOptions != (pgx.TxOptions{}) {
		t.Fatalf("ExecWrite() options = %#v, want zero options", starter.gotOptions)
	}
}

func assertEvents(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func assertLiveBoundedCleanup(t *testing.T, tx *fakeWriteTx) {
	t.Helper()
	if err := tx.rollbackErrAtCall; err != nil {
		t.Fatalf("cleanup context was canceled during rollback: %v", err)
	}
	if !tx.rollbackHasDeadline {
		t.Fatal("cleanup context has no deadline")
	}
	remaining := time.Until(tx.rollbackDeadline)
	if remaining <= 0 || remaining > 5*time.Second {
		t.Fatalf("cleanup deadline remaining = %v, want within five seconds", remaining)
	}
}
