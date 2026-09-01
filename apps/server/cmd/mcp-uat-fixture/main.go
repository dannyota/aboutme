// Command mcp-uat-fixture seeds and cleans the one reserved local identity
// used by the native HTTPS MCP proof.
//
// Usage:
//
//	mcp-uat-fixture seed    --database-url <dsn>
//	mcp-uat-fixture cleanup --database-url <dsn>
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
		fmt.Fprintln(os.Stderr, "mcp-uat-fixture:", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := run(ctx, cmd, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "mcp-uat-fixture:", err)
		os.Exit(1)
	}
}
