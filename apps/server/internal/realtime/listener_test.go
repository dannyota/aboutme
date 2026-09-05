package realtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

type fakeListenerConn struct {
	listenFn func(context.Context) error
	waitFn   func(context.Context) (*pgconn.Notification, error)
	closeFn  func(context.Context)
}

func (c *fakeListenerConn) listen(ctx context.Context) error { return c.listenFn(ctx) }
func (c *fakeListenerConn) wait(ctx context.Context) (*pgconn.Notification, error) {
	return c.waitFn(ctx)
}
func (c *fakeListenerConn) close(ctx context.Context) { c.closeFn(ctx) }

func notification(payload string) *pgconn.Notification {
	return &pgconn.Notification{Channel: notificationChannel, Payload: payload}
}

func validNotification(revision int64) *pgconn.Notification {
	return notification(`{"account_id":"` + accountA.String() + `","resume_id":"` + resumeA.String() + `","revision":` + revisionString(revision) + `,"deleted":false}`)
}

func revisionString(revision int64) string {
	if revision == 1 {
		return "1"
	}
	if revision == 2 {
		return "2"
	}
	panic("unsupported test revision")
}

func waitSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func listenerResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(time.Second):
		t.Fatal("listener did not stop")
		return nil
	}
}

func runTestListener(ctx context.Context, hub *Hub, deps listenerDeps) <-chan error {
	result := make(chan error, 1)
	go func() { result <- runListener(ctx, &store.Pool{}, hub, deps) }()
	return result
}

func TestParseNotificationRequiresExactMetadata(t *testing.T) {
	got, err := parseNotification(validNotification(1))
	if err != nil || got != (Change{AccountID: accountA, ResumeID: resumeA, Revision: 1}) {
		t.Fatalf("parse valid = %+v, %v", got, err)
	}
	valid := `{"account_id":"` + accountA.String() + `","resume_id":"` + resumeA.String() + `","revision":1,"deleted":false}`
	tests := []*pgconn.Notification{
		nil,
		{Channel: "other", Payload: valid},
		notification(`{"account_id":"` + accountA.String() + `","resume_id":"` + resumeA.String() + `","revision":1,"deleted":false,"extra":1}`),
		notification(`{"account_id":"` + accountA.String() + `","account_id":"` + accountA.String() + `","resume_id":"` + resumeA.String() + `","revision":1,"deleted":false}`),
		notification(`{"account_id":"00000000-0000-0000-0000-000000000000","resume_id":"` + resumeA.String() + `","revision":1,"deleted":false}`),
		notification(`{"account_id":"` + accountA.String() + `","resume_id":"` + resumeA.String() + `","revision":0,"deleted":false}`),
		notification(`{"account_id":"` + accountA.String() + `","resume_id":"` + resumeA.String() + `","revision":1,"deleted":null}`),
		notification(`{"account_id":"` + accountA.String() + `","resume_id":"` + resumeA.String() + `","revision":1,"deleted":false} trailing`),
		notification(string(make([]byte, 513))),
	}
	for i, n := range tests {
		if _, err := parseNotification(n); err == nil {
			t.Errorf("case %d accepted malformed notification", i)
		}
	}
}

func TestListenerAcquireAndListenFailuresCloseStreamsBeforeBackoff(t *testing.T) {
	for _, tt := range []struct {
		name                  string
		acquireErr, listenErr error
		wantClose             bool
	}{
		{"acquire", errors.New("acquire"), nil, false},
		{"listen", nil, errors.New("listen"), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newAvailableHub(t, Config{})
			s := subscribe(t, h, Scope{AccountID: accountA, IP: "127.0.0.1"})
			closed := false
			conn := &fakeListenerConn{listenFn: func(context.Context) error { return tt.listenErr }, waitFn: func(context.Context) (*pgconn.Notification, error) {
				t.Fatal("wait called after setup failure")
				return nil, nil
			}, closeFn: func(context.Context) { closed = true }}
			ctx, cancel := context.WithCancel(context.Background())
			deps := listenerDeps{
				acquire: func(context.Context, *store.Pool) (listenerConn, error) {
					if tt.acquireErr != nil {
						return nil, tt.acquireErr
					}
					return conn, nil
				},
				backoff: func(_ context.Context, delay time.Duration) bool {
					if delay != 100*time.Millisecond {
						t.Errorf("initial backoff = %s", delay)
					}
					requireClosed(t, s.Done)
					if closed != tt.wantClose {
						t.Errorf("connection closed = %t, want %t", closed, tt.wantClose)
					}
					cancel()
					return false
				},
			}
			if err := listenerResult(t, runTestListener(ctx, h, deps)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestListenerNilAcquiredConnectionFailsClosed(t *testing.T) {
	h := newAvailableHub(t, Config{})
	s := subscribe(t, h, Scope{AccountID: accountA, IP: "127.0.0.1"})
	ctx, cancel := context.WithCancel(context.Background())
	deps := listenerDeps{
		acquire: func(context.Context, *store.Pool) (listenerConn, error) { return nil, nil },
		backoff: func(context.Context, time.Duration) bool {
			requireClosed(t, s.Done)
			cancel()
			return false
		},
	}
	if err := listenerResult(t, runTestListener(ctx, h, deps)); err != nil {
		t.Fatal(err)
	}
}

func TestListenerWaitLossAndMalformedNotificationCloseBeforeRetry(t *testing.T) {
	for _, tt := range []struct {
		name   string
		result *pgconn.Notification
		err    error
	}{
		{"wait loss", nil, errors.New("lost")},
		{"malformed", notification(`{}`), nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newAvailableHub(t, Config{})
			s := subscribe(t, h, Scope{AccountID: accountA, IP: "127.0.0.1"})
			closed := false
			conn := &fakeListenerConn{listenFn: func(context.Context) error { return nil }, waitFn: func(context.Context) (*pgconn.Notification, error) { return tt.result, tt.err }, closeFn: func(context.Context) { requireClosed(t, s.Done); closed = true }}
			ctx, cancel := context.WithCancel(context.Background())
			deps := listenerDeps{acquire: func(context.Context, *store.Pool) (listenerConn, error) { return conn, nil }, backoff: func(context.Context, time.Duration) bool {
				if !closed {
					t.Error("retry began before cleanup")
				}
				cancel()
				return false
			}}
			if err := listenerResult(t, runTestListener(ctx, h, deps)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestListenerDispatchesValidNotificationThenCancelsBlockedWait(t *testing.T) {
	h, err := NewHub(Config{AdmitFD: func() bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	waiting := make(chan struct{})
	release := make(chan struct{})
	secondWait := make(chan struct{})
	var calls int
	conn := &fakeListenerConn{listenFn: func(context.Context) error { return nil }, waitFn: func(ctx context.Context) (*pgconn.Notification, error) {
		calls++
		if calls == 1 {
			close(waiting)
			<-release
			return validNotification(2), nil
		}
		close(secondWait)
		<-ctx.Done()
		return nil, ctx.Err()
	}, closeFn: func(context.Context) {}}
	ctx, cancel := context.WithCancel(context.Background())
	result := runTestListener(ctx, h, listenerDeps{acquire: func(context.Context, *store.Pool) (listenerConn, error) { return conn, nil }, backoff: waitBackoff})
	waitSignal(t, waiting, "first notification wait")
	s := subscribe(t, h, Scope{AccountID: accountA, IP: "127.0.0.1"})
	close(release)
	if got := <-s.Events; got != (Change{AccountID: accountA, ResumeID: resumeA, Revision: 2}) {
		t.Fatalf("event = %+v", got)
	}
	waitSignal(t, secondWait, "blocked notification wait")
	cancel()
	if err := listenerResult(t, result); err != nil {
		t.Fatal(err)
	}
	requireClosed(t, s.Done)
}

func TestListenerLossClosesOldStreamsAndReconnectAllowsNewSubscription(t *testing.T) {
	h, err := NewHub(Config{AdmitFD: func() bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	firstWaiting := make(chan struct{})
	loseFirst := make(chan struct{})
	secondWaiting := make(chan struct{})
	connections := 0
	ctx, cancel := context.WithCancel(context.Background())
	deps := listenerDeps{
		acquire: func(context.Context, *store.Pool) (listenerConn, error) {
			connections++
			if connections == 1 {
				return &fakeListenerConn{
					listenFn: func(context.Context) error { return nil },
					waitFn: func(context.Context) (*pgconn.Notification, error) {
						close(firstWaiting)
						<-loseFirst
						return nil, errors.New("lost")
					},
					closeFn: func(context.Context) {},
				}, nil
			}
			return &fakeListenerConn{
				listenFn: func(context.Context) error { return nil },
				waitFn: func(ctx context.Context) (*pgconn.Notification, error) {
					close(secondWaiting)
					<-ctx.Done()
					return nil, ctx.Err()
				},
				closeFn: func(context.Context) {},
			}, nil
		},
		backoff: func(context.Context, time.Duration) bool { return true },
	}
	result := runTestListener(ctx, h, deps)
	waitSignal(t, firstWaiting, "first listener wait")
	old := subscribe(t, h, Scope{AccountID: accountA, IP: "127.0.0.1"})
	close(loseFirst)
	waitSignal(t, secondWaiting, "reconnected listener wait")
	requireClosed(t, old.Done)
	newSubscription := subscribe(t, h, Scope{AccountID: accountA, IP: "127.0.0.1"})
	cancel()
	if err := listenerResult(t, result); err != nil {
		t.Fatal(err)
	}
	requireClosed(t, newSubscription.Done)
}

func TestListenerBackoffGrowsAcrossImmediateListenFailuresAndResetsAfterNotification(t *testing.T) {
	h, err := NewHub(Config{AdmitFD: func() bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	ctx, cancel := context.WithCancel(context.Background())
	delays := make([]time.Duration, 0, 4)
	attempt := 0
	deps := listenerDeps{
		acquire: func(context.Context, *store.Pool) (listenerConn, error) {
			attempt++
			thisAttempt := attempt
			waits := 0
			return &fakeListenerConn{
				listenFn: func(context.Context) error {
					if thisAttempt <= 2 {
						return errors.New("listen")
					}
					return nil
				},
				waitFn: func(context.Context) (*pgconn.Notification, error) {
					waits++
					if thisAttempt == 3 && waits == 1 {
						return validNotification(1), nil
					}
					return nil, errors.New("lost")
				},
				closeFn: func(context.Context) {},
			}, nil
		},
		backoff: func(context.Context, time.Duration) bool { return true },
	}
	deps.backoff = func(_ context.Context, delay time.Duration) bool {
		delays = append(delays, delay)
		if len(delays) == 4 {
			cancel()
			return false
		}
		return true
	}
	if err := listenerResult(t, runTestListener(ctx, h, deps)); err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond}
	for i := range want {
		if delays[i] != want[i] {
			t.Fatalf("backoff[%d] = %s, want %s; all=%v", i, delays[i], want[i], delays)
		}
	}
}

func TestRunListenerRejectsNilDependencies(t *testing.T) {
	h, err := NewHub(Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := RunListener(context.Background(), nil, h); err == nil {
		t.Error("nil pool accepted")
	}
	if err := RunListener(context.Background(), &store.Pool{}, nil); err == nil {
		t.Error("nil hub accepted")
	}
}

func TestListenerCancellationDuringBackoffDoesNotRetry(t *testing.T) {
	h, err := NewHub(Config{AdmitFD: func() bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	deps := listenerDeps{
		acquire: func(context.Context, *store.Pool) (listenerConn, error) { attempts++; return nil, errors.New("down") },
		backoff: func(context.Context, time.Duration) bool { cancel(); return false },
	}
	if err := listenerResult(t, runTestListener(ctx, h, deps)); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("acquire attempts = %d, want 1", attempts)
	}
}
