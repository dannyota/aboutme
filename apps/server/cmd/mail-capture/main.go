// Command mail-capture runs the local, loopback-only authentication-email
// capture server used by native and HTTPS development (D7). It binds
// 127.0.0.1:20091 by default, requires a 32-byte bearer secret read from a
// mode-0600 harness-state file, and never logs the secret.
//
// Usage:
//
//	mail-capture --secret-file <path> [--addr 127.0.0.1:20091]
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/mailcapture"
)

const (
	defaultAddr       = mailcapture.NativeAddr
	secretLen         = 32
	shutdownTimeout   = 5 * time.Second
	readHeaderTimeout = 5 * time.Second
)

type config struct {
	secretFile string
	addr       string
}

func main() {
	cfg, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "mail-capture:", err)
		os.Exit(2)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(cfg, logger); err != nil {
		fmt.Fprintln(os.Stderr, "mail-capture:", err)
		os.Exit(1)
	}
}

// parseArgs parses and validates the command line. addr must be a loopback
// host, because the capture server never binds a wildcard or non-loopback
// address (D7).
func parseArgs(args []string) (config, error) {
	fs := flag.NewFlagSet("mail-capture", flag.ContinueOnError)
	var (
		secretFile = fs.String("secret-file", "", "path to the 32-byte capture secret (mode 0600)")
		addr       = fs.String("addr", defaultAddr, "loopback listen address")
	)
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if fs.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if *secretFile == "" {
		return config{}, errors.New("--secret-file is required")
	}
	host, _, err := net.SplitHostPort(*addr)
	if err != nil {
		return config{}, fmt.Errorf("invalid --addr %q", *addr)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return config{}, fmt.Errorf("--addr %q must be a loopback address", *addr)
	}
	return config{secretFile: *secretFile, addr: *addr}, nil
}

// readSecret reads the mode-0600 harness-state secret file and requires exactly
// 32 bytes. It never echoes the bytes.
func readSecret(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read secret file: %w", err)
	}
	if len(b) != secretLen {
		return nil, fmt.Errorf("secret file %s has %d bytes, want %d", path, len(b), secretLen)
	}
	return b, nil
}

// run starts the capture server and blocks until SIGINT/SIGTERM, then shuts
// down gracefully.
func run(cfg config, logger *slog.Logger) error {
	secret, err := readSecret(cfg.secretFile)
	if err != nil {
		return err
	}
	server, err := mailcapture.NewServer(secret, logger)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.addr, err)
	}

	httpServer := &http.Server{
		Handler:           server.Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	done := make(chan error, 1)
	go func() {
		logger.Info("mail-capture: serving", "addr", ln.Addr().String())
		done <- httpServer.Serve(ln)
	}()

	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	logger.Info("mail-capture: shutting down")
	return httpServer.Shutdown(shutdownCtx)
}
