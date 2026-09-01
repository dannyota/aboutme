package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

const (
	fixtureDatabase         = "aboutme_dev"
	fixtureClientNamePrefix = "aboutme MCP UAT "
	fixtureRedirectURI      = "http://127.0.0.1:20090/callback"
)

var fixtureRedirectsJSON = `["` + fixtureRedirectURI + `"]`

type Config struct {
	DatabaseURL string
	ClientName  string
}

type fixtureAccount struct {
	ID             uuid.UUID
	IdentityID     uuid.UUID
	Email          string
	Name           string
	Provider       string
	ProviderUserID string
}

var fixtureUser = fixtureAccount{
	ID:             uuid.MustParse("53000000-0000-4000-8000-000000000001"),
	IdentityID:     uuid.MustParse("53000000-0000-4000-8000-000000000011"),
	Email:          "bob@example.invalid",
	Name:           "Bob Local",
	Provider:       "google",
	ProviderUserID: "uat-google-003",
}

func parseConfig(args []string) (string, Config, error) {
	if len(args) == 0 {
		return "", Config{}, errors.New("subcommand is required (seed or cleanup)")
	}
	cmd := args[0]
	if cmd != "seed" && cmd != "cleanup" {
		return "", Config{}, fmt.Errorf("unknown subcommand %q (want seed or cleanup)", cmd)
	}

	var databaseURL, clientName string
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--database-url":
			i++
			if i >= len(args) {
				return "", Config{}, errors.New("--database-url requires a value")
			}
			databaseURL = args[i]
		case "--client-name":
			i++
			if i >= len(args) {
				return "", Config{}, errors.New("--client-name requires a value")
			}
			clientName = args[i]
		default:
			return "", Config{}, fmt.Errorf("unknown argument %q", args[i])
		}
	}
	if err := validateDatabaseURL(databaseURL); err != nil {
		return "", Config{}, err
	}
	if err := validateClientName(clientName); err != nil {
		return "", Config{}, err
	}
	return cmd, Config{DatabaseURL: databaseURL, ClientName: clientName}, nil
}

func validateClientName(name string) error {
	if name == "" {
		return errors.New("--client-name is required")
	}
	suffix, ok := strings.CutPrefix(name, fixtureClientNamePrefix)
	if !ok {
		return errors.New("--client-name must use the reserved prefix and a lowercase UUIDv4")
	}
	parsed, err := uuid.Parse(suffix)
	if err != nil || parsed.String() != suffix || parsed.Version() != 4 || parsed.Variant() != uuid.RFC4122 {
		return errors.New("--client-name must use the reserved prefix and a lowercase UUIDv4")
	}
	return nil
}

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
		return fmt.Errorf("--database-url must target database %q, got %q", fixtureDatabase, name)
	}
	return nil
}

func run(ctx context.Context, cmd string, cfg Config) error {
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	switch cmd {
	case "seed":
		if err := cleanFixture(ctx, db, cfg.ClientName); err != nil {
			return err
		}
		return seedFixture(ctx, db)
	case "cleanup":
		return cleanFixture(ctx, db, cfg.ClientName)
	default:
		return fmt.Errorf("unknown subcommand %q", cmd)
	}
}

func seedFixture(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin seed: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO users (id, email, name, avatar_key) VALUES ($1, $2, $3, NULL)`,
		fixtureUser.ID, fixtureUser.Email, fixtureUser.Name); err != nil {
		return fmt.Errorf("insert fixture user: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO identities (id, user_id, provider, provider_user_id)
		 VALUES ($1, $2, $3, $4)`,
		fixtureUser.IdentityID, fixtureUser.ID, fixtureUser.Provider,
		fixtureUser.ProviderUserID); err != nil {
		return fmt.Errorf("insert fixture identity: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit seed: %w", err)
	}
	return nil
}

func cleanFixture(ctx context.Context, db *sql.DB, clientName string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin cleanup: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM oauth_clients
		 WHERE client_name = $1 AND redirect_uris = $2::jsonb`,
		clientName, fixtureRedirectsJSON); err != nil {
		return fmt.Errorf("delete fixture clients: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM users WHERE id = $1 AND email::text = $2`,
		fixtureUser.ID, fixtureUser.Email); err != nil {
		return fmt.Errorf("delete fixture user: %w", err)
	}

	checks := []struct {
		name  string
		query string
		args  []any
	}{
		{
			name:  "fixture user ID",
			query: `SELECT count(*) FROM users WHERE id = $1`,
			args:  []any{fixtureUser.ID},
		},
		{
			name:  "fixture email",
			query: `SELECT count(*) FROM users WHERE email::text = $1`,
			args:  []any{fixtureUser.Email},
		},
		{
			name:  "fixture identity",
			query: `SELECT count(*) FROM identities WHERE provider = $1 AND provider_user_id = $2`,
			args:  []any{fixtureUser.Provider, fixtureUser.ProviderUserID},
		},
		{
			name:  "fixture client",
			query: `SELECT count(*) FROM oauth_clients WHERE client_name = $1 AND redirect_uris = $2::jsonb`,
			args:  []any{clientName, fixtureRedirectsJSON},
		},
		{
			name: "fixture OAuth rows",
			query: `SELECT
			  (SELECT count(*) FROM oauth_authorization_codes WHERE user_id = $1) +
			  (SELECT count(*) FROM oauth_grants WHERE user_id = $1) +
			  (SELECT count(*) FROM oauth_tokens WHERE user_id = $1)`,
			args: []any{fixtureUser.ID},
		},
		{
			name:  "fixture resumes",
			query: `SELECT count(*) FROM resumes WHERE user_id = $1`,
			args:  []any{fixtureUser.ID},
		},
	}
	for _, check := range checks {
		var count int64
		if err := tx.QueryRowContext(ctx, check.query, check.args...).Scan(&count); err != nil {
			return fmt.Errorf("verify %s cleanup: %w", check.name, err)
		}
		if count != 0 {
			return fmt.Errorf("verify %s cleanup: %d rows remain", check.name, count)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit cleanup: %w", err)
	}
	return nil
}
