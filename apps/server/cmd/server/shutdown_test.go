package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/renderjob"
)

type stuckRenderer struct {
	started chan struct{}
	release chan struct{}
}

func (r stuckRenderer) Render(context.Context, renderjob.Navigation) ([]byte, error) {
	close(r.started)
	<-r.release
	return nil, errors.New("late")
}

type stepLog struct {
	mu        sync.Mutex
	order     []string
	deadlines map[string]time.Time
}

func (l *stepLog) hold(ctx context.Context, name string) {
	deadline, ok := ctx.Deadline()
	l.mu.Lock()
	l.order = append(l.order, name)
	if ok {
		l.deadlines[name] = deadline
	}
	l.mu.Unlock()
	<-ctx.Done()
}

func (l *stepLog) blockUntilDeadline(name string) func(context.Context) error {
	return func(ctx context.Context) error {
		l.hold(ctx, name)
		return ctx.Err()
	}
}

func TestServeUntilStoppedEndsWithinTheBudgetDespiteAStuckConnectionAndRender(t *testing.T) {
	budget := stopBudget{
		drain: 200 * time.Millisecond, join: 300 * time.Millisecond,
		flush: 450 * time.Millisecond, total: 600 * time.Millisecond,
	}
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	var listenConfig net.ListenConfig
	publicListener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	printListener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	handlerStarted := make(chan struct{})
	publicHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(handlerStarted)
		<-release
	})

	renderer := stuckRenderer{started: make(chan struct{}), release: release}
	queue, err := renderjob.New(renderjob.Config{Renderer: renderer})
	if err != nil {
		t.Fatal(err)
	}
	renderDone := make(chan error, 1)
	go func() {
		_, renderErr := queue.Render(context.Background(), renderjob.Request{Format: renderjob.PDF, Prepare: func(context.Context) (renderjob.Snapshot, error) {
			return renderjob.Snapshot{
				ResumeID: uuid.New(), Revision: 1, SchemaVersion: 1, Payload: []byte("{}"), RevisionTime: time.Now(),
			}, nil
		}})
		renderDone <- renderErr
	}()
	<-renderer.started

	recorded := &stepLog{deadlines: map[string]time.Time{}}
	stuckWorker := make(chan struct{})
	steps := stopSteps{
		stopRendering: queue.Shutdown,
		stopEvidence:  recorded.blockUntilDeadline("evidence"),
		stopWorkers:   func() {},
		joins:         []<-chan struct{}{stuckWorker},
		flushViews:    recorded.blockUntilDeadline("flush"),
		closePool:     func(ctx context.Context) { recorded.hold(ctx, "pool") },
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- serveUntilStopped(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), budget,
			publicListener, publicHandler, printListener, http.NotFoundHandler(), steps)
	}()
	clientDone := make(chan error, 1)
	go func() {
		request, requestErr := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+publicListener.Addr().String()+"/", nil)
		if requestErr != nil {
			clientDone <- requestErr
			return
		}
		response, doErr := http.DefaultClient.Do(request)
		if doErr == nil {
			doErr = errors.Join(errors.New("stuck request completed"), response.Body.Close())
		}
		clientDone <- doErr
	}()
	<-handlerStarted

	signal := time.Now()
	cancel()
	select {
	case serveErr := <-done:
		if !errors.Is(serveErr, context.DeadlineExceeded) {
			t.Fatalf("serveUntilStopped error = %v, want the drain deadline", serveErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown outlived its budget")
	}
	if elapsed := time.Since(signal); elapsed > budget.total+300*time.Millisecond {
		t.Fatalf("shutdown took %v, budget %v", elapsed, budget.total)
	}
	select {
	case clientErr := <-clientDone:
		if clientErr == nil {
			t.Fatal("the stuck request reported no error")
		}
	case <-time.After(time.Second):
		t.Fatal("the stuck connection stayed open after the drain deadline")
	}
	select {
	case renderErr := <-renderDone:
		t.Fatalf("the stuck render returned during shutdown: %v", renderErr)
	default:
	}
	if readyErr := queue.Ready(); !errors.Is(readyErr, renderjob.ErrClosed) {
		t.Fatalf("print queue Ready() = %v, want ErrClosed", readyErr)
	}

	recorded.mu.Lock()
	defer recorded.mu.Unlock()
	if got := len(recorded.order); got != 3 || recorded.order[0] != "evidence" || recorded.order[1] != "flush" || recorded.order[2] != "pool" {
		t.Fatalf("step order = %v, want evidence, flush, pool", recorded.order)
	}
	limits := map[string]time.Duration{"evidence": budget.join, "flush": budget.flush, "pool": budget.total}
	for name, limit := range limits {
		deadline, ok := recorded.deadlines[name]
		if !ok {
			t.Fatalf("%s ran without a deadline", name)
		}
		if deadline.After(signal.Add(limit + 100*time.Millisecond)) {
			t.Fatalf("%s deadline %v after the signal, want at most %v", name, deadline.Sub(signal), limit)
		}
	}
	if !recorded.deadlines["flush"].Before(recorded.deadlines["pool"]) {
		t.Fatal("the final flush may still write after the pool closes")
	}
}
