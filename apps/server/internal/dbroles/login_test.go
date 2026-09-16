package dbroles

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestScramVerifierMatchesKnownVector(t *testing.T) {
	salt, err := base64.StdEncoding.DecodeString("W22ZaJ0SNY7soEsUEjb6gQ==")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ScramVerifier("pencil", salt, 4096)
	if err != nil {
		t.Fatal(err)
	}
	const want = "SCRAM-SHA-256$4096:W22ZaJ0SNY7soEsUEjb6gQ==$WG5d8oPm3OtcPnkdi4Uo7BkeZkBFzpcXkuLmtbsT4qY=:wfPLwcE6nTWhTAmQ7tl2KeoiWGPlZqQxSrmfPwDl2dU="
	if got != want {
		t.Fatalf("verifier = %s", got)
	}
}

func TestScramVerifierRejectsWeakParameters(t *testing.T) {
	for _, tc := range []struct {
		salt       []byte
		iterations int
	}{{nil, 4096}, {make([]byte, 15), 4096}, {make([]byte, 16), 4095}} {
		if _, err := ScramVerifier("x", tc.salt, tc.iterations); err == nil {
			t.Fatalf("accepted salt=%d iterations=%d", len(tc.salt), tc.iterations)
		}
	}
}

func TestSetLoginVerifiersRejectsBadPasswordsBeforeSQL(t *testing.T) {
	strong := strings.Repeat("a", 32)
	for _, p := range []LoginPasswords{
		{Migrator: "short", App: strong},
		{Migrator: strong, App: ""},
		{Migrator: strong, App: strong},
		{Migrator: strong, App: strings.Repeat("b", 1025)},
	} {
		if err := SetLoginVerifiers(context.Background(), nil, p, bytes.NewReader(make([]byte, 64))); err == nil {
			t.Fatalf("accepted %d/%d-byte passwords", len(p.Migrator), len(p.App))
		}
	}
}

func TestSetLoginVerifiersFailsWithoutSalt(t *testing.T) {
	p := LoginPasswords{Migrator: strings.Repeat("m", 32), App: strings.Repeat("p", 32)}
	if err := setLoginVerifiers(context.Background(), nil, p, bytes.NewReader(make([]byte, 8))); err == nil {
		t.Fatal("accepted a short random source")
	}
}

func TestSetLoginVerifiersWritesOnlyFixedRoles(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("REQUIRE_TEST_DB") == "1" {
			t.Fatal("TEST_DATABASE_URL is required")
		}
		t.Skip("TEST_DATABASE_URL not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Roll back so the shared cluster's role passwords never change.
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			t.Error(rollbackErr)
		}
	}()
	var before string
	if err = tx.QueryRowContext(ctx, `SELECT string_agg(rolname || '=' || coalesce(rolpassword, ''), ',' ORDER BY rolname) FROM pg_authid WHERE rolname NOT IN ('aboutme_migrator','aboutme_app')`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	p := LoginPasswords{Migrator: strings.Repeat("m", 32), App: strings.Repeat("p", 32)}
	if err = setLoginVerifiers(ctx, tx, p, bytes.NewReader(make([]byte, 64))); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT rolname, rolpassword FROM pg_authid WHERE rolname IN ('aboutme_migrator','aboutme_app') ORDER BY rolname`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	count := 0
	for rows.Next() {
		var name, verifier string
		if err = rows.Scan(&name, &verifier); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(verifier, "SCRAM-SHA-256$4096:") || strings.Contains(verifier, p.Migrator) || strings.Contains(verifier, p.App) {
			t.Fatalf("%s stored an unexpected value", name)
		}
		count++
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("updated %d roles", count)
	}
	var after string
	if err = tx.QueryRowContext(ctx, `SELECT string_agg(rolname || '=' || coalesce(rolpassword, ''), ',' ORDER BY rolname) FROM pg_authid WHERE rolname NOT IN ('aboutme_migrator','aboutme_app')`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("another role's password changed")
	}
}

func TestScramVerifierAuthenticatesRealLogin(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("REQUIRE_TEST_DB") == "1" {
			t.Fatal("TEST_DATABASE_URL is required")
		}
		t.Skip("TEST_DATABASE_URL not set")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := admin.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	role := fmt.Sprintf("aboutme_scram_test_%d", time.Now().UnixNano())
	password := strings.Repeat("v", 40)
	verifier, err := ScramVerifier(password, bytes.Repeat([]byte{7}, 16), 4096)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.ExecContext(ctx, `CREATE ROLE `+role+` LOGIN PASSWORD '`+verifier+`'`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, dropErr := admin.ExecContext(context.Background(), `DROP ROLE IF EXISTS `+role); dropErr != nil {
			t.Error(dropErr)
		}
	}()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(role, password)
	login, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := login.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	var user string
	if err = login.QueryRowContext(ctx, `SELECT session_user`).Scan(&user); err != nil {
		t.Fatalf("login with the derived verifier failed: %v", err)
	}
	if user != role {
		t.Fatalf("session_user %q", user)
	}
	u.User = url.UserPassword(role, password+"x")
	wrong, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := wrong.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	if err = wrong.PingContext(ctx); err == nil {
		t.Fatal("a wrong password logged in")
	}
}
