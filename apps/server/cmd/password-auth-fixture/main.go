// Command password-auth-fixture seeds and cleans native HTTPS proof state in
// the shared native development database (aboutme_dev). It prepares three
// fixed accounts and the MCP proof's runtime-random reserved account. Browser
// proofs create their own runtime-random accounts. Cleanup removes the fixed
// accounts and reserved test rows.
//
// Usage:
//
//	password-auth-fixture seed    --database-url <dsn>
//	password-auth-fixture mcp-seed --database-url <dsn> --credential-file <path>
//	password-auth-fixture cleanup --database-url <dsn>
//	password-auth-fixture linkedin-cleanup --database-url <dsn>
//
// linkedin-cleanup removes the rows the LinkedIn browser proof creates.
// mcp-seed requires ENV=dev and the MCP proof's private credential file.
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
