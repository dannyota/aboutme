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
	personalDetails, err = withoutPhoto(doc.PersonalDetails)
	if err != nil {
		return nil, nil, nil, err
	}
	return personalDetails, doc.Content, doc.Customization, nil
}

// withoutPhoto drops the fixture's photo reference: the seed has no media
// backend, and a key without bytes fails the owner photo read. It removes the
// top-level member directly from the original JSON so all remaining bytes and
// key order stay unchanged.
func withoutPhoto(raw json.RawMessage) (json.RawMessage, error) {
	if !json.Valid(raw) {
		return nil, errors.New("decode personalDetails: invalid JSON")
	}
	entries, err := scanObjectEntries(raw)
	if err != nil {
		return nil, fmt.Errorf("decode personalDetails: %w", err)
	}
	for index, entry := range entries {
		if entry.key != "photo" {
			continue
		}
		removeStart := entry.leadingStart
		removeEnd := entry.valueEnd
		if entry.commaStart >= 0 {
			removeEnd = entry.commaStart + 1
		} else if index > 0 {
			removeStart = entries[index-1].commaStart
		}
		out := make([]byte, 0, len(raw)-(removeEnd-removeStart))
		out = append(out, raw[:removeStart]...)
		out = append(out, raw[removeEnd:]...)
		return out, nil
	}
	return append(json.RawMessage(nil), raw...), nil
}

type jsonObjectEntry struct {
	key          string
	leadingStart int
	valueEnd     int
	commaStart   int
}

func scanObjectEntries(raw []byte) ([]jsonObjectEntry, error) {
	index := skipJSONSpace(raw, 0)
	if index >= len(raw) || raw[index] != '{' {
		return nil, errors.New("personalDetails must be a JSON object")
	}
	index++
	entries := make([]jsonObjectEntry, 0)
	for {
		leadingStart := index
		index = skipJSONSpace(raw, index)
		if index >= len(raw) {
			return nil, errors.New("personalDetails object is truncated")
		}
		if raw[index] == '}' {
			if skipJSONSpace(raw, index+1) != len(raw) {
				return nil, errors.New("personalDetails has trailing data")
			}
			return entries, nil
		}
		keyEnd, err := scanJSONString(raw, index)
		if err != nil {
			return nil, err
		}
		var key string
		if err := json.Unmarshal(raw[index:keyEnd], &key); err != nil {
			return nil, fmt.Errorf("decode personalDetails key: %w", err)
		}
		index = skipJSONSpace(raw, keyEnd)
		if index >= len(raw) || raw[index] != ':' {
			return nil, errors.New("personalDetails object key is missing a colon")
		}
		index = skipJSONSpace(raw, index+1)
		valueEnd, err := scanJSONValue(raw, index)
		if err != nil {
			return nil, err
		}
		index = skipJSONSpace(raw, valueEnd)
		commaStart := -1
		if index < len(raw) && raw[index] == ',' {
			commaStart = index
			index++
			nextIndex := skipJSONSpace(raw, index)
			if nextIndex >= len(raw) || raw[nextIndex] == '}' {
				return nil, errors.New("personalDetails object has a trailing comma")
			}
		} else if index >= len(raw) || raw[index] != '}' {
			return nil, errors.New("personalDetails object member is missing a comma")
		}
		entries = append(entries, jsonObjectEntry{
			key:          key,
			leadingStart: leadingStart,
			valueEnd:     valueEnd,
			commaStart:   commaStart,
		})
		if commaStart < 0 {
			if skipJSONSpace(raw, index+1) != len(raw) {
				return nil, errors.New("personalDetails has trailing data")
			}
			return entries, nil
		}
	}
}

func skipJSONSpace(raw []byte, index int) int {
	for index < len(raw) {
		switch raw[index] {
		case ' ', '\t', '\r', '\n':
			index++
		default:
			return index
		}
	}
	return index
}

func scanJSONString(raw []byte, index int) (int, error) {
	if index >= len(raw) || raw[index] != '"' {
		return 0, errors.New("personalDetails object key is not a JSON string")
	}
	for index = index + 1; index < len(raw); index++ {
		switch raw[index] {
		case '\\':
			index++
		case '"':
			return index + 1, nil
		default:
			if raw[index] < 0x20 {
				return 0, errors.New("personalDetails contains an invalid JSON string")
			}
		}
	}
	return 0, errors.New("personalDetails contains an unterminated JSON string")
}

func scanJSONValue(raw []byte, index int) (int, error) {
	if index >= len(raw) {
		return 0, errors.New("personalDetails object member has no value")
	}
	switch raw[index] {
	case '"':
		return scanJSONString(raw, index)
	case '{':
		return scanJSONContainer(raw, index, '}')
	case '[':
		return scanJSONContainer(raw, index, ']')
	case 't':
		return scanJSONLiteral(raw, index, "true")
	case 'f':
		return scanJSONLiteral(raw, index, "false")
	case 'n':
		return scanJSONLiteral(raw, index, "null")
	default:
		end := index
		for end < len(raw) && !strings.ContainsRune(" \t\r\n,]}", rune(raw[end])) {
			end++
		}
		if end == index || !json.Valid(raw[index:end]) {
			return 0, errors.New("personalDetails contains an invalid JSON value")
		}
		return end, nil
	}
}

func scanJSONLiteral(raw []byte, index int, literal string) (int, error) {
	end := index + len(literal)
	if end > len(raw) || string(raw[index:end]) != literal {
		return 0, errors.New("personalDetails contains an invalid JSON literal")
	}
	return end, nil
}

func scanJSONContainer(raw []byte, index int, closing byte) (int, error) {
	index++
	for {
		index = skipJSONSpace(raw, index)
		if index >= len(raw) {
			return 0, errors.New("personalDetails contains an unterminated JSON value")
		}
		if raw[index] == closing {
			return index + 1, nil
		}
		var err error
		if closing == '}' {
			keyEnd, keyErr := scanJSONString(raw, index)
			if keyErr != nil {
				return 0, keyErr
			}
			index = skipJSONSpace(raw, keyEnd)
			if index >= len(raw) || raw[index] != ':' {
				return 0, errors.New("personalDetails object key is missing a colon")
			}
			index = skipJSONSpace(raw, index+1)
		}
		index, err = scanJSONValue(raw, index)
		if err != nil {
			return 0, err
		}
		index = skipJSONSpace(raw, index)
		if index < len(raw) && raw[index] == ',' {
			index++
			continue
		}
		if index < len(raw) && raw[index] == closing {
			return index + 1, nil
		}
		return 0, errors.New("personalDetails JSON value is missing a comma")
	}
}
