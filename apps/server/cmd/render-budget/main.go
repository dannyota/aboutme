// Command render-budget measures the production print queue and browser
// controller against the fixed local Phase 7 fixture protocol.
//
// Usage:
//
//	render-budget --repository-root /absolute/aboutme \
//	  --chromium-executable /absolute/chromium \
//	  --output-directory /absolute/aboutme/.dev/phase-7/render-budget [--probe]
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	settings, err := parseConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "render-budget:", err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, settings); err != nil {
		fmt.Fprintln(os.Stderr, "render-budget:", err)
		os.Exit(1)
	}
}
