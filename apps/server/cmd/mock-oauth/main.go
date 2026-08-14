// Command mock-oauth runs the local-only Google OIDC provider for the native
// HTTPS authentication proof.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/uatmock"
)

const (
	listenHost         = "127.0.0.1"
	listenPort         = "20442"
	publicOrigin       = "https://localhost:20443"
	googleClientID     = "aboutme-local-google"
	googleClientSecret = "not-a-secret-local-google"
	shutdownTimeout    = 5 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	svc, err := uatmock.New(cfg)
	if err != nil {
		return fmt.Errorf("create OAuth mock: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", net.JoinHostPort(listenHost, listenPort))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	return serve(ctx, ln, svc.Handler())
}

func loadConfig(getenv func(string) string) (uatmock.Config, error) {
	expected := []struct {
		name  string
		value string
	}{
		{name: "LISTEN_HOST", value: listenHost},
		{name: "PORT", value: listenPort},
		{name: "PUBLIC_ORIGIN", value: publicOrigin},
		{name: "GOOGLE_CLIENT_ID", value: googleClientID},
		{name: "GOOGLE_CLIENT_SECRET", value: googleClientSecret},
	}
	for _, item := range expected {
		if getenv(item.name) != item.value {
			return uatmock.Config{}, fmt.Errorf("mock-oauth: %s does not match the native HTTPS harness", item.name)
		}
	}
	return uatmock.Config{
		IssuerURL:    "http://" + net.JoinHostPort(listenHost, listenPort) + "/google",
		PublicOrigin: publicOrigin,
		RedirectURL:  publicOrigin + "/api/v1/auth/google/callback",
		ClientID:     googleClientID,
		ClientSecret: googleClientSecret,
		Now:          time.Now,
		Random:       rand.Reader,
	}, nil
}

func serve(ctx context.Context, ln net.Listener, handler http.Handler) error {
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	done := make(chan error, 1)
	go func() {
		err := srv.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	//nolint:contextcheck // ctx is already canceled, so the drain needs its own timeout context.
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return <-done
}
