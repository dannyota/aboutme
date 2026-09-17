# Code tasks

These tasks change application code only. They need the shared test database
(`make test-db-up`) and no cloud access.

Every task below is complete. Each entry keeps its goal, the files it owns, its
interfaces, and its recorded result; the full steps are in Git history at commit
`a37d863`.

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

Result (2026-09-16): `provisionDatabase` accepts a non-superuser database owner,
and `TestProvisionDatabaseAcceptsNonSuperuserOwner` proves it: a session owned
by a non-superuser role provisions successfully and leaves the migrator role
with schema `CREATE` and grant option, while a session under a different owner
is rejected. `go test -race` passes for `./migrations ./cmd/migrate` and
`golangci-lint run` reports `0 issues.` The design doc sentence now says the
database owner `aboutme`, which need not be a superuser, performs this step.

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

Result (2026-09-16): `ScramVerifier` matches the known SCRAM-SHA-256 test vector
and rejects a salt under 16 bytes or fewer than 4096 iterations.
`SetLoginVerifiers` runs in one transaction, rejects passwords under 32 or over
1024 bytes or a matching pair before touching SQL, and writes only
`aboutme_migrator` and `aboutme_app` with `SCRAM-SHA-256$4096:...` verifiers;
the role names are fixed literals and the verifier is checked against a closed
pattern before use, so the statement cannot carry injected SQL. `db-set-login`
rejects any argument and any missing `DATABASE_URL`, `MIGRATOR_PASSWORD` or
`APP_PASSWORD`, and prints only `outcome=set roles=2`, never a password.
`go test -race`, `go vet` and `golangci-lint run` pass with `0 issues.`; the
live role-write test rolls back so it does not change the shared cluster's
passwords.

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

Result (2026-09-16): the print listener allow-list accepts `172.17.0.1:8081`
only in `prod` and `staging`, and rejects other addresses, ports and
environments. `requiresProviderCredentials` gates the Google, GitHub and
LinkedIn credential loaders on `PROVIDER_LOGIN_ENABLED`, so `config.Load` starts
in `prod` with the flag unset or `false` and no provider credentials; every
existing test that expects a missing-credential error in `prod` or `staging` now
sets `PROVIDER_LOGIN_ENABLED=true`. `redemption.ts` allows
`http://172.17.0.1:8081` as a fifth direct print origin and still rejects
`172.17.0.2:8081`. `make server-build server-vet server-test`, the Vitest
print-redemption test, and `make web-lint web-typecheck` all pass.

## Task 5

### Server image verifies RDS TLS and ships `db-set-login`

**Files:**

- Modify: `deploy/server.Dockerfile`

**Interfaces:** The image contains `/etc/ssl/rds/global-bundle.pem` and
`/usr/local/bin/db-set-login`.

Result (2026-09-16): the build stage compiles `db-set-login`, downloads the RDS
global bundle and checks it against a pinned `RDS_BUNDLE_SHA256` before the
runtime stage copies both `/usr/local/bin/db-set-login` and
`/etc/ssl/rds/global-bundle.pem` into the image. `podman build` succeeds; the
built image reports `bundle-ok`, and running `db-set-login` with no environment
prints the required-variables message and exits 1.
