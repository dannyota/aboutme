package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/password"
)

const seedDatabase = "aboutme_dev"

//go:embed testdata/full.json
var fullFixture []byte

type Config struct {
	DatabaseURL string
}

type seedAccount struct {
	ID       uuid.UUID
	Email    string
	Name     string
	Password string
}

// Frozen identities. The spec, runbook, and entry proof pin them.
var (
	seedUser = seedAccount{
		ID:       uuid.MustParse("5d000000-0000-4000-8000-000000000001"),
		Email:    "dev@aboutme.invalid",
		Name:     "Dev User",
		Password: "aboutme-dev-password-1",
	}
	seedResumeID    = uuid.MustParse("5d000000-0000-4000-8000-000000000002")
	seedResumeTitle = "Sample resume"
)

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

func validateDatabaseURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return errors.New("--database-url is required")
	}
	connConfig, err := pgx.ParseConfig(raw)
	if err != nil {
		return errors.New("--database-url must be a valid postgres connection string")
	}
	if connConfig.Host != "127.0.0.1" {
		return fmt.Errorf("--database-url must target loopback 127.0.0.1, got %q", connConfig.Host)
	}
	for _, fallback := range connConfig.Fallbacks {
		if fallback.Host != "127.0.0.1" {
			return fmt.Errorf("--database-url must target loopback 127.0.0.1, got %q", fallback.Host)
		}
	}
	if connConfig.Database != seedDatabase {
		return fmt.Errorf("--database-url must target database %q (explicit opt-in), got %q", seedDatabase, connConfig.Database)
	}
	return nil
}

func run(ctx context.Context, cmd string, cfg Config) error {
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			fmt.Fprintln(os.Stderr, "dev-seed: close database:", closeErr)
		}
	}()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	switch cmd {
	case "seed":
		return runSeedWithDB(ctx, db)
	case "cleanup":
		return runCleanupWithDB(ctx, db)
	default:
		return fmt.Errorf("unknown subcommand %q", cmd)
	}
}

// runSeedWithDB creates the user, credential, and resume when their fixed IDs
// are absent. It never updates an existing row.
func runSeedWithDB(ctx context.Context, db *sql.DB) error {
	var existingEmail string
	err := db.QueryRowContext(ctx,
		`SELECT email::text FROM users WHERE id = $1`, seedUser.ID,
	).Scan(&existingEmail)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check seed user id: %w", err)
	}
	if err == nil && existingEmail != seedUser.Email {
		return fmt.Errorf("seed fixed id exists under a different email; remove that account or change the seed")
	}

	var resumeOwnerID uuid.UUID
	err = db.QueryRowContext(ctx,
		`SELECT user_id FROM resumes WHERE id = $1`, seedResumeID,
	).Scan(&resumeOwnerID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check seed resume id: %w", err)
	}
	if err == nil && resumeOwnerID != seedUser.ID {
		return errors.New("seed resume id is owned by a different user; remove that resume or change the seed")
	}

	var otherID uuid.NullUUID
	err = db.QueryRowContext(ctx,
		`SELECT id FROM users WHERE email = $1::citext AND id <> $2`, seedUser.Email, seedUser.ID,
	).Scan(&otherID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check seed email: %w", err)
	}
	if otherID.Valid {
		return fmt.Errorf("seed email %s exists under a different id; remove that account or change the seed", seedUser.Email)
	}

	_, err = db.ExecContext(ctx,
		`INSERT INTO users (id, email, name, avatar_key) VALUES ($1, $2, $3, NULL)
		 ON CONFLICT (id) DO NOTHING`,
		seedUser.ID, seedUser.Email, seedUser.Name)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}

	hasher, err := password.NewHasher(password.DefaultHashPolicy(), rand.Reader, password.NewAdmission())
	if err != nil {
		return fmt.Errorf("create hasher: %w", err)
	}
	normalized, err := password.Normalize(seedUser.Password)
	if err != nil {
		return fmt.Errorf("normalize password: %w", err)
	}
	encoded, err := hasher.Hash(ctx, normalized)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO password_credentials (user_id, encoded_hash, created_at, changed_at)
		 VALUES ($1, $2, now(), now())
		 ON CONFLICT (user_id) DO NOTHING`,
		seedUser.ID, []byte(encoded))
	if err != nil {
		return fmt.Errorf("insert credential: %w", err)
	}

	var exists bool
	err = db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM resumes WHERE id = $1)`, seedResumeID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check resume: %w", err)
	}
	if exists {
		// Skipping also avoids the three-resume cap trigger, which fires on
		// INSERT before ON CONFLICT resolution.
		return nil
	}
	personalDetails, content, customization, err := splitResumeDoc(fullFixture)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO resumes
			(id, user_id, title, slug, live, download_enabled, seo_geo_enabled,
			 schema_version, revision, personal_details, content, customization)
		VALUES ($1, $2, $3, NULL, false, true, false, 2, 1, $4, $5, $6)`,
		seedResumeID, seedUser.ID, seedResumeTitle, personalDetails, content, customization)
	if err != nil {
		return fmt.Errorf("insert resume: %w", err)
	}
	return nil
}

// runCleanupWithDB deletes exactly the two seed rows by ID; sessions,
// credentials, and idempotency records cascade from the user.
func runCleanupWithDB(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin cleanup: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			fmt.Fprintln(os.Stderr, "dev-seed: rollback cleanup:", rollbackErr)
		}
	}()

	var lockedUserEmail string
	err = tx.QueryRowContext(ctx, `SELECT email::text FROM users WHERE id = $1 FOR UPDATE`, seedUser.ID).Scan(&lockedUserEmail)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("lock seed user for cleanup: %w", err)
	}
	if err == nil && lockedUserEmail != seedUser.Email {
		return errors.New("refusing cleanup: seed fixed id exists under a different email")
	}

	var resumeOwnerID uuid.UUID
	err = tx.QueryRowContext(ctx, `SELECT user_id FROM resumes WHERE id = $1`, seedResumeID).Scan(&resumeOwnerID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check seed resume owner for cleanup: %w", err)
	}
	if err == nil && resumeOwnerID != seedUser.ID {
		return errors.New("refusing cleanup: seed resume id is owned by a different user")
	}

	var hasNonSeedResume bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM resumes WHERE user_id = $1 AND id <> $2)`, seedUser.ID, seedResumeID,
	).Scan(&hasNonSeedResume); err != nil {
		return fmt.Errorf("check non-seed resumes: %w", err)
	}
	if hasNonSeedResume {
		return errors.New("refusing cleanup: seed user owns a non-seed resume")
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM resumes WHERE id = $1 AND user_id = $2`, seedResumeID, seedUser.ID); err != nil {
		return fmt.Errorf("delete resume: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = $1 AND email = $2::citext`, seedUser.ID, seedUser.Email); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit cleanup: %w", err)
	}
	return nil
}

// splitResumeDoc pulls the three jsonb columns out of the embedded current
// document and confirms the schema version.
func splitResumeDoc(raw []byte) (personalDetails, content, customization []byte, err error) {
	var doc struct {
		SchemaVersion   int             `json:"schemaVersion"`
		PersonalDetails json.RawMessage `json:"personalDetails"`
		Content         json.RawMessage `json:"content"`
		Customization   json.RawMessage `json:"customization"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, nil, fmt.Errorf("decode full.json: %w", err)
	}
	if doc.SchemaVersion != 2 {
		return nil, nil, nil, fmt.Errorf("full.json schemaVersion = %d, want 2", doc.SchemaVersion)
	}
	return doc.PersonalDetails, doc.Content, doc.Customization, nil
}
