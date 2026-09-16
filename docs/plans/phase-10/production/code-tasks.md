# Code tasks

These tasks change application code only. They need the shared test database
(`make test-db-up`) and no cloud access.

## Task 2

### Provisioning accepts the non-superuser database owner

**Files:**

- Modify: `apps/server/migrations/migrator_provision.go`
- Test: `apps/server/migrations/migrator_provision_test.go`
- Modify: `docs/design/scaling/migration-provisioning.md` (one sentence)

**Interfaces:**

- Produces: unexported
  `provisionDatabase(ctx context.Context, db *sql.DB, owner string) error`.
  Exported `ProvisionDatabase(ctx, db)` keeps its signature and passes the fixed
  owner `aboutme`. The CLI still takes no role input.

RDS never grants superuser. The grants that provisioning installs need only
database ownership, so the check drops `is_superuser` and keeps the owner
requirement. History adoption is unchanged and still requires superuser.

- [ ] **Step 1: Write the failing test**

Append to `migrator_provision_test.go` (add `database/sql`, `fmt`, `net/url`
imports):

```go
func TestProvisionDatabaseAcceptsNonSuperuserOwner(t *testing.T) {
    base := compositionBaseDSN(t)
    admin := compositionAdmin(t, base)
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    suffix := time.Now().UnixNano()
    owner := fmt.Sprintf("aboutme_prov_owner_%d", suffix)
    name := fmt.Sprintf("aboutme_migrate_test_%d_9", suffix)
    const password = "local-provision-owner-test"
    for _, statement := range []string{
        `CREATE ROLE ` + owner + ` LOGIN NOSUPERUSER CREATEROLE PASSWORD '` + password + `'`,
        `CREATE DATABASE ` + name + ` OWNER ` + owner,
    } {
        if _, err := admin.ExecContext(ctx, statement); err != nil {
            t.Fatal(err)
        }
    }
    t.Cleanup(func() {
        cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
        defer done()
        _, _ = admin.ExecContext(cleanup, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
        _, _ = admin.ExecContext(cleanup, `DROP ROLE IF EXISTS `+owner)
    })
    u, err := url.Parse(base)
    if err != nil {
        t.Fatal(err)
    }
    u.User = url.UserPassword(owner, password)
    u.Path = "/" + name
    db, err := sql.Open("pgx", u.String())
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() { _ = db.Close() })

    if err := provisionDatabase(ctx, db, owner); err != nil {
        t.Fatalf("non-superuser owner: %v", err)
    }
    var granted bool
    if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_namespace n CROSS JOIN LATERAL aclexplode(n.nspacl) x WHERE n.oid='public'::regnamespace AND x.grantee='aboutme_migrator'::regrole AND x.privilege_type='CREATE' AND x.is_grantable)`).Scan(&granted); err != nil {
        t.Fatal(err)
    }
    if !granted {
        t.Fatal("migrator lacks schema CREATE with grant option")
    }
    if err := provisionDatabase(ctx, db, "aboutme"); err == nil {
        t.Fatal("a session that is not the named owner was accepted")
    }
}
```

- [ ] **Step 2: Run it and confirm it fails**

```sh
cd apps/server
REQUIRE_TEST_DB=1 TEST_DATABASE_URL='postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable' go test -count=1 -timeout 10m ./migrations -run '^TestProvisionDatabase' -v
```

Expected: build failure, `undefined: provisionDatabase`.

- [ ] **Step 3: Implement**

In `migrator_provision.go`, split the function and replace the two identity
checks:

```go
const provisioningOwner = "aboutme"

// ProvisionDatabase installs the fixed pre-foundation grants as the database
// owner. The owner need not be a superuser.
func ProvisionDatabase(ctx context.Context, db *sql.DB) error {
    return provisionDatabase(ctx, db, provisioningOwner)
}

func provisionDatabase(ctx context.Context, db *sql.DB, owner string) (resultErr error) {
    // body of the former ProvisionDatabase, with the two changes below
}
```

Replace the authority check:

```go
    if user != owner || databaseOwner != owner || (schemaOwner != owner && schemaOwner != "pg_database_owner") {
        return errors.New("migrations: provisioning authority or owner mismatch")
    }
```

Replace the final identity check so it pins the value read at the start:

```go
    if err := verifySessionIdentity(cleanupCtx, conn, pid, owner, superuser); err != nil {
        return err
    }
```

- [ ] **Step 4: Run the package tests**

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL='postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable' go test -race -count=1 -timeout 30m ./migrations ./cmd/migrate
GOGC=50 golangci-lint run ./migrations ./cmd/migrate --tests
```

Expected: PASS and `0 issues.`

- [ ] **Step 5: Update the design sentence**

In `docs/design/scaling/migration-provisioning.md`, change "the existing
bootstrap administrator performs" to "the database owner `aboutme`, which need
not be a superuser, performs". Run `make docs-lint`.

## Task 3

### `db-set-login` sends SCRAM verifiers

**Files:**

- Create: `apps/server/internal/dbroles/login.go`
- Create: `apps/server/internal/dbroles/login_test.go`
- Create: `apps/server/cmd/db-set-login/main.go`
- Create: `apps/server/cmd/db-set-login/main_test.go`

**Interfaces:**

- Produces in `dbroles`:
  - `func ScramVerifier(password string, salt []byte, iterations int) (string, error)`
  - `type LoginPasswords struct{ Migrator, App string }`
  - `func SetLoginVerifiers(ctx context.Context, db *sql.DB, p LoginPasswords, random io.Reader) error`
- Produces the binary `/usr/local/bin/db-set-login`. Environment: `DATABASE_URL`
  (master user, no password), `PGPASSWORD`, `MIGRATOR_PASSWORD`, `APP_PASSWORD`.
  No arguments. Prints `outcome=set roles=2`.

PostgreSQL stores a value already in `SCRAM-SHA-256$` form as the verifier, so
no plaintext password reaches the server or its logs.

- [ ] **Step 1: Write the failing tests**

`login_test.go`:

```go
package dbroles

import (
    "bytes"
    "context"
    "database/sql"
    "encoding/base64"
    "os"
    "strings"
    "testing"
    "time"

    _ "github.com/jackc/pgx/v5/stdlib"
)

func TestScramVerifierMatchesKnownVector(t *testing.T) {
    salt, err := base64.StdEncoding.DecodeString("W22ZaJ0SNY7soEsUEjb6gQ==")
    if err != nil {
        t.Fatal(err)
    }
    got, err := ScramVerifier("pencil", salt, 4096)
    if err != nil {
        t.Fatal(err)
    }
    const want = "SCRAM-SHA-256$4096:W22ZaJ0SNY7soEsUEjb6gQ==$WG5d8oPm3OtcPnkdi4Uo7BkeZkBFzpcXkuLmtbsT4qY=:wfPLwcE6nTWhTAmQ7tl2KeoiWGPlZqQxSrmfPwDl2dU="
    if got != want {
        t.Fatalf("verifier = %s", got)
    }
}

func TestScramVerifierRejectsBadInput(t *testing.T) {
    for _, tc := range []struct {
        salt       []byte
        iterations int
    }{{nil, 4096}, {make([]byte, 15), 4096}, {make([]byte, 16), 4095}} {
        if _, err := ScramVerifier("x", tc.salt, tc.iterations); err == nil {
            t.Fatalf("accepted salt=%d iterations=%d", len(tc.salt), tc.iterations)
        }
    }
}

func TestSetLoginVerifiersRejectsWeakPasswordsBeforeSQL(t *testing.T) {
    strong := strings.Repeat("a", 32)
    for _, p := range []LoginPasswords{
        {Migrator: "short", App: strong},
        {Migrator: strong, App: ""},
        {Migrator: strong, App: strong},
    } {
        if err := SetLoginVerifiers(context.Background(), nil, p, bytes.NewReader(make([]byte, 64))); err == nil {
            t.Fatalf("accepted %d/%d-byte passwords", len(p.Migrator), len(p.App))
        }
    }
}

func TestSetLoginVerifiersWritesOnlyFixedRoles(t *testing.T) {
    dsn := os.Getenv("TEST_DATABASE_URL")
    if dsn == "" {
        if os.Getenv("REQUIRE_TEST_DB") == "1" {
            t.Fatal("TEST_DATABASE_URL is required")
        }
        t.Skip("TEST_DATABASE_URL not set")
    }
    db, err := sql.Open("pgx", dsn)
    if err != nil {
        t.Fatal(err)
    }
    defer db.Close()
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    tx, err := db.BeginTx(ctx, nil)
    if err != nil {
        t.Fatal(err)
    }
    defer func() { _ = tx.Rollback() }()
    p := LoginPasswords{Migrator: strings.Repeat("m", 32), App: strings.Repeat("p", 32)}
    if err := setLoginVerifiers(ctx, tx, p, bytes.NewReader(make([]byte, 64))); err != nil {
        t.Fatal(err)
    }
    rows, err := tx.QueryContext(ctx, `SELECT rolname, rolpassword FROM pg_authid WHERE rolname IN ('aboutme_migrator','aboutme_app') ORDER BY rolname`)
    if err != nil {
        t.Fatal(err)
    }
    defer rows.Close()
    count := 0
    for rows.Next() {
        var name, verifier string
        if err := rows.Scan(&name, &verifier); err != nil {
            t.Fatal(err)
        }
        if !strings.HasPrefix(verifier, "SCRAM-SHA-256$4096:") {
            t.Fatalf("%s stored a non-SCRAM value", name)
        }
        count++
    }
    if count != 2 {
        t.Fatalf("updated %d roles", count)
    }
}
```

`cmd/db-set-login/main_test.go`:

```go
package main

import (
    "bytes"
    "context"
    "strings"
    "testing"

    "github.com/dannyota/aboutme/apps/server/internal/dbroles"
)

func TestRunRejectsArgumentsAndMissingEnvironment(t *testing.T) {
    strong := strings.Repeat("s", 32)
    full := map[string]string{
        "DATABASE_URL": "postgres://aboutme@db.example:5432/aboutme?sslmode=verify-full",
        "MIGRATOR_PASSWORD": strong, "APP_PASSWORD": strong + "x",
    }
    never := func(context.Context, string, dbroles.LoginPasswords) error {
        t.Fatal("set called")
        return nil
    }
    if err := run([]string{"extra"}, mapEnv(full), &bytes.Buffer{}, never); err == nil {
        t.Fatal("accepted an argument")
    }
    for _, key := range []string{"DATABASE_URL", "MIGRATOR_PASSWORD", "APP_PASSWORD"} {
        env := map[string]string{}
        for k, v := range full {
            env[k] = v
        }
        delete(env, key)
        if err := run(nil, mapEnv(env), &bytes.Buffer{}, never); err == nil {
            t.Fatalf("accepted missing %s", key)
        }
    }
}

func TestRunReportsOutcomeWithoutSecrets(t *testing.T) {
    strong := strings.Repeat("s", 32)
    env := map[string]string{
        "DATABASE_URL": "postgres://aboutme@db.example:5432/aboutme?sslmode=verify-full",
        "MIGRATOR_PASSWORD": strong, "APP_PASSWORD": strong + "x",
    }
    var out bytes.Buffer
    err := run(nil, mapEnv(env), &out, func(context.Context, string, dbroles.LoginPasswords) error { return nil })
    if err != nil {
        t.Fatal(err)
    }
    if out.String() != "outcome=set roles=2\n" || strings.Contains(out.String(), strong) {
        t.Fatalf("output %q", out.String())
    }
}

func mapEnv(m map[string]string) func(string) string {
    return func(k string) string { return m[k] }
}
```

- [ ] **Step 2: Run them and confirm they fail**

```sh
cd apps/server
REQUIRE_TEST_DB=1 TEST_DATABASE_URL='postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable' go test -count=1 ./internal/dbroles ./cmd/db-set-login -run 'Scram|LoginVerifiers|TestRun' -v
```

Expected: build failure, `undefined: ScramVerifier`.

- [ ] **Step 3: Implement `login.go`**

```go
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
)

const (
    scramIterations   = 4096
    scramSaltBytes    = 16
    minPasswordBytes  = 32
    maxPasswordBytes  = 1024
)

var verifierPattern = regexp.MustCompile(`^SCRAM-SHA-256\$[0-9]+:[A-Za-z0-9+/=]+\$[A-Za-z0-9+/=]+:[A-Za-z0-9+/=]+$`)

// LoginPasswords holds the two fixed login-role passwords.
type LoginPasswords struct{ Migrator, App string }

// ScramVerifier returns the PostgreSQL SCRAM-SHA-256 verifier for password.
func ScramVerifier(password string, salt []byte, iterations int) (string, error) {
    if len(salt) < scramSaltBytes || iterations < scramIterations {
        return "", errors.New("dbroles: weak SCRAM parameters")
    }
    salted, err := pbkdf2.Key(sha256.New, password, salt, iterations, sha256.Size)
    if err != nil {
        return "", err
    }
    clientKey := hmacSHA256(salted, "Client Key")
    storedKey := sha256.Sum256(clientKey)
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

// SetLoginVerifiers stores verifiers for the two fixed login roles in one
// transaction. It never sends a plaintext password.
func SetLoginVerifiers(ctx context.Context, db *sql.DB, p LoginPasswords, random io.Reader) error {
    if err := validatePasswords(p); err != nil {
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

func validatePasswords(p LoginPasswords) error {
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
    if err := validatePasswords(p); err != nil {
        return err
    }
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
        if !verifierPattern.MatchString(verifier) {
            return errors.New("dbroles: malformed verifier")
        }
        if _, err := tx.ExecContext(ctx, `ALTER ROLE `+role.name+` PASSWORD '`+verifier+`'`); err != nil {
            return fmt.Errorf("dbroles: set %s login: %w", role.name, err)
        }
    }
    return nil
}
```

Add `"time"` to the imports. The role names are fixed literals and the verifier
matches a closed pattern, so the statement cannot carry injected SQL. Use
`crypto/rand.Reader` in production.

- [ ] **Step 4: Implement the command**

`cmd/db-set-login/main.go`:

```go
// Command db-set-login stores SCRAM verifiers for the fixed login roles.
package main

import (
    "context"
    "crypto/rand"
    "database/sql"
    "errors"
    "fmt"
    "io"
    "os"
    "strings"
    "time"

    _ "github.com/jackc/pgx/v5/stdlib"

    "github.com/dannyota/aboutme/apps/server/internal/dbroles"
)

type setFunc func(context.Context, string, dbroles.LoginPasswords) error

func main() {
    if err := run(os.Args[1:], os.Getenv, os.Stdout, setURL); err != nil {
        fmt.Fprintln(os.Stderr, "db-set-login:", err)
        os.Exit(1)
    }
}

func run(args []string, getenv func(string) string, stdout io.Writer, set setFunc) error {
    if len(args) != 0 {
        return errors.New("arguments are not accepted")
    }
    databaseURL := strings.TrimSpace(getenv("DATABASE_URL"))
    p := dbroles.LoginPasswords{Migrator: getenv("MIGRATOR_PASSWORD"), App: getenv("APP_PASSWORD")}
    if databaseURL == "" || p.Migrator == "" || p.App == "" {
        return errors.New("DATABASE_URL, MIGRATOR_PASSWORD and APP_PASSWORD are required")
    }
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    if err := set(ctx, databaseURL, p); err != nil {
        return errors.New("setting login verifiers failed")
    }
    _, err := fmt.Fprintln(stdout, "outcome=set roles=2")
    return err
}

func setURL(ctx context.Context, databaseURL string, p dbroles.LoginPasswords) error {
    db, err := sql.Open("pgx", databaseURL)
    if err != nil {
        return err
    }
    defer db.Close()
    return dbroles.SetLoginVerifiers(ctx, db, p, rand.Reader)
}
```

- [ ] **Step 5: Run the checks**

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL='postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable' go test -race -count=1 ./internal/dbroles ./cmd/db-set-login
go vet ./internal/dbroles ./cmd/db-set-login
GOGC=50 golangci-lint run ./internal/dbroles ./cmd/db-set-login --tests
```

Expected: PASS and `0 issues.` The live test rolls back, so the shared cluster's
role passwords do not change.

## Task 4

### Print listener and provider credential rules

**Files:**

- Modify: `apps/server/internal/config/print.go`
- Modify: `apps/server/internal/config/print_test.go`
- Modify: `apps/server/internal/config/config.go`
- Modify: `apps/server/internal/config/config_test.go`
- Modify: `apps/web/server/utils/print/redemption.ts`
- Modify: `apps/web/test/print/redemption.test.ts`

**Interfaces:** Production accepts `PRINT_LISTEN_ADDR=172.17.0.1:8081` and
`NUXT_PRINT_ORIGIN=http://172.17.0.1:8081`. Production starts without provider
credentials while `PROVIDER_LOGIN_ENABLED` is not `true`.

- [ ] **Step 1: Write the failing tests**

In `print_test.go`, add these cases to the table in
`TestPrintConfigurationRestrictsPrivateListener`:

```go
        {"prod", "172.17.0.1:8081", true}, {"staging", "172.17.0.1:8081", true},
        {"dev", "172.17.0.1:8081", false}, {"prod", "172.17.0.2:8081", false},
        {"prod", "172.17.0.1:8082", false},
```

In `config_test.go`, add:

```go
func TestLoad_ProdStartsWithoutProviderCredentialsWhenLoginDisabled(t *testing.T) {
    t.Parallel()
    for _, flag := range []string{"", "false"} {
        _, err := config.Load(env(map[string]string{
            "DATABASE_URL":           "postgres://user:pass@localhost:5432/aboutme",
            "PUBLIC_ORIGIN":          "https://aboutme.vn",
            "ENV":                    "prod",
            "TRUSTED_PROXY_CIDRS":    "127.0.0.1/32",
            "PROVIDER_LOGIN_ENABLED": flag,
        }))
        if err != nil {
            t.Fatalf("flag %q: %v", flag, err)
        }
    }
}
```

Every existing test that expects a missing-credential error in `prod` or
`staging` (the `GOOGLE_`, `GITHUB_` and `LINKEDIN_` cases near lines 1079, 1102,
1210, 1235, 1340 and 1369) sets `"PROVIDER_LOGIN_ENABLED": "true"` in its
environment map.

In `redemption.test.ts`, add `'http://172.17.0.1:8081'` to the `valid` list, add
`'http://172.17.0.2:8081'` to the rejected list, and rename the test to
`'allows only the five configured direct origins'`.

- [ ] **Step 2: Run them and confirm they fail**

```sh
cd apps/server && go test -count=1 ./internal/config
cd ../web && npx vitest run test/print/redemption.test.ts
```

Expected: the new Go cases fail, and the TypeScript test rejects the new origin.

- [ ] **Step 3: Implement**

`print.go`:

```go
    allowed := (address == "127.0.0.1:8081" || address == "172.17.0.1:8081") && requiresProductionTrustBoundary(environment)
```

`config.go`: move the `loadProviderLoginFlag` call above the three credential
loaders, and make each loader take the flag:

```go
func requiresProviderCredentials(env string, providerLogin bool) bool {
    return providerLogin && requiresProductionOAuthCredentials(env)
}
```

Each of `loadGoogleCredentials`, `loadGitHubCredentials` and
`loadLinkedInCredentials` gains a final `providerLogin bool` parameter and tests
`requiresProviderCredentials(env, providerLogin)` instead of
`requiresProductionOAuthCredentials(env)`. Update their doc comments to say the
credentials are required only when provider login is enabled.

`redemption.ts`: add `'http://172.17.0.1:8081',` to `printOrigins`.

- [ ] **Step 4: Run the checks**

```sh
make server-build server-vet server-test
cd apps/web && npx vitest run test/print/redemption.test.ts && cd ../..
make web-lint web-typecheck
```

Expected: all pass.

## Task 5

### Server image verifies RDS TLS and ships `db-set-login`

**Files:**

- Modify: `deploy/server.Dockerfile`

**Interfaces:** The image contains `/etc/ssl/rds/global-bundle.pem` and
`/usr/local/bin/db-set-login`.

- [ ] **Step 1: Pin the bundle hash**

```sh
curl -fsSL https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem | sha256sum
```

Record the hash as `RDS_BUNDLE_SHA256` in the Dockerfile.

- [ ] **Step 2: Edit the Dockerfile**

In the build stage, after the existing `go build` lines:

```dockerfile
RUN CGO_ENABLED=0 go -C apps/server build -trimpath -ldflags="-s -w" -o /out/db-set-login ./cmd/db-set-login
ARG RDS_BUNDLE_SHA256=<hash from step 1>
RUN wget -qO /out/rds-global-bundle.pem https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem \
    && echo "${RDS_BUNDLE_SHA256}  /out/rds-global-bundle.pem" | sha256sum -c -
```

In the runtime stage, beside the other `COPY` lines:

```dockerfile
COPY --from=build /out/db-set-login /usr/local/bin/db-set-login
COPY --from=build /out/rds-global-bundle.pem /etc/ssl/rds/global-bundle.pem
```

Write the real hash in place of `<hash from step 1>`; the build fails on a
mismatch.

- [ ] **Step 3: Build and smoke**

```sh
podman build -f deploy/server.Dockerfile -t localhost/aboutme/server:probe .
podman run --rm --entrypoint sh localhost/aboutme/server:probe -c 'test -s /etc/ssl/rds/global-bundle.pem && echo bundle-ok'
podman run --rm --entrypoint /usr/local/bin/db-set-login localhost/aboutme/server:probe; echo "exit=$?"
```

Expected: `bundle-ok`, then the required-variables message and `exit=1`.
