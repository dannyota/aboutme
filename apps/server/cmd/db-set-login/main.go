// Command db-set-login stores SCRAM verifiers for the fixed login roles.
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/dannyota/aboutme/apps/server/internal/dbroles"
)

type setFunc func(context.Context, string, dbroles.LoginPasswords) error

func main() {
	if err := run(os.Args[1:], os.Getenv, os.Stdout, setURL); err != nil {
		fmt.Fprintln(os.Stderr, "db-set-login:", err)
		os.Exit(1)
	}
}

// run reads DATABASE_URL, MIGRATOR_PASSWORD and APP_PASSWORD. The database
// password comes from PGPASSWORD, which pgx reads itself.
func run(args []string, getenv func(string) string, stdout io.Writer, set setFunc) error {
	if len(args) != 0 {
		return errors.New("arguments are not accepted")
	}
	databaseURL := strings.TrimSpace(getenv("DATABASE_URL"))
	p := dbroles.LoginPasswords{Migrator: getenv("MIGRATOR_PASSWORD"), App: getenv("APP_PASSWORD")}
	if databaseURL == "" || p.Migrator == "" || p.App == "" {
		return errors.New("DATABASE_URL, MIGRATOR_PASSWORD and APP_PASSWORD are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := set(ctx, databaseURL, p); err != nil {
		return errors.New("setting login verifiers failed")
	}
	_, err := fmt.Fprintln(stdout, "outcome=set roles=2")
	return err
}

func setURL(ctx context.Context, databaseURL string, p dbroles.LoginPasswords) (err error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	return dbroles.SetLoginVerifiers(ctx, db, p, rand.Reader)
}
