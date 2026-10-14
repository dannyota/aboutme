# Vietnam host operations delivery and proofs

The [operations design](vietnam-host-operations.md) owns behavior,
authorization, recovery and release order. This companion owns recovery archive
and repair details, proof transport, implementation ownership, tests, and
shipping documentation. The combined contract exceeds 450 lines, so proofs and
delivery have a separate home.

## Host protocol details

`operation-run.sh request` decodes the stdin envelope into the operator verbs in
the main design plus private `run <id>`,
`proof <id> <phase> <nonce> <pass|fail>`, `recovery-copy <id> <sha256>`. Direct
internal verbs are `run <id>` and `prune`. The service executes `run <id>`; only
a saved authorized intent can advance it. Private verbs require root and closed
argument parsing. `prune` is the retention timer's idempotent operation; it
never starts a request, changes a key, or clears a fence. All use the wrapper's
0/1/2 exit convention. The export verb's stdout is ciphertext only; its
hash/receipt metadata is available through request status.

Phases are `planning`, `planned`, `accepted`, `prepared`, `maintenance`,
`switching`, `starting`, `serving`, `reencrypting`, `waiting`, `retiring`,
`retired`, and `closed`; each kind accepts only applicable transitions. Outcomes
are `pending`, `complete`, `refused`, `unknown`, `absent_after_unknown`,
`aborted`, `suspended`, and `unverified`. Account deletion tracks write outcome,
public verification and physical cleanup separately, so pending cleanup cannot
turn into a second delete. State includes `schemaVersion:1`; unknown versions
refuse mutation. Proof phases are the subset defined below. Terminal duplicates
return saved results. A suspended TOTP request alone may leave `closed` through
explicit resume after checks; rebind updates its saved release while suspended.
An aborted or completed request never restarts.

Store a proof reply under the request flock, at most once per nonce, with the
same fsync/rename discipline. Reject mismatched request/digest/phase, unknown or
expired nonce, contradictory duplicates, oversized input, and extra fields. An
identical duplicate returns the saved receipt without advancing twice. Receipt
handling does not acquire a second fence/workload lock: the runner releases its
request flock while waiting and reacquires it before consuming saved evidence. A
new challenge invalidates the old nonce atomically. A runner must not hold the
request flock while waiting for an SSH receipt or proof.

Retained state selects the next reconciliation step, never substitutes for a
fresh fence, runtime, ring or quiescence check. Write intents before Podman
create/replace, flags rename, unit render, marker removal and job dispatch.
Persist only validated result fields. Bound attempts to 1 through 999999, then
refuse further work; the DB application_name remains under 63 bytes. Unknown
commit outcomes use a separate read attempt after old backends disappear.

## Quiescent repair

This is a privileged host procedure for a rotation that cannot serve, not a new
public command or ordinary `rebind`. It ships with TOTP rotation and applies to
view-pass rotation when that feature ships. The operator runs a reviewed repair
bundle from clean, current reviewed `main` over pinned SSH as `aboutme-admin`
with sudo, independently of the broken runner. The repair may replace only the
reviewed operation helpers/units and bind a compatible forward image. It must
support the saved version-1 protocol and existing database schema; no migration,
DB restore, key generation, credential reset, or general installer runs here.

1. Verify the repair bundle's exact hashes, independent review and green CI;
   verify any image's normal tag eligibility, provenance and amd64 digests. The
   image must meet every current floor and enabled flag, and the tested rotation
   contract allowlist. No healthy live release or successful suspension is an
   entry condition. Unknown/malformed state requires a separately reviewed
   recovery; this path never guesses a missing key or marker owner.
2. Stop the old request unit before taking its flock; never treat stop success
   as quiescence. Take the request flock and fence as its same owner, drain
   jobs, then take the workload lock. Stop serving units and all matching
   containers; prove all matching operation and serving DB backends absent. Keep
   timers stopped and prove external maintenance 503. Take a root-only
   checkpoint of request state, fence, marker, flags and key metadata. Preserve
   key bytes, staging, archives and recovery receipts in place, without emitting
   values. Retain the key marker, or create one for this request before code or
   binding changes. Keep it through repair; ordinary starts and installation
   stay blocked.
3. Fsync a repair intent in the request with old/new release records, bundle
   hashes, reason, UTC and stage (`installing|bound|verified`). Retain attempt
   history, target rotation, fence owner, markers, flags and release floors.
   Install only the reviewed files while quiescent; verify all installed hashes
   before running any repaired helper. Interrupted installation stays fenced.
4. If the image changes, reconcile the request, fence tag and existing marker's
   release binding to the new record under that intent. Partial binding blocks
   every start; recovery uses the intent to finish the same binding, never clear
   ownership. Keep the old record as evidence. Invalidate proof nonces and zero
   snapshots and restart the TOTP wait. A bundle-only repair keeps the image.
5. The repaired owning runner resumes the saved rotation forward with the same
   keys and target ring. Only its normal file/secret/unit consistency checks may
   remove a key marker. Then use `reload-live` and fresh readiness, ring and
   external serving checks. No repair step bypasses a marker to start an image.

The operator records `verified` only after installed hashes and running digests
match the repair intent, configuration matches the intended ring/key, and fresh
action-specific serving proof passes (or view-pass closes `unverified` under its
lost-witness rule). TOTP re-encryption, zero snapshots, wait and retirement
still finish through normal resume/retire. Repair success never implies rotation
completion. Failure retains maintenance and the fence; a retry adopts the same
intent after quiescence. No automatic rollback, fence clear or state deletion.

## Recovery archive

TOTP rotation ships this versioned format; deletion needs no archive or age
recipient. `operation-copy` normalizes only TOTP and view keys from current
files, staging and archives into an age-encrypted, uncompressed tar. Plaintext
contains exactly `manifest.json` and the regular files it lists, no host paths,
other credentials, database, request audit or identity. Key files contain
exactly 43 ASCII base64url bytes without a newline. Directories are implicit.
Limit the manifest to 1 MiB, keys to 4096 entries, and decrypted tar to 8 MiB;
refuse export overflow without pruning keys. Manifest JSON has exactly these
required fields:

| Field                       | Contract                                                                                                              |
| --------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| `schemaVersion`             | Integer 1. Unknown versions refuse restore.                                                                           |
| `requestId`, `kind`         | Existing request grammar; kind is `totp-rotate` or `view-pass-rotate`.                                                |
| `createdAt`, `purpose`      | UTC RFC3339; `switch` or `retire` (TOTP only).                                                                        |
| `release`, `minimumRelease` | Closed saved release-json; nonnegative integer host floor at export.                                                  |
| `totpBefore`, `totpAfter`   | Each exactly `{active,previous}`; active is `{slot,keyId}`, previous is that shape or null. Slot is `a                | b`; ID uses the TOTP derived-ID grammar. |
| `viewKeys`                  | Array of exactly `{sha256,restoreState}`; state is always `retired`. Includes current, staged and archived view keys. |
| `files`                     | Array of exactly `{path,sha256}`; digests are 64 lowercase hex over the normalized file bytes.                        |

TOTP paths are exactly `keys/totp/<derived-id>`; view paths are exactly
`keys/view/<sha256>`. Sort files by path and `viewKeys` by digest; reject
duplicates. Every ring/view reference must name a file; every view file needs a
`viewKeys` entry. Unreferenced TOTP files are historical keys, never implicit
active slots. For a TOTP switch, `totpBefore` is the saved old-only ring and
`totpAfter` is staged active plus old previous. For retirement, before is the
two-key ring and after removes previous. View rotation keeps both TOTP ring
records identical. Slot and ID must be distinct within a ring; the union
contains at most two TOTP IDs, each mapped to the same slot across both rings.
The manifest describes intent, not proof of activation. `restoreState:retired`
forbids loading any exported view key on restore, even if still live or staged
when exported. Activation itself continues to use the original request state.
Export validates this contract against the saved request, release and staged key
ID before encryption. Every `sha256` field uses 64 lowercase hex characters.

Restore is an operator-run, reviewed host recovery under maintenance. Decrypt
through a process using the separately held age identity, never through model
context, logs or the repository. Use a fresh root-only destination and validate
the complete archive before installing anything: age authentication, closed JSON
without unknown or duplicate fields, sizes, canonical encoding, digests, derived
TOTP IDs, ring transitions and exact file set. Reject links, special files,
absolute/traversal paths, duplicate tar entries and unlisted/trailing content;
never extract over live files. Restore modes are directories 0700, keys 0400.

Inventory distinct key IDs from **all** restored TOTP credential and enrollment
rows, including expired enrollments, without printing row data. Select a
retained manifest whose before/after union covers that inventory; timestamps and
current slot filenames never establish precedence. Install `totpAfter.active`,
including its staged bytes when activation was partial, and map the union's
other key as previous in its recorded slot. This deliberately retains both keys
even for a retirement export. A pre-switch DB can also move forward through this
ring. More than two row IDs, missing keys or no matching manifest stops
recovery; historical TOTP files require their matching retained manifest, not a
guessed mapping. Do not merge conflicting manifests or rewrite factors to make
them fit.

Generate a fresh view key on the restored host; every historical view key stays
retired. The manifest never overwrites surviving request state, markers, flags
or floors, and never authorizes replay of an interrupted request. Resolve those
under the reviewed recovery before serving. Use an eligible schema-compatible
image at or above the current host floor, manifest floor and all enabled-flag
floors; loss of current floor authority requires reviewed reconstruction before
serving, not adoption of the archived floor. Start a fresh ten-minute wait when
the restored ring becomes active; reboot restarts it. Prove normal
re-encryption, both zero snapshots and TOTP sign-in before retiring the previous
key. Require readiness and external serving proofs; retain both keys on failure.

## Wire schemas

Every request envelope is one JSON object, no unknown fields or extra lines.
`verb` is mandatory; required additional fields are exactly:

| Verb                                            | Fields                                       |
| ----------------------------------------------- | -------------------------------------------- |
| `show`                                          | `kind:"account-delete"`, `slug`              |
| `plan`                                          | `kind:"totp-rotate"` or `"view-pass-rotate"` |
| `apply`, `rebind`                               | `requestId`, `confirmation`                  |
| `status`, `resume`, `verify`, `abort`, `retire` | `requestId`                                  |
| `proof`                                         | `requestId`, `phase`, `nonce`, `result`      |
| `recovery-copy`                                 | `requestId`, `sha256` (64 lowercase hex)     |

IDs, slug and confirmation use the main design's grammar. Proof result is
`pass|fail`. The local `account-delete show` syntax maps to `verb:"show"`.
Arguments of internal `run`/`prune` and `operation-copy` remain positional,
contain no target/key value, and are never accepted as arbitrary envelope verbs.

Successful wrapper output is exactly
`{schemaVersion, requestId, kind, phase, outcome, release, expiresAt, confirmation, action, challenge, recoveryCopy}`.
`schemaVersion` is 1; `requestId` uses the ID grammar; kind is `account-delete`,
`totp-rotate`, or `view-pass-rotate`; phase/outcome use the sets above.
`release` is the existing closed release-json object. `expiresAt` is a UTC
RFC3339 string for a plan, otherwise null. `confirmation` is the exact required
text for a planned apply, otherwise null. Rebind confirmation is constructed
from the separately inspected eligible live release and the saved ring, then
checked by the host; status never invents a new binding. Output is at most 16
KiB. Do not emit a partial success object on failure; stderr is one fixed code:
`invalid_request`, `not_found`, `refused`, `transport_failed`, or
`protocol_failed`. Exit-4 show emits no stdout. Reading a nonexistent request
exits 1.

`action` is a closed discriminated object selected by `kind`:

- Account: `userId`, `reportedSlug`, `shownSlugs`, `returnedSlugs`,
  `writeOutcome`, `publicVerification`, `physicalCleanup`. UUID/slug are null
  after evidence expiry. Shown slugs are a sorted unique array of at most three
  valid slugs; returned slugs have the same shape or null for an unknown result.
  `writeOutcome` is
  `not_started|committed|not_found|mismatch|unknown|present| absent_after_unknown|evidence_expired`
  (one literal, no newline). `publicVerification` is
  `pending|passed|failed|limited|expired`; `physicalCleanup` is
  `pending|complete`. Expiry replaces slug arrays with [].
- TOTP: `activeSlot` (`a|b`), `previousSlot` (`a|b|null`), `nextSlot` (`a|b`),
  `activeKeyId`, `previousKeyId`, `stagedKeyId`, `credentialsOffActive`,
  `enrollmentsOffActive`. Key IDs use the derived-ID grammar; previous/staged
  IDs are null when absent. Counts are nonnegative integers or null before a
  successful read. Both null on a failed refresh, never stale zero evidence.
- View-pass: `oldSecretId`, `newSecretId`, `stagedKeyId`, `witnessVerified`.
  Secret metadata IDs are bounded ASCII strings of 1 to 128 characters, or null
  before creation. Staged ID is `vp1_` plus request ID, or null before staging;
  it names the immutable staged file, not a key hash. `witnessVerified` is a
  boolean, true only after the old/fresh pass comparison.

`challenge` is null or exactly `{requestId,digest,phase,nonce,expiresAt}`;
digest is the saved `sha256:<64 lowercase hex>` server digest, phase is a proof
phase, and expiry is UTC RFC3339. `recoveryCopy` is null or exactly
`{stagedKeyId,sha256,received}` where the digest is the ciphertext hash and
`received` is boolean. A new export invalidates its previous receipt. Never
export file paths, key bytes, environment arrays, raw diagnostics or audit
history through status. Private proof/copy acknowledgements return this same
status object after their durable write.

New server read-command usage diagnostics are exactly
`usage: server totp-key-status`, `usage: server media-deletion-status`, and
`usage: server account-delete {show <slug> | confirm <slug> <user-id> | status <user-id>}`,
with one trailing newline. Fixed failure diagnostics are respectively
`totp-key-status failed`, `media-deletion-status failed`, and
`account-delete status failed`. They include configuration/query/timeout/output
failures without exposing the cause's contents. Existing command diagnostics
remain unchanged except the expanded account-delete usage.

## External proofs

New `scripts/host-operations-proof.sh <id>` reads the saved action over SSH.
Account deletion uses bounded HTTPS requests only, with a 10-second request
limit and five-minute total limit; no login or browser dependency. It checks
known slugs and discovery/showcase membership, discards bodies, and forwards
only fixed outcomes. No screenshots or content artifacts are saved. The host
publishes a `serving` challenge for this check after resolving deletion.

Rotation uses the pinned production Playwright image and a named fictional
account/resume supplied by the primary. The wrapper requires an 8 GiB memory
floor, the shared local-check lock, no active local stack, 2 GiB/no-swap/two-CPU
container limits, and a 60-minute timeout. It stages only allowlisted sources;
no host dependency install. Modes are `hostops-prod-totp` and
`hostops-prod-view-pass`. The origin is exactly `https://aboutme.vn`. These
proofs are run by the primary, not a worker or public CI.

Owner-only 0600 `request.json` contains `requestId`, `action`, `digest`,
`slugs`, and view-pass `witnessSlug`. Rotation `account.env` contains only the
named fictional account's inputs: TOTP `ABOUTME_TEST_EMAIL` and
`ABOUTME_TEST_PASSWORD`; view-pass `PROVIDER` (`google|linkedin`),
`PROVIDER_USERNAME`, `PROVIDER_PASSWORD`. The wrapper checks metadata and mounts
these read-only; only the browser process reads values. Missing inputs or a
provider challenge blocks. Never create accounts/resumes or use the owner's
account. Precheck the witness has sign-in-to-view, photo and PDF enabled.

The browser runs in laptop tmpfs with a 0700 `control/` directory mounted
read-only and `reply/` writable. Wrapper `control/phase.json` and browser
`reply/result.json` are atomically renamed, at most 1 KiB each. Closed fields
are `requestId`, `digest`, `phase`, `nonce` (32-byte random base64url); replies
add `result` (`pass|fail`). Reject symlinks, unknown fields, wrong tuple, stale
nonce and duplicate advance. Poll once per second, one outstanding challenge.
Forward valid replies through the private host `proof` verb. Host challenges
expire after five minutes; timeout/exit/failure never advances an effect. The
host unit can exit at its durable waiting phase; resume challenges again.

Disable traces, screenshots, video, downloads, raw browser output, and assertion
value dumps. No SSH agent/key, Podman socket, host directory or recovery archive
is mounted. Browser memory holds cookies, TOTP setup seed, recovery codes, and
provider session only. Its finalizer removes only its own proof factor, logs
out, and deletes profiles/channel files. Failed cleanup is explicit, not
success. Existing unrelated factors must never be removed.

| Challenge            | What passes                                                                                                                                               |
| -------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `prepared` TOTP      | Refuse an existing TOTP factor; enroll a temporary one under the old active key and prove sign-in.                                                        |
| `prepared` view-pass | Obtain a pass, prove it admits every protected route and no-pass requests are gated; retain exact cookie plus decoded resume ID, epoch, expiry in memory. |
| `maintenance`        | TLS verified; public origin returns 503 with `aboutme:maintenance` after Go and serving Caddy stop.                                                       |
| `serving` TOTP       | Public readiness/reads and sign-in with the prepared factor succeed after re-encryption.                                                                  |
| `serving` view-pass  | Old-cookie/fresh-cookie comparison below succeeds.                                                                                                        |
| `retired` TOTP       | Sign-in succeeds with only new active mapped; public readiness/reads succeed.                                                                             |

A lost TOTP browser can permanently lose the fictional account if every factor
and recovery code was lost
([ADR 0017](../adr/0017-second-factor-authentication.md)). It never authorizes
an operator reset. Resume can use another named factorless fictional account
under the current ring. Keep prior switch evidence: before activation repeat
`prepared`; after activation repeat `serving`/`retired` without rewinding keys.
A replacement witness alone does not prove old rows decrypt; the successful
re-encryption result and both zero snapshots remain required.

For view-pass, obtain a fresh provider pass in a separate browser context after
reload. Require the same resume ID and epoch, unchanged live/gated state and
slug, and old expiry at least 60 seconds later than server HTTP `Date`
throughout the comparison. Replay the exact old cookie in an isolated context
without the fresh cookie. HTML must gate; JSON, photo, PDF, live stream and view
start must return the uniform 404. The fresh cookie must admit the same routes.
Check
[discovery and preview exceptions](viewer-analytics/sign-in-to-view.md#gated-routes),
readiness and ordinary public reads. Do not toggle publish state to simplify the
proof. Lost/expired witness, changed epoch/ID, provider challenge or ambiguous
state follows the main design's `unverified` outcome, never a false pass.

## Implementation file map

Paths below are implementation scope proposals, not edits in this design change.
New command files get adjacent `_test.go` files; avoid expanding main.go beyond
its dispatch role. Split shell helpers before the 700-line limit.

| Release            | New files                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          | Changed files                                                                                                                                                                                                                                                                                                                                                                                                        |
| ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Account deletion   | `deploy/vn/scripts/operations.sh`; `deploy/vn/host/operation-run.sh`, `operation-state.sh`, `operation-account.sh`; `deploy/vn/host/etc/systemd/system/aboutme-operation@.service`, `aboutme-operation-retention.service`, `aboutme-operation-retention.timer`; `apps/server/cmd/server/account_delete_status.go`, `account_delete_status_test.go`, `media_deletion_status.go`, `media_deletion_status_test.go`; `apps/server/internal/accountapi/operator_status.go`, `operator_status_test.go`; `scripts/host-operations-proof.sh`; `deploy/vn/tests/host-operations-test.sh`, `host-operations-failure-test.sh` | `deploy/vn/host/install.sh`; `deploy/vn/scripts/fence.sh`, `deploy-safety.sh`; `apps/server/cmd/server/main.go`, `main_test.go`, `account_delete.go`, `account_delete_test.go`; `apps/server/internal/accountapi/operator_test.go`; `apps/server/internal/mediacleanup/integration_test.go`; `.github/actions/release-image-check/action.yml`                                                                        |
| TOTP rotation      | `deploy/vn/host/operation-keys.sh`, `deploy/vn/host/env/operations.json.example`; `apps/server/cmd/server/totp_key_status.go`, `totp_key_status_test.go`; `apps/server/internal/secondfactor/totp_status.go`, `totp_status_test.go`; `deploy/vn/tests/host-operations-keys-test.sh`; `deploy/dev-https-browser/host-operations-production.spec.ts`                                                                                                                                                                                                                                                                 | Shared runner/state/wrapper/fence/install and proof wrapper; `deploy/vn/scripts/deploy-host.sh`, `deploy-safety.sh`, `secrets-host.sh`, `secrets.sh`; `deploy/vn/host/job-run.sh`; `apps/server/cmd/server/main.go`, `main_test.go`, `totp_keys_test.go`; `apps/server/internal/secondfactor/totp_rotation_test.go`; `deploy/dev-https-browser/run.sh`, `spec-sources.txt`, `static-test.sh`, `production.config.ts` |
| View-pass rotation | `deploy/vn/tests/host-operations-view-pass-test.sh`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                | Shared runner/key/state/wrapper/proof files and tests, host installer, secret helpers and browser spec                                                                                                                                                                                                                                                                                                               |

Use existing store queries where possible. A new account existence read belongs
in `internal/accountapi/operator_status.go` as a parameterized `SELECT EXISTS`
in the read-only transaction, returning no personal row data. TOTP status
belongs in the named domain helper and reuses both existing off-active count
queries inside the same snapshot. Queue status projects only `pending` from the
existing query. No new SQL source, generated output, grant, or migration is
required by this proposal.

`operation-state.sh` owns version/field validation, safe IO, reservations,
retention and closed output. `operation-account.sh` owns deletion and cleanup
reconciliation; `operation-keys.sh` owns rotation. Install all helpers as root
owned, non-writable by other users; unit and script hashes join the allowlist.
Modify `fence.sh release` as well as clear/check so direct supported release
cannot bypass an unresolved mutation. Generic commands accept no caller-supplied
"already locked" escape that skips ownership checks.

## Required tests and CI ownership

Write regression cases before implementation. These suites use synthetic keys
and accounts only. Never fetch secrets or touch production from hosted CI.

The failure suite must accept a 33rd request after 32 compact closed records,
release expired-plan reservations only after quiescence, preserve unexpired
audit, and test concurrent admission, restart accounting and later verification.
The key suite must repair an image that cannot start with the changed ring and a
runner broken under a marker, including interrupted installation/binding,
without healthy-entry checks or lost keys/state/floors. It must reject malformed
archives and restore partial TOTP activation from staged bytes, retirement
exports and older manifests. View tests reject every exported view key on
restore.

| Suite                                  | Required cases                                                                                                                                                                                                                                                                                                                                                                                          | Hosted job                                                                                                       |
| -------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| VN `host-operations-test.sh`           | Strict command/confirmation grammar; exact UUID/slug/request binding; stale plans/live drift; eligibility/hash refusal; singleton dispatch; allowlisted secrets, socket-only network, memory/no-swap/log/core limits; markers and floor checks.                                                                                                                                                         | `ci.yml` -> `vn-infra.yml` job `check`: executable `deploy/vn/tests/*-test.sh` glob, bash syntax and shellcheck. |
| VN `host-operations-failure-test.sh`   | Lost SSH before/after dispatch and commit; complete-marker parsing; slug rename with account present; backend still alive; audit full/unwritable; concurrent apply/resume/abort; unknown status; retained locks; 180/30-day expiry and evidence-expired abort without target/replay; queue failure/nonzero/zero; cleanup after fence release.                                                           | Same VN job.                                                                                                     |
| VN `host-operations-keys-test.sh`      | Crash at each intent/effect/result boundary; boot with missing/malformed/symlink marker; activation receipt lost; staged key reused; import/sync bypass refused; drain-before-workload order; no nested lock; partial batch; suspended resume and confirmed image rebind preserving ring; both zero snapshots; reboot wait reset; retirement interrupted; no key in captured argv/log/inspect fixtures. | Same VN job.                                                                                                     |
| VN `host-operations-view-pass-test.sh` | Replace/reload crash; old key never restored; epoch/flag/floor preserved; old secret metadata rejected; old witness lost/expired/changed; completed duplicate; backup restore cannot resurrect old passes.                                                                                                                                                                                              | Same VN job.                                                                                                     |
| Command unit tests                     | Strict args/exits/JSON; output writer failure; fixed diagnostics; reduced config without media/OAuth; context cancellation; dispatch; status failure never emits zero; no secret/PII output.                                                                                                                                                                                                            | `ci.yml` job `server`, `make server-test` plus build/vet/lint.                                                   |
| Domain DB tests                        | Read-only snapshot; unknown/expired/locked off-active rows counted; concurrent lazy rotation/enrollment/deletion; batch commit loss; account rename vs absence; queue delayed/leased/nonzero rows.                                                                                                                                                                                                      | `server-integration`, `make server-test-db` (`-race -count=1`) and `make server-test-integration`.               |
| Proof harness static tests             | Mode/source allowlist, size/nonce/phase/replay checks, missing inputs, provider challenge, browser exit/cleanup, no artifacts with credentials. Keep old TOTP modes on their original spec.                                                                                                                                                                                                             | `contracts`, `make operational-test-browser`.                                                                    |

Put live DB behavior in the domain packages named by `server-test-db`, not only
in `cmd/server`: that target does not run cmd/server with DB enforcement. Unit
command tests inject the domain read. Extend `server-test-db` only if a new
package requires it, with an explicit primary-owned Makefile brief. The VN glob
already finds the new executable shell tests; no CI job name or workflow change
is needed. Existing hosted `semgrep-code`, `semgrep-supply-chain`, full-history
gitleaks in `contracts`, docs lint/link/length checks, and aggregate `ci` remain
required. Extend `.github/actions/release-image-check/action.yml` with bounded,
no-network, credential-free command dispatch probes on the built server image.
For each new command, send invalid arguments and require its exact usage string
and exit 2; for account status also send a valid synthetic UUID without DB
configuration and require its fixed failure diagnostic (exit 1), not the older
usage exit 2. Probe existing re-encryption with invalid arguments and its exact
usage/exit 1. Each probe has a 30-second timeout and captures only fixed
diagnostics. Add probes with each command's release and run them through both
`release-check.yml` and `release-images.yml`, which use this action. Bare-server
readiness alone does not prove subcommand dispatch. Production proofs establish
live behavior separately.

## Shipping docs

Update each item in its feature's implementation release, not ahead of code:

- **Account deletion:** `docs/runbooks/account-delete.md` Run it, Steps, Audit
  and failures; `docs/runbooks/production.md` Access and state, a Host
  operations section, Passkey release fence, Alarms; `deploy/vn/README.md`
  operator command and bundle installation instructions. Update
  `docs/design/showcase.md`'s open host-audit paragraph and
  `showcase-contract.md`'s audit store wording to link to the root-only 180-day
  record. Add the operations link in `docs/design/README.md`. Remove exactly the
  backlog item "Add and review a host procedure for abuse account deletion."
- **TOTP:** `docs/runbooks/totp-keys.md` Key rotation, TOTP unavailable,
  Production proofs, Forward fix; production Secrets, Failed deploy, Rollback,
  Passkey release fence, Backups and restore, Reboot; host README secret import
  guards, quiescent repair steps and recovery archive validation/restore
  examples. Replace open-procedure text with links in
  `docs/design/totp-key-management.md` and `passkey-release-fence.md`; update
  `vietnam-production.md` Secrets and host identity and Release fence marker
  rules. Remove exactly "Add and review a host procedure for TOTP key
  re-encryption."
- **View-pass:** `docs/runbooks/sign-in-to-view.md` Key rotation and Rollback;
  production Secrets and Backups and restore; host README and
  `docs/design/viewer-analytics/sign-in-to-view.md` key-rotation link. Remove
  exactly "Add and review a host procedure for rotating the sign-in-to-view
  key." Keep other backlog items.

Each runbook names the first supporting release, complete command examples,
expected closed output, unknown-outcome recovery, and proof input prerequisites.
No version number is assigned before the preceding patch lands. The owner has
approved building the operations and the product effects follow accepted ADRs;
no additional product decision is required. Independent review still must
approve this proposed contract before implementation. If a release change would
contradict an accepted ADR, stop for owner approval of that topic record.

## Sources and verification limits

Repository sources at the brief's fixed base are primary evidence:

- [Server dispatch](https://github.com/dannyota/aboutme/blob/f63edede34b9986f8aa336dfca25793e0d132457/apps/server/cmd/server/main.go),
  [account deletion](https://github.com/dannyota/aboutme/blob/f63edede34b9986f8aa336dfca25793e0d132457/apps/server/cmd/server/account_delete.go),
  [TOTP command](https://github.com/dannyota/aboutme/blob/f63edede34b9986f8aa336dfca25793e0d132457/apps/server/cmd/server/totp_keys.go).
- [Host secrets](https://github.com/dannyota/aboutme/blob/f63edede34b9986f8aa336dfca25793e0d132457/deploy/vn/scripts/secrets-host.sh),
  [release fence](https://github.com/dannyota/aboutme/blob/f63edede34b9986f8aa336dfca25793e0d132457/deploy/vn/scripts/fence.sh),
  [VN gate](https://github.com/dannyota/aboutme/blob/f63edede34b9986f8aa336dfca25793e0d132457/.github/workflows/vn-infra.yml).

First-party external references to verify against the installed versions before
implementation:
[Podman secret creation](https://docs.podman.io/en/latest/markdown/podman-secret-create.1.html),
[Podman run isolation and secret injection](https://docs.podman.io/en/latest/markdown/podman-run.1.html),
[systemd service timeouts](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html),
and [age encryption](https://github.com/FiloSottile/age). This offline design
review did not fetch those pages. No secondary-source claim is used. The
implementation must prove secret replacement and stop/reboot behavior on its
pinned runtime; metadata or a successful shell stub alone does not prove those
runtime properties.
