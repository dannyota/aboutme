package main

import (
	"context"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
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
