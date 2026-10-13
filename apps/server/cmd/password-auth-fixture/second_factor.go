package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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

type secondFactorAccount struct {
	Role     string `json:"role"`
	ID       string `json:"id"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type secondFactorAccounts struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Proof         string                `json:"proof"`
	Accounts      []secondFactorAccount `json:"accounts"`
}

var factorPassword = regexp.MustCompile(`\A[A-Za-z0-9_-]{32}\z`)

func secondFactorRoles(proof string) []string {
	switch proof {
	case "passkey":
		return []string{"primary", "recovery", "attempts"}
	case "totp":
		return []string{"primary", "skew", "replay", "concurrent", "replace", "epoch", "locale", "recovery", "attempts"}
	default:
		return nil
	}
}

func newSecondFactorAccounts(proof string, entropy io.Reader, newID func() (uuid.UUID, error)) (secondFactorAccounts, error) {
	doc := secondFactorAccounts{SchemaVersion: 1, Proof: proof}
	roles := secondFactorRoles(proof)
	if len(roles) == 0 {
		return doc, errors.New("invalid second-factor proof")
	}
	for _, role := range roles {
		id, err := newID()
		if err != nil {
			return doc, errors.New("second-factor identity generation failed")
		}
		var marker [8]byte
		var secret [24]byte
		if _, err := io.ReadFull(entropy, marker[:]); err != nil {
			return doc, errors.New("second-factor address generation failed")
		}
		if _, err := io.ReadFull(entropy, secret[:]); err != nil {
			return doc, errors.New("second-factor password generation failed")
		}
		doc.Accounts = append(doc.Accounts, secondFactorAccount{
			Role: role, ID: id.String(),
			Email:    "pa-test-" + proof + "-" + role + "-" + hex.EncodeToString(marker[:]) + "@example.invalid",
			Password: base64.RawURLEncoding.EncodeToString(secret[:]),
		})
	}
	return doc, nil
}

func validateSecondFactorAccounts(doc secondFactorAccounts, proof string) error {
	invalid := errors.New("invalid private second-factor accounts")
	roles := secondFactorRoles(proof)
	if doc.SchemaVersion != 1 || doc.Proof != proof || len(roles) == 0 || len(doc.Accounts) != len(roles) {
		return invalid
	}
	seen := make(map[string]bool)
	for index, account := range doc.Accounts {
		id, err := uuid.Parse(account.ID)
		emailPattern := `\Apa-test-` + proof + "-" + roles[index] + `-[0-9a-f]{16}@example\.invalid\z`
		if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 || id.String() != account.ID || account.Role != roles[index] || !regexp.MustCompile(emailPattern).MatchString(account.Email) || !factorPassword.MatchString(account.Password) || seen[account.ID] || seen[account.Email] {
			return invalid
		}
		seen[account.ID], seen[account.Email] = true, true
	}
	return nil
}

// Private files are run-scoped, never overwritten, and never follow links.
func openSecondFactorFile(path string, create bool) (*os.File, error) {
	invalid := errors.New("invalid private second-factor account file")
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, invalid
	}
	parent := filepath.Dir(path)
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil || realParent != parent {
		return nil, invalid
	}
	info, err := os.Stat(parent) //nolint:gosec // G703: parent comes from an absolute, cleaned CLI path; EvalSymlinks above rejects linked ancestors.
	if err != nil {
		return nil, invalid
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm() != 0o700 || int(stat.Uid) != os.Geteuid() {
		return nil, invalid
	}
	flags := os.O_RDONLY
	if create {
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	file, err := os.OpenFile(path, flags|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600) //nolint:gosec // G703: the absolute CLI path has a validated private parent; O_NOFOLLOW and the descriptor checks below reject unsafe files.
	if err != nil {
		return nil, invalid
	}
	info, err = file.Stat()
	if err == nil {
		stat, ok = info.Sys().(*syscall.Stat_t)
	}
	if err != nil || !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || info.Size() > 8192 || int(stat.Uid) != os.Geteuid() || stat.Nlink != 1 {
		closeSecondFactorFile(file)
		return nil, invalid
	}
	return file, nil
}

func closeSecondFactorFile(file *os.File) {
	if err := file.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "second-factor account file close failed")
	}
}

func writeSecondFactorAccounts(path string, doc secondFactorAccounts) error {
	file, err := openSecondFactorFile(path, true)
	if err != nil {
		return err
	}
	writeErr := json.NewEncoder(file).Encode(doc)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return errors.New("second-factor account file write failed")
	}
	return nil
}

func readSecondFactorAccounts(path, proof string) (secondFactorAccounts, error) {
	var doc secondFactorAccounts
	file, err := openSecondFactorFile(path, false)
	if err != nil {
		return doc, err
	}
	defer closeSecondFactorFile(file)
	body, err := io.ReadAll(io.LimitReader(file, 8193))
	if err != nil || len(body) > 8192 || json.Unmarshal(body, &doc) != nil {
		return doc, errors.New("invalid private second-factor accounts")
	}
	canonical, err := json.Marshal(doc)
	if err != nil || !bytes.Equal(body, append(canonical, '\n')) {
		return doc, errors.New("invalid private second-factor accounts")
	}
	return doc, validateSecondFactorAccounts(doc, proof)
}

func runSecondFactorFixture(ctx context.Context, cfg Config, cleanup bool) error {
	if os.Getenv("ENV") != "dev" {
		return errors.New("second-factor fixture requires the development environment")
	}
	if err := devdb.ValidateURL(cfg.DatabaseURL, fixtureDatabase); err != nil {
		return errors.New("second-factor fixture requires the loopback development database")
	}
	var doc secondFactorAccounts
	var err error
	if cleanup {
		doc, err = readSecondFactorAccounts(cfg.CredentialFile, cfg.Proof)
	} else {
		doc, err = newSecondFactorAccounts(cfg.Proof, rand.Reader, uuid.NewV7)
	}
	if err != nil {
		return err
	}
	hashes := make([]string, len(doc.Accounts))
	if !cleanup {
		hasher, hashErr := password.NewHasher(password.DefaultHashPolicy(), rand.Reader, password.NewAdmission())
		if hashErr != nil {
			return errors.New("second-factor fixture hasher unavailable")
		}
		for index, account := range doc.Accounts {
			normalized, normalizeErr := password.Normalize(account.Password)
			if normalizeErr != nil {
				return errors.New("invalid second-factor fixture password")
			}
			hashes[index], err = hasher.Hash(ctx, normalized)
			if err != nil {
				return errors.New("second-factor fixture password hashing failed")
			}
		}
	}
	db, err := open(ctx, cfg)
	if err != nil {
		return errors.New("second-factor fixture database unavailable")
	}
	defer func() {
		if err := db.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "second-factor fixture database close failed")
		}
	}()
	if cleanup {
		return cleanupSecondFactorAccounts(ctx, db, doc)
	}
	return seedSecondFactorAccounts(ctx, db, doc, hashes, time.Now(), func() error {
		return writeSecondFactorAccounts(cfg.CredentialFile, doc)
	})
}

// The private credential handoff precedes commit so a failed commit can be
// cleaned by exact identities; a collision never publishes that handoff.
func seedSecondFactorAccounts(ctx context.Context, db *sql.DB, doc secondFactorAccounts, hashes []string, now time.Time, publish func() error) error {
	if err := validateSecondFactorAccounts(doc, doc.Proof); err != nil || len(hashes) != len(doc.Accounts) {
		return errors.New("invalid second-factor fixture accounts")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("second-factor fixture transaction unavailable")
	}
	defer rollbackSecondFactor(tx)
	for index, account := range doc.Accounts {
		result, err := tx.ExecContext(ctx,
			`INSERT INTO users (id, email, name, avatar_key) VALUES ($1, $2, $3, NULL)
			 ON CONFLICT DO NOTHING`, account.ID, account.Email, "Fictional Factor Owner")
		if err != nil {
			return errors.New("second-factor fixture user creation failed")
		}
		rows, err := result.RowsAffected()
		if err != nil || rows != 1 {
			return errors.New("second-factor fixture account already exists")
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO password_credentials (user_id, encoded_hash, created_at, changed_at)
			 VALUES ($1, $2, $3, $3)`, account.ID, []byte(hashes[index]), now); err != nil {
			return errors.New("second-factor fixture credential creation failed")
		}
	}
	if err := publish(); err != nil {
		return errors.New("second-factor fixture credential publication failed")
	}
	if err := tx.Commit(); err != nil {
		return errors.New("second-factor fixture commit failed")
	}
	return nil
}

func rollbackSecondFactor(tx *sql.Tx) {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		fmt.Fprintln(os.Stderr, "second-factor fixture rollback failed")
	}
}

func cleanupSecondFactorAccounts(ctx context.Context, db *sql.DB, doc secondFactorAccounts) error {
	if err := validateSecondFactorAccounts(doc, doc.Proof); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("second-factor fixture cleanup unavailable")
	}
	defer rollbackSecondFactor(tx)
	for _, account := range doc.Accounts {
		if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = $1 AND email = $2`, account.ID, account.Email); err != nil {
			return errors.New("second-factor fixture cleanup failed")
		}
	}
	if err := tx.Commit(); err != nil {
		return errors.New("second-factor fixture cleanup commit failed")
	}
	return nil
}
