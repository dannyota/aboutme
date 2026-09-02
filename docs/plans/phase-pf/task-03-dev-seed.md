# Task 03 — `dev-seed` command and native wiring

**Acceptance:** AC-OPS-021.

**Depends on:** T00 (rows exist). Independent of T01 and T02.

**Owned paths:** `apps/server/cmd/dev-seed/{main.go,seed.go,seed_test.go}`,
`apps/server/cmd/dev-seed/testdata/full.json`. Shared edits the owner makes from
the report: `scripts/dev-native.sh`, root `Makefile`,
`docs/runbooks/native-development.md`.

## Interfaces

- Consumes: `password.NewHasher`, `password.DefaultHashPolicy`,
  `password.NewAdmission`, `password.Normalize` from
  `apps/server/internal/auth/password`; the `users`, `password_credentials`, and
  `resumes` tables as defined by migrations 00004 and 00008.
- Produces: the binary `dev-seed` with `seed --database-url <dsn>` and
  `cleanup --database-url <dsn>`; package-level `runSeedWithDB(ctx, db)` and
  `runCleanupWithDB(ctx, db)` for tests; the seed identities T06 signs in with.

## Contract

Fixed IDs, idempotent `seed`, exact `cleanup`, database guard `aboutme_dev` on
`127.0.0.1`, password hashed with the production policy, resume document from an
embedded copy of `packages/schema/fixtures/full.json` that a test keeps
byte-identical. Never overwrite an existing credential or document. Fail when
the seed email exists under another ID.

## Steps

- [ ] **Step 1: Copy the fixture**

```sh
mkdir -p apps/server/cmd/dev-seed/testdata
cp packages/schema/fixtures/full.json apps/server/cmd/dev-seed/testdata/full.json
```

- [ ] **Step 2: Write the failing static tests**

Create `apps/server/cmd/dev-seed/seed_test.go`:

```go
package main

import (
    "bytes"
    "context"
    "database/sql"
    "os"
    "strings"
    "testing"

    _ "github.com/jackc/pgx/v5/stdlib"
)

const validDSN = "postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable"

func TestParseConfigGuardsTheDatabase(t *testing.T) {
    t.Parallel()
    for _, tt := range []struct {
        name string
        args []string
        want string // substring the error must contain; empty = must succeed
    }{
        {name: "seed ok", args: []string{"seed", "--database-url", validDSN}},
        {name: "cleanup ok", args: []string{"cleanup", "--database-url", validDSN}},
        {name: "missing subcommand", args: nil, want: "subcommand"},
        {name: "unknown subcommand", args: []string{"drop", "--database-url", validDSN}, want: "unknown subcommand"},
        {name: "missing url", args: []string{"seed"}, want: "--database-url is required"},
        {name: "test database refused", args: []string{"seed", "--database-url", "postgres://127.0.0.1/aboutme"}, want: "aboutme_dev"},
        {name: "remote host refused", args: []string{"seed", "--database-url", "postgres://db.example.com/aboutme_dev"}, want: "127.0.0.1"},
        {name: "mysql refused", args: []string{"seed", "--database-url", "mysql://127.0.0.1/aboutme_dev"}, want: "postgres"},
    } {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            cmd, cfg, err := parseConfig(tt.args)
            if tt.want == "" {
                if err != nil {
                    t.Fatalf("parseConfig() error = %v", err)
                }
                if cmd != tt.args[0] || cfg.DatabaseURL != validDSN {
                    t.Fatalf("cmd=%q dsn=%q", cmd, cfg.DatabaseURL)
                }
                return
            }
            if err == nil || !strings.Contains(err.Error(), tt.want) {
                t.Fatalf("parseConfig() error = %v, want substring %q", err, tt.want)
            }
        })
    }
}

func TestEmbeddedFixtureMatchesSchemaPackage(t *testing.T) {
    t.Parallel()
    upstream, err := os.ReadFile("../../../../packages/schema/fixtures/full.json")
    if err != nil {
        t.Fatalf("read schema fixture: %v", err)
    }
    if !bytes.Equal(upstream, fullFixture) {
        t.Fatal("cmd/dev-seed/testdata/full.json drifted from packages/schema/fixtures/full.json; copy it again")
    }
    if _, _, _, err := splitResumeDoc(fullFixture); err != nil {
        t.Fatalf("splitResumeDoc: %v", err)
    }
}

func TestSeedIdentitiesAreFixed(t *testing.T) {
    t.Parallel()
    if seedUser.ID.String() != "5d000000-0000-4000-8000-000000000001" ||
        seedResumeID.String() != "5d000000-0000-4000-8000-000000000002" ||
        seedUser.Email != "dev@aboutme.invalid" || seedUser.Password != "aboutme-dev-password-1" {
        t.Fatal("seed identities changed; the spec, runbook, and entry proof pin them")
    }
}
```

Add the live test in the same file. It uses the shared `aboutme` test database
through `TEST_DATABASE_URL` and the DB-level entry points, so the CLI guard
(which only admits `aboutme_dev`) is bypassed on purpose:

```go
func openTestDB(t *testing.T) *sql.DB {
    t.Helper()
    dsn := os.Getenv("TEST_DATABASE_URL")
    if dsn == "" {
        t.Skip("TEST_DATABASE_URL is not set")
    }
    db, err := sql.Open("pgx", dsn)
    if err != nil {
        t.Fatalf("open: %v", err)
    }
    t.Cleanup(func() { _ = db.Close() })
    return db
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
    t.Helper()
    var n int
    if err := db.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
        t.Fatalf("%s: %v", query, err)
    }
    return n
}

func TestSeedIsIdempotentAndCleanupIsExact(t *testing.T) {
    db := openTestDB(t)
    ctx := context.Background()
    if err := runCleanupWithDB(ctx, db); err != nil {
        t.Fatalf("pre-clean: %v", err)
    }
    t.Cleanup(func() { _ = runCleanupWithDB(ctx, db) })

    for i := 0; i < 2; i++ {
        if err := runSeedWithDB(ctx, db); err != nil {
            t.Fatalf("seed run %d: %v", i+1, err)
        }
    }
    if n := countRows(t, db, `SELECT count(*) FROM users WHERE id = $1`, seedUser.ID); n != 1 {
        t.Fatalf("users = %d, want 1", n)
    }
    if n := countRows(t, db, `SELECT count(*) FROM password_credentials WHERE user_id = $1`, seedUser.ID); n != 1 {
        t.Fatalf("credentials = %d, want 1", n)
    }
    if n := countRows(t, db, `SELECT count(*) FROM resumes WHERE id = $1 AND user_id = $2 AND live = false AND slug IS NULL AND revision = 1 AND schema_version = 2`, seedResumeID, seedUser.ID); n != 1 {
        t.Fatalf("seed resume rows = %d, want 1 private v2 resume at revision 1", n)
    }

    // An edited document survives a re-seed.
    if _, err := db.ExecContext(ctx, `UPDATE resumes SET title = 'edited', revision = 7 WHERE id = $1`, seedResumeID); err != nil {
        t.Fatal(err)
    }
    if err := runSeedWithDB(ctx, db); err != nil {
        t.Fatalf("re-seed: %v", err)
    }
    if n := countRows(t, db, `SELECT count(*) FROM resumes WHERE id = $1 AND title = 'edited' AND revision = 7`, seedResumeID); n != 1 {
        t.Fatal("re-seed overwrote an existing document")
    }

    if err := runCleanupWithDB(ctx, db); err != nil {
        t.Fatalf("cleanup: %v", err)
    }
    if n := countRows(t, db, `SELECT count(*) FROM users WHERE id = $1`, seedUser.ID) + countRows(t, db, `SELECT count(*) FROM resumes WHERE id = $1`, seedResumeID); n != 0 {
        t.Fatalf("rows after cleanup = %d, want 0", n)
    }
}

func TestSeedFailsWhenEmailBelongsToAnotherAccount(t *testing.T) {
    db := openTestDB(t)
    ctx := context.Background()
    if err := runCleanupWithDB(ctx, db); err != nil {
        t.Fatalf("pre-clean: %v", err)
    }
    other := "5d000000-0000-4000-8000-0000000000ff"
    if _, err := db.ExecContext(ctx, `INSERT INTO users (id, email, name) VALUES ($1, $2, 'Other')`, other, seedUser.Email); err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, other) })

    err := runSeedWithDB(ctx, db)
    if err == nil || !strings.Contains(err.Error(), "exists under a different id") {
        t.Fatalf("runSeedWithDB error = %v, want the different-id refusal", err)
    }
}
```

- [ ] **Step 3: Run and watch them fail**

```sh
cd apps/server && go test ./cmd/dev-seed/ -count=1
```

Expected: `no Go files` or `undefined: parseConfig`.

- [ ] **Step 4: Implement `main.go`**

```go
// Command dev-seed creates one signed-in-ready development account with a
// sample resume in the native development database (aboutme_dev). It is
// idempotent, refuses every other database, and never runs in Compose or the
// cloud (docs/design/security.md, "No operator surface").
//
// Usage:
//
//    dev-seed seed    --database-url <dsn>
//    dev-seed cleanup --database-url <dsn>
package main

import (
    "context"
    "fmt"
    "os"
    "time"

    _ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
    cmd, cfg, err := parseConfig(os.Args[1:])
    if err != nil {
        fmt.Fprintln(os.Stderr, "dev-seed:", err)
        os.Exit(1)
    }
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()
    if err := run(ctx, cmd, cfg); err != nil {
        fmt.Fprintln(os.Stderr, "dev-seed:", err)
        os.Exit(1)
    }
}
```

- [ ] **Step 5: Implement `seed.go`**

```go
package main

import (
    "context"
    "crypto/rand"
    "database/sql"
    _ "embed"
    "encoding/json"
    "errors"
    "fmt"
    "net/url"
    "os"
    "strings"

    "github.com/google/uuid"

    "github.com/dannyota/aboutme/apps/server/internal/auth/password"
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
    if name != seedDatabase {
        return fmt.Errorf("--database-url must target database %q (explicit opt-in), got %q", seedDatabase, name)
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
    var otherID uuid.NullUUID
    if err := db.QueryRowContext(ctx,
        `SELECT id FROM users WHERE email = $1 AND id <> $2`, seedUser.Email, seedUser.ID,
    ).Scan(&otherID); err != nil && !errors.Is(err, sql.ErrNoRows) {
        return fmt.Errorf("check seed email: %w", err)
    }
    if otherID.Valid {
        return fmt.Errorf("seed email %s exists under a different id; remove that account or change the seed", seedUser.Email)
    }

    if _, err := db.ExecContext(ctx,
        `INSERT INTO users (id, email, name, avatar_key) VALUES ($1, $2, $3, NULL)
         ON CONFLICT (id) DO NOTHING`,
        seedUser.ID, seedUser.Email, seedUser.Name); err != nil {
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
    if _, err := db.ExecContext(ctx,
        `INSERT INTO password_credentials (user_id, encoded_hash, created_at, changed_at)
         VALUES ($1, $2, now(), now())
         ON CONFLICT (user_id) DO NOTHING`,
        seedUser.ID, []byte(encoded)); err != nil {
        return fmt.Errorf("insert credential: %w", err)
    }

    var exists bool
    if err := db.QueryRowContext(ctx,
        `SELECT EXISTS(SELECT 1 FROM resumes WHERE id = $1)`, seedResumeID).Scan(&exists); err != nil {
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
    if _, err := db.ExecContext(ctx, `
        INSERT INTO resumes
            (id, user_id, title, slug, live, download_enabled, seo_geo_enabled,
             schema_version, revision, personal_details, content, customization)
        VALUES ($1, $2, $3, NULL, false, true, false, 2, 1, $4, $5, $6)`,
        seedResumeID, seedUser.ID, seedResumeTitle, personalDetails, content, customization); err != nil {
        return fmt.Errorf("insert resume: %w", err)
    }
    return nil
}

// runCleanupWithDB deletes exactly the two seed rows by ID; sessions,
// credentials, and idempotency records cascade from the user.
func runCleanupWithDB(ctx context.Context, db *sql.DB) error {
    if _, err := db.ExecContext(ctx, `DELETE FROM resumes WHERE id = $1`, seedResumeID); err != nil {
        return fmt.Errorf("delete resume: %w", err)
    }
    if _, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, seedUser.ID); err != nil {
        return fmt.Errorf("delete user: %w", err)
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
```

If `users.email` is a `citext` column that rejects a plain `text` comparison,
write the email checks as `email = $1::citext`. If the `users` table has no
`avatar_key` column in the current migrations, drop it from the insert; the
password fixture's insert is the reference.

- [ ] **Step 6: Run to GREEN (static, then live)**

```sh
cd apps/server && go test ./cmd/dev-seed/ -count=1
cd apps/server && TEST_DATABASE_URL='postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable' go test ./cmd/dev-seed/ -count=1 -run 'Seed'
```

Expected: `ok` both times; the second run exercises the live tests against the
shared container's `aboutme` database.

- [ ] **Step 7: Run it for real against the native database**

```sh
cd apps/server && go run ./cmd/dev-seed seed --database-url 'postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable'
```

Then sign in at `http://localhost:20080/login` with the seed credentials and
confirm the resume list shows `Sample resume`. Run the command a second time and
confirm it exits 0 with no change.

- [ ] **Step 8: Report the shared edits for the owner**

`scripts/dev-native.sh`: add after `run_migrations`:

```sh
seed_dev_account() {
  info "--- seed (dev@aboutme.invalid)"
  (
    cd "$ROOT/apps/server"
    go build -o "$BIN_DIR/dev-seed" ./cmd/dev-seed
  )
  "$BIN_DIR/dev-seed" seed --database-url "$DEV_DATABASE_URL"
  info "dev account: dev@aboutme.invalid / aboutme-dev-password-1"
}
```

Call it in `cmd_up` right after `run_migrations`. Add `cmd_seed()` that runs
`ensure_database`, `run_migrations`, and `seed_dev_account`, add `seed` to
`usage()` ("seed create or refresh the development account and sample resume;
idempotent") and to the `main()` case. Root `Makefile`:

```make
dev-seed: ## Create the development account (dev@aboutme.invalid) and sample resume in aboutme_dev; idempotent
    bash scripts/dev-native.sh seed
```

The recipe line is indented with one tab, as every Makefile recipe is.

`docs/runbooks/native-development.md`, in "Start" after the `make dev-native`
block: "The stack seeds one account, `dev@aboutme.invalid` with password
`aboutme-dev-password-1`, and one private sample resume. `make dev-seed` repeats
the seed on its own; it never overwrites edits you made to the sample resume."

- [ ] **Step 9: Owner verification after wiring**

```sh
make dev-native-down && make dev-native
bash -n scripts/dev-native.sh
make operational-test
```

Expected: the `up` log shows the seed line; sign-in works; the operational tests
pass.

## Adversarial checklist

- The guard rejects the `aboutme` test database, any non-loopback host, and any
  non-Postgres scheme before a connection is opened.
- The email-under-another-ID case fails before any write.
- A second run performs no `UPDATE`: the live test edits the title and revision
  and proves they survive.
- The fixture drift test fails the build if either copy changes alone.

## Handoff

Report RED and GREEN outputs for steps 3 and 6, the exact insert columns you
used if they differ from step 5, and the three shared edits for the owner.
Suggested commit: `feat(dev): seed a development account and sample resume`.
