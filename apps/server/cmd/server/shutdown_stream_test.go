package main

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/realtime"
	"danny.vn/aboutme/apps/server/internal/realtimeapi"
	"danny.vn/aboutme/apps/server/internal/store"
)

type liveResumeStore struct {
	resumeID uuid.UUID
}

func (s liveResumeStore) GetPublicRealtimeResume(context.Context, string) (store.GetPublicRealtimeResumeRow, error) {
	return store.GetPublicRealtimeResumeRow{ID: s.resumeID, Revision: 1}, nil
}

func (liveResumeStore) GetSessionByID(context.Context, uuid.UUID) (store.Session, error) {
	return store.Session{}, io.EOF
}

func TestServeUntilStoppedDoesNotHoldTheDrainForAnOpenRealtimeStream(t *testing.T) {
	budget := stopBudget{drain: 3 * time.Second, join: 3 * time.Second, flush: 3 * time.Second, total: 3 * time.Second}
	hub, err := realtime.NewHub(realtime.Config{AdmitFD: func() bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	hub.SetAvailable(true)
	t.Cleanup(hub.Close)
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	streams, err := realtimeapi.New(realtimeapi.Dependencies{
		Hub: hub, Store: liveResumeStore{resumeID: uuid.New()}, Sessions: auth.NewSessionManager(nil), Coordinator: coordinator,
	})
	if err != nil {
		t.Fatal(err)
	}

	var listenConfig net.ListenConfig
	publicListener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	printListener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	noop := func(context.Context) error { return nil }
	steps := stopSteps{
		closeStreams:  hub.Close,
		stopRendering: noop, stopEvidence: noop, stopWorkers: func() {}, flushViews: noop,
		closePool: func(context.Context) {},
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- serveUntilStopped(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), budget,
			publicListener, streams.PublicHandler(), printListener, http.NotFoundHandler(), steps)
	}()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+publicListener.Addr().String()+"/api/v1/live/test-resume", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", response.StatusCode)
	}
	heartbeat, err := bufio.NewReader(response.Body).ReadString('\n')
	if err != nil || !strings.HasPrefix(heartbeat, "event: heartbeat") {
		t.Fatalf("first stream line = %q, %v", heartbeat, err)
	}

	signal := time.Now()
	cancel()
	select {
	case serveErr := <-done:
		if serveErr != nil {
			t.Fatalf("serveUntilStopped error = %v, want a clean drain", serveErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not return")
	}
	if elapsed := time.Since(signal); elapsed >= time.Second {
		t.Fatalf("an open stream held the drain for %v", elapsed)
	}
}
