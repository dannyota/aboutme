# U10 — Verified evidence tooling

Status: **High risk; blocked until U6 freezes sentinel custody and U7 freezes
the browser output contract**.

U10 implements the only path from volatile browser output to persistent P9
evidence. It runs before the catalog author, so criteria can require only
formats the verified tool can export safely.

Its input contracts are U6's closed sentinel schema and
`deploy/uat/sentinel-coverage.json`, plus U7's
`.superpowers/uat/p9-tooling/<commit>/browser-output-contract.json`. U10 records
both file digests, requires the startup-recorded coverage digest, and fails if a
contract changed, is incomplete, or conflicts with `formats.json`.

## Fixed interface

The implementation owns these exact paths:

- `deploy/uat-evidence.Dockerfile`;
- `scripts/uat-evidence/package.json` (the integration owner generates
  `package-lock.json` in its serialized lockfile window);
- `scripts/uat-evidence/.containerignore`;
- `scripts/uat-evidence/export.mjs` and `run.sh`;
- `scripts/uat-evidence/manifest.schema.json`;
- `scripts/uat-evidence/record.schema.json`;
- `scripts/uat-evidence/catalog.schema.json`;
- `scripts/uat-evidence/formats.json`;
- `scripts/uat-evidence/patterns.json`;
- `scripts/uat-evidence/export.test.mjs`;
- `scripts/uat-evidence/testdata/author/**`.

The root `Makefile` and package lock remain integration-owner-only. Once the
package manifest is reviewed, the owner generates the lock before the
implementation author installs dependencies. After both suites pass, the owner
adds `uat-evidence-test`. That target installs the lock, runs both Node suites,
builds with `--pull=never` and an `--iidfile`, verifies embedded source, lock,
schema, format, and pattern digests, and passes that immutable image ID to
`run.sh self-test`. It never trusts a pre-existing tag. The owner then adds the
target to `make ci`. No worker edits a shared file.

The image pins its base digest, Node runtime, optical character recognition
(OCR) engine with English and Vietnamese language data, image decoders, and
archive reader. The task-local lock pins JavaScript dependencies. The image runs
as a fixed non-root user with no network, a read-only root filesystem, bounded
tmpfs, no extra capabilities, and no host source or final-evidence mount. Its
build context is exactly `scripts/uat-evidence/`; `.containerignore` starts
deny-all and admits only the reviewed runtime files, lock, schemas, matrices,
and required OCR fixtures. The Dockerfile uses named `COPY` sources, never
`COPY .`. Static and image-inventory tests prove `.env`, `.git`, `.superpowers`,
`.dev`, test output, and an unexpected file cannot enter the context or image.

`scripts/uat-evidence/run.sh` mounts each mode's raw directory, record, catalog,
sentinel file, coverage map, browser contract, and prior-document snapshot only
as needed and read-only with `--security-opt label=disable`. A distinct
`/dev/shm` safe-staging directory is read-write; no workspace or final evidence
path enters the container. After success, the host wrapper validates staged
paths and digests, persists allowlisted files read-only, and atomically renames
them.

The command modes are:

```sh
scripts/uat-evidence/run.sh self-test --image-id "$P9_EVIDENCE_IMAGE_ID"
scripts/uat-evidence/run.sh export \
  --image-id "$P9_EVIDENCE_IMAGE_ID" \
  --record-file "$P9_CRITERION_RECORD" \
  --catalog-file "$P9_CATALOG_FILE" \
  --raw-dir "$P9_RAW_DIR" \
  --sentinels-file "$P9_SENTINELS_FILE" \
  --sentinel-coverage-file "$P9_SENTINEL_COVERAGE_FILE" \
  --browser-output-contract "$P9_BROWSER_OUTPUT_CONTRACT" \
  --safe-staging-dir "$P9_SAFE_STAGING_DIR" \
  --final-run-dir "$P9_RUN_DIR"
scripts/uat-evidence/run.sh checkpoint \
  --image-id "$P9_EVIDENCE_IMAGE_ID" \
  --record-file "$P9_LIVE_CHECKPOINT_RECORD" \
  --catalog-file "$P9_CATALOG_FILE" \
  --raw-dir "$P9_LIVE_RAW_DIR" \
  --sentinels-file "$P9_SENTINELS_FILE" \
  --sentinel-coverage-file "$P9_SENTINEL_COVERAGE_FILE" \
  --browser-output-contract "$P9_BROWSER_OUTPUT_CONTRACT" \
  --safe-staging-dir "$P9_SAFE_STAGING_DIR" \
  --final-run-dir "$P9_RUN_DIR"
scripts/uat-evidence/run.sh review \
  --image-id "$P9_EVIDENCE_IMAGE_ID" \
  --record-file "$P9_LIVE_REVIEW_RECORD" \
  --catalog-file "$P9_CATALOG_FILE" \
  --final-run-dir "$P9_RUN_DIR"
scripts/uat-evidence/run.sh checkpoint \
  --image-id "$P9_EVIDENCE_IMAGE_ID" \
  --record-file "$P9_CLEANUP_CHECKPOINT_RECORD" \
  --catalog-file "$P9_CATALOG_FILE" \
  --final-run-dir "$P9_RUN_DIR"
scripts/uat-evidence/run.sh review \
  --image-id "$P9_EVIDENCE_IMAGE_ID" \
  --record-file "$P9_CLEANUP_REVIEW_RECORD" \
  --catalog-file "$P9_CATALOG_FILE" \
  --final-run-dir "$P9_RUN_DIR"
scripts/uat-evidence/run.sh finalize \
  --image-id "$P9_EVIDENCE_IMAGE_ID" \
  --final-run-dir "$P9_RUN_DIR"
scripts/uat-evidence/run.sh verify \
  --expected-image-id "$P9_EVIDENCE_IMAGE_ID" \
  --final-run-dir "$P9_RUN_DIR"
```

`self-test`, `export`, both `checkpoint` calls, both `review` calls, and
`finalize` are container-backed. Each requires the immutable image ID and
rejects a tag or a missing or changed ID. `verify` is the sole host-only mode.
It never parses raw data or writes; it checks the final tree, recorded image and
source identities, file digests and modes, and absence of volatile P9 roots.

Every record is a mode-`0600` regular file under the run's mode-`0700` volatile
record directory. `run.sh` rejects another owner, broader mode, symlink, hard
link, special file, path escape, unknown field, or schema mismatch before it
reads raw output. `export` accepts only a canonical `P9-R1-###` criterion
record. The live `checkpoint` accepts its matching record and scans its declared
raw inventory with the run sentinels, coverage map, and browser contract.
Cleanup accepts only schema-closed IDs, booleans, and prior digests from its
record; it cannot ingest raw text, logs, URLs, headers, or command output after
the sentinel file is destroyed. Neither checkpoint writes the run manifest.
`review` accepts only its matching reviewer record and appends one stage
document. `finalize` requires both checkpoints and reviews, verifies their
identities and digests, and writes the run manifest once. `verify` is read-only
and prints only its digest and closed result. No mode overwrites an artifact.

## Volatile record contract

`record.schema.json` has `additionalProperties: false` for three input kinds:

- a criterion record identifies the commit, catalog, run, row, timestamps, retry
  count, catalog-defined expected and observed assertion IDs, permitted and
  observed state-change IDs, command IDs, verdict, reason codes, and exact
  expected raw inventory;
- a checkpoint record identifies its `live` or `cleanup` stage, criterion
  manifest digests, closed runtime or cleanup assertions, command IDs, state
  changes, and required sentinel-class coverage without sentinel values;
- a reviewer record identifies its stage, checkpoint digest, verdict, closed
  reason codes, and catalog-defined read-only probe IDs.

The tool validates the frozen JSON against `catalog.schema.json`, verifies its
digest and record identity, then cross-checks every criterion, assertion,
command, state-change, reason, probe, raw-type, and export-mode ID. There is no
free-form result, command, state, header, URL, or log field. The executor
records an observation before export. The reviewer records a verdict before
`review`. A missing, late, duplicate, unknown, or contradictory record fails the
run. Input records stay volatile and are destroyed after their schema-valid
output and digest are verified.

## Manifest contract

`scripts/uat-evidence/manifest.schema.json` is the output JSON Schema with
`additionalProperties: false`. It covers these closed document kinds:

- criterion manifests at
  `.superpowers/uat/p9/<commit>/<run-id>/criteria/<criterion-id>/evidence-manifest.json`;
- live and cleanup checkpoints under `checkpoints/`;
- live and cleanup reviewer verdicts under `reviews/`;
- the final `.superpowers/uat/p9/<commit>/<run-id>/run-manifest.json`.

Required fields include schema version; document kind; candidate commit; catalog
revision and both file digests; run and criterion IDs; exporter image ID and
source digest; browser-output-contract, sentinel-coverage, lockfile,
supported-format, scanner-ruleset, schema, and OCR identities; start and finish
timestamps; retry count; declared state changes; closed outcome and reason
codes; raw source type, byte count, and SHA-256; export mode; safe relative
artifact path, media type, byte count, and SHA-256; and reviewer and cleanup
states where they apply.

Paths are normalized relative paths. Digests are lowercase SHA-256 hex. Verdicts
are only `PASS`, `FAIL`, or `BLOCKED`. Required arrays are present even when
empty. Free-form headers, bodies, URLs, environment values, connection strings,
and raw log text have no schema field. The scanner processes the manifests too.

## Fail-closed export behavior

The tool implements [the evidence allowlist](evidence.md#safe-export) as code,
not caller-selected field names. It must:

- load the exact raw-type, parser, safe-output, and export-mode matrix from
  `formats.json`, record its digest, and reject caller-added formats;
- parse supported JSON, JSON Lines, text, HAR, and pinned Playwright trace
  members and emit only the declared safe fields;
- strip every URL query and fragment before any safe value leaves volatile
  memory;
- OCR every persisted image and every image member extracted from a supported
  trace, then scan both recognized text and bytes;
- reject video until a pinned exporter can inspect every frame; U7 disables
  video capture for r1, and an unexpected video fails export;
- decode and scan supported archive members recursively within its fixed
  structural limits, reject unknown members and formats, and never extract a
  path outside volatile staging;
- scan for every run sentinel plus cookie, authorization, OAuth, CSRF, token,
  credential, private-key, query, fragment, and external-origin pattern;
- require exactly the six coverage-map classes and reject a class with no closed
  injection proof or expected raw-source use by the live checkpoint;
- load the exact closed pattern set from `patterns.json`, record its digest, and
  reject a caller-supplied rule or exclusion;
- delete partial staged output and write nothing persistent on a match, OCR or
  parser error, timeout, unsupported type, missing raw file, extra raw file,
  digest mismatch, schema failure, or incomplete inventory;
- produce deterministic key order, artifact order, hashes, and reason codes for
  identical inputs.

Failures emit only a closed reason code and a normalized relative source name.
They never echo source bytes, recognized text, a sentinel, an environment value,
or an absolute host path.

Raw data is streamed. The exporter never loads an unbounded archive, trace,
image, or log into memory. U10 freezes its file-count, byte, nesting, pixel,
frame, and runtime limits in `formats.json`, code, and the self-test manifest
before catalog authoring. A criterion whose required artifact exceeds a frozen
limit is not split or retried silently; the catalog freeze is `BLOCKED` until
the evidence contract changes through review.

## Independent blind tests

A fresh test author owns only:

- `scripts/uat-evidence/adversarial.test.mjs`;
- `scripts/uat-evidence/testdata/adversarial/**`.

Before reading any implementation diff, derive fixtures for plain, encoded,
nested-archive, image, and trace canaries; cookie and authorization fields;
OAuth and CSRF values; JSON Web Tokens; AWS-style keys; credential URLs;
queries, fragments, and external origins; malformed and traversal archives;
symlinks and special files; missing and extra raw files; missing, extra,
duplicate, or unused sentinel classes; OCR failure; unsupported video; digest
and schema mismatch; partial-output cleanup; deterministic order; exact catalog,
input, and output schema closure; catalog index mismatch or unknown runtime ID;
unsafe record mode, owner, type, link, or path; build-context and image
sensitive-path controls; missing or contradictory record state; pattern override
attempts; and source/destination copy digest or browser-output-contract
mismatch.

Run and record the expected failure caused by the missing tool:

```sh
node --test scripts/uat-evidence/adversarial.test.mjs
```

Freeze this test-only diff before the implementation author reads it.

## Implementation and author tests

A different implementation author owns only the fixed implementation paths
except the integration-owned lockfile. First write `export.test.mjs` for each
supported input, record and output kind, path boundary, atomic copy,
checkpoint/review ordering, single finalization, read-only verification,
immutable image identity, and failure cleanup. Run and record the expected
failure before writing implementation:

```sh
node --test scripts/uat-evidence/export.test.mjs
```

Implement the smallest tool that passes both suites. Resolve and record every
image and binary version before building. Run:

```sh
npm --prefix scripts/uat-evidence ci --ignore-scripts
npm --prefix scripts/uat-evidence test
node --test scripts/uat-evidence/adversarial.test.mjs
. scripts/uat-identities.sh paths
podman build --pull=never -f deploy/uat-evidence.Dockerfile \
  --iidfile "$P9_EVIDENCE_IID_FILE" \
  scripts/uat-evidence
read -r P9_EVIDENCE_IMAGE_ID < "$P9_EVIDENCE_IID_FILE"
scripts/uat-evidence/run.sh self-test --image-id "$P9_EVIDENCE_IMAGE_ID"
make scan
```

The self-test runs the built image with network disabled, exercises one safe
fixture for every `formats.json` row and every closed failure class, validates
every document kind and state transition, and proves that no failed case created
a persistent file.

## Freeze and review

A third fresh reviewer who authored neither suite nor implementation reviews the
source, Dockerfile, dependency lock, schema, fixtures, Makefile target, and
command output. The review covers path confinement, archive handling, English
and Vietnamese OCR, token patterns, schema closure, deterministic output, copy
verification, atomic persistence, secret-free logs, and negative-control
quality. Blocking findings return to the implementation author; an independent
reviewer rechecks the fix.

The integration owner reruns both test suites, the image build, `self-test`,
`make uat-evidence-test`, `make ci`, and `make scan` at one unchanged commit.
The owner records the image ID, source digest, lockfile, schema,
sentinel-coverage, scanner-ruleset, and `formats.json` digests, OCR identity,
and frozen resource limits. U10 passes only when the fresh review and every
command pass without retry.

The catalog author consumes the frozen `formats.json` and may not add an
evidence type or field. Any tool, schema, dependency, format, pattern, OCR, or
limit change after catalog freeze creates a new catalog revision and stales all
evidence produced by the prior tool identity.
