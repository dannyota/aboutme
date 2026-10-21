// Command render-budget exports synthetic fixtures offline or measures the
// production print queue and browser controller against the fixed fixture protocol.
//
// Usage:
//
//	render-budget --repository-root /absolute/aboutme \
//	  --chromium-executable /absolute/chromium \
//	  --output-directory /absolute/aboutme/.dev/phase-7/render-budget [--probe]
//	render-budget --repository-root /absolute/aboutme --export-fixtures /absolute/new-directory
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
	if settings.ExportFixtures != "" {
		if err := exportFixtures(settings.RepositoryRoot, settings.ExportFixtures); err != nil {
			fmt.Fprintln(os.Stderr, "render-budget:", err)
			os.Exit(1)
		}
		if _, outputErr := fmt.Fprintln(os.Stdout, "render-budget: fixtures_exported"); outputErr != nil {
			os.Exit(1)
		}
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, settings); err != nil {
		fmt.Fprintln(os.Stderr, "render-budget:", err)
		os.Exit(1)
	}
}
