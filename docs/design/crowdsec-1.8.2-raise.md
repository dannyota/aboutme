# CrowdSec 1.8.2 host upgrade

Owner decision, 2026-11-10: install 1.8.2 with startup suppressed, then reset.

Reset loses CrowdSec alerts, bans, windows, and machine/bouncer registrations;
protection and registrations rebuild. Application and resume data stay intact. A
few minutes of outage are accepted. [Release 1.8.2] includes the fix and
[upstream test] in PR 4678; the integration flake's cause remains inferred.

## Pin and document map

Baseline references below `host/`, `tests/`, and `edge/` start at `deploy/vn/`.

| Location                                                                                                                    | Change                                                                           |
| --------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `host/install.sh:27`, `:469`                                                                                                | Package pin and installed catalog filename. Keep ordinary migration invocation.  |
| `host/crowdsec-db.sh:17`, `:225`, `:227`, `:264`                                                                            | Sole catalog path, exact comparison, and epoch digest.                           |
| `host/crowdsec-provision.sh:78`, `host/crowdsec-vacuum.sh:20`                                                               | Package assertion and catalog path.                                              |
| `host/crowdsec-schema-v1.8.1.tsv:1`                                                                                         | Replace with generated `crowdsec-schema-v1.8.2.tsv`.                             |
| `tests/crowdsec-db-test.sh:30`, `:113`, `:332`, `:468`, `:592`                                                              | Catalog fixtures, binary assertion, tamper cases, derivation delimiters.         |
| `tests/crowdsec-offline-test.sh:27`, `:30`, `:33`, `:97`, `:231`                                                            | Package/archive pins, reviewed 1.8.2 digest, version and suppression assertions. |
| `tests/crowdsec-retention-fixture.sh:65`, `:343`                                                                            | Proof module and catalog fixture.                                                |
| `tests/install-packages-test.sh:259`, `:263`, `:284`, `:294`                                                                | Held/exact package cases, commands, diagnostics.                                 |
| `tests/install-admission-test.sh:92`, `:290`, `tests/crowdsec-vacuum-test.sh:18`, `tests/crowdsec-integration-test.sh:1017` | Catalog fixtures and success label.                                              |
| `.github/workflows/vn-infra.yml:157`                                                                                        | Derivation delimiters and artifact filename.                                     |
| `edge/crowdsec/scenarios/aboutme-http-rate.yaml:12`, `edge/crowdsec/scenarios/aboutme-http-status.yaml:14`                  | Version-neutral comments after re-proving six-sample bounds; keep values.        |
| `deploy/vn/README.md:311`, `docs/design/crowdsec-http-privacy.md:42`, `:83`                                                 | Current package/catalog and verified engine behavior.                            |
| `docs/design/crowdsec-retention-batching.md:190`, `:230`, `:246`                                                            | Current proof targets; preserve labelled historical findings and citations.      |
| `docs/runbooks/crowdsec-postgresql.md:68`                                                                                   | Own the operator procedure below; no incidental hub refresh.                     |
| `docs/plans/traceability/ac-inf.md:19`, `docs/plans/backlog.md:36`                                                          | Append acceptance evidence; close only the CrowdSec upgrade portion.             |

Keep `docs/design/crowdsec-retention-reload.md:60`, `:104` as historical 1.8.1
findings. Keep firewall bouncer 0.0.36, pgBackRest 2.59.3-1.pgdg24.04+1, and
`deploy/caddy/production/build/go.mod:46` / `go.sum:138` client dependencies. No
app API, resume schema, or older-client fallback changes. Update every test pin.

## Catalog and admission

Generate through hosted **CrowdSec PostgreSQL** after reviewing the signed 1.8.2
package, maintainer scripts, digest, and suppression proof. Its derive command
is
`sudo env GITHUB_ACTIONS=true unshare --mount --propagation private bash deploy/vn/tests/crowdsec-db-test.sh derive`.
Download `crowdsec-postgresql-catalog` from that exact commit's run.
Independently review its complete diff against 1.8.1 and commit the generated
TSV unchanged. Expect replacement of `decision_value_type_scope_until` by the
index including `simulated` and its relation metadata, per the [decision
schema]; investigate any additional changes. Artifact upload precedes
assertions, so it is not a pass. The generated catalog (run 42731076259) also
adds the nullable `machines.features` jsonb column, which lists the engine's
enabled features ([PR 4770]); it holds no request data.

Ordinary admission compares exactly one compiled-in 1.8.2 catalog and binds its
digest into the epoch. Keep ownership, role, statistics, notice, lock, and lease
checks. Only empty provisioning initializes the catalog; old files grant no
access.

## Installation and recovery

Use the existing operator reset, with no installer reset flag or automatic loss.
`install.sh:415` suppresses package startup and registration; `:466` installs
helpers and catalog before the ordinary migration at `:577`. On an occupied
1.8.1 database that call fails target preflight, stops consumers, and refuses
`fresh-admission` (`crowdsec-migrate.sh:177`, `:185`, `:186`). It cannot
complete without reset. No new controller, admission exception, or marker
protocol.

1. Require independent review, green exact-commit main-push CI, a patch tag and
   successful release-image publication under `instructions/releases.md:16`.
   Before host installation, take the encrypted incremental pgBackRest backup
   through the workload lock and release annotation in
   `deploy/vn/scripts/deploy-host.sh:277`. Require completion and healthy WAL
   archiving; the later deploy backup is too late. Keep keys on the host.
2. Record the intended active edge and keep the reviewed old bundle/package
   available. Copy `deploy/vn` from clean reviewed main; run
   `sudo bash <bundle>/host/install.sh <bundle>`. Accept only the expected
   `crowdsec_migrate_refused step=fresh-admission` boundary after verifying
   installed 1.8.2, its hold, matching helpers/catalog, and stopped consumers.
   Any other failure needs diagnosis before proceeding.
3. After installer exit releases its bundle lock, run
   `sudo /usr/local/lib/aboutme/crowdsec-migrate.sh --reset`. The installed
   root-only helper locks the bundle, checks idle/notice admission, stops and
   joins consumers, and drops only `crowdsec` without `FORCE`. Fresh
   provisioning gates the catalog before registration, startup retention,
   timers, engine, and bouncers (`crowdsec-migrate.sh:187`). Protected files
   replace all credentials; old credentials fail. Startup clears feeds/caches
   and rebuilds firewall state.
4. Reset sees no active edge after the failed installer. Restore only the
   recorded mode through normal guards, for example
   `sudo systemctl start aboutme-caddy.service`. Rerun the same installer to
   `install: done`, completing activation and settings skipped by the refusal
   (`docs/runbooks/crowdsec-postgresql.md:51`). Budget a brief PostgreSQL
   restart. Then run
   `DEPLOY_HOST=<host> bash deploy/vn/scripts/deploy.sh <tag>`.

A failed reset revokes its lease, retains failure denial, and stops consumers
(`crowdsec-migrate.sh:213`). Diagnose the fixed step, repair the cause, rerun
explicit `--reset`, restore the recorded edge, and finish the installer. Do not
clear fences/markers manually. For downgrade, use the reviewed 1.8.1 bundle's
installer to install its authenticated package with startup suppressed and its
matching helpers/catalog (`install.sh:154`). At occupied-catalog refusal, reset
under that old bundle, restore the edge, and finish its installer. CrowdSec
state is lost again. Preserve notice/fence fixes; never restore the application
cluster for CrowdSec. Image rollback alone retains the working host bundle.

## Acceptance

Keep package/version assertions and the upstream partial-line test as regression
evidence. Require two green hosted **CrowdSec integration** runs on the exact
final head, with URLs and SHA recorded; diagnose failures before rerunning. Only
then call the flake closed by inference, never a proven root cause. Require the
existing PostgreSQL, suppression, installer, migration, retention, and vacuum
proofs, including install/refusal/reset/reinstall, failed reset and old-bundle
rollback, credential rejection, application isolation, flush/sample bounds, and
existing bouncer compatibility. Keep resource limits unchanged.

After deployment, run `docs/runbooks/crowdsec-postgresql.md:107` checks for
catalog, LAPI, lease, services and timers; verify package/hold, public
readiness, ban enforcement/removal, PostgreSQL health, backup/WAL state, and
successful scheduled retention and vacuum. Retain aggregate results only. The
[privacy security boundaries](crowdsec-http-privacy.md#security-boundaries)
remain: web/token attackers cannot reset; containers lack peer/root authority;
network attackers cannot replace signed packages; database writers cannot bless
catalogs or leases; leaked backups keep existing custody and reset requirements.
Digest, catalog, review, CI, and production proofs remain pending.

[Release 1.8.2]: https://github.com/crowdsecurity/crowdsec/releases/tag/v1.8.2
[upstream test]: https://github.com/crowdsecurity/crowdsec/pull/4678/files
[decision schema]:
  https://github.com/crowdsecurity/crowdsec/blob/v1.8.2/pkg/database/ent/schema/decision.go#L51-L57
[PR 4770]: https://github.com/crowdsecurity/crowdsec/pull/4770
