package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/password"
)

// fixtureDatabase is the only database name the command will touch. Matching
// it by exact name is the explicit opt-in that keeps the fixture from ever
// mutating the test aboutme database or a stray development database.
const fixtureDatabase = "aboutme_dev"

// testEmailPrefix reserves the runtime-random account namespace the browser
// proof creates, so cleanup can remove exactly those rows without ever
// touching a real development account.
const testEmailPrefix = "pa-test-"

// Config holds the validated seed/cleanup input.
type Config struct {
	DatabaseURL string
}

func parseConfig(args []string) (string, Config, error) {
	if len(args) == 0 {
		return "", Config{}, errors.New("subcommand is required (seed or cleanup)")
	}
	cmd := args[0]
	if cmd != "seed" && cmd != "cleanup" {
		return "", Config{}, fmt.Errorf("unknown subcommand %q (want seed or cleanup)", cmd)
	}

	var databaseURL string
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--database-url":
			i++
			if i >= len(args) {
				return "", Config{}, errors.New("--database-url requires a value")
			}
			databaseURL = args[i]
		default:
			return "", Config{}, fmt.Errorf("unknown argument %q", args[i])
		}
	}

	if err := validateDatabaseURL(databaseURL); err != nil {
		return "", Config{}, err
	}
	return cmd, Config{DatabaseURL: databaseURL}, nil
}

// validateDatabaseURL enforces loopback-only, postgres-only, and the exact
// native development database name before any connection is opened.
func validateDatabaseURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return errors.New("--database-url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("--database-url is not a valid URL: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return fmt.Errorf("--database-url scheme must be postgres or postgresql, got %q", u.Scheme)
	}
	if host := u.Hostname(); host != "127.0.0.1" {
		return fmt.Errorf("--database-url must target loopback 127.0.0.1, got %q", host)
	}
	name := strings.Trim(u.Path, "/")
	if name == "" {
		return errors.New("--database-url must name a database")
	}
	if name != fixtureDatabase {
		return fmt.Errorf("--database-url must target database %q (explicit opt-in), got %q", fixtureDatabase, name)
	}
	return nil
}

// providerIdentity is one linked OAuth identity for a fixture account.
type providerIdentity struct {
	Provider       string
	ProviderUserID string
}

// fixtureAccount is one deterministic account the seed writes.
type fixtureAccount struct {
	ID         uuid.UUID
	IdentityID uuid.UUID
	Email      string
	Name       string
	Provider   *providerIdentity
	Password   string // raw password; hashed at seed time when non-empty
}

// Frozen fixture accounts. The fixed UUIDs can never collide with real
// development data, and the provider subjects match the local mock accounts
// (uat-google-001/002) so the proof can sign into them.
var fixtureAccounts = []fixtureAccount{
	{
		ID:         uuid.MustParse("52000000-0000-4000-8000-000000000001"),
		IdentityID: uuid.MustParse("52000000-0000-4000-8000-000000000011"),
		Email:      "pa-provider-only@example.invalid",
		Name:       "PA Provider Only",
		Provider:   &providerIdentity{Provider: "google", ProviderUserID: "uat-google-004"},
	},
	{
		ID:         uuid.MustParse("52000000-0000-4000-8000-000000000002"),
		IdentityID: uuid.MustParse("52000000-0000-4000-8000-000000000012"),
		Email:      "pa-password-provider@example.invalid",
		Name:       "PA Password Provider",
		Provider:   &providerIdentity{Provider: "google", ProviderUserID: "uat-google-002"},
		Password:   "correct-horse-battery-staple",
	},
	{
		ID:       uuid.MustParse("52000000-0000-4000-8000-000000000003"),
		Email:    "pa-second-password@example.invalid",
		Name:     "PA Second Password",
		Password: "second-password-value",
	},
}

func run(ctx context.Context, cmd string, cfg Config) error {
	switch cmd {
	case "seed":
		return runSeed(ctx, cfg)
	case "cleanup":
		return runCleanup(ctx, cfg)
	default:
		return fmt.Errorf("unknown subcommand %q", cmd)
	}
}

func open(ctx context.Context, cfg Config) (*sql.DB, error) {
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			fmt.Fprintln(os.Stderr, "password-auth-fixture: close database:", closeErr)
		}
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}

// runSeed writes the three fixed accounts. It is idempotent: re-running
// against an already-seeded database is a no-op rather than an error.
func runSeed(ctx context.Context, cfg Config) error {
	db, err := open(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			fmt.Fprintln(os.Stderr, "password-auth-fixture: close database:", closeErr)
		}
	}()

	hasher, err := password.NewHasher(
		password.DefaultHashPolicy(), rand.Reader, password.NewAdmission())
	if err != nil {
		return fmt.Errorf("create hasher: %w", err)
	}

	for _, acct := range fixtureAccounts {
		if err := seedAccount(ctx, db, hasher, acct); err != nil {
			return err
		}
	}
	return nil
}

func seedAccount(ctx context.Context, db *sql.DB, hasher *password.Hasher, acct fixtureAccount) error {
	if _, err := db.ExecContext(ctx,
		`INSERT INTO users (id, email, name, avatar_key) VALUES ($1, $2, $3, NULL)
		 ON CONFLICT (id) DO NOTHING`,
		acct.ID, acct.Email, acct.Name); err != nil {
		return fmt.Errorf("insert user %s: %w", acct.Email, err)
	}

	if acct.Provider != nil {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO identities (id, user_id, provider, provider_user_id)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (provider, provider_user_id) DO NOTHING`,
			acct.IdentityID, acct.ID, acct.Provider.Provider, acct.Provider.ProviderUserID); err != nil {
			return fmt.Errorf("insert identity %s: %w", acct.Email, err)
		}
	}

	if acct.Password != "" {
		normalized, err := password.Normalize(acct.Password)
		if err != nil {
			return fmt.Errorf("normalize password %s: %w", acct.Email, err)
		}
		encoded, err := hasher.Hash(ctx, normalized)
		if err != nil {
			return fmt.Errorf("hash password %s: %w", acct.Email, err)
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO password_credentials (user_id, encoded_hash, created_at, changed_at)
			 VALUES ($1, $2, now(), now())
			 ON CONFLICT (user_id) DO NOTHING`,
			acct.ID, []byte(encoded)); err != nil {
			return fmt.Errorf("insert credential %s: %w", acct.Email, err)
		}
	}
	return nil
}

// runCleanup removes exactly the fixed fixture accounts (by their frozen IDs,
// cascading to identities, credentials, reset tokens, sessions, and email
// jobs) and any runtime-random proof account (by the reserved email prefix).
// It is idempotent: absent rows are not errors.
func runCleanup(ctx context.Context, cfg Config) error {
	db, err := open(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			fmt.Fprintln(os.Stderr, "password-auth-fixture: close database:", closeErr)
		}
	}()

	ids := make([]uuid.UUID, 0, len(fixtureAccounts))
	for _, acct := range fixtureAccounts {
		ids = append(ids, acct.ID)
	}

	// Password registrations have no user FK (they precede the user), so the
	// reserved-prefix rows are removed first, before their later user row.
	if _, err := db.ExecContext(ctx,
		`DELETE FROM password_registrations WHERE email::text LIKE $1`,
		testEmailPrefix+"%@example.invalid"); err != nil {
		return fmt.Errorf("delete proof registrations: %w", err)
	}

	if _, err := db.ExecContext(ctx,
		`DELETE FROM users WHERE id = ANY($1) OR email::text LIKE $2`,
		ids, testEmailPrefix+"%@example.invalid"); err != nil {
		return fmt.Errorf("delete proof users: %w", err)
	}
	return nil
}
