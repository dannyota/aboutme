// Command password-auth-fixture seeds and cleans the deterministic Phase PA
// native-HTTPS proof state in the shared native development database
// (aboutme_dev). It is the only authority that mutates the three fixed
// fixture accounts; the browser proof creates its own runtime-random account
// and the cleanup removes both the fixed accounts and any leftover test rows.
//
// Usage:
//
//	password-auth-fixture seed    --database-url <dsn>
//	password-auth-fixture cleanup --database-url <dsn>
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
	cmd, cfg, err := parseConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "password-auth-fixture:", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := run(ctx, cmd, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "password-auth-fixture:", err)
		os.Exit(1)
	}
}
