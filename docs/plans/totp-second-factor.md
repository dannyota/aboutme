# Authenticator-app second-factor implementation plan

Status: proposed. The owner approved every marked choice in
`docs/design/totp-second-factor-contract.md` on 2026-09-22. Implementation is
blocked until ADR 0049 is accepted.

**Goal:** Ship v0.4.7 with optional authenticator-app enrollment, verification,
safe replacement and removal, shared recovery, bilingual mail and web flows,
encrypted secret rotation, and a v0.4.7 rollback floor.

**Architecture:** TOTP extends the v0.4.2 pending-authentication and
authentication-epoch boundary. PostgreSQL owns one encrypted credential and one
short-lived enrollment per account. The existing durable release fence rises to
v0.4.7 before the enrollment flag turns on.

**Tech stack:** Go standard-library cryptography, PostgreSQL, sqlc, OpenAPI,
Nuxt 4, Vue 3, TypeScript, a pinned local QR encoder, OpenTofu, ECS, Vitest, and
Playwright.

**Spec:** `docs/design/second-factor-authentication.md`,
`docs/design/totp-second-factor-contract.md`,
`docs/design/totp-key-management.md`,
`docs/design/passkey-second-factor-contract.md`,
`docs/design/passkey-release-fence.md`, ADRs 0048 and 0049,
`docs/design/budgets.md`, `docs/design/security.md`, `docs/design/api.md`, and
the accepted TOTP `AC-AUTH-*` and `AC-SEC-*` rows.

## Prerequisites

- Done: the owner approved all seven contract choices on 2026-09-22. The
  contract and ADR 0049 record the approval.
- ADR 0049 and the TOTP contract pass one fresh Sol review before product code
  starts.
- V0.4.2 is integrated. Its pending, factor, recovery, epoch, mail, web, fence,
  and hosted-browser contracts are green on the v0.4.7 base. Its factor service
  counts active factors of every type through registered counters, decides first
  enrollment from policy existence, and answers passkey options without a
  passkey with `404 factor_not_found`. Its pending page shows a refresh prompt
  for unknown methods.
- Done on base `767a1d14`: the architect closed the accepted contract into the
  API, security, data, deployment, fence, budgets, indexes, decisions, and
  traceability authorities.
- The manager verified the signed upstream `unjs/uqr` v0.1.3 release and its MIT
  license. The frontend pins that version, already present in the lock, as the
  direct local-only QR encoder. A different version needs contract review.

## Global constraints

- V0.4.7 adds TOTP to the existing passkey boundary. It does not change
  passkeys, recovery-code format, resume documents, public pages, PDFs, or MCP
  tools and scopes.
- The exact TOTP profile is HMAC-SHA-1, 20 secret bytes, six digits, a 30-second
  period, `T0=0`, and previous, current, and next-step comparison.
- One account has at most one active TOTP credential and one unconsumed
  ten-minute enrollment. Starting replacement preserves the active credential.
- A TOTP step is single-use across login and reauthentication. Enrollment proof
  becomes the credential's first used step.
- TOTP secrets use AES-256-GCM with a fresh 12-byte nonce, exact associated
  data, application-generated row IDs, derived key IDs, one active key, and at
  most one previous key from two parameter slots. No secret material enters
  logs, mail, export, evidence, URLs, browser persistence, plans, or source.
- The browser renders the canonical provisioning URI locally. It clears the URI
  and grouped secret on every dialog and lifecycle exit.
- Every mutation uses the v0.4.2 current-session, recent-proof, CSRF, Origin,
  epoch, revocation, rotation, and transactional-mail boundary. The lock order
  is user, current session when present, policy, credential, enrollment, then
  pending authentication when applicable.
- First TOTP completion creates a factor policy with a random unique 32-byte
  WebAuthn user handle. It tries at most three candidates. Later passkey
  enrollment reuses the persisted handle exactly.
- Every v0.4.2 and TOTP route, capabilities included, keeps the router's exact
  `Cache-Control: no-store, no-transform`.
- Recovery codes remain one shared set. First factor creates ten. Addition and
  replacement preserve them. Final-factor removal deletes them.
- `TOTP_ENROLLMENT_ENABLED` defaults false. It gates start and completion only.
  Disabled completion consumes its matching enrollment and stores nothing.
- The first production deploy has valid keys and enrollment false. The fence
  rises to numeric release 4007 before enablement.
- Migration 00005 replaces both closed auth-mail constraints for all three TOTP
  mail kinds. Mail insertion remains in the factor transaction.
- Only the app ECS execution role reads exact TOTP parameter ARNs. The
  `aboutme-prod-totp-reencrypt` family reuses the app roles and injection under
  the `totp_reencrypt` fence operation. Scheduled jobs receive no TOTP key
  access.
- An unknown key ID or decryption failure fails closed for that TOTP row only.
  `/readyz` and `internal/publicstate/readiness.go` have no TOTP input. The
  `totp_unavailable` log signal drives a dedicated alarm.
- The final active factor counts passkeys and TOTP. TOTP registers its counter
  with the shared count and changes no passkey route or handler.
- Each TOTP credential has a per-account failure budget and cool-down. Attempt
  mail is capped at one per account per hour.
- Every visible and accessible string has Vietnamese and English copy. User and
  protocol data do not change with locale.
- Only the top manager performs Git operations, pushes, tags, deploys, fence or
  flag changes, and production proof.
- Every worker starts with `git status --short`, reads its authorities, writes a
  failing test first, and stops on a contract conflict or overlapping ownership.
- GitHub CI runs tests, builds, lint, database, migration, browser,
  infrastructure, Semgrep, and gitleaks checks. Workers report them as unrun
  pending hosted verification.
- No worker runs local tests, builds, lint, installs, database writes, browsers,
  development stacks, OpenTofu, AWS, or network commands.
- At most four workers run across all managers and worktrees. Tasks at the same
  order may overlap only when their owned paths are disjoint.
- Code, tests, comments, and living docs do not cite this plan or task numbers.
  Write short plain text with no em dash.

## Review focus

- Race code use across login and reauthentication, enrollment supersession,
  replacement, removal, session rotation, recovery use, and account deletion.
  Each race must have one valid winner and no partial authority.
- Prove first-policy handle collision bounds and exact reuse by later passkey
  enrollment. Prove both closed mail constraints through migration up and down.
- Reject malformed codes, Unicode digits, duplicate JSON, unknown fields, wrong
  enrollment binding, expired state, negative clock, reused steps, ciphertext
  swaps, tampering, and unknown keys before authority changes.
- Trace password, every provider, passkey, TOTP, recovery, reset, consent,
  grants, authorization codes, refresh, bearer authentication, and every
  sensitive action across an epoch change.
- Prove flag-off enrollment, older-client behavior, mixed v0.4.2 and v0.4.7
  startup, floor 4007, same-tag activation, rollback, and restoration.
- Prove exact cache headers on every TOTP route, per-row key failure with green
  `/readyz`, the alarm signal, app-execution-role-only key reads, derived key
  IDs, slot rotation order, and the dedicated one-shot rotation task.
- Prove final-factor removal across types, the per-account cool-down without
  passkey or recovery lockout, and the attempt-mail cap.
- Inspect QR and secret cleanup, phishing guidance, phone and desktop layout,
  keyboard and screen-reader behavior, both locales, mail, evidence, logs,
  metrics, traces, and export for secret material.

## Dispatch order and ownership

| Order | Role      | Model           | Brief                                                               | Dependency                               |
| ----- | --------- | --------------- | ------------------------------------------------------------------- | ---------------------------------------- |
| 0     | architect | `gpt-5.6-sol`   | [Contract acceptance](totp-second-factor/00-contract-acceptance.md) | Owner decisions and fresh design review  |
| 1     | backend   | `gpt-5.6-terra` | [Storage foundation](totp-second-factor/01-storage-foundation.md)   | Accepted contracts                       |
| 1     | backend   | `gpt-5.6-terra` | [TOTP cryptography](totp-second-factor/02-totp-cryptography.md)     | Accepted contracts                       |
| 1     | backend   | `gpt-5.6-terra` | [Security mail](totp-second-factor/03-security-mail.md)             | Accepted contracts                       |
| 1     | devops    | `gpt-5.6-terra` | [Runtime secrets](totp-second-factor/09-runtime-secrets.md)         | Accepted contracts and v0.4.2 fence      |
| 2     | backend   | `gpt-5.6-terra` | [TOTP lifecycle](totp-second-factor/04-totp-lifecycle.md)           | Storage, cryptography, and mail verified |
| 3     | backend   | `gpt-5.6-terra` | [API and composition](totp-second-factor/05-api-composition.md)     | Lifecycle and runtime-secret reports     |
| 4     | frontend  | `gpt-5.6-terra` | [Pending login UI](totp-second-factor/06-pending-login-ui.md)       | OpenAPI client generated                 |
| 4     | frontend  | `gpt-5.6-terra` | [Settings UI](totp-second-factor/07-settings-ui.md)                 | OpenAPI client generated                 |
| 5     | frontend  | `gpt-5.6-terra` | [Web source gate](totp-second-factor/08-web-gate.md)                | Both frontend reports verified           |
| 6     | qa        | `gpt-5.6-terra` | [Hosted browser proof](totp-second-factor/10-browser-proof.md)      | Backend and web integrated               |
| 7     | devops    | `gpt-5.6-terra` | [Release path](totp-second-factor/11-release-path.md)               | QA harness and runtime secrets verified  |
| 8     | reviewer  | `gpt-5.6-sol`   | [Integrated review](totp-second-factor/12-integrated-review.md)     | Every author report verified             |

Order 1 uses all four global worker slots. Later same-order tasks may run
together only while no other manager has consumed a slot. Migrations,
`apps/server/sql/` query files, generated sqlc, OpenAPI, generated web types,
package manifests and lockfiles, the root Makefile, source manifest, Terraform
roots, workflows, deploy scripts, and runbooks stay serialized.

## Integrated verification

After authors finish, the top manager checks every exact file set and obtains
the fresh integrated review. Original authors fix findings. The same reviewer
confirms each fix. The manager then pushes the unchanged candidate branch and
runs `ci.yml` with the full trusted ancestor SHA.

GitHub CI must provide these checks. Do not run them locally:

```bash
make schema-check
make sqlc-check server-test-db server-test-integration server-migration-test
make server-build server-vet server-test
(cd apps/server && golangci-lint run ./...)
(cd apps/server && govulncheck ./...)
make api-check
make web-lint web-typecheck web-test web-build
make web-source-manifest-check
make web-e2e
bash -n deploy/aws/scripts/deploy.sh deploy/aws/scripts/deploy_test.sh deploy/aws/scripts/secrets.sh
bash deploy/aws/scripts/deploy_test.sh
tofu fmt -check -recursive deploy/aws
```

CI also runs `tofu init -backend=false -input=false` and `tofu validate` in
`deploy/aws/bootstrap`, `deploy/aws/probe`, and `deploy/aws/prod`. The docs job
compiles and lists TOTP browser mode through `make operational-test`. Listing is
not acceptance evidence.

The separate `totp-browser-proof` job has a 45-minute timeout. It starts the
repository HTTPS harness, builds the pinned browser image, executes
`make dev-https-totp-check`, uploads only bounded secret-free
`.dev/native-https/evidence/totp-*`, and always stops its stack and runner-local
database. The manager verifies the job SHA and artifact before release.

## Release gate

The top manager owns this sequence:

1. Integrate one exact v0.4.7 file set and obtain the fresh integrated review.
2. Push the release commit alone to `main` and wait for green CI on that commit.
3. Tag `v0.4.7`, wait for release images, and inspect the reviewed production
   OpenTofu plan. Stop on unexpected destroy, fence replacement, or secret
   output.
4. Apply the reviewed secret and task changes. Deploy v0.4.7 with TOTP
   enrollment false.
5. Run the first bounded production proof. Prove health, existing unenrolled,
   passkey and recovery login, existing sessions and grants, and disabled TOTP
   enrollment.
6. Acquire the durable production operation lock and raise the release fence
   conditionally to numeric 4007 and tag v0.4.7. Never lower it on failure.
7. Enable TOTP enrollment through the reviewed OpenTofu apply. Redeploy the same
   tag through a new serialized operation.
8. Run the second bounded production proof with only the named fictional
   account. Prove first TOTP enrollment, pending login, replacement, shared
   recovery, passkey coexistence, both locales, and removal.
9. Remove the proof factor, recovery plaintext, sessions, and browser profile.
   Record bounded redacted evidence. Fix forward at v0.4.7 or later on failure.

GitHub CI cannot observe the deployed flag, release fence, live providers, or
production origin. Steps 5 and 8 are the only local runtime exceptions. The top
manager runs each as the scripted Playwright proof that the QA brief authors, in
run mode `totp-prod-flag-off` for step 5 and `totp-prod-enabled` for step 8. No
one drives the browser by hand. The run uses the pinned browser image from
`deploy/dev-https-browser/Dockerfile` (Playwright 1.62.1 and its bundled
Chromium), not a host install. It reads the fictional account from the
owner-only ignored file `.dev/v0.4.7/production-input/account.env`, mounted
read-only. It reads the setup secret and recovery codes from the page into
process memory, computes codes in that process, and creates any passkey with a
Chrome DevTools virtual authenticator inside the run.

The proof config keeps secrets out of every artifact:

- `outputDir` is `/tmp/totp-proof-artifacts` on the container tmpfs. The
  `finally` step deletes it, and `--rm` discards the tmpfs on every exit.
- `use` sets `screenshot: 'off'`, `video: 'off'`, and `trace: 'off'`. Failure
  page snapshots (the `error-context.md` ARIA snapshot) must also be off; verify
  the exact Playwright 1.62 option before relying on it, and keep the tmpfs
  `outputDir` either way.
- The reporter is `line` only, with no HTML, JSON, or attachment reporter.
- No assertion that prints a received value runs on a locator or value that
  holds a secret, URI, code, recovery code, or password. Those checks compare in
  test code and fail with a fixed message. Each step catches its error and
  rethrows a fixed message naming only the step.
- It prints only fixed step names and outcomes, so no secret enters output,
  evidence, or an agent transcript.

Before each run, the manager confirms at least 8 GiB `MemAvailable`, no other
holder of the shared local-check lock, no aboutme local check process, and no
active aboutme development stack. It builds the image once under the same lock
and timeout with
`podman build --memory=2g --memory-swap=2g deploy/dev-https-browser` and records
the immutable image ID. `run.sh` starts the proof modes with
`--memory=2g --memory-swap=2g --cpus=2` added to its existing hardened
`podman run` flags, so the container has a 2 GiB hard limit, no swap, and two
CPUs. One command runs at a time:

```bash
common_dir=$(git rev-parse --path-format=absolute --git-common-dir)
flock -o -n "$common_dir/aboutme-local-check.lock" \
  timeout --signal=TERM 60m \
  deploy/dev-https-browser/run.sh "$image_id" \
    "$PWD/.dev/v0.4.7/production-input" \
    "$PWD/deploy/dev-https-browser" \
    "$PWD/.dev/v0.4.7/production-evidence" totp-prod-enabled
```

The proof starts no local product stack. The browser profile lives on the
container tmpfs. On success, failure, a blocker, or browser exit, the `finally`
path removes only that account's proof factors, recovery plaintext, and proof
sessions, deletes `outputDir`, then logs out. If cleanup cannot finish, the next
action is the same command with mode `totp-prod-cleanup`. Missing memory floor,
lock, container limit, image, or account file blocks the proof. Never install a
host runner, use an uncapped browser, or use the owner's account.
