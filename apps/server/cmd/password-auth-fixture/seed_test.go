package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

const seedEmail = "pa-test-mcp-0123456789abcdef@example.invalid"
const seedPassword = "MCP_fixture_password_0123456789a"

func seedCredential(t *testing.T, email, secret string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "login.env")
	if err := os.WriteFile(path, []byte("ABOUTME_TEST_EMAIL="+email+"\nABOUTME_TEST_PASSWORD="+secret+"\n"), 0o600); err != nil {
		t.Fatal("write synthetic credential failed")
	}
	return path
}

func TestMCPSeedCredentialRejectsUnauthorizedAccounts(t *testing.T) {
	for _, email := range []string{
		"developer@example.invalid", "pa-test-other-0123456789abcdef@example.invalid",
		"pa-test-mcp-0123456789abcde@example.invalid", "pa-test-mcp-0123456789abcdef@example.com",
		"PA-TEST-MCP-0123456789abcdef@example.invalid", seedEmail + "\nOTHER=value",
	} {
		t.Run(email, func(t *testing.T) {
			if _, err := readMCPSeedCredential(seedCredential(t, email, seedPassword)); err == nil {
				t.Fatal("unauthorized account was accepted")
			}
		})
	}
}

func TestMCPSeedCredentialRequiresPrivateBoundedRegularFile(t *testing.T) {
	for _, kind := range []string{"public", "special-mode", "symlink", "hardlink", "directory", "oversized", "extra-field", "short-password"} {
		t.Run(kind, func(t *testing.T) {
			path := seedCredential(t, seedEmail, seedPassword)
			switch kind {
			case "public":
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatal(err)
				}
			case "special-mode":
				if err := os.Chmod(path, 0o600|os.ModeSticky); err != nil {
					t.Fatal(err)
				}
			case "symlink", "hardlink":
				link := filepath.Join(t.TempDir(), "link.env")
				var err error
				if kind == "symlink" {
					err = os.Symlink(path, link)
				} else {
					err = os.Link(path, link)
				}
				if err != nil {
					t.Fatal(err)
				}
				path = link
			case "directory":
				path = t.TempDir()
			case "oversized", "extra-field", "short-password":
				body := "ABOUTME_TEST_EMAIL=" + seedEmail + "\nABOUTME_TEST_PASSWORD=" + seedPassword + "\n"
				if kind == "oversized" {
					body += strings.Repeat("x", 1024)
				}
				if kind == "extra-field" {
					body += "OTHER=value\n"
				}
				if kind == "short-password" {
					body = strings.ReplaceAll(body, seedPassword, "short")
				}
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := readMCPSeedCredential(path); err == nil {
				t.Fatal("unsafe credential file was accepted")
			}
		})
	}
	got, err := readMCPSeedCredential(seedCredential(t, seedEmail, seedPassword))
	if err != nil || got.Email != seedEmail || got.Password != seedPassword {
		t.Fatal("valid private synthetic credential was rejected")
	}
}

func TestMCPSeedRequiresDevelopmentEnvironment(t *testing.T) {
	for _, environment := range []string{"", "prod", "staging"} {
		t.Run(environment, func(t *testing.T) {
			t.Setenv("ENV", environment)
			err := runMCPSeed(context.Background(), Config{DatabaseURL: validDSN})
			if err == nil || !strings.Contains(err.Error(), "development environment") {
				t.Fatal("non-development seed was not refused before database access")
			}
		})
	}
}

func TestMCPSeedTransactionRefusesCollisionAndRollsBackCredentialFailure(t *testing.T) {
	for _, scenario := range []string{"success", "collision", "credential-failure"} {
		t.Run(scenario, func(t *testing.T) {
			state := &seedDBState{firstRows: 1}
			if scenario == "collision" {
				state.firstRows = 0
			}
			if scenario == "credential-failure" {
				state.credentialError = errors.New("database detail must stay private")
			}
			db := sql.OpenDB(seedConnector{state})
			t.Cleanup(func() {
				if err := db.Close(); err != nil {
					t.Error("close fixture database failed")
				}
			})
			account := fixtureAccount{ID: uuid.MustParse("52000000-0000-4000-8000-000000000099"), Email: seedEmail, Name: "Fictional Workflow Owner"}
			err := seedMCPAccount(context.Background(), db, account, "synthetic encoded hash", time.Unix(1234, 0))
			if scenario == "success" {
				if err != nil || state.executions != 2 || !state.committed {
					t.Fatal("account and credential did not commit together")
				}
				return
			}
			if err == nil || state.committed || !state.rolledBack {
				t.Fatal("failed seed did not roll back")
			}
			if scenario == "collision" && state.executions != 1 {
				t.Fatal("collision reached the credential write")
			}
			if strings.Contains(err.Error(), seedEmail) || strings.Contains(err.Error(), "database detail") {
				t.Fatal("seed exposed private error detail")
			}
		})
	}
}

// The database boundary returns controlled collision and write-failure results.
type seedDBState struct {
	firstRows             int64
	credentialError       error
	executions            int
	committed, rolledBack bool
}

type seedConnector struct{ state *seedDBState }

func (c seedConnector) Connect(context.Context) (driver.Conn, error) { return seedConn(c), nil }
func (seedConnector) Driver() driver.Driver                          { return seedDriver{} }

type seedDriver struct{}

func (seedDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("direct open unsupported")
}

type seedConn struct{ state *seedDBState }

func (seedConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("prepare unsupported") }
func (seedConn) Close() error                        { return nil }
func (c seedConn) Begin() (driver.Tx, error)         { return seedTx(c), nil }
func (c seedConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.state.executions++
	if c.state.executions == 1 {
		if !strings.Contains(query, "INSERT INTO users") || !strings.Contains(query, "ON CONFLICT DO NOTHING") || len(args) != 3 || args[1].Value != seedEmail {
			return nil, errors.New("unsafe user insert")
		}
		return driver.RowsAffected(c.state.firstRows), nil
	}
	if !strings.Contains(query, "INSERT INTO password_credentials") || len(args) != 3 {
		return nil, errors.New("unexpected credential insert")
	}
	encoded, ok := args[1].Value.([]byte)
	if !ok || string(encoded) != "synthetic encoded hash" || args[2].Value != time.Unix(1234, 0) {
		return nil, errors.New("invalid credential insert")
	}
	return driver.RowsAffected(1), c.state.credentialError
}

type seedTx struct{ state *seedDBState }

func (tx seedTx) Commit() error   { tx.state.committed = true; return nil }
func (tx seedTx) Rollback() error { tx.state.rolledBack = true; return nil }
