package pgtransport

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"testing"
	"time"
)

func TestExactTCPRejectsUnsupportedAndTypedNilTransports(t *testing.T) {
	var tcp *net.TCPConn
	if _, err := exactTCP(tcp); err == nil {
		t.Fatal("typed nil TCP transport was accepted")
	}
	var tlsConn *tls.Conn
	if _, err := exactTCP(tlsConn); err == nil {
		t.Fatal("typed nil TLS transport was accepted")
	}
	wrappedNilTCP := tls.Client(tcp, &tls.Config{MinVersion: tls.VersionTLS12})
	if _, err := exactTCP(wrappedNilTCP); err == nil {
		t.Fatal("TLS transport over typed nil TCP was accepted")
	}
	left, right := net.Pipe()
	t.Cleanup(func() {
		if err := left.Close(); err != nil {
			t.Logf("close pipe: %v", err)
		}
	})
	t.Cleanup(func() {
		if err := right.Close(); err != nil {
			t.Logf("close pipe: %v", err)
		}
	})
	if _, err := exactTCP(left); err == nil {
		t.Fatal("wrapped transport was accepted")
	}
}

func TestCapabilityRetireClosesCapturedSocketOnce(t *testing.T) {
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := listener.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			t.Errorf("close listener: %v", closeErr)
		}
	})
	accepted := make(chan *net.TCPConn, 1)
	go func() {
		conn, acceptErr := listener.AcceptTCP()
		if acceptErr == nil {
			accepted <- conn
		}
	}()
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address type %T", listener.Addr())
	}
	client, err := net.DialTCP("tcp", nil, address)
	if err != nil {
		t.Fatal(err)
	}
	server := <-accepted
	t.Cleanup(func() {
		if err := server.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("close server connection: %v", err)
		}
	})

	capability := &Capability{tcp: client, cleanupDone: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := capability.retire(ctx, nil, nil)
	if result.Err != nil || !result.PhysicalClosed {
		t.Fatalf("retire result = %+v", result)
	}
	if second := capability.retire(ctx, nil, nil); second.Err == nil {
		t.Fatal("second retirement succeeded")
	}
}
