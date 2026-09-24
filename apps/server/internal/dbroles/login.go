package dbroles

import (
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"
)

const (
	scramIterations  = 4096
	scramSaltBytes   = 16
	minPasswordBytes = 32
	maxPasswordBytes = 1024
)

var verifierPattern = regexp.MustCompile(`^SCRAM-SHA-256\$[0-9]+:[A-Za-z0-9+/=]+\$[A-Za-z0-9+/=]+:[A-Za-z0-9+/=]+$`)

// LoginPasswords holds the passwords for the aboutme_migrator and
// aboutme_app login roles.
type LoginPasswords struct{ Migrator, App string }

// ScramVerifier returns the PostgreSQL SCRAM-SHA-256 verifier for password.
func ScramVerifier(password string, salt []byte, iterations int) (string, error) {
	if len(salt) < scramSaltBytes || iterations < scramIterations {
		return "", errors.New("dbroles: weak SCRAM parameters")
	}
	salted, err := pbkdf2.Key(sha256.New, password, salt, iterations, sha256.Size)
	if err != nil {
		return "", fmt.Errorf("dbroles: derive SCRAM key: %w", err)
	}
	storedKey := sha256.Sum256(hmacSHA256(salted, "Client Key"))
	serverKey := hmacSHA256(salted, "Server Key")
	enc := base64.StdEncoding
	return fmt.Sprintf("SCRAM-SHA-256$%d:%s$%s:%s", iterations,
		enc.EncodeToString(salt), enc.EncodeToString(storedKey[:]), enc.EncodeToString(serverKey)), nil
}

func hmacSHA256(key []byte, message string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(message))
	return mac.Sum(nil)
}

// SetLoginVerifiers stores SCRAM verifiers for aboutme_migrator and
// aboutme_app in one transaction. PostgreSQL keeps a pre-hashed value as the
// verifier, so no plaintext password reaches the server or its logs.
func SetLoginVerifiers(ctx context.Context, db *sql.DB, p LoginPasswords, random io.Reader) error {
	if err := ValidatePasswords(p); err != nil {
		return err
	}
	if db == nil {
		return errors.New("dbroles: nil database")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("dbroles: begin: %w", err)
	}
	if err := setLoginVerifiers(ctx, tx, p, random); err != nil {
		rollbackBounded(tx, 5*time.Second)
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("dbroles: commit outcome unknown: %w", err)
	}
	return nil
}

// ValidatePasswords checks the length and inequality rules SetLoginVerifiers
// requires, without touching a database. Callers can run it before opening a
// connection to fail fast on a bad password input.
func ValidatePasswords(p LoginPasswords) error {
	for _, value := range []string{p.Migrator, p.App} {
		if len(value) < minPasswordBytes || len(value) > maxPasswordBytes {
			return errors.New("dbroles: login password must be 32 to 1024 bytes")
		}
	}
	if p.Migrator == p.App {
		return errors.New("dbroles: login passwords must differ")
	}
	return nil
}

func setLoginVerifiers(ctx context.Context, tx *sql.Tx, p LoginPasswords, random io.Reader) error {
	if err := ValidatePasswords(p); err != nil {
		return err
	}
	type roleVerifier struct{ name, verifier string }
	var verifiers []roleVerifier
	for _, role := range []struct{ name, password string }{
		{"aboutme_migrator", p.Migrator},
		{"aboutme_app", p.App},
	} {
		salt := make([]byte, scramSaltBytes)
		if _, err := io.ReadFull(random, salt); err != nil {
			return fmt.Errorf("dbroles: read salt: %w", err)
		}
		verifier, err := ScramVerifier(role.password, salt, scramIterations)
		if err != nil {
			return err
		}
		// ALTER ROLE takes no bind parameters. The role name is a fixed
		// literal and the verifier matches a closed pattern with no quote.
		if !verifierPattern.MatchString(verifier) {
			return errors.New("dbroles: malformed verifier")
		}
		verifiers = append(verifiers, roleVerifier{role.name, verifier})
	}
	// Every verifier is derived before any SQL runs. The lock then
	// serializes the role writes with Ensure and other SetLoginVerifiers
	// calls on this database (ADR 0038).
	if err := lockCatalog(ctx, tx); err != nil {
		return fmt.Errorf("dbroles: acquire lock: %w", err)
	}
	for _, rv := range verifiers {
		if _, err := tx.ExecContext(ctx, `ALTER ROLE `+rv.name+` PASSWORD '`+rv.verifier+`'`); err != nil {
			return fmt.Errorf("dbroles: set %s login: %w", rv.name, err)
		}
	}
	return nil
}
