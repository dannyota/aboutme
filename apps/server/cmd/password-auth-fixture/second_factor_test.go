package main

import (
	"bytes"
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

func factorTestAccounts(t *testing.T, proof string) secondFactorAccounts {
	t.Helper()
	var sequence byte
	doc, err := newSecondFactorAccounts(proof, bytes.NewReader(bytes.Repeat([]byte{42}, 512)), func() (uuid.UUID, error) {
		id := uuid.MustParse("01900000-0000-7000-8000-000000000001")
		sequence++
		id[15] = sequence
		return id, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestSecondFactorSeedRequiresDevelopmentEnvironment(t *testing.T) {
	for _, environment := range []string{"", "prod", "staging"} {
		t.Run(environment, func(t *testing.T) {
			t.Setenv("ENV", environment)
			for _, cleanup := range []bool{false, true} {
				err := runSecondFactorFixture(context.Background(), Config{DatabaseURL: validDSN, Proof: "totp"}, cleanup)
				if err == nil || !strings.Contains(err.Error(), "development environment") {
					t.Fatal("non-development fixture reached input or database access")
				}
			}
		})
	}
}

func TestSecondFactorFixtureRejectsOtherDatabasesBeforeFileAccess(t *testing.T) {
	t.Setenv("ENV", "dev")
	for _, dsn := range []string{"postgres://127.0.0.1/aboutme", validDSN + "&host=example.invalid"} {
		for _, cleanup := range []bool{false, true} {
			err := runSecondFactorFixture(context.Background(), Config{DatabaseURL: dsn, Proof: "totp"}, cleanup)
			if err == nil || !strings.Contains(err.Error(), "loopback development database") {
				t.Fatal("unsafe database was accepted")
			}
		}
	}
}

func TestSecondFactorCredentialsHaveClosedRolesAndReservedAccounts(t *testing.T) {
	for _, proof := range []string{"passkey", "totp"} {
		doc := factorTestAccounts(t, proof)
		want := "primary,recovery,attempts"
		if proof == "totp" {
			want = "primary,skew,replay,concurrent,replace,epoch,locale,recovery,attempts"
		}
		roles := make([]string, 0, len(doc.Accounts))
		for _, account := range doc.Accounts {
			roles = append(roles, account.Role)
		}
		if strings.Join(roles, ",") != want || validateSecondFactorAccounts(doc, proof) != nil {
			t.Fatal("generated accounts do not match the proof roles")
		}
		for _, mutation := range []string{"email", "id", "password", "role", "proof", "duplicate", "missing"} {
			t.Run(proof+"/"+mutation, func(t *testing.T) {
				bad := doc
				bad.Accounts = append([]secondFactorAccount(nil), doc.Accounts...)
				switch mutation {
				case "email":
					bad.Accounts[0].Email = "developer@example.invalid"
				case "id":
					bad.Accounts[0].ID = "52000000-0000-4000-8000-000000000001"
				case "password":
					bad.Accounts[0].Password = "short"
				case "role":
					bad.Accounts[0].Role = "disabled"
				case "proof":
					bad.Proof = "other"
				case "duplicate":
					bad.Accounts[1] = bad.Accounts[0]
				case "missing":
					bad.Accounts = bad.Accounts[1:]
				}
				if validateSecondFactorAccounts(bad, proof) == nil {
					t.Fatal("unauthorized fixture account set was accepted")
				}
			})
		}
	}
}

func TestSecondFactorCredentialFileRejectsUnsafePaths(t *testing.T) {
	doc := factorTestAccounts(t, "passkey")
	for _, mutation := range []string{"valid", "existing", "relative", "public-parent", "symlink-parent", "public-file", "symlink", "hardlink", "oversized", "extra-field", "duplicate-field"} {
		t.Run(mutation, func(t *testing.T) {
			parent := t.TempDir()
			path := filepath.Join(parent, "accounts.json")
			if mutation == "relative" {
				path = "relative-accounts.json"
			}
			if mutation == "public-parent" {
				if err := os.Chmod(parent, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if mutation == "symlink-parent" {
				link := filepath.Join(t.TempDir(), "parent")
				if err := os.Symlink(parent, link); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(link, "accounts.json")
			}
			if mutation == "existing" {
				if err := os.WriteFile(path, []byte("existing input"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			err := writeSecondFactorAccounts(path, doc)
			if mutation == "relative" || mutation == "public-parent" || mutation == "symlink-parent" || mutation == "existing" {
				if err == nil {
					t.Fatal("unsafe output path was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "public-file":
				if chmodErr := os.Chmod(path, 0o644); chmodErr != nil {
					t.Fatal(chmodErr)
				}
			case "symlink", "hardlink":
				link := filepath.Join(t.TempDir(), "accounts.json")
				if mutation == "symlink" {
					err = os.Symlink(path, link)
				} else {
					err = os.Link(path, link)
				}
				if err != nil {
					t.Fatal(err)
				}
				path = link
			case "oversized", "extra-field", "duplicate-field":
				body, readErr := os.ReadFile(path)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if mutation == "oversized" {
					body = []byte(strings.Repeat("x", 8193))
				}
				if mutation == "extra-field" {
					body = []byte(strings.Replace(string(body), "{", `{"extra":true,`, 1))
				}
				if mutation == "duplicate-field" {
					body = []byte(strings.Replace(string(body), "{", `{"schemaVersion":1,`, 1))
				}
				if writeErr := os.WriteFile(path, body, 0o600); writeErr != nil {
					t.Fatal(writeErr)
				}
			}
			got, err := readSecondFactorAccounts(path, "passkey")
			if mutation == "valid" {
				if err != nil || len(got.Accounts) != 3 {
					t.Fatal("private account file was rejected")
				}
			} else if err == nil {
				t.Fatal("unsafe private account file was accepted")
			}
		})
	}
}

func TestSecondFactorSeedIsAtomicAndPublishesOnlyAfterAllInserts(t *testing.T) {
	doc := factorTestAccounts(t, "passkey")
	for _, scenario := range []string{"success", "collision", "credential-failure", "publish-failure", "commit-failure"} {
		t.Run(scenario, func(t *testing.T) {
			state := &factorDBState{scenario: scenario, accounts: doc.Accounts}
			db := sql.OpenDB(factorConnector{state})
			t.Cleanup(func() {
				if err := db.Close(); err != nil {
					t.Error(err)
				}
			})
			published := false
			err := seedSecondFactorAccounts(context.Background(), db, doc, []string{"encoded", "encoded", "encoded"}, time.Unix(1234, 0), func() error {
				if state.executions != 6 || state.committed {
					t.Fatal("credentials published before account creation or after commit")
				}
				published = true
				if scenario == "publish-failure" {
					return errors.New("private detail")
				}
				return nil
			})
			if scenario == "success" {
				if err != nil || !state.committed || !published {
					t.Fatal("fixture did not publish and commit all accounts")
				}
				return
			}
			if err == nil || state.committed || (scenario != "commit-failure" && !state.rolledBack) || strings.Contains(err.Error(), "private detail") {
				t.Fatal("failed fixture was not atomic and private")
			}
			if (scenario == "collision" || scenario == "credential-failure") && published {
				t.Fatal("failed account writes published credentials")
			}
			if scenario == "collision" && state.executions != 3 {
				t.Fatal("colliding user reached its credential write")
			}
		})
	}
}

func TestSecondFactorCleanupBindsEveryDeletionToItsAccountIdentity(t *testing.T) {
	doc := factorTestAccounts(t, "passkey")
	state := &factorDBState{scenario: "cleanup", accounts: doc.Accounts}
	db := sql.OpenDB(factorConnector{state})
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := cleanupSecondFactorAccounts(context.Background(), db, doc); err != nil || state.executions != 3 || !state.committed {
		t.Fatal("cleanup did not delete the exact fixture identities")
	}
}

type factorDBState struct {
	scenario              string
	accounts              []secondFactorAccount
	executions            int
	committed, rolledBack bool
}
type factorConnector struct{ state *factorDBState }

func (c factorConnector) Connect(context.Context) (driver.Conn, error) { return factorConn(c), nil }
func (factorConnector) Driver() driver.Driver                          { return seedDriver{} }

type factorConn struct{ state *factorDBState }

func (factorConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("prepare unsupported") }
func (factorConn) Close() error                        { return nil }
func (c factorConn) Begin() (driver.Tx, error)         { return factorTx(c), nil }
func (c factorConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.state.executions++
	if c.state.scenario == "cleanup" {
		account := c.state.accounts[c.state.executions-1]
		if !strings.Contains(query, "DELETE FROM users WHERE id = $1 AND email = $2") || len(args) != 2 || args[0].Value != account.ID || args[1].Value != account.Email {
			return nil, errors.New("unbounded account deletion")
		}
		return driver.RowsAffected(0), nil
	}
	account := c.state.accounts[(c.state.executions-1)/2]
	if args[0].Value != account.ID {
		return nil, errors.New("wrong account")
	}
	if c.state.executions%2 == 1 {
		if !strings.Contains(query, "INSERT INTO users") || !strings.Contains(query, "ON CONFLICT DO NOTHING") || len(args) != 3 || args[1].Value != account.Email {
			return nil, errors.New("unsafe user insert")
		}
		if c.state.scenario == "collision" && c.state.executions == 3 {
			return driver.RowsAffected(0), nil
		}
	} else {
		if !strings.Contains(query, "INSERT INTO password_credentials") || len(args) != 3 {
			return nil, errors.New("wrong credential insert")
		}
		encoded, ok := args[1].Value.([]byte)
		if !ok || string(encoded) != "encoded" || args[2].Value != time.Unix(1234, 0) {
			return nil, errors.New("wrong credential values")
		}
		if c.state.scenario == "credential-failure" {
			return nil, errors.New("private detail")
		}
	}
	return driver.RowsAffected(1), nil
}

type factorTx struct{ state *factorDBState }

func (tx factorTx) Commit() error {
	if tx.state.scenario == "commit-failure" {
		return errors.New("private detail")
	}
	tx.state.committed = true
	return nil
}
func (tx factorTx) Rollback() error { tx.state.rolledBack = true; return nil }
