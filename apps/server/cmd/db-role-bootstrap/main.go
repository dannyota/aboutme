// Command db-role-bootstrap creates or verifies aboutme's fixed cluster roles.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/dannyota/aboutme/apps/server/internal/dbroles"
)

const databaseURLEnv = "CLUSTER_BOOTSTRAP_DATABASE_URL"

var (
	errArguments     = errors.New("arguments are not accepted")
	errConfiguration = errors.New("invalid cluster bootstrap database configuration")
	errDatabase      = errors.New("database role bootstrap failed")
)

type bootstrapFunc func(context.Context, string) (dbroles.Result, error)

func main() {
	if err := run(os.Args[1:], os.Getenv, os.Stdout, ensureURL); err != nil {
		fmt.Fprintln(os.Stderr, "db-role-bootstrap:", err)
		os.Exit(1)
	}
}

func run(args []string, getenv func(string) string, stdout io.Writer, bootstrap bootstrapFunc) error {
	if len(args) != 0 {
		return errArguments
	}
	databaseURL := strings.TrimSpace(getenv(databaseURLEnv))
	if !validPostgresURL(databaseURL) {
		return errConfiguration
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := bootstrap(ctx, databaseURL)
	if err != nil {
		return errDatabase
	}
	outcome := "verified"
	if result.Created > 0 {
		outcome = "created"
	}
	if _, err := fmt.Fprintf(stdout, "outcome=%s created=%d verified=%d\n", outcome, result.Created, result.Verified); err != nil {
		return errors.New("write bootstrap outcome failed")
	}
	return nil
}

func validPostgresURL(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		return false
	}
	config, err := pgx.ParseConfig(raw)
	return err == nil && config.Database == "postgres"
}

func ensureURL(ctx context.Context, databaseURL string) (dbroles.Result, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return dbroles.Result{}, err
	}
	cleanupDeadline := time.Now().Add(5 * time.Second)
	if deadline, ok := ctx.Deadline(); ok {
		cleanupDeadline = deadline.Add(5 * time.Second)
	}
	defer closeBy(db, cleanupDeadline)
	return dbroles.Ensure(ctx, db)
}

func closeBy(db interface{ Close() error }, deadline time.Time) {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return
	}
	if remaining > 5*time.Second {
		remaining = 5 * time.Second
	}
	done := make(chan error, 1)
	go func() { done <- db.Close() }()
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}
