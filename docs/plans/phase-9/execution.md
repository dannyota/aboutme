# P9 execution

> Detailed reference. Where this page describes frozen catalogs, blind authors,
> sealed image identities, or the U10 export tool, ADR 0024 and the
> [phase README](README.md) apply instead.

The executor runs one frozen catalog against one unchanged candidate commit.
This run does not authorize external infrastructure.

## Candidate preparation

At the start of this executor shell, and again after any session interruption,
run `. scripts/uat-identities.sh final`. It restores both immutable image IDs,
all commit-keyed paths, and the coverage-map path, then revalidates their sealed
commit and digests before any deployment or evidence command runs.

1. Record the clean candidate commit, catalog revision, and matching Markdown
   and JSON digests.
2. Run `make ci` at that commit. Do not reuse an earlier run or a passing GitHub
   check.
3. Complete the host prerequisite, worker acknowledgements, native shutdown,
   test-service shutdown, and resource checks in the
   [phase index](README.md#host-and-shared-resource-gate).
4. Record Podman, Compose, Chrome, Playwright MCP, browser and exporter image
   IDs, migration head, and Caddy root identity. Record the manifest-schema,
   sentinel-coverage, supported-format, scanner-ruleset, and OCR identities.
   Record configuration names without values.
5. Confirm that no AWS, Cloudflare, registry, certificate, DNS, or personal
   credential is available to the stack or browser.

A failed prerequisite ends the run as `BLOCKED`. Do not change the host,
candidate, catalog, fixture, or command and call the same run a retry.

## Deployment and reset

Run exactly:

```sh
. scripts/uat-identities.sh final
make uat-up
make uat-reset
make uat-browser-check P9_BROWSER_IMAGE_ID="$P9_BROWSER_IMAGE_ID"
```

`uat-up` resolves the base and overlay before mutation and saves the redacted
resolved service, network, port, and volume inventory. It proves that only
`127.0.0.1:443` is published and that the resolved runtime configuration names
no external service endpoint. Browser evidence separately proves that no
external origin was contacted during the run.

`uat-reset` records the allowlisted resources it recreates and the identifiers
of the deterministic database, OAuth, and media fixture set. Reset is the only
permitted baseline transition before the first criterion.

`uat-up` creates `$P9_SENTINELS_FILE` from `deploy/uat/sentinels.schema.json` as
a mode-`0600` regular file below the run's mode-`0700` `/dev/shm` root. It
contains one independently generated high-entropy value for each closed class:
session, CSRF, OAuth, MinIO, database, and mock-provider secret. MinIO,
database, and mock values become the disposable stack credentials or fixture
secrets. `uat-browser-check` injects the session, CSRF, and OAuth values into
its disposable negative-control cookie, header, and callback capture, then
proves the safe derivative contains none of them. The sentinel file is never
printed, persisted, or mounted into an app container.

`P9_SENTINEL_COVERAGE_FILE` is the exact committed
`deploy/uat/sentinel-coverage.json`. Its closed, value-free rows map all six
classes to injection-target and expected raw-source IDs. `uat-up` and
`uat-browser-check` emit matching closed use IDs. Record its SHA-256 before
startup; a later byte change blocks the run.

## Criterion loop

For each frozen row, in order:

1. Record its initial-state identity, expected assertion IDs, permitted state
   changes, and command IDs from the frozen catalog.
2. Create a new mode-`0700` raw directory in volatile storage. Mount it as the
   browser's only writable path.
3. Perform only the catalog actions through `https://localhost`. Browser name
   resolution remains pinned to `127.0.0.1`. Use accessibility snapshots and
   role/name locators for interaction; screenshots do not replace semantic
   inspection.
4. Capture the required browser, HTTP, application-log, and database
   observations. Database inspection is read-only unless the row names a state
   change.
5. Before export, write the observed assertion IDs, observed state-change IDs,
   exact raw inventory, timestamps, retry count, verdict, and closed reason
   codes into `$P9_CRITERION_RECORD`. Create it atomically as a mode-`0600`
   regular file in the volatile record directory and validate it against U10's
   closed record schema.
6. Export safe evidence before starting another row. Run U10 exactly:

   ```sh
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
   ```

   Values stay in mode-`0600` files and never appear as command arguments. The
   command enforces [the evidence rules](evidence.md) and writes the
   schema-valid criterion manifest.

7. Verify the manifest contains every expected raw source and safe evidence
   digest, then destroy that row's raw and input-record files before continuing.

Direct API calls and database queries may verify a browser action. They never
replace the user action being accepted.

No best-of retry is allowed. A disclosed rerun is a new run ID after the failed
run is preserved. An unplanned mutation, unexplained console or server error,
unsafe export, missing evidence, or output hash mismatch fails the row.

## Failure handling

On failure, stop at the safest boundary and preserve the safe export. A fresh
diagnostic worker reads the design, logs, and evidence and reports the cause. A
separate implementer writes the failing regression, fixes it, and passes the
required risk-tier review.

Any product or harness change creates a new candidate commit. Rebuild and reset
the deployment, then rerun every stale criterion under a new run ID. Never edit
the frozen catalog or a prior run.

## Review and teardown order

The executor does not tear down after the last scenario. It writes a closed live
checkpoint record and volatile runtime inventory, then runs U10's exact live
`checkpoint` command with the immutable image ID, coverage map, and both paths.
The checkpoint proves every criterion digest and all six sentinel classes were
mapped and used, but contains no sentinel value. It does **not** write the run
manifest. The fresh reviewer verifies it, performs the live read-only review in
[Evidence](evidence.md#two-stage-review), writes a mode-`0600` reviewer record
in its volatile directory, and invokes `review` with that record.

After the reviewer records a live `PASS`, the executor runs:

```sh
make uat-down
```

Teardown first removes the runtime raw, record, sentinel, credential, and live-
review roots. The executor then creates a fresh mode-`0700` post-teardown input
directory containing only the closed cleanup record. It encodes allowlisted
assertion IDs, booleans, and prior digests, never raw command output. The
container-backed `checkpoint`, `review`, and `finalize` calls each receive
`--image-id "$P9_EVIDENCE_IMAGE_ID"`; each verified wrapper call deletes its
input after persistence. The reviewer then runs host-only `verify` with
`--expected-image-id "$P9_EVIDENCE_IMAGE_ID"`. It asserts that no volatile P9
root remains, recomputes the manifest digest, and records it in the gate report.
If live review fails, preserve runtime until the failure is recorded and the
owner chooses a safe teardown time; do not keep driving scenarios.
