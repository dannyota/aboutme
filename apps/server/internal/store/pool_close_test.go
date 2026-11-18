package store_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgproto3"

	"danny.vn/aboutme/apps/server/internal/store"
)

func fakePostgres(t *testing.T) string {
	t.Helper()
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var sessions sync.WaitGroup
	t.Cleanup(func() {
		if closeErr := listener.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			t.Error(closeErr)
		}
		sessions.Wait()
	})
	sessions.Go(func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			sessions.Go(func() { serveFakeSession(t, conn) })
		}
	})
	return fmt.Sprintf("postgres://app@%s/app?sslmode=disable", listener.Addr())
}

func serveFakeSession(t *testing.T, conn net.Conn) {
	defer func() {
		if closeErr := conn.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			t.Error(closeErr)
		}
	}()
	backend := pgproto3.NewBackend(conn, conn)
	if _, err := backend.ReceiveStartupMessage(); err != nil {
		return
	}
	backend.Send(&pgproto3.AuthenticationOk{})
	backend.Send(&pgproto3.BackendKeyData{ProcessID: 1, SecretKey: []byte{0, 0, 0, 1}})
	backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	if err := backend.Flush(); err != nil {
		return
	}
	for {
		message, err := backend.Receive()
		if err != nil {
			return
		}
		if _, ok := message.(*pgproto3.Terminate); ok {
			return
		}
	}
}

func TestPoolCloseReturnsAtItsDeadlineWhileAConnectionIsCheckedOut(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	pool, err := store.NewPool(ctx, fakePostgres(t))
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	held, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	defer held.Release()

	closeCtx, cancelClose := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelClose()
	done := make(chan struct{})
	go func() {
		pool.Close(closeCtx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Pool.Close waited past its deadline for a checked-out connection")
	}
}
