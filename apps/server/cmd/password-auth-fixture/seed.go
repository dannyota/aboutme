package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/devdb"
	"github.com/dannyota/aboutme/apps/server/internal/password"
)

var mcpSeedEmail = regexp.MustCompile(`\Apa-test-mcp-[0-9a-f]{16}@example\.invalid\z`)
var mcpSeedCredential = regexp.MustCompile(`\AABOUTME_TEST_EMAIL=(pa-test-mcp-[0-9a-f]{16}@example\.invalid)\nABOUTME_TEST_PASSWORD=([A-Za-z0-9_-]{32})\n\z`)

// readMCPSeedCredential reads only the private file emitted by the MCP proof.
func readMCPSeedCredential(path string) (fixtureAccount, error) {
	invalid := errors.New("invalid private MCP fixture credential")
	if !filepath.IsAbs(path) {
		return fixtureAccount{}, invalid
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fixtureAccount{}, invalid
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			fmt.Fprintln(os.Stderr, "MCP fixture credential close failed")
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return fixtureAccount{}, invalid
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || info.Size() > 512 || int(stat.Uid) != os.Geteuid() || stat.Nlink != 1 {
		return fixtureAccount{}, invalid
	}
	body, err := io.ReadAll(io.LimitReader(file, 513))
	if err != nil || len(body) > 512 {
		return fixtureAccount{}, invalid
	}
	match := mcpSeedCredential.FindSubmatch(body)
	if match == nil {
		return fixtureAccount{}, invalid
	}
	return fixtureAccount{Email: string(match[1]), Password: string(match[2]), Name: "Fictional Workflow Owner"}, nil
}

// runMCPSeed prepares one disposable login; the MCP journey proves sign-in.
func runMCPSeed(ctx context.Context, cfg Config) error {
	if os.Getenv("ENV") != "dev" {
		return errors.New("MCP fixture requires the development environment")
	}
	if err := devdb.ValidateURL(cfg.DatabaseURL, fixtureDatabase); err != nil {
		return errors.New("MCP fixture requires the loopback development database")
	}
	account, err := readMCPSeedCredential(cfg.CredentialFile)
	if err != nil {
		return err
	}
	account.ID, err = uuid.NewV7FromReader(rand.Reader)
	if err != nil {
		return errors.New("MCP fixture identity generation failed")
	}
	hasher, err := password.NewHasher(password.DefaultHashPolicy(), rand.Reader, password.NewAdmission())
	if err != nil {
		return errors.New("MCP fixture hasher unavailable")
	}
	normalized, err := password.Normalize(account.Password)
	if err != nil {
		return errors.New("invalid MCP fixture password")
	}
	encoded, err := hasher.Hash(ctx, normalized)
	if err != nil {
		return errors.New("MCP fixture password hashing failed")
	}
	db, err := open(ctx, cfg)
	if err != nil {
		return errors.New("MCP fixture database unavailable")
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			fmt.Fprintln(os.Stderr, "MCP fixture database close failed")
		}
	}()
	return seedMCPAccount(ctx, db, account, encoded, time.Now())
}

// A collision refuses the seed instead of adopting or changing another user.
func seedMCPAccount(ctx context.Context, db *sql.DB, account fixtureAccount, encoded string, now time.Time) error {
	if !mcpSeedEmail.MatchString(account.Email) || account.ID == uuid.Nil || account.Provider != nil {
		return errors.New("invalid MCP fixture account")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("MCP fixture transaction unavailable")
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			fmt.Fprintln(os.Stderr, "MCP fixture rollback failed")
		}
	}()
	result, err := tx.ExecContext(ctx,
		`INSERT INTO users (id, email, name, avatar_key) VALUES ($1, $2, $3, NULL)
		 ON CONFLICT DO NOTHING`, account.ID, account.Email, account.Name)
	if err != nil {
		return errors.New("MCP fixture user creation failed")
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return errors.New("MCP fixture account already exists")
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO password_credentials (user_id, encoded_hash, created_at, changed_at)
		 VALUES ($1, $2, $3, $3)`, account.ID, []byte(encoded), now); err != nil {
		return errors.New("MCP fixture credential creation failed")
	}
	if err := tx.Commit(); err != nil {
		return errors.New("MCP fixture commit failed")
	}
	return nil
}
