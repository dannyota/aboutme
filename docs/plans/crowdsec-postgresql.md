# CrowdSec PostgreSQL and Probe Paths Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Store CrowdSec state in a separate PostgreSQL 18 database on the existing Vietnam cluster, retain bounded unknown-route target paths for attack investigation, and remove every active SQLite dependency without weakening live-retention or recovery guarantees.

**Architecture:** A notice release first publishes dated bilingual policy text, seals the existing-account cohort in a durable ledger, sends one fixed encrypted-outbox email per cohort member, and creates a root-owned activation receipt only after every required outcome passes. Caddy then emits a strict seven-field tmpfs feed. CrowdSec 1.8.1 owns a separate `crowdsec` database and least-privilege role through PostgreSQL peer authentication on the Unix socket. A manifest gate precedes every schema-capable action. Host jobs perform bounded retention, stopped-consumer cache reconciliation when acknowledgement is unavailable, and ordinary vacuum without analyze. Cluster backups include encrypted CrowdSec data, while every restore discards that database and creates fresh security state.

**Tech Stack:** Go Caddy module, CrowdSec 1.8.1, PostgreSQL 18, pgx, Bash, systemd, Podman, nftables, pgBackRest, Nuxt i18n, GitHub Actions.

**Specs:** [CrowdSec HTTP privacy](../design/crowdsec-http-privacy.md), [cutover and restore](../design/crowdsec-cutover.md), and [privacy-change notification](../design/crowdsec-privacy-notification.md)

## Global Constraints

- [ ] Treat the accepted design and ADR 0027 as authority. Stop at a conflict instead of inventing a contract.
- [ ] Use one implementer worktree per bounded lane task, with no overlapping writers. The primary owns Git, exact-path commits, pull requests, releases, host installation, deployment, and production proof.
- [ ] Start every worker task with `git status --short`, read the named authorities and nearest `AGENTS.md`, and report the exact files and hunks changed.
- [ ] Write the failing regression test first. Observe the expected failure in the named GitHub CI job, or report it as unrun when the phase cannot safely create a test-only commit.
- [ ] Do not run full CI locally. Run only formatting, focused static checks allowed by `instructions/resources.md`, and primary `make pre-push`; GitHub CI is the full gate.
- [ ] Never print or retain credentials, recipient addresses, job or recipient IDs, IP addresses, target paths, raw CrowdSec rows, PostgreSQL diagnostics containing values, or incident data in CI, journals, evidence, external reports, commits, or pull-request discussion.
- [ ] Keep the application database, application roles, app containers, and CrowdSec database mutually inaccessible. Sharing the PostgreSQL cluster does not authorize shared tables, roles, sockets, or migrations.
- [ ] Keep CrowdSec offline. Do not add Community API, console, intelligence, GeoIP, autonomous-system, reverse-DNS, or external threat-sharing clients.
- [ ] Keep live records below the 24-hour access bound. Keep encrypted cluster backups under the existing 30-day recovery window, normally about five weeks after live deletion.
- [ ] Preserve the accepted 4 GiB host budget, PostgreSQL `max_connections = 40`, 1 GiB PostgreSQL ceiling, four-connection CrowdSec pool, and eight-connection role limit unless a separately reviewed design changes them. Free memory at one moment is not capacity evidence.
- [ ] Do not migrate SQLite data. Preserve one approved incident snapshot only on restricted encrypted local storage, record its capture and deletion deadline without recording content, and delete it 30 days after capture.
- [ ] Remove every production SQLite path, checkpoint, command, package dependency, fallback, and rollback route. Image rollback must retain the PostgreSQL host bundle and notice.
- [x] Owner approved the fixed bilingual email, SMTP acceptance threshold, possible duplicate delivery after an ambiguous retry, no added waiting period, deleted accounts resolving without email, and permanent campaign abort for an irrecoverable active account on 2026-10-19. Release and verify the dated public notice, complete its exact-cohort email gate, and create the matching activation receipt before merging or deploying target-path ingestion.
- [ ] Raise the monotonic release fence after each prerequisite activates: first to the notice release, then to the PostgreSQL/feed release during cutover. No deploy, failed-deploy restoration, rollback, or startup may return an older notice, five-field Caddy image, or SQLite host behavior.
- [ ] Use fixed SQL identifiers and fixed reason codes. Never build SQL, logs, or alarms from request text.
- [ ] Gate schema-capable commands, including `crowdsec -t` and registration. The pinned package's exact hub update/upgrade, suppressed unattended setup, and fixed `Config.API.Server`, `.Enable`, and `.ListenURI` reads do not open the database. Classify only those source-proven paths as pure, verify the signed package hooks and runtime behavior, and discard their output. Engine package setup requires `lapi=false`, `capi=false`, and `CROWDSEC_SETUP_UNATTENDED_DISABLE=1`; bouncer setup requires managed credentials first. No general command exemption, binary diversion, or bootstrap authority is added.
- [ ] Follow the [start authority and bootstrap contract](../design/crowdsec-cutover.md#start-authority). Retained operation/marker state blocks unattended starts. An exact one-shot permit controls a unit start; only its consumed live invocation can receive the bounded first-runtime admission. Full cutover mode requires all actual safety passes and a successful watchdog tick before consumer starts.
- [ ] For the storage cutover, use the existing release-fence operation lock and `reset-db` mutation marker. Do not add a second cutover lock or ambiguous cutover marker. The separate notice capture and restore markers have the exact authority in the notification design.
- [ ] Store execution briefs under `docs/plans/crowdsec-postgresql/`. Store worker reports only in `/home/danny/.local/state/aboutme-agent-reports/crowdsec-postgresql/`, outside the repository, with no sensitive values.
- [ ] The next tags are `v0.8.7` for the notice and `v0.8.8` for implementation only if no release lands first. Recalculate tags from current `main` at release time.
- [ ] Migration `00013` is next only on this plan base. Recalculate its append-only number from current `main` before the Task 1 branch starts.

## Review Focus

- Target paths can appear only for unknown routes returning 400, 404, or 405, with queries and protected routes excluded across every bounded decoded and normalized view.
- PostgreSQL peer authentication, ownership, grants, logging, statistics, connection bounds, and preflight fail closed without exposing attack values.
- Runtime deletion never removes a decision before every consumer has applied expiry or the stopped-consumer fallback has cleared owned cache and nftables state.
- Restore and interruption paths cannot resurrect old rows, statistics, credentials, bans, SQLite state, or an unattended consumer start.
- Release evidence proves dated notice and exact-cohort acceptance/deletion completion first, fresh PostgreSQL state, enforcement, retention, backup inclusion, and incident-snapshot expiry without retaining sensitive data.

---

## Task 1: Phase 11.1, deploy the notice and complete the email gate

**Owners:** backend implementer for application database, command, outbox, and worker files; frontend implementer for legal files; devops implementer for the host wrapper, receipt, and fence. Use separate isolated worktrees and integrate in that order. The primary owns shared generated output, traceability, Git, release, cohort operation, and production proof.

**Authorities:** `docs/design/crowdsec-privacy-notification.md`, `docs/design/crowdsec-http-privacy.md#public-notice-and-release`, `apps/server/AGENTS.md`, `apps/web/AGENTS.md`, `deploy/vn/AGENTS.md`, `instructions/go.md`, `instructions/web.md`, `instructions/deploy.md`, `instructions/testing.md`, `instructions/releases.md`.

**Files:**

- Modify: `apps/web/app/i18n/legal-en.ts`
- Modify: `apps/web/app/i18n/legal-vi.ts`
- Modify: `apps/web/app/i18n/legal.ts`
- Modify: `apps/web/app/pages/privacy.vue`
- Modify: `apps/web/app/pages/terms.vue`
- Modify: `apps/web/test/legal.test.ts`
- Create: `apps/server/migrations/00013_crowdsec_privacy_notice.sql`
- Create: `apps/server/migrations/crowdsec_privacy_notice_test.go`
- Create package and tests: `apps/server/internal/privacynotice/`
- Modify with tests: `apps/server/internal/authmail/{payload,outbox,templates,worker}.go`
- Modify with tests: `apps/server/internal/accountapi/delete.go`
- Modify with tests: `apps/server/cmd/server/`
- Modify and regenerate: `apps/server/sql/queries.sql`, `apps/server/internal/store/queries.sql.go`, `apps/server/internal/store/querier.go`, and affected generated models
- Create: `deploy/vn/scripts/crowdsec-privacy-notice.sh`
- Create: `deploy/vn/host/crowdsec-privacy-notice-host.sh`
- Create: `deploy/vn/tests/crowdsec-privacy-notice-test.sh`
- Modify: `deploy/vn/host/install.sh`
- Modify: `deploy/vn/host/quadlet/aboutme-server.container.in`
- Modify: `deploy/vn/scripts/fence.sh`
- Modify: `deploy/vn/scripts/deploy-safety.sh`
- Modify: `deploy/vn/scripts/deploy-host.sh`
- Modify: `deploy/vn/scripts/restore-drill.sh`
- Modify: `deploy/vn/tests/cutover-fence-test.sh`
- Modify: `deploy/vn/tests/deploy-safety-test.sh`
- Modify: `deploy/vn/tests/host-deploy-regression-test.sh`
- Modify: `deploy/vn/tests/restore-drill-journal-test.sh`
- Modify: `deploy/vn/tests/restore-drill-state-proof-test.sh`
- Modify only to register hosted proof: `.github/workflows/vn-infra.yml`
- Modify after hosted proof: `docs/plans/traceability/ac-priv.md`
- Create backend brief: `docs/plans/crowdsec-postgresql/11.1a-notice-backend.md`
- Create frontend brief: `docs/plans/crowdsec-postgresql/11.1b-notice-frontend.md`
- Create host brief: `docs/plans/crowdsec-postgresql/11.1c-notice-host.md`
- Backend report: `/home/danny/.local/state/aboutme-agent-reports/crowdsec-postgresql/11.1a-notice-backend.md`
- Frontend report: `/home/danny/.local/state/aboutme-agent-reports/crowdsec-postgresql/11.1b-notice-frontend.md`
- Host report: `/home/danny/.local/state/aboutme-agent-reports/crowdsec-postgresql/11.1c-notice-host.md`

- [ ] Add migration, package, SQL, command, outbox, worker, deletion, and host tests for exact quiescent cohort capture; resumed batches of at most 100; one logical encrypted `privacy_notice` job per account; ordinary-mail priority; atomic accepted/failed/deleted outcomes; seven-day outbox cleanup with lifetime recipient evidence; bounded aggregate status; send and activation receipts; restore fencing; and fixed-output failure. Use the complete hosted-proof matrix in the notification design.
- [ ] In `apps/web/test/legal.test.ts`, replace the old blanket path-exclusion expectations with exact assertions for separate privacy and terms dates, the actual privacy publication date, the unchanged terms date, conditional escaped target paths, 512-byte limit, forbidden queries/headers/bodies, 24-hour live records, about-five-week encrypted backups, fresh state after restore, no community sharing, and the unchanged pre-change email promise in both languages.
- [ ] The primary integrates the exact reported test-only file sets into one notice-release branch, commits them, pushes one draft pull request, and observes the named server, migration, web, and `vn-infra` jobs fail for the missing ledger, mail kind, notice, command, and receipt. Record only CI URLs and fixed assertion names in external reports.
- [ ] Implement the two-table durable ledger, strict payload version 3, fixed bilingual templates and canonical link, bounded command surface, worker finalization, account-deletion semantics, lifetime recipient evidence, retirement-only purge, and migration grants exactly as accepted. Keep addresses and message data inside the encrypted production outbox and current Bizfly path.
- [ ] Implement a root-only host wrapper that pins the current release image, holds the existing operation and workload locks, persists the cohort-capture and restore-reconciliation markers, rotates the host epoch on restore, runs prepare/enqueue/status/activate with no recipient or copy arguments, and writes validated send and activation receipts. The worker must check current send authority before claim and again before SMTP while ordinary mail continues. No HTTP/MCP endpoint, timer, address export, success override, or hand-written receipt is allowed.
- [ ] Fence every cluster and point-in-time restore before database replacement: stop and join notice sends, persist reconciliation, rotate the epoch, durably revoke both receipts, and refuse restored queue replay until reviewed reconciliation issues new authority. Test interruption, reboot, host replacement, stale receipts, delayed activation recovery, and known late failure after outbox cleanup.
- [ ] Replace the English and Vietnamese attack-protection paragraphs with the exact accepted text, set both privacy dates to the actual publication date, and keep the terms date unchanged.
- [ ] Run allowed formatters and static checks, regenerate store output through the repository command, then run primary `make pre-push`.
- [ ] Push the integrated implementation and require the exact branch commit's full CI, an independent Sol xhigh code review, and the requested Copilot review to pass.
- [ ] Merge the up-to-date pull request locally with a signed `--no-ff` merge, push only the merge commit to `main`, and require green `main` CI on that exact commit.
- [ ] Tag the current next patch release, expected `v0.8.7`, wait for `release-images`, deploy it through the production runbook, and verify the public English and Vietnamese privacy pages contain every accepted clause and the actual publication date.
- [ ] Raise the release floor to the verified notice release before any enqueue. Under the workload lock, enter maintenance, persist the capture marker, stop all account writers, prepare and seal the exact existing-account cohort, then resume the updated app. An interrupted capture resumes the same cutoff and cursor under maintenance.
- [ ] Issue the matching send receipt, enqueue bounded batches, and let the normal worker drain them. Require aggregate ledger totals to match the sealed cohort with every member accepted or deleted and no queued, failed, missing, or unexplained row. Check known Bizfly delivery failures without exporting recipient evidence. Retry active-account failures only through reviewed generation recovery; otherwise abort permanently and leave changed ingestion disabled.
- [ ] Run the owner-controlled activation command, verify its matching root-owned receipt, and record only release/content digests, publication date, cutoff, aggregate counts, timestamps, and fixed gate outcomes under `AC-PRIV-006`. Do not begin Task 2 until the receipt passes.

**Exit:** Production serves the dated accepted notice, every cutoff member has an accepted or deleted outcome, and the matching activation receipt exists before any seven-field feed or target sample exists.

## Task 2: Phase 11.2, emit and parse the seven-field feed

**Owner:** devops implementer in an isolated worktree.

**Authorities:** `docs/design/crowdsec-http-privacy.md#request-feed`, `#detection`, and `#required-proof`; `deploy/AGENTS.md`; `deploy/caddy/AGENTS.md`; `instructions/deploy.md`; `instructions/testing.md`.

**Files:**

- Modify: `deploy/caddy/production/build/crowdsec_feed.go`
- Modify: `deploy/caddy/production/build/crowdsec_feed_test.go`
- Modify: `deploy/caddy/production/test/crowdsec_feed.sh`
- Modify: `deploy/caddy/production/edges/direct.sites`
- Modify: `deploy/vn/edge/crowdsec/parsers/s01-parse/aboutme-http.yaml`
- Modify only seven-field feed fixtures and assertions in: `deploy/vn/tests/crowdsec-integration-test.sh`
- Inspect without behavior change unless a failing test proves it is required: `deploy/vn/edge/crowdsec/parsers/s02-enrich/zz-aboutme-http-source.yaml`
- Inspect and preserve coarse grouping: `deploy/vn/edge/crowdsec/scenarios/aboutme-http-rate.yaml`
- Inspect and preserve coarse grouping: `deploy/vn/edge/crowdsec/scenarios/aboutme-http-status.yaml`
- Create execution brief: `docs/plans/crowdsec-postgresql/11.2-feed.md`
- External report: `/home/danny/.local/state/aboutme-agent-reports/crowdsec-postgresql/11.2-feed.md`

- [ ] Add table-driven Go tests for exactly seven mandatory keys, no extras, allowed methods, canonical unmapped socket addresses, eligible statuses, empty target values on known routes, and invalid base-record rejection.
- [ ] Add table-driven classification tests for every protected route class across ASCII case changes, the first two decoded views, dot segments, backslashes, encoded slashes, double encoding, and encoded protected aliases.
- [ ] Add path-capture tests for valid escapes and escape case, UTF-8, literal encoded question marks, query stripping, malformed percent triplets, inconsistent `RawPath`, controls, authority-only/opaque/asterisk targets, and exact 512/513-byte boundaries.
- [ ] Add runtime feed tests proving host, query, header, body, and forbidden markers never appear, while one eligible unknown-route marker appears only as the exact escaped path.
- [ ] Extend runtime tests for fixed error reasons, URI-free Caddy errors, exact private logger names, 1 MiB bounded feed rolls, five-minute truncation, roll removal, reboot cleanup, and parser continuity after truncation. Parsing failures must emit no raw diagnostic.
- [ ] Push the test-only commit and observe `server-integration` fail in `make caddy-prod-test` because the encoder and parser still expose five fields. Record the CI run and failed test names in the external report.
- [ ] Add `ProbeClass` and `TargetPath` to the allowlisted feed record and a bounded helper equivalent to `feedTarget(mode, requestURI, status) (routeClass, probeClass, targetPath string, ok bool)`.
- [ ] Capture the parsed original URL before rewrites. Use `URL.EscapedPath()` only after explicit raw-path, UTF-8, percent, control, absolute-path, and length validation. Never use `URL.String()` or raw `RequestURI()` as the stored field.
- [ ] Update the CrowdSec parser to require exactly seven keys, validate combinations and allowlists, and reject path/class mismatches. Do not reconstruct a missing path.
- [ ] Update the existing hosted CrowdSec integration feed fixtures and assertions to seven fields in the same commit. Leave its SQLite database sections unchanged until the atomic Task 4 switch so this task's full CI can pass without mixed five/seven-field records.
- [ ] Keep scenarios grouped by source address and coarse fields only. Add a static assertion that neither scenario groups, filters, labels, nor capacity keys use `target_path`.
- [ ] Update the direct-listener comment to describe seven bounded fields, then run formatters and primary `make pre-push`.
- [ ] Require the exact branch commit's `server-integration` and full CI to pass before review acceptance.

**Exit:** The encoder and parser accept only the seven-field contract, and hosted tests prove forbidden request material cannot persist.

## Task 3: Phase 11.3, build the inactive PostgreSQL provisioner and gate

**Owner:** devops implementer in an isolated worktree.

**Authorities:** `docs/design/crowdsec-http-privacy.md#database-and-authorization`, `#planner-statistics-and-vacuum`, and `#log-privacy`; `deploy/vn/AGENTS.md`; `instructions/deploy.md`; PostgreSQL 18 and CrowdSec 1.8.1 pinned sources linked by the design.

**Files:**

- Create: `deploy/vn/host/crowdsec-db.sh`
- Create: `deploy/vn/host/crowdsec-provision.sh`
- Create: `deploy/vn/host/crowdsec-schema-v1.8.1.tsv`
- Create: `deploy/vn/host/etc/postgresql/18/main/pg_ident.conf`
- Create staged candidate: `deploy/vn/edge/crowdsec/config.yaml.postgresql`
- Create: `deploy/vn/tests/crowdsec-db-test.sh`
- Modify: `deploy/vn/host/etc/postgresql/18/main/pg_hba.conf`
- Modify: `deploy/vn/host/etc/postgresql/18/main/conf.d/90-aboutme.conf`
- Modify to add a separate PostgreSQL 18 provisioner job while retaining the existing SQLite integration: `.github/workflows/vn-infra.yml`
- Create execution brief: `docs/plans/crowdsec-postgresql/11.3-database.md`
- External report: `/home/danny/.local/state/aboutme-agent-reports/crowdsec-postgresql/11.3-database.md`

- [ ] Derive and review the CrowdSec 1.8.1 PostgreSQL catalog manifest from the pinned binary in hosted integration. Freeze table, column, type, constraint, index, ownership, and privilege expectations without adding a vendor schema-version table.
- [ ] Add static and integration tests for one `crowdsec` login role, one owned `crowdsec` database, no role memberships or elevated attributes, public revocations, role connection limit 8, and no application database access in either direction.
- [ ] Add tests for the named peer map from OS `root` to database role `crowdsec`, database-specific HBA scope, Unix-socket-only operation, and rejection of password, TCP, wildcard, `trust`, application-role, or application-container access.
- [ ] Add tests for effective `db_config`: `type: pgx`, database and user `crowdsec`, socket directory `/run/postgresql`, empty host, port 0, SSL disabled, maximum four open connections, `flush.max_age: 0s`, and null `max_items`.
- [ ] Add tests that provision, config-check, registration, startup, and SQL helpers run a read-only `psql -X` transaction with `ON_ERROR_STOP=1` before each schema-capable CrowdSec or `cscli` action, and that an empty or mismatched database blocks the action outside fenced provisioning without creating objects.
- [ ] Add tests for role-specific protected logging settings, zero column statistics targets, disabled ordinary autovacuum, absence of expression indexes and extended statistics, and aggregate absence of CrowdSec rows in `pg_stats` without selecting values.
- [ ] Have the primary push the test-only commit and observe the new PostgreSQL provisioner assertions fail without changing the existing hosted SQLite integration. Record fixed test names and CI links only in the external report.
- [ ] Implement `crowdsec-db.sh preflight-empty`, `preflight-ready`, and `apply-policy` with fixed SQL and fixed reason codes. `preflight-empty` succeeds only inside the fenced provisioner against a verified empty database.
- [ ] Implement `crowdsec-provision.sh fresh` and `restore-reset`. Both require stopped consumers and the existing operation lock plus `reset-db` mutation marker. `restore-reset` drops and recreates only the CrowdSec database, never the application database.
- [ ] Configure peer authentication and role logging, install PostgreSQL configuration before any CrowdSec binary action, and refuse conflicting existing ownership, grants, roles, maps, or relations.
- [ ] Encode the PostgreSQL restart/recovery invalidation contract in the gate interfaces and isolated tests. Do not attach it to active CrowdSec units until Task 4 switches storage and retention together.
- [ ] Add PostgreSQL 18 beside SQLite in the hosted workflow and run the fenced provisioner, staged pgx config, pinned CrowdSec 1.8.1 initialization, policy application, and full preflight in a separate isolated harness. Stop and remove that harness before the unchanged SQLite integration starts. Do not modify active `config.yaml.local`, active units, installer activation, retention, or existing integration fixtures in this task.
- [ ] Run shell format/static checks and primary `make pre-push`; require the new provisioner harness, the unchanged SQLite integration, both VN jobs, and full CI to pass.

**Exit:** An inactive fresh PostgreSQL database initializes with the pinned schema and privacy policy in its own harness, all mismatches fail closed, and the still-active SQLite integration remains green until the atomic Task 4 switch.

## Task 4: Phase 11.4, switch CrowdSec and retention to PostgreSQL atomically

**Owner:** devops implementer in an isolated worktree.

**Authorities:** `docs/design/crowdsec-http-privacy.md#live-retention-and-failure`, `#planner-statistics-and-vacuum`, `#log-privacy`, and `#required-proof`; `deploy/vn/AGENTS.md`; `instructions/deploy.md`; `instructions/testing.md`.

**Files:**

- Modify: `deploy/vn/edge/crowdsec/config.yaml.local`
- Delete after copying the reviewed candidate: `deploy/vn/edge/crowdsec/config.yaml.postgresql`
- Modify: `deploy/vn/host/install.sh`
- Rewrite: `deploy/vn/host/crowdsec-retention.sh`
- Modify: `deploy/vn/host/crowdsec-offline.sh`
- Modify: `deploy/vn/tests/crowdsec-offline-test.sh`
- Create: `deploy/vn/host/crowdsec-vacuum.sh`
- Create: `deploy/vn/host/etc/systemd/system/aboutme-crowdsec-vacuum.service`
- Create: `deploy/vn/host/etc/systemd/system/aboutme-crowdsec-vacuum.timer`
- Modify: `deploy/vn/host/crowdsec-retention-fail.sh`
- Modify: `deploy/vn/host/crowdsec-retention-lease-check.sh`
- Modify: `deploy/vn/host/bin/watch.sh`
- Modify: `deploy/vn/host/etc/systemd/system/aboutme-crowdsec-retention-startup.service`
- Modify: `deploy/vn/host/etc/systemd/system/aboutme-crowdsec-retention.service`
- Modify: `deploy/vn/host/etc/systemd/system/aboutme-crowdsec-retention.timer`
- Modify: `deploy/vn/host/etc/systemd/system/crowdsec.service.d/90-aboutme-alert.conf`
- Modify: `deploy/vn/host/etc/systemd/system/crowdsec-firewall-bouncer.service.d/90-aboutme-alert.conf`
- Modify: `deploy/vn/edge/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml.local`
- Rewrite: `deploy/vn/tests/crowdsec-retention-test.sh`
- Create: `deploy/vn/tests/crowdsec-vacuum-test.sh`
- Modify: `deploy/vn/tests/crowdsec-lease-test.sh`
- Modify: `deploy/vn/tests/watch-test.sh`
- Modify: `deploy/vn/tests/crowdsec-config-test.sh`
- Rewrite database and retention sections: `deploy/vn/tests/crowdsec-integration-test.sh`
- Modify to remove SQLite and make the PostgreSQL integration canonical: `.github/workflows/vn-infra.yml`
- Create execution brief: `docs/plans/crowdsec-postgresql/11.4-retention.md`
- External report: `/home/danny/.local/state/aboutme-agent-reports/crowdsec-postgresql/11.4-retention.md`

- [ ] Add tests for fixed UTC cutoffs, every relevant timestamp, batches of at most 1,000 candidates, 10-second statement timeout, 2-second lock timeout, 60-second runtime deadline, 300-second startup deadline, and no record at least 23 hours 30 minutes old after success.
- [ ] Add tests that fresh records and registrations survive while stale HTTP and SSH graphs, stale children, expired decisions, and orphans are removed. Missing/non-finite times, excessive durations, dangling links, and schema mismatches must fail closed.
- [ ] Add tests that the exact candidate decision IDs are reconciled and rechecked before deletion. Routine cleanup must not restart Caddy when there are no unreconciled decision candidates.
- [ ] Lock in the accepted finding that the pinned Caddy and firewall consumers expose no sufficient applied-deletion acknowledgement. Test that neither `last_pull` nor a stream response is accepted, and require a future reviewed design change before enabling an acknowledgement path.
- [ ] For the fallback, add tests that stop both Caddy modes, the firewall bouncer, and CrowdSec; prove quiescence; validate and delete only bouncer-owned `ip crowdsec` and `ip6 crowdsec6` nftables tables; delete the exact database candidates; rebuild from current decisions; and restart only after full verification.
- [ ] Add tests that unrelated nftables tables and application PostgreSQL stay untouched, and that timeout, database loss, lock contention, clock failure, invalid lease, or disabled timer stops every consumer and removes tmpfs feeds with fixed alarms.
- [ ] Add vacuum tests for an hourly, non-overlapping, five-minute-deadline `VACUUM (ANALYZE FALSE)` over the fixed manifest only. Reject any `ANALYZE`, forced vacuum, checkpoint, SQLite, or WAL-archive dependency.
- [ ] Add config, installer, and hosted integration tests that activate the reviewed pgx configuration and gate dependencies only with PostgreSQL retention, then assert the workflow and installed host have no SQLite package or command. Preserve Task 2's seven-field fixtures.
- [ ] Have the primary push the test-only commit and observe the VN static and hosted integration jobs fail because active storage and retention are still SQLite-based and vacuum units do not exist.
- [ ] Rewrite retention to run `preflight-ready` before startup and runtime SQL, select and recheck exact candidates, use sequential fixed SQL, and renew the root-owned lease only after complete verification.
- [ ] Implement the stopped-consumer fallback whenever the bounded set contains a decision candidate. Never run a blanket flush while consumers serve. Keep the 20-second acknowledgement bound represented in the interface and tests so a future proven consumer path cannot exceed it.
- [ ] Add the separate vacuum service and timer. Keep wraparound protection active while ordinary CrowdSec autovacuum and all analyze paths stay disabled.
- [ ] Pin only the bouncer-owned nftables table/chain/set triples `ip crowdsec`/`crowdsec-chain`/`crowdsec-blacklists` and `ip6 crowdsec6`/`crowdsec6-chain`/`crowdsec6-blacklists`, and make consumer rebuild deterministic from current decisions.
- [ ] Activate the staged pgx config, installer, unit dependencies, PostgreSQL invalidation, and hosted harness in the same implementation commit as the retention rewrite. Remove the staged candidate and every SQLite workflow/package/fixture only after the canonical PostgreSQL integration covers their behavior.
- [ ] Run shell static checks and primary `make pre-push`; require VN static tests, the canonical PostgreSQL hosted integration, and full CI to pass.

**Exit:** PostgreSQL retention, cache reconciliation, lease enforcement, and non-analyzing vacuum satisfy every bound without any SQLite or broad firewall operation.

## Task 5: Phase 11.5, make cutover, credentials, restore, and hosted integration atomic

**Owner:** devops implementer in an isolated worktree; primary performs the later production operation.

**Authorities:** `docs/design/crowdsec-http-privacy.md#backups-and-restore`, `#migration-loss-and-release-order`, and `#required-proof`; `deploy/vn/AGENTS.md`; `instructions/deploy.md`; `instructions/releases.md`.

**Files:**

- Create: `deploy/vn/scripts/crowdsec-cutover.sh`
- Create: `deploy/vn/host/crowdsec-cutover-host.sh`
- Modify: `deploy/vn/host/install.sh`
- Modify for narrow restore admission and internal held-lock validation: `deploy/vn/host/crowdsec-db.sh`
- Modify for restore-reset composition: `deploy/vn/host/crowdsec-provision.sh`
- Modify for consumed-invocation bootstrap admission: `deploy/vn/host/crowdsec-retention.sh`
- Modify: `deploy/vn/scripts/fence.sh`
- Modify: `deploy/vn/scripts/deploy-safety.sh`
- Modify: `deploy/vn/scripts/deploy-host.sh`
- Modify: `deploy/vn/scripts/secrets-host.sh`
- Modify: `deploy/vn/scripts/secrets.sh`
- Modify: `deploy/vn/scripts/restore-drill.sh`
- Modify and extend: `deploy/vn/tests/crowdsec-integration-test.sh`
- Modify: `deploy/vn/tests/cutover-fence-test.sh`
- Modify: `deploy/vn/tests/cutover-ssh-failure-test.sh`
- Modify: `deploy/vn/tests/host-deploy-regression-test.sh`
- Modify: `deploy/vn/tests/deploy-failure-test.sh`
- Modify: `deploy/vn/tests/secrets-regression-test.sh`
- Create: `deploy/vn/tests/crowdsec-restore-test.sh`
- Modify: `deploy/vn/tests/deployment-document-test.sh`
- Modify: `deploy/vn/host/etc/systemd/system/crowdsec.service.d/90-aboutme-alert.conf`
- Modify: `deploy/vn/host/etc/systemd/system/crowdsec-firewall-bouncer.service.d/90-aboutme-alert.conf`
- Modify: `deploy/vn/host/crowdsec-retention-lease-check.sh`
- Modify: `deploy/vn/host/bin/watch.sh`
- Modify: `deploy/vn/host/etc/systemd/system/aboutme-crowdsec-retention-startup.service`
- Modify: `deploy/vn/host/etc/systemd/system/aboutme-crowdsec-retention.service`
- Modify: `deploy/vn/host/etc/systemd/system/aboutme-crowdsec-vacuum.service`
- Modify as required for exact permit consumption: `deploy/vn/host/quadlet/aboutme-server.container.in`
- Modify as required for exact permit consumption: `deploy/vn/host/quadlet/aboutme-web.container.in`
- Modify as required for exact permit consumption: `deploy/vn/host/quadlet/aboutme-caddy.container.in`
- Modify as required for exact permit consumption: `deploy/vn/host/quadlet/aboutme-maintenance.container.in`
- Modify: `deploy/vn/tests/crowdsec-lease-test.sh`
- Modify: `deploy/vn/tests/crowdsec-db-test.sh`
- Modify: `deploy/vn/tests/crowdsec-retention-test.sh`
- Modify: `deploy/vn/tests/restore-drill-journal-test.sh`
- Modify: `deploy/vn/tests/restore-drill-state-proof-test.sh`
- Modify: `deploy/vn/tests/watch-test.sh`
- Modify only final proof cases as needed: `.github/workflows/vn-infra.yml`
- Create execution brief: `docs/plans/crowdsec-postgresql/11.5-cutover.md`
- External report: `/home/danny/.local/state/aboutme-agent-reports/crowdsec-postgresql/11.5-cutover.md`

- [ ] Add secretless tests for generating replacement machine and bouncer credentials directly into protected files, synchronizing the Caddy Podman secret, proving old credentials fail, and preventing values from appearing in arguments, standard output, journals, or external reports.
- [ ] Add cutover tests for the operation lock and existing `reset-db` mutation marker, package-hook restart suppression, all-consumer quiescence, PostgreSQL configuration installation before CrowdSec execution, fresh provisioning, policy gate, credential rotation, startup cleanup, cache rebuild, enforcement, and marker clearing only after success.
- [ ] Add installer transition tests for an inactive staging mode that installs control-plane scripts and PostgreSQL configuration before the lock, but cannot replace active CrowdSec configuration, call CrowdSec or `cscli`, restart a consumer, delete SQLite, or expose mixed five/seven-field feed state. Only the owning cutover operation may activate staged artifacts.
- [ ] Implement the [exact permit contract](../design/crowdsec-cutover.md#start-authority), including the closed unit allowlist, consumed live-invocation binding, trusted service-manager/caller identity, and retirement on every exit. Place admission before vendor pre-start actions; gate or refuse reload.
- [ ] Implement [quiescent bootstrap](../design/crowdsec-cutover.md#quiescent-bootstrap): join disabled safety scheduling and pending jobs, join watchdog scheduling before lease creation, run vacuum then startup then runtime, and admit only the consumed same-service runtime invocation. Preserve ordinary checks. Establish [cutover mode](../design/crowdsec-cutover.md#cutover-mode) only from actual successful outcomes; restore watchdog scheduling and pass its tick before any consumer start.
- [ ] Implement [completion and failure](../design/crowdsec-cutover.md#completion-and-failure), including permit retirement, existing-lock reuse for the fixed watchdog/database checks, scheduled-watchdog shared-lock admission retained through ordinary checks, timer catch-up exclusion through owner release, no waits on guards blocked by that lock, and durable marker restoration on failure. Preserve the existing quiescent recovery path.
- [ ] Cover every [bootstrap and completion proof](../design/crowdsec-cutover.md#required-proof) in hosted tests. Include real systemd invocation identity, wrong-owner/unit/invocation and same-unit replay, joined watchdog races, incomplete outcomes, pending/catch-up jobs, scheduled-watchdog guard timeout before its fail-closed body, no recursive lock/deadlock, both watchdog ticks, reboot, and interruptions through owner release.
- [ ] Implement and test [restore composition](../design/crowdsec-cutover.md#restore-composition): keep the exact `notice_restore` owner and workload-locked child through `restore-reset`, all DB checks, postcheck, private completion, helper join, and finish. Add only purpose-specific restore-reset admission, with inherited context and under-lock authority revalidation. Preserve cutover fresh/reset admission, notice denial, protocol framing, and application DB state. Prove app-absent provisioning dependencies without a consumer runtime; refuse missing dependencies before restore effects.
- [ ] Extend the canonical Task 4 PostgreSQL hosted integration. Cover seven-field ingestion, one exact eligible alert sample, forbidden-field absence, 15-minute and repeat one-hour bans, benign traffic, PostgreSQL peer-only access, privilege isolation, retention, fallback, lease, restore, credential replacement, and offline operation.
- [ ] Add forced bad-value and constraint-error cases with synthetic markers and prove the markers appear in no PostgreSQL log, client diagnostic, journal, or alarm sink. Report only fixed sink names and pass/fail outcomes.
- [ ] Sustain synthetic event ingestion, run ordinary `VACUUM (ANALYZE FALSE)`, and prove aggregate `pg_stats` absence. Inject planner-statistics contamination through the isolated administrator fixture, prove every consumer and schema-capable command refuses it, then prove `restore-reset` returns to fresh accepted state without catalog edits.
- [ ] Exercise natural expiry, explicit revocation, missed polls, and concurrent expiry against the exact candidate set. Prove current consumers always take the stopped-consumer fallback, clear only owned caches/nftables state before SQL deletion, rebuild from current decisions, and do not restart when no decision candidate exists.
- [ ] Restart PostgreSQL during the isolated test and prove the gate and lease invalidate immediately, every consumer remains stopped, and a new full preflight plus startup retention pass is required before owner-authorized restart.
- [ ] Make hosted tests use distinct synthetic markers but print only fixed outcomes and aggregate counts. Add a final filesystem and process scan proving no SQLite database, path, package command, checkpoint, or fallback was created or invoked.
- [ ] Inventory every production `crowdsec` and `cscli` entry point, including `deploy-host.sh` decision smoke commands. For each schema-capable invocation, corrupt the manifest and prove preflight refusal before the vendor command, except the fenced provisioner's fixed initialization. For only the exact pinned pure package calls named above, prove absent schema stays absent and no database state changes. No arbitrary config expression or blanket `cscli` exemption is allowed.
- [ ] Add compatibility tests that the notice release floor and matching activation receipt block an older web image or any changed ingestion, and the cutover floor blocks every older Caddy image and host behavior through deploy, failed-deploy restoration, explicit rollback, activation, startup, and retained release state. A pre-cutover deploy failure may restore only a release still above the current floor.
- [ ] Push the test-only commit and observe the `crowdsec-integration`, cutover-fence, deployment, and restoration assertions fail against the incomplete cutover. Record only fixed failing check names and the CI link in the external report.
- [ ] Implement the atomic install, cutover, fence, compatibility, credential, and restore paths. Preserve non-database CrowdSec assets and credential directories, but never preserve database state across restore.
- [ ] Run shell static checks and primary `make pre-push`; require both `vn-infra` jobs and full CI to pass.

**Exit:** CI proves a fresh PostgreSQL cutover, fresh restore state, rotated credentials, working bans, and complete absence of SQLite behavior.

## Task 6: Phase 11.6, reconcile current-state documentation and complete adversarial review

**Owner:** devops implementer for current-state docs; primary for traceability and coordination; independent code reviewer for review only.

**Authorities:** accepted design and ADR, `docs/AGENTS.md`, `instructions/documentation.md`, `instructions/workflow.md`, `instructions/verification.md`.

**Files:**

- Modify: `docs/architecture.md`
- Modify: `docs/runbooks/production.md`
- Modify: `deploy/vn/README.md`
- Modify: `docs/plans/traceability/ac-priv.md`
- Modify: `docs/plans/traceability/ac-inf.md`
- Modify: `docs/plans/traceability/ac-ops.md`
- Create execution brief: `docs/plans/crowdsec-postgresql/11.6-review.md`
- External report: `/home/danny/.local/state/aboutme-agent-reports/crowdsec-postgresql/11.6-review.md`

- [ ] Replace current-state SQLite descriptions with the shipped PostgreSQL socket, role/database boundary, preflight, retention, vacuum, restore-reset, and failure behavior. Keep credentials and sensitive operator commands abstract where the runbook already delegates to protected scripts.
- [ ] Document the one-time local incident snapshot as a restricted encrypted copy with its exact deletion deadline in private operator state only. Public docs state the 30-day rule but never the path, filename, hash, size, content, IPs, or target paths.
- [ ] Update the four acceptance rows with exact test and CI evidence, using `LANDED` until production proves the remaining hosted clauses.
- [ ] Dispatch an independent Sol xhigh code reviewer with the accepted design, exact diff, hosted CI links, and an adversarial focus on mutation paths, credentials, restore, logs, SQL, target-path privacy, consumer acknowledgement, and SQLite leftovers.
- [ ] Route every finding to the original implementer. The reviewer confirms each fix in a separate read-only pass. The primary inspects every worker diff and report before acceptance.
- [ ] Run primary `make pre-push`, push the reviewed candidate, request Copilot review, resolve every applicable thread, and require full GitHub CI green on the exact up-to-date pull-request commit.

**Exit:** Code, host configuration, tests, current-state docs, and traceability agree; independent adversarial review has no open findings.

## Task 7: Phase 11.7, release, cut over, prove production, and close the plan

**Owner:** primary for Git, release, host operation, deployment, proof, and closure; QA for bounded production proof only.

**Authorities:** `instructions/releases.md`, `docs/runbooks/production.md`, `docs/design/crowdsec-http-privacy.md#required-proof`, `docs/plans/traceability/README.md`.

**Files:**

- Modify after production proof: `docs/plans/traceability/ac-priv.md`
- Modify after production proof: `docs/plans/traceability/ac-inf.md`
- Modify after production proof: `docs/plans/traceability/ac-ops.md`
- Modify after production proof: `docs/plans/traceability/vietnam-operations.md`
- Modify if open work remains: `docs/plans/backlog.md`
- Delete at closure: `docs/plans/crowdsec-postgresql.md`
- Delete at closure: `docs/plans/crowdsec-postgresql/`

- [ ] Merge the up-to-date implementation pull request locally with a signed `--no-ff` merge, push only that merge commit to `main`, and wait for green CI on the exact `main` commit.
- [ ] Tag the current next patch release, expected `v0.8.8`, wait for `release-images`, and confirm every release artifact matches the exact reviewed commit.
- [ ] Validate the already captured approved incident snapshot without opening its contents. Ensure it is the only incident copy, on restricted encrypted storage excluded from backup and sync, and record its original capture time and 30-day deadline in private operator state. Do not download or create a second snapshot.
- [ ] At that validation, create and verify a persistent local expiry service/timer outside the repository. The timer must destroy the snapshot's unique envelope key, unlink the encrypted file without trash, and reject backup/snapshot/sync destinations. A persistent missed-run policy must execute at the next boot. Record only the deadline and armed state.
- [ ] Copy the exact reviewed `deploy/vn` bundle to the host and run its storage-transition staging mode before application deployment. This mode may install reviewed control-plane scripts and PostgreSQL authentication/configuration, but must not replace active CrowdSec storage/parser configuration, invoke CrowdSec or `cscli`, restart a consumer, or remove SQLite. Verify the current notice release continues serving.
- [ ] Run the reviewed `crowdsec-cutover.sh` orchestration for the exact release instead of ordinary `deploy.sh`. It acquires the existing production operation lock, persists the `reset-db` marker, stops both Caddy modes, CrowdSec, the firewall bouncer, and retention scheduling, proves quiescence, and then activates the staged host configuration and exact release images.
- [ ] Remove every production SQLite database, WAL, shared-memory file, checkpoint path, and fallback after quiescence. Drain tmpfs feeds; provision fresh CrowdSec state; apply and verify the statistics/logging policy; generate fresh credentials; synchronize the Caddy secret. Follow the ordered safety passes below.
- [ ] Raise the monotonic release fence to the exact PostgreSQL/feed release, then follow the [quiescent bootstrap and cutover mode](../design/crowdsec-cutover.md#quiescent-bootstrap): join watchdog scheduling, run vacuum then startup then runtime under exact permits, establish full mode, restore watchdog scheduling, and pass a tick before permitted consumer starts. Prove peer authentication and a bounded expiring synthetic ban without retaining its IP/path; repeat runtime retention if the proof changes candidate state.
- [ ] Confirm the public notice is still live, the seven-field parser is active, eligible target samples persist while forbidden markers do not, and no SQLite file, command, configuration, package dependency, checkpoint, process, or systemd reference remains active.
- [ ] Verify the gate and lease, manual runtime-retention and vacuum passes, backup inclusion and age, offline operation, connection bounds, application health, and no durable log or alarm leakage. Record only timestamps, fixed states, counts, and pass/fail outcomes.
- [ ] Repeat the accepted host rehearsal with CrowdSec on PostgreSQL and record only aggregate resource evidence. Any out-of-memory event, sustained pressure, tmpfs exhaustion, connection exhaustion, or less than 256 MiB unused at the combined peak blocks cutover completion.
- [ ] Invoke [successful-cutover completion](../design/crowdsec-cutover.md#completion-and-failure) only after every gate, safety pass, cutover-mode tick, and final state proof. Require safe lock reuse, permit retirement, timer activation, ordinary watchdog tick, and durable owner release. Keep or restore the marker and stop consumers on uncertainty; recovery remains forward-only.
- [ ] After completion, observe the next scheduled runtime-retention and vacuum passes as production proof. Their failure invokes the normal fail-closed path; it does not authorize SQLite rollback or bypass the PostgreSQL gate.
- [ ] Have QA perform the bounded production proof and report metadata-only evidence. The primary compares it with every required-proof clause.
- [ ] After the release proof, update rows only to `LANDED` where the still-live incident snapshot prevents full closure. Keep this plan open until its deadline instead of moving expiry to backlog.
- [ ] At the recorded 30-day deadline, run or verify the persistent expiry action. Prove the unique envelope key, encrypted snapshot, trash entry, backup/sync copy, and expiry unit are absent. Record only the deadline and pass/fail completion state, with no filename, hash, size, path, or content.
- [ ] Open the final docs-only closure pull request after snapshot expiry. Mark rows `PROVEN` only where retained production evidence covers the entire statement; leave partial rows `LANDED` with the exact missing proof.
- [ ] Move any other unresolved item to `docs/plans/backlog.md`, delete this plan and its execution-brief directory, require independent doc review and green CI, then merge through the normal pull-request flow.

**Exit:** Production runs only the reviewed PostgreSQL design, bounded target-path evidence supports attack investigation, recovery starts CrowdSec fresh, and the temporary SQLite incident snapshot plus every recoverable copy has expired.
