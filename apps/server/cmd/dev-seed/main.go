// Command dev-seed creates one signed-in-ready development account with a
// sample resume in the native development database (aboutme_dev). It is
// idempotent, refuses every other database, and never runs in Compose or the
// cloud (docs/design/security.md, "No operator surface").
//
// Usage:
//
//	dev-seed seed    --database-url <dsn>
//	dev-seed cleanup --database-url <dsn>
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	cmd, cfg, err := parseConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "dev-seed:", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := run(ctx, cmd, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "dev-seed:", err)
		os.Exit(1)
	}
}
