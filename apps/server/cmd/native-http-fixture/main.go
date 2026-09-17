// Command native-http-fixture seeds and cleans the deterministic
// public-render fixture that make native-http-check captures against. It is
// the only authority that mutates fixture rows; the capture script creates
// and migrates the fixture database before seeding and drops it after
// cleanup.
//
// Usage:
//
//	native-http-fixture seed    --database-url <dsn> --media-root <dir> --now 2035-01-01T00:00:00Z
//	native-http-fixture cleanup --database-url <dsn> --media-root <dir> --now 2035-01-01T00:00:00Z
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	// Registers the pgx driver under database/sql's "pgx" name.
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	// The capture script invokes this command from the repository root, so the
	// working directory is the root the media root is resolved against.
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "native-http-fixture: get working directory:", err)
		os.Exit(1)
	}

	cmd, cfg, err := parseConfig(os.Args[1:], root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "native-http-fixture:", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := run(ctx, cmd, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "native-http-fixture:", err)
		os.Exit(1)
	}
}
