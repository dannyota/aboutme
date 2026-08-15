package main

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/media"
)

// frozenNow is the single accepted --now value. Freezing it keeps every seed
// and cleanup deterministic: the tombstone age, discovery generation, and
// resume revisions are all derived from this one injected clock.
const frozenNow = "2035-01-01T00:00:00Z"

// fixtureDatabase is the only database name the command will touch. Matching
// it by exact name is the explicit opt-in that keeps the fixture from ever
// mutating aboutme_dev or the test aboutme database.
const fixtureDatabase = "aboutme_p5a_fixture"

// Config holds the validated seed/cleanup inputs.
type Config struct {
	DatabaseURL string
	MediaRoot   string
	Now         time.Time
}

// parseConfig parses and validates the seed/cleanup command line. root is the
// repository root the media root is resolved and confined below.
func parseConfig(args []string, root string) (string, Config, error) {
	if len(args) == 0 {
		return "", Config{}, errors.New("subcommand is required (seed or cleanup)")
	}
	cmd := args[0]
	if cmd != "seed" && cmd != "cleanup" {
		return "", Config{}, fmt.Errorf("unknown subcommand %q (want seed or cleanup)", cmd)
	}

	var databaseURL, mediaRoot, nowStr string
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--database-url":
			i++
			if i >= len(args) {
				return "", Config{}, errors.New("--database-url requires a value")
			}
			databaseURL = args[i]
		case "--media-root":
			i++
			if i >= len(args) {
				return "", Config{}, errors.New("--media-root requires a value")
			}
			mediaRoot = args[i]
		case "--now":
			i++
			if i >= len(args) {
				return "", Config{}, errors.New("--now requires a value")
			}
			nowStr = args[i]
		default:
			return "", Config{}, fmt.Errorf("unknown argument %q", args[i])
		}
	}

	if err := validateDatabaseURL(databaseURL); err != nil {
		return "", Config{}, err
	}
	resolvedMediaRoot, err := resolveMediaRoot(mediaRoot, root)
	if err != nil {
		return "", Config{}, err
	}
	now, err := time.Parse(time.RFC3339, nowStr)
	if err != nil {
		return "", Config{}, fmt.Errorf("--now must be an RFC3339 timestamp: %w", err)
	}
	if nowStr != frozenNow {
		return "", Config{}, fmt.Errorf("--now must be exactly %s (fixture clock is frozen)", frozenNow)
	}

	return cmd, Config{DatabaseURL: databaseURL, MediaRoot: resolvedMediaRoot, Now: now}, nil
}

// validateDatabaseURL enforces loopback-only, postgres-only, and the exact
// fixture database name before any connection is opened.
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

// resolveMediaRoot canonicalizes the media root against the repository root
// and rejects anything that is the filesystem root, the repository root, or a
// path that escapes the repository.
func resolveMediaRoot(raw, root string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("--media-root is required")
	}
	root = filepath.Clean(root)
	var candidate string
	if filepath.IsAbs(raw) {
		candidate = raw
	} else {
		candidate = filepath.Join(root, raw)
	}
	candidate = filepath.Clean(candidate)
	if candidate == string(os.PathSeparator) || candidate == root {
		return "", fmt.Errorf("--media-root must name a directory below the repository root, got %q", raw)
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("--media-root must stay below the repository root, got %q", raw)
	}
	return candidate, nil
}

// Frozen fixture identities. These are literal so the capture script's
// expected routes and response hashes are deterministic, and the fixed UUIDs
// can never collide with real development data in aboutme_dev.
var (
	fixtureOwnerID = uuid.MustParse("51000000-0000-4000-8000-000000000000")

	fixtureResumes = []resumeFixture{
		{ID: uuid.MustParse("51000000-0000-4000-8000-000000000001"), Slug: "p5a-live-photo", Live: true, SEOGeo: true, Revision: 11, HasPhoto: true},
		{ID: uuid.MustParse("51000000-0000-4000-8000-000000000002"), Slug: "p5a-live-noindex", Live: true, SEOGeo: false, Revision: 12, HasPhoto: false},
		{ID: uuid.MustParse("51000000-0000-4000-8000-000000000003"), Slug: "p5a-private", Live: false, SEOGeo: false, Revision: 13, HasPhoto: false},
	}

	fixtureTombstoneSlug = "p5a-renamed-old"
	fixtureGeneration    = int64(41)
	fixturePhotoKey      = "p5a-fixture/51000000-0000-4000-8000-000000000001.png"
	fixtureOwnerEmail    = "p5a-fixture@example.invalid"
	fixtureOwnerName     = "P5A Fixture Owner"
)

// resumeFixture is one deterministic resume row the seed writes.
type resumeFixture struct {
	ID       uuid.UUID
	Slug     string
	Live     bool
	SEOGeo   bool
	Revision int64
	HasPhoto bool
}

// seedPlan is the complete deterministic fixture state, derived only from
// Config. Every field is literal except the tombstone timestamp, which is
// always exactly one hour before the frozen --now clock.
type seedPlan struct {
	OwnerID             uuid.UUID
	OwnerEmail          string
	OwnerName           string
	Resumes             []resumeFixture
	TombstoneSlug       string
	TombstoneReleasedAt time.Time
	Generation          int64
	PhotoKey            string
}

func buildSeedPlan(cfg Config) seedPlan {
	return seedPlan{
		OwnerID:             fixtureOwnerID,
		OwnerEmail:          fixtureOwnerEmail,
		OwnerName:           fixtureOwnerName,
		Resumes:             fixtureResumes,
		TombstoneSlug:       fixtureTombstoneSlug,
		TombstoneReleasedAt: cfg.Now.Add(-time.Hour),
		Generation:          fixtureGeneration,
		PhotoKey:            fixturePhotoKey,
	}
}

// The fixture command is self-contained: the current-v2 document and the
// source photo are embedded, so the binary never depends on a working
// directory or on testdata remaining adjacent to the source checkout.
//
//go:embed testdata/resume-v2.json
var resumeV2JSON []byte

//go:embed testdata/photo.png
var photoPNG []byte

// fixtureTombstoneID is a fixed UUID for the seeded tombstone row, distinct
// from every resume and owner UUID.
var fixtureTombstoneID = uuid.MustParse("53000000-0000-4000-8000-000000000001")

// run dispatches the validated subcommand.
func run(ctx context.Context, cmd string, cfg Config) error {
	plan := buildSeedPlan(cfg)
	switch cmd {
	case "seed":
		return runSeed(ctx, cfg, plan)
	case "cleanup":
		return runCleanup(ctx, cfg, plan)
	default:
		return fmt.Errorf("unknown subcommand %q", cmd)
	}
}

// runSeed writes the complete deterministic fixture state. It is idempotent:
// re-running against an already-seeded database and media root is a no-op
// rather than an error.
func runSeed(ctx context.Context, cfg Config, plan seedPlan) error {
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			fmt.Fprintln(os.Stderr, "p5a-native-fixture: close database:", closeErr)
		}
	}()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	personalDetails, content, customization, err := splitResumeDoc(resumeV2JSON)
	if err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx,
		`INSERT INTO users (id, email, name) VALUES ($1, $2, $3) ON CONFLICT (id) DO NOTHING`,
		plan.OwnerID, plan.OwnerEmail, plan.OwnerName); err != nil {
		return fmt.Errorf("insert owner: %w", err)
	}

	for _, r := range plan.Resumes {
		var exists bool
		if err := db.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM resumes WHERE id = $1)`, r.ID).Scan(&exists); err != nil {
			return fmt.Errorf("check resume %s: %w", r.Slug, err)
		}
		if exists {
			// Idempotent re-seed. Skipping also avoids the resume cap
			// trigger, which fires on INSERT before any ON CONFLICT
			// resolution and would otherwise reject a re-seed of an
			// already-full owner.
			continue
		}
		pd := personalDetails
		if r.HasPhoto {
			if pd, err = injectPhoto(personalDetails, plan.PhotoKey); err != nil {
				return fmt.Errorf("resume %s: %w", r.Slug, err)
			}
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO resumes
			    (id, user_id, title, slug, live, download_enabled,
			     seo_geo_enabled, schema_version, revision,
			     personal_details, content, customization)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			r.ID, plan.OwnerID, r.Slug, r.Slug, r.Live, true,
			r.SEOGeo, 2, r.Revision, pd, content, customization); err != nil {
			return fmt.Errorf("insert resume %s: %w", r.Slug, err)
		}
	}

	if _, err := db.ExecContext(ctx,
		`INSERT INTO slug_tombstones (id, slug, released_by_user_id, released_at)
		 VALUES ($1, $2, $3, $4) ON CONFLICT (slug) DO NOTHING`,
		fixtureTombstoneID, plan.TombstoneSlug, plan.OwnerID, plan.TombstoneReleasedAt); err != nil {
		return fmt.Errorf("insert tombstone: %w", err)
	}

	if _, err := db.ExecContext(ctx,
		`UPDATE public_state SET discovery_generation = $1 WHERE singleton = true`,
		plan.Generation); err != nil {
		return fmt.Errorf("set discovery generation: %w", err)
	}

	return writePhoto(ctx, cfg.MediaRoot, plan.PhotoKey)
}

// runCleanup removes exactly the fixture rows and the fixture photo object.
// It is idempotent: absent rows or an absent object are not errors.
func runCleanup(ctx context.Context, cfg Config, plan seedPlan) error {
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			fmt.Fprintln(os.Stderr, "p5a-native-fixture: close database:", closeErr)
		}
	}()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	resumeIDs := make([]uuid.UUID, 0, len(plan.Resumes))
	for _, r := range plan.Resumes {
		resumeIDs = append(resumeIDs, r.ID)
	}

	// media_deletion_jobs has no foreign key to resumes, so it must be
	// removed before its referenced rows disappear.
	if _, err := db.ExecContext(ctx,
		`DELETE FROM media_deletion_jobs WHERE resume_id = ANY($1)`, resumeIDs); err != nil {
		return fmt.Errorf("delete media jobs: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`DELETE FROM slug_tombstones WHERE slug = $1`, plan.TombstoneSlug); err != nil {
		return fmt.Errorf("delete tombstone: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`DELETE FROM resumes WHERE id = ANY($1)`, resumeIDs); err != nil {
		return fmt.Errorf("delete resumes: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`DELETE FROM users WHERE id = $1`, plan.OwnerID); err != nil {
		return fmt.Errorf("delete owner: %w", err)
	}

	return deletePhoto(ctx, cfg.MediaRoot, plan.PhotoKey)
}

// splitResumeDoc pulls the three jsonb columns out of the embedded current-v2
// document and confirms the schema version, so the seed never writes a
// document the store did not project.
func splitResumeDoc(raw []byte) (personalDetails, content, customization []byte, err error) {
	var doc struct {
		SchemaVersion   int             `json:"schemaVersion"`
		PersonalDetails json.RawMessage `json:"personalDetails"`
		Content         json.RawMessage `json:"content"`
		Customization   json.RawMessage `json:"customization"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, nil, fmt.Errorf("decode resume-v2.json: %w", err)
	}
	if doc.SchemaVersion != 2 {
		return nil, nil, nil, fmt.Errorf("resume-v2.json schemaVersion = %d, want 2", doc.SchemaVersion)
	}
	return doc.PersonalDetails, doc.Content, doc.Customization, nil
}

// injectPhoto adds the private photo key to a copy of the personalDetails
// object, leaving the embedded base document untouched.
func injectPhoto(personalDetails []byte, photoKey string) ([]byte, error) {
	var pd map[string]json.RawMessage
	if err := json.Unmarshal(personalDetails, &pd); err != nil {
		return nil, fmt.Errorf("decode personalDetails: %w", err)
	}
	pd["photo"] = json.RawMessage(fmt.Sprintf(`{"key":%q}`, photoKey))
	return json.Marshal(pd)
}

// writePhoto normalizes the embedded source photo and stores the normalized
// object. An existing object is treated as already-seeded, never overwritten.
func writePhoto(ctx context.Context, mediaRoot, key string) error {
	normalized, err := media.NormalizePhoto(photoPNG)
	if err != nil {
		return fmt.Errorf("normalize photo: %w", err)
	}
	backend, err := media.NewFS(mediaRoot)
	if err != nil {
		return fmt.Errorf("open media root: %w", err)
	}
	outcome, err := backend.Put(ctx, key, normalized.ContentType, bytes.NewReader(normalized.Bytes), int64(len(normalized.Bytes)))
	if errors.Is(err, media.ErrAlreadyExists) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("write photo: %w", err)
	}
	if outcome != media.PutCreated {
		return fmt.Errorf("write photo: unexpected outcome %d", outcome)
	}
	return nil
}

// deletePhoto removes the fixture photo object. Absence is not an error.
func deletePhoto(ctx context.Context, mediaRoot, key string) error {
	backend, err := media.NewFS(mediaRoot)
	if err != nil {
		return fmt.Errorf("open media root: %w", err)
	}
	if err := backend.Delete(ctx, key); err != nil && !errors.Is(err, media.ErrNotFound) {
		return fmt.Errorf("delete photo: %w", err)
	}
	return nil
}
