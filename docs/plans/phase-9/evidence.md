# P9 evidence and review

> Detailed reference. Where this page describes frozen catalogs, blind authors,
> sealed image identities, or the U10 export tool, ADR 0024 and the
> [phase README](README.md) apply instead.

Only redacted, allowlisted evidence persists. Raw browser and network output
stays in volatile storage and is destroyed after safe export and review.

Evidence stays on this machine under an ignored path and is never committed; see
the [phase README](README.md). The executor and reviewer cannot replace it with
manual filtering, a new script, or an unpinned OCR command during a run.

## Paths and identity

Final evidence lives under `.superpowers/uat/p9/<commit>/<run-id>/`. Raw output
uses a mode-`0700` directory created with `mktemp -d` under `/dev/shm` and is
mounted only into the browser's `/evidence`. If `/dev/shm` cannot hold the
configured bound, the run is `BLOCKED`; raw output does not fall back to a
persistent path.

The report and machine-readable manifest record:

- commit, catalog revision and both catalog-file digests, run ID, and
  timestamps;
- browser and exporter image IDs, migration head, Caddy root digest, tool
  versions, and commands;
- configuration names without values and the redacted runtime inventory;
- every state change, retry count, criterion verdict, evidence digest, and the
  sentinel-coverage digest;
- executor live-state handoff, teardown result, and both reviewer verdicts.

Input records validate against `scripts/uat-evidence/record.schema.json`; output
documents validate against `scripts/uat-evidence/manifest.schema.json`. Each
criterion writes
`.superpowers/uat/p9/<commit>/<run-id>/criteria/<criterion-id>/evidence-manifest.json`.
The verified tool also writes immutable live and cleanup checkpoints plus the
two structured reviewer verdicts. After both review stages, the finalizer writes
`.superpowers/uat/p9/<commit>/<run-id>/run-manifest.json`. Every closed document
kind validates with no unknown properties before persistence and again during
review. The ruleset is `scripts/uat-evidence/patterns.json`; callers cannot add,
remove, or weaken a pattern.

## Safe export

After each criterion, `scripts/uat-evidence/run.sh export` inventories every raw
file. It parses supported formats in volatile space and writes a new artifact
containing only the allowlisted fields below. Copying or renaming a raw trace,
HAR, video sidecar, or network dump is forbidden.

| Source          | Fields allowed to persist                                                                                 |
| --------------- | --------------------------------------------------------------------------------------------------------- |
| Network event   | Timestamp; method; `https://localhost` path without query or fragment; status; duration; error class      |
| Response header | `Content-Type`, `Cache-Control`, `ETag`, `X-Request-ID`, `X-Robots-Tag`, and security-header names/values |
| Console event   | Timestamp; level; source path without query; redacted message; expected/unexpected classification         |
| Server event    | Timestamp; level; route name; status; request ID; closed error code; no free-form request data            |
| Database check  | Criterion-owned counts, booleans, revisions, and opaque fixture IDs; no credentials or connection string  |
| Screenshot      | Catalog-named viewport and page only, after visual inspection for secret or token exposure                |
| Accessibility   | Role/name/state tree after fake fixture values and token-like text are redacted                           |

Request and response bodies, request headers, cookies, storage values, full
URLs, connection strings, environment values, and raw SQL output never persist.
A trace or video is required only when a pinned format-aware exporter can create
an allowlisted derivative and prove those fields are absent. The catalog freeze
is `BLOCKED` if a criterion requires one and no such exporter exists. An
unexpected raw type or sidecar fails the run; discarding it does not cure the
missing safe export.

Before persistence, the exporter replaces catalog-declared fake personal data
and scans the candidate artifact for:

- every run-specific canary value injected into session, CSRF, OAuth, MinIO,
  database, and mock-provider secret fields;
- `Authorization`, `Cookie`, `Set-Cookie`, `__Host-session`, `__Host-oauth-tx`,
  OAuth code/state/nonce, CSRF, bearer, basic-auth, and JSON Web Token patterns;
- AWS access-key and secret-key patterns, private-key headers, credential URL
  forms, and environment-assignment forms containing credential values;
- a URL containing a query or fragment and any path outside `https://localhost`.

The scan runs on decoded text, archive members, and optical character
recognition output for images or video frames, not only outer bytes. A match,
parser error, unknown member, unsupported raw format, or incomplete redaction
fails the criterion. The exporter deletes its partial output. It never weakens a
pattern to make an artifact pass.

`scripts/uat.sh` creates the sentinel file before stack startup from
`deploy/uat/sentinels.schema.json`. Its six values are unique within the run and
differ from test fixtures. The exact committed coverage map is
`deploy/uat/sentinel-coverage.json`. It is a closed, value-free JSON object with
`schemaVersion` and exactly six sorted `classes` rows. Each row has only
`classId`, `injectionTargetIds`, and `expectedRawSourceIds`.

U5 tests generation, mode, owner, injection, negative-control capture, complete
mapping, absence from resolved configuration and logs, and destruction. U10
mounts the map read-only for every `export` and the live `checkpoint`, records
its SHA-256, and rejects a missing, duplicate, extra, malformed, or unused
class. Unused means a row lacks a closed injection proof or none of its expected
raw-source IDs appears in the completed live inventory. `uat-down` deletes every
runtime sentinel, credential, raw, staging, input-record, and live-review root
before cleanup inputs are created.

After a clean scan, the tool writes the safe artifact and criterion manifest in
volatile staging. The host wrapper validates them, copies each safe file to a
temporary name in the final run directory, sets read-only permissions, and
compares the staged and copied SHA-256 values before renaming it atomically. It
records the source type, export mode, tool identity, and digest. It records a
digest of each raw source before deletion so an overwrite or unassigned file is
detectable, but never persists raw bytes.

## Evidence completeness

Each row links its expected and observed result to the catalog-required safe
artifacts. After every scenario, compare the raw inventory with the exporter
manifest before clearing the volatile directory. An unassigned output, missing
artifact, digest mismatch, undisclosed retry or state change, unexplained error,
secret-pattern match, or `BLOCKED` fails the run.

A later product-code commit makes every row probing a changed path stale. The
run manifest cannot be edited to point at the new commit.

## Two-stage review

The same fresh reviewer performs both stages without changing runtime or
executor files. Its only write is a new mode-`0600` volatile reviewer record,
which the verified `review` command validates and persists once at the matching
append-only `reviews/<stage>.json` path.

### Live review before teardown

The reviewer verifies the candidate and catalog identities, image and root
digests, runtime labels, exact `127.0.0.1:443` publication, service and network
allowlists, and the PostgreSQL and MinIO isolation. The reviewer samples every
safe evidence type and reruns a catalog-declared deterministic read-only subset
through the trusted browser. Reviewer output stays in a separate volatile
directory and is discarded; it is not added to executor evidence.

The subset may use GET/HEAD, accessibility inspection, public navigation, and
read-only database or container inspection. It cannot reset fixtures, submit a
mutation, change evidence, or stop a service. The reviewer records only a
`PASS`, `FAIL`, or `BLOCKED` verdict, closed reason codes, and catalog-defined
read-only probe IDs through the verified `review` mode. It changes no runtime,
product, catalog, criterion, or executor artifact.

Teardown cannot begin without a live `PASS`.

### Cleanup review after teardown

After the executor runs `make uat-down`, the reviewer verifies that:

- no `aboutme-uat` container or network remains;
- every allowlisted UAT data, media, trust, and runtime volume is gone;
- no other project, container, network, volume, or host trust store changed, and
  the sysctl still equals its recorded post-administrator pre-run value;
- every runtime volatile directory is gone;
- criterion and checkpoint evidence remains read-only and every digest matches.

The cleanup checkpoint and review each use a new, separate volatile input
directory. The verified wrapper removes each immediately after it persists the
closed document. The executor then writes the run manifest exactly once from the
two checkpoints and two reviewer verdicts. The reviewer runs the verified
read-only check, which fails if any P9 volatile root remains, recomputes its
SHA-256, and records that digest in the gate report. P9 passes only when both
review stages and every criterion are `PASS` at the same candidate commit.
