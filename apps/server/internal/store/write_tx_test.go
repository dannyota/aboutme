package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeWriteLease struct {
	events             []string
	tx                 *fakeRunnerTx
	beginErr           error
	gotContext         context.Context
	gotOptions         pgx.TxOptions
	releases, destroys int
	destroyErrAtCall   error
	destroyErr         error
	destroyDeadline    time.Time
	destroyHasDeadline bool
}

func newFakeWriteLease() *fakeWriteLease {
	l := &fakeWriteLease{}
	l.tx = &fakeRunnerTx{lease: l}
	return l
}
func (l *fakeWriteLease) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	l.events = append(l.events, "begin")
	l.gotContext = ctx
	l.gotOptions = o
	return l.tx, l.beginErr
}
func (l *fakeWriteLease) Release(context.Context) error {
	l.events = append(l.events, "release")
	l.releases++
	return nil
}
func (l *fakeWriteLease) Destroy(ctx context.Context) error {
	l.events = append(l.events, "destroy")
	l.destroys++
	l.destroyErrAtCall = ctx.Err()
	l.destroyDeadline, l.destroyHasDeadline = ctx.Deadline()
	return l.destroyErr
}

type fakeRunnerTx struct {
	pgx.Tx
	lease                                                         *fakeWriteLease
	entryErr, callbackQueryErr, finishErr, commitErr, rollbackErr error
	rollbackErrAtCall                                             error
	rollbackDeadline                                              time.Time
	rollbackHasDeadline                                           bool
	callbacks, finishes, commits, rollbacks                       int
}

func (tx *fakeRunnerTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	switch {
	case sql == "SELECT public.runtime_enter_write()" && len(args) == 0:
		tx.lease.events = append(tx.lease.events, "entry")
		return pgconn.CommandTag{}, tx.entryErr
	case sql == "SELECT public.runtime_finish_write()" && len(args) == 0:
		tx.lease.events = append(tx.lease.events, "finish")
		tx.finishes++
		return pgconn.CommandTag{}, tx.finishErr
	default:
		tx.lease.events = append(tx.lease.events, "callback-query")
		return pgconn.CommandTag{}, tx.callbackQueryErr
	}
}
func (tx *fakeRunnerTx) Commit(context.Context) error {
	tx.lease.events = append(tx.lease.events, "commit")
	tx.commits++
	return tx.commitErr
}
func (tx *fakeRunnerTx) Rollback(ctx context.Context) error {
	tx.lease.events = append(tx.lease.events, "rollback")
	tx.rollbacks++
	tx.rollbackErrAtCall = ctx.Err()
	tx.rollbackDeadline, tx.rollbackHasDeadline = ctx.Deadline()
	return tx.rollbackErr
}

func fakeRunner(l *fakeWriteLease) WriteTxRunner {
	return &writeTxRunner{acquire: func(context.Context) (writeTxLease, error) { l.events = append(l.events, "acquire"); return l, nil }}
}

func TestWriteTxRunnerSuccessOrderAndBinding(t *testing.T) {
	ctx := context.Background()
	opts := pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: pgx.ReadWrite}
	l := newFakeWriteLease()
	err := fakeRunner(l).WithWriteTx(ctx, opts, func(q *Queries) error {
		l.events = append(l.events, "callback")
		l.tx.callbacks++
		_, e := q.db.Exec(ctx, "UPDATE x")
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if l.gotContext != ctx || l.gotOptions != opts {
		t.Fatal("context/options not forwarded")
	}
	assertEvents(t, l.events, "acquire", "begin", "entry", "callback", "callback-query", "finish", "commit", "release")
	assertCounts(t, l, 1, 0, 1, 0, 1, 1)
}

func TestWriteTxRunnerEntryFailureDisposition(t *testing.T) {
	tests := []struct {
		name             string
		entry, rollback  error
		release, destroy int
	}{
		{"55000", &pgconn.PgError{Code: "55000"}, nil, 1, 0}, {"AM001", fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: "AM001"}), nil, 0, 1},
		{"unknown", errors.New("transport"), nil, 0, 1}, {"cleanup unknown", &pgconn.PgError{Code: "55000"}, errors.New("rollback"), 0, 1}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newFakeWriteLease()
			l.tx.entryErr = tt.entry
			l.tx.rollbackErr = tt.rollback
			calls := 0
			err := fakeRunner(l).ExecWrite(context.Background(), func(*Queries) error { calls++; return nil })
			if !errors.Is(err, tt.entry) || calls != 0 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
			if l.releases != tt.release || l.destroys != tt.destroy {
				t.Fatalf("release=%d destroy=%d", l.releases, l.destroys)
			}
			assertEvents(t, l.events, "acquire", "begin", "entry", "rollback", disposition(tt.destroy))
		})
	}
}

func TestWriteTxRunnerCallbackFailureDispositionAndCause(t *testing.T) {
	tests := []struct {
		name               string
		callback, rollback error
		release, destroy   int
	}{
		{"ordinary", errors.New("callback"), nil, 1, 0}, {"canceled", context.Canceled, nil, 1, 0},
		{"poison", fmt.Errorf("mutate: %w", &pgconn.PgError{Code: "AM001"}), nil, 0, 1}, {"cleanup unknown", errors.New("callback"), errors.New("rollback"), 0, 1}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newFakeWriteLease()
			l.tx.rollbackErr = tt.rollback
			err := fakeRunner(l).ExecWrite(context.Background(), func(*Queries) error { return tt.callback })
			if !errors.Is(err, tt.callback) {
				t.Fatalf("lost primary: %v", err)
			}
			var want, got *pgconn.PgError
			if errors.As(tt.callback, &want) && (!errors.As(err, &got) || got.Code != want.Code) {
				t.Fatalf("lost PgError: %v", err)
			}
			if l.releases != tt.release || l.destroys != tt.destroy {
				t.Fatalf("release=%d destroy=%d", l.releases, l.destroys)
			}
			assertEvents(t, l.events, "acquire", "begin", "entry", "rollback", disposition(tt.destroy))
		})
	}
}

func TestWriteTxRunnerFinishAndCommitFailuresDestroyWithoutReplay(t *testing.T) {
	t.Run("finish", func(t *testing.T) {
		l := newFakeWriteLease()
		want := fmt.Errorf("finish: %w", &pgconn.PgError{Code: "08006"})
		l.tx.finishErr = want
		err := fakeRunner(l).ExecWrite(context.Background(), func(*Queries) error { return nil })
		assertCause(t, err, want, "08006")
		assertEvents(t, l.events, "acquire", "begin", "entry", "finish", "rollback", "destroy")
		assertCounts(t, l, 0, 1, 1, 1, 0, 0)
	})
	t.Run("commit", func(t *testing.T) {
		l := newFakeWriteLease()
		want := fmt.Errorf("commit: %w", &pgconn.PgError{Code: "08006"})
		l.tx.commitErr = want
		calls := 0
		err := fakeRunner(l).ExecWrite(context.Background(), func(*Queries) error { calls++; return nil })
		assertCause(t, err, want, "08006")
		if calls != 1 {
			t.Fatalf("callbacks=%d", calls)
		}
		assertEvents(t, l.events, "acquire", "begin", "entry", "finish", "commit", "destroy")
		assertCounts(t, l, 0, 1, 1, 0, 0, 1)
	})
}

func TestWriteTxRunnerPreservesPrimaryBeforeRetirementFailure(t *testing.T) {
	primary := errors.New("callback failed")
	retirement := errors.New("physical retirement failed")
	l := newFakeWriteLease()
	l.tx.rollbackErr = errors.New("rollback response unknown")
	l.destroyErr = retirement

	err := fakeRunner(l).ExecWrite(context.Background(), func(*Queries) error { return primary })
	if !errors.Is(err, primary) || !errors.Is(err, retirement) {
		t.Fatalf("error=%v, want primary and retirement errors", err)
	}
	if !strings.HasPrefix(err.Error(), primary.Error()) {
		t.Fatalf("error=%q, want primary error first", err)
	}
	assertEvents(t, l.events, "acquire", "begin", "entry", "rollback", "destroy")
}

func TestWriteTxRunnerPreservesEntryAndFinishCleanupFailures(t *testing.T) {
	for _, stage := range []string{"entry", "finish"} {
		t.Run(stage, func(t *testing.T) {
			primary := errors.New(stage + " failed")
			rollback := errors.New("rollback response unknown")
			retirement := errors.New("physical retirement failed")
			l := newFakeWriteLease()
			l.tx.rollbackErr = rollback
			l.destroyErr = retirement
			if stage == "entry" {
				l.tx.entryErr = primary
			} else {
				l.tx.finishErr = primary
			}

			err := fakeRunner(l).ExecWrite(context.Background(), func(*Queries) error { return nil })
			for _, want := range []error{primary, rollback, retirement} {
				if !errors.Is(err, want) {
					t.Fatalf("error=%v, want cause %v", err, want)
				}
			}
			primaryAt := strings.Index(err.Error(), primary.Error())
			cleanupAt := strings.Index(err.Error(), rollback.Error())
			if primaryAt < 0 || cleanupAt < 0 || primaryAt > cleanupAt {
				t.Fatalf("error=%q, want primary error before cleanup", err)
			}
		})
	}
}

func TestWriteTxRunnerCleanupIsIndependentBoundedAndShared(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	l := newFakeWriteLease()
	l.tx.entryErr = fmt.Errorf("poison: %w", &pgconn.PgError{Code: "AM001"})
	if err := fakeRunner(l).ExecWrite(ctx, func(*Queries) error { return nil }); err == nil {
		t.Fatal("expected error")
	}
	if l.tx.rollbackErrAtCall != nil || l.destroyErrAtCall != nil {
		t.Fatalf("canceled cleanup: %v %v", l.tx.rollbackErrAtCall, l.destroyErrAtCall)
	}
	if !l.tx.rollbackHasDeadline || !l.destroyHasDeadline || !l.tx.rollbackDeadline.Equal(l.destroyDeadline) {
		t.Fatal("cleanup did not share deadline")
	}
	d := time.Until(l.destroyDeadline)
	if d <= 0 || d > 5*time.Second {
		t.Fatalf("deadline=%v", d)
	}
}

func TestWriteTxRunnerPanicCleansUpAndRethrows(t *testing.T) {
	p := &struct{ v string }{"panic"}
	l := newFakeWriteLease()
	func() {
		defer func() {
			if got := recover(); got != p {
				t.Fatalf("panic=%#v", got)
			}
		}()
		if err := fakeRunner(l).ExecWrite(context.Background(), func(*Queries) error { panic(p) }); err != nil {
			t.Fatal(err)
		}
	}()
	assertEvents(t, l.events, "acquire", "begin", "entry", "rollback", "release")
	assertCounts(t, l, 1, 0, 0, 1, 0, 0)
}

func TestWriteTxRunnerGoexitCleansUpLease(t *testing.T) {
	l := newFakeWriteLease()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := fakeRunner(l).ExecWrite(context.Background(), func(*Queries) error {
			runtime.Goexit()
			return nil
		}); err != nil {
			panic(err)
		}
		panic("ExecWrite returned after runtime.Goexit")
	}()
	<-done
	assertEvents(t, l.events, "acquire", "begin", "entry", "rollback", "release")
	assertCounts(t, l, 1, 0, 0, 1, 0, 0)
}

func TestWriteTxRunnerPanicDisposition(t *testing.T) {
	tests := []struct {
		name             string
		panicValue       error
		rollbackErr      error
		release, destroy int
	}{
		{"wrapped AM001", fmt.Errorf("callback panic: %w", &pgconn.PgError{Code: "AM001"}), nil, 0, 1},
		{"rollback failure", errors.New("callback panic"), errors.New("rollback failed"), 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newFakeWriteLease()
			l.tx.rollbackErr = tt.rollbackErr
			func() {
				defer func() {
					got, ok := recover().(error)
					if !ok || !errors.Is(got, tt.panicValue) {
						t.Fatalf("panic=%#v, want original %#v", got, tt.panicValue)
					}
				}()
				if err := fakeRunner(l).ExecWrite(context.Background(), func(*Queries) error { panic(tt.panicValue) }); err != nil {
					t.Fatalf("ExecWrite() returned after panic: %v", err)
				}
			}()
			assertEvents(t, l.events, "acquire", "begin", "entry", "rollback", "destroy")
			assertCounts(t, l, 0, 1, 0, 1, 0, 0)
		})
	}
}

func TestWriteTxRunnerFinishAndCommitDisposition(t *testing.T) {
	tests := []struct {
		name       string
		finishErr  error
		commitErr  error
		wantEvents []string
	}{
		{"finish AM001", fmt.Errorf("finish: %w", &pgconn.PgError{Code: "AM001"}), nil, []string{"acquire", "begin", "entry", "finish", "rollback", "destroy"}},
		{"finish 55000", &pgconn.PgError{Code: "55000"}, nil, []string{"acquire", "begin", "entry", "finish", "rollback", "destroy"}},
		{"canceled finish", context.Canceled, nil, []string{"acquire", "begin", "entry", "finish", "rollback", "destroy"}},
		{"canceled commit", nil, context.Canceled, []string{"acquire", "begin", "entry", "finish", "commit", "destroy"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newFakeWriteLease()
			l.tx.finishErr = tt.finishErr
			l.tx.commitErr = tt.commitErr
			err := fakeRunner(l).ExecWrite(context.Background(), func(*Queries) error { return nil })
			wantErr := tt.finishErr
			if wantErr == nil {
				wantErr = tt.commitErr
			}
			if !errors.Is(err, wantErr) {
				t.Fatalf("error=%v, want %v", err, wantErr)
			}
			assertEvents(t, l.events, tt.wantEvents...)
			if l.releases != 0 || l.destroys != 1 {
				t.Fatalf("release=%d destroy=%d, want 0,1", l.releases, l.destroys)
			}
		})
	}
}

func TestWriteTxRunnerBeginAcquireAndNilFailures(t *testing.T) {
	want := errors.New("acquire")
	var r WriteTxRunner = &writeTxRunner{acquire: func(context.Context) (writeTxLease, error) { return nil, want }}
	if err := r.ExecWrite(context.Background(), func(*Queries) error { return nil }); !errors.Is(err, want) {
		t.Fatal(err)
	}
	l := newFakeWriteLease()
	l.beginErr = errors.New("begin")
	if err := fakeRunner(l).ExecWrite(context.Background(), func(*Queries) error { return nil }); !errors.Is(err, l.beginErr) {
		t.Fatal(err)
	}
	assertEvents(t, l.events, "acquire", "begin", "destroy")
	l = newFakeWriteLease()
	r = fakeRunner(l)
	if err := r.ExecWrite(context.Background(), nil); err == nil {
		t.Fatal("nil callback accepted")
	}
	if len(l.events) != 0 {
		t.Fatalf("events=%v", l.events)
	}
	if err := NewWriteTxRunner(nil).ExecWrite(context.Background(), func(*Queries) error { return nil }); err == nil {
		t.Fatal("nil pool accepted")
	}
}

func TestWriteTxRunnerExecUsesDefaultOptions(t *testing.T) {
	l := newFakeWriteLease()
	if err := fakeRunner(l).ExecWrite(context.Background(), func(*Queries) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if l.gotOptions != (pgx.TxOptions{}) {
		t.Fatalf("options=%#v", l.gotOptions)
	}
}
func disposition(d int) string {
	if d == 1 {
		return "destroy"
	}
	return "release"
}
func assertEvents(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events=%v want=%v", got, want)
	}
}
func assertCounts(t *testing.T, l *fakeWriteLease, r, d, f, rb, cb, c int) {
	t.Helper()
	if l.releases != r || l.destroys != d || l.tx.finishes != f || l.tx.rollbacks != rb || l.tx.callbacks != cb || l.tx.commits != c {
		t.Fatalf("counts=%d,%d,%d,%d,%d,%d want=%d,%d,%d,%d,%d,%d", l.releases, l.destroys, l.tx.finishes, l.tx.rollbacks, l.tx.callbacks, l.tx.commits, r, d, f, rb, cb, c)
	}
}
func assertCause(t *testing.T, got, want error, code string) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("error=%v want=%v", got, want)
	}
	var p *pgconn.PgError
	if !errors.As(got, &p) || p.Code != code {
		t.Fatalf("lost PgError: %v", got)
	}
}
