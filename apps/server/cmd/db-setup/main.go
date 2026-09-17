// Command db-setup prepares a database for the aboutme_migrator and
// aboutme_app roles. Run it as the database owner with DATABASE_URL. It is
// idempotent: it creates missing roles, fails on drift in existing ones, and
// grants their database and schema privileges. If MIGRATOR_PASSWORD and
// APP_PASSWORD are both set, it also stores their SCRAM verifiers. If only
// one is set, it fails before connecting. See ADR 0038.
package main

import (
	"context"
	"crypto/rand"
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

var (
	errArguments        = errors.New("arguments are not accepted")
	errConfiguration    = errors.New("invalid DATABASE_URL configuration")
	errPartialPasswords = errors.New("MIGRATOR_PASSWORD and APP_PASSWORD must both be set, or both left unset")
	errDatabase         = errors.New("database setup failed")
	errLogin            = errors.New("setting login verifiers failed")
)

type ensureFunc func(context.Context, string) (dbroles.Result, error)
type setLoginFunc func(context.Context, string, dbroles.LoginPasswords) error

func main() {
	if err := run(os.Args[1:], os.Getenv, os.Stdout, ensureURL, setLoginURL); err != nil {
		fmt.Fprintln(os.Stderr, "db-setup:", err)
		os.Exit(1)
	}
}

// run reads DATABASE_URL and the optional passwords, then prints one
// outcome line. Its output and errors never contain a password or the DSN.
func run(args []string, getenv func(string) string, stdout io.Writer, ensure ensureFunc, setLogin setLoginFunc) error {
	if len(args) != 0 {
		return errArguments
	}
	databaseURL := strings.TrimSpace(getenv("DATABASE_URL"))
	if !validDatabaseURL(databaseURL) {
		return errConfiguration
	}
	passwords, setPasswords, err := passwordsFromEnv(getenv)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := ensure(ctx, databaseURL)
	if err != nil {
		return errDatabase
	}
	login := "skipped"
	if setPasswords {
		if err := setLogin(ctx, databaseURL, passwords); err != nil {
			return errLogin
		}
		login = "set"
	}

	outcome := "verified"
	if result.Created > 0 {
		outcome = "created"
	}
	if _, err := fmt.Fprintf(stdout, "outcome=%s created=%d verified=%d login=%s\n", outcome, result.Created, result.Verified, login); err != nil {
		return errors.New("write db-setup outcome failed")
	}
	return nil
}

func validDatabaseURL(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		return false
	}
	_, err = pgx.ParseConfig(raw)
	return err == nil
}

// passwordsFromEnv returns ok=false when neither password is set, and an
// error when only one is set.
func passwordsFromEnv(getenv func(string) string) (dbroles.LoginPasswords, bool, error) {
	migrator := getenv("MIGRATOR_PASSWORD")
	app := getenv("APP_PASSWORD")
	switch {
	case migrator == "" && app == "":
		return dbroles.LoginPasswords{}, false, nil
	case migrator == "" || app == "":
		return dbroles.LoginPasswords{}, false, errPartialPasswords
	default:
		return dbroles.LoginPasswords{Migrator: migrator, App: app}, true, nil
	}
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

func setLoginURL(ctx context.Context, databaseURL string, p dbroles.LoginPasswords) (err error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	return dbroles.SetLoginVerifiers(ctx, db, p, rand.Reader)
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
