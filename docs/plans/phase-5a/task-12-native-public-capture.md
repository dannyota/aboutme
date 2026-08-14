# Task 12 — Deterministic native fixture and public HTTP capture

**Owner:** Integration owner in serialized window W7.

**Acceptance:** AC-PUB-003, AC-OPS-005/012b, and AC-SEC-001 integrated native
evidence. This task owns no revocation-row test name and does not mutate through
the product API. Composite rows with later-phase clauses remain planned.

**Authorities:** All four Phase 5A docs, `docs/design/deployment.md`, ADR 0017,
ADR 0022, ADR 0024, and the completed T00–T11 reports.

**Files:** The Task 12 row in `file-structure.md`. Do not edit producer
packages, topology/Caddy, OpenAPI, migrations/sqlc, or master records.

**Interfaces:** Starts only after root-owned Phase 4 T15 and consumes the
complete T00–T11 HTTP surface plus T00's isolated native-root overrides.
Produces `p5a-native-fixture seed|cleanup`, the capture script, root target, and
bounded evidence/hash manifest.

## Step 1 — RED a fixed seed matrix and bounded capture

- [ ] Add static script tests for explicit opt-in, loopback-only
      `aboutme_p5a_fixture` DSN, rooted `.dev/p5a-fixture-runtime` process
      state, rooted `.dev/p5a-fixture-media`, fixed
      IDs/slugs/revisions/generation, idempotent seed/cleanup, signal/error
      cleanup, no redirect/external origin, bounded output, no secrets, and
      final process/database/media no-residue checks. Assert every Phase 4 T15
      HTTPS scenario remains in its literal test after the T12 additions.
- [ ] Freeze the fixture command at `--now=2035-01-01T00:00:00Z` and
      `public_state.discovery_generation=41` with exactly three resumes:

  | Fixed state                                          | Fixed UUID; slug/revision                                                                                                              | Expected routes                                           |
  | ---------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------- |
  | Discoverable live + normalized PNG + renamed current | `51000000-0000-4000-8000-000000000001`; current `p5a-live-photo`, old tombstone `p5a-renamed-old` released `2034-12-31T23:00:00Z`; r11 | current JSON/photo/HTML/MD + sitemap/llms; old all absent |
  | Nondiscoverable live                                 | `51000000-0000-4000-8000-000000000002`; `p5a-live-noindex`; r12                                                                        | JSON/HTML; no MD/aggregate inclusion                      |
  | Private                                              | `51000000-0000-4000-8000-000000000003`; `p5a-private`; r13                                                                             | all dynamic routes absent                                 |

  Use owner `51000000-0000-4000-8000-000000000000`, document-local IDs
  `52000000-0000-4000-8000-000000000001` through
  `52000000-0000-4000-8000-000000000008`, current-schema fixture
  `testdata/resume-v2.json`, and photo key
  `p5a-fixture/51000000-0000-4000-8000-000000000001.png`. Freeze PNG and
  response SHA-256 values in testdata. Include fixed hostile rich text whose
  expected JSON/Markdown/HTML contains only sanitizer output. Seed verifies the
  old tombstone is one hour old at the injected command clock. Cleanup is
  literal-allowlisted and leaves no matching resume, claim, tombstone, media,
  job, idempotency row, or object.

- [ ] Run RED:

  ```sh
  bash scripts/test/p5a-native-http-capture-test.sh
  make p5a-native-http-check
  ```

  Expected: fixture command, capture script, and target are absent.

## Step 2 — GREEN seed, capture, cleanup

- [ ] Implement these exact invocations with strict loopback/database/root
      validation, one transaction, and the exact literal allowlist:

  ```sh
  p5a-native-fixture seed --database-url 'postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_p5a_fixture?sslmode=disable' --media-root '.dev/p5a-fixture-media' --now '2035-01-01T00:00:00Z'
  p5a-native-fixture cleanup --database-url 'postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_p5a_fixture?sslmode=disable' --media-root '.dev/p5a-fixture-media' --now '2035-01-01T00:00:00Z'
  ```

  It is the only fixture-row mutation authority. The capture phase issues viewer
  GET/HEAD/conditional/wrong-method requests and cannot call the owner API or
  fixture command.

- [ ] In the capture script, record whether the normal native stack is up and
      stop it if needed. Create/migrate only `aboutme_p5a_fixture`, seed it
      before server startup, then launch fresh Go/Nuxt/Caddy with
      `ABOUTME_DEV_DATABASE_URL` targeting that database,
      `ABOUTME_DEV_STATE_DIR=.dev/p5a-fixture-runtime`, and
      `ABOUTME_DEV_MEDIA_DIR=.dev/p5a-fixture-media`. Never create a second
      database container.
- [ ] Capture through `http://localhost:20080`: JSON GET/HEAD/304, photo, HTML
      discovery both states, Markdown, sitemap, robots, llms, uniform private,
      renamed-old, and tombstone absence, wrong methods, Content-Length, no
      `Set-Cookie`, internal-render denial, gzip/zstd/identity body/ETag parity,
      and no active or raw hostile markup in JSON/Markdown/HTML.
- [ ] Bound readiness stop/restart and store only synthetic selected headers,
      bodies, verdicts, and SHA-256 under `.dev/p5a-evidence/<run-id>/`. Always
      kill and join the isolated stack before cleanup. Run literal cleanup, drop
      only `aboutme_p5a_fixture`, remove only the two validated fixture roots,
      and prove no fixture process/database/media/generation remains even after
      capture failure. If the normal stack was initially up, restart it against
      untouched `aboutme_dev`; otherwise leave it stopped. Process replacement
      discards every fixture cache and coordinator instance.
- [ ] Add root target and run GREEN alone:

  ```sh
  bash scripts/test/p5a-native-http-capture-test.sh
  make test-db-up
  make p5a-native-http-check
  ```

## Executable RED → GREEN checkpoints

- [ ] Fixture RED: run
      `bash scripts/test/p5a-native-http-capture-test.sh -k fixture` and observe
      the command and isolated-root validation are absent. GREEN: implement
      strict `seed|cleanup` parsing, the three literal resumes, generation 41,
      fixed clock/tombstone, current `resume-v2.json`, normalized PNG, and
      transactional literal cleanup; rerun the same static test.
- [ ] Lifecycle RED: run the static test with `-k lifecycle` and observe the
      script can reuse normal database/media/process state. GREEN: record/stop
      normal native state, create/migrate/seed only `aboutme_p5a_fixture`, start
      with the two isolated roots, and install one trap that kills/joins,
      cleans, drops, removes, verifies, and conditionally restores normal native
      state; rerun the test.
- [ ] Capture RED: run `make p5a-native-http-check` and observe missing expected
      hashes/routes. GREEN: after seed and startup, issue only the listed public
      HTTP requests, save bounded synthetic bytes/headers/verdicts, compare all
      hashes, and run the no-residue trap; rerun `make p5a-native-http-check`
      alone.

## Completion

- [ ] Return evidence directory/hash manifest, cleanup result, exact report, and
      all unrun checks. This closes only first-public-JSON-route OPS-012b, not
      authenticated publish UI, HTTPS/443, P5B, P8, or P9.
- [ ] Suggest commit: `test(public): capture native public HTTP evidence`.
