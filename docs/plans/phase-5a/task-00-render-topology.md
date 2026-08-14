# Task 00 — Render topology, public roots, and edge parity

**Owner:** Integration owner in serialized window W0a.

**Acceptance:** AC-OPS-005. This task supplies route/topology primitives for
Task 02's named rows 2, 19, and 21; it owns no revocation-row test name.

**Authorities:** `design.md`, `public-contract.md`, `public-formats.md`, Design
v4 deployment/security, ADR 0004, ADR 0017, and ADR 0022.

**Files:** The Task 00 row in `file-structure.md`. Do not edit OpenAPI,
migrations/sqlc, Web manifests/lockfiles, or public handler packages.

**Interfaces:** Produces `publicroots.Reserved(string) bool`, typed dispatch and
Caddy outputs, the direct-origin environment contract, and topology hashes.
Consumes the approved v4 root matrix and existing Caddy trust boundary.

## Step 1 — RED the network and generated-route contracts

- [ ] Add the closed source fixture below and static fixtures for duplicate,
      missing, extra, unknown-dispatch, and nondeterministic generator cases;
      and missing/stale Caddy fragments.

  ```json
  {
    "version": 4,
    "roots": [
      { "root": "admin", "dispatch": "reserved" },
      { "root": "api", "dispatch": "go" },
      { "root": "app", "dispatch": "nuxt" },
      { "root": "healthz", "dispatch": "go" },
      { "root": "_nuxt", "dispatch": "nuxt" },
      { "root": "internal-render", "dispatch": "deny" },
      { "root": "llms.txt", "dispatch": "go" },
      { "root": "login", "dispatch": "nuxt" },
      { "root": "people", "dispatch": "reserved" },
      { "root": "print", "dispatch": "deny" },
      { "root": "readyz", "dispatch": "go" },
      { "root": "robots.txt", "dispatch": "go" },
      { "root": "sitemap.xml", "dispatch": "go" },
      { "root": "u", "dispatch": "reserved" }
    ]
  }
  ```

  Reject any top-level/row property beyond those shown, any version other than
  integer 4, duplicate root, changed row/order, or dispatch outside
  `reserved|go|nuxt|deny`.

- [ ] Add closed version-1 app and renderer source manifests. App roots are
      exactly `apps/server/cmd/server` recursive, `apps/server/go.mod` file,
      `apps/server/go.sum` file, `apps/server/internal` recursive,
      `packages/publicroots/public-roots.v4.json` file, and
      `packages/schema/gen/go` recursive. Renderer roots are exactly
      `apps/web/app` recursive, `apps/web/nuxt.config.ts` file,
      `apps/web/package-lock.json` file, `apps/web/package.json` file,
      `apps/web/public` recursive, `apps/web/server` recursive, and the v4
      registry file. Require raw-byte-sorted paths, all regular descendants, no
      symlink/outside/missing path, and D1's length-prefixed SHA-256 stream.
- [ ] Add `scripts/test/render-topology-test.sh`. Parse Compose and assert an
      internal `render` network contains only server/web, has no published port,
      and never appears in `TRUSTED_PROXY_CIDRS`. Assert server has fixed edge
      address `10.90.0.2`, binds only that address, web never joins edge, and
      Caddy/database/migrate/media never join render.
- [ ] Extend literal native/HTTPS tests for direct origins
      `http://127.0.0.1:20030` and `http://127.0.0.1:20440`; the three digest
      variables; deterministic fragment inline/hash/drift failure; Caddy denial
      of both internal-render paths; no web-as-trusted-proxy path; and native
      `ABOUTME_DEV_STATE_DIR`/`ABOUTME_DEV_MEDIA_DIR` rooted overrides used by
      the isolated T12 fixture without changing their `.dev` defaults.
- [ ] Extend `deploy/dev-https-browser/static-test.sh` for generated-fragment
      and manifest presence/hash; extend `network-policy.ts` to prove the
      browser cannot reach the direct render port and viewer requests to both
      internal-render paths receive edge denial. Add root Make target
      `public-roots-check`; make `route-table-test` depend on it and add it plus
      topology syntax/static checks to `operational-test`.
- [ ] Pin route fixtures for this order: deny trees; fixed Go roots; fixed Nuxt
      trees; reserved namespace trees as edge 404; valid 4–30-byte slug or valid
      slug plus `.md` to Go; all invalid/unregistered single segments and
      unmatched nested/framework paths to Nuxt. Include minimum/maximum valid
      slugs, invalid grammar/length, `admin|people|u`, and every fixed root.
- [ ] Add real-Caddy cases for generated dispatch plus upstream unsuffixed body-
      digest ETags across gzip, zstd, conditional suffix stripping, and
      identity.
- [ ] Run RED:

  ```sh
  bash scripts/test/render-topology-test.sh
  bash scripts/dev-https-test.sh --static
  make route-table-test
  ```

  Expected: render isolation, direct origins, and the generated fragment are
  absent; `/internal-render` can reach the Nuxt fallback.

## Step 2 — GREEN the smallest topology and generator

- [ ] Implement the registry generator with this exported Go surface:

  ```go
  package publicroots

  func Reserved(root string) bool
  ```

  It validates closed schema/version/dispatch values and emits bytewise-stable
  Go, Nuxt, Caddy, and test-fixture outputs from one JSON source. It also
  validates both source manifests and prints the two D1 digest values without
  reading Git metadata, clocks, environment secrets, or random state.

- [ ] Add Compose network `render` at `10.91.0.0/28`, `internal: true`, with
      only server and web. Put server on render plus edge `10.90.0.2`, set
      `LISTEN_HOST=10.90.0.2`, `PUBLIC_RENDER_ORIGIN=http://web:3000`, and make
      its healthcheck target `http://10.90.0.2:8080/healthz`. Keep web on
      frontend/render and never edge. Publish no render port.
- [ ] Set `PUBLIC_RENDER_ORIGIN`, `APP_BUILD_DIGEST`, and
      `PUBLIC_RENDERER_BUILD_DIGEST` in Compose and native/HTTPS launch paths.
      Local values are deterministic source-manifest hashes. Production values
      are injected release/image digests. ECS stays same-task loopback
      `http://127.0.0.1:3000`.
- [ ] Make `scripts/dev-native.sh` resolve `ABOUTME_DEV_STATE_DIR` and
      `ABOUTME_DEV_MEDIA_DIR` as repository-rooted paths, reject `/`, the
      repository root, and paths outside the repository, and default to
      `.dev`/`.dev/media`. All pid/log/bin/media operations use those resolved
      roots so T12 can kill/join fixture processes without touching normal
      native state.
- [ ] Make `deploy/web.Dockerfile` copy `apps/web/server/` before build. Mount
      the Caddy fragment directory read-only in Compose. Both `dev-native.sh`
      and `dev-https.sh`, including temporary Caddyfiles used by their tests,
      first run the generator check, require exactly one generated marker,
      replace it with the verified fragment bytes, and write lowercase SHA-256
      for raw registry source, fragment, and final effective Caddyfile. They do
      not use a relative Caddy `import`.
- [ ] Replace handwritten public roots in Caddy with the generated fragment.
      Keep client-IP trust unchanged. Emit the pinned dispatch order. Deny both
      internal-render trees for every method, edge-404 reserved-only trees,
      route fixed/dynamic viewer public artifacts to Go, and preserve Nuxt
      fallthrough for invalid/unregistered paths.
- [ ] Run GREEN:

  ```sh
  node scripts/generate-public-roots.mjs --check
  bash scripts/test/render-topology-test.sh
  bash scripts/dev-https-test.sh --static
  bash deploy/dev-https-browser/static-test.sh
  bash scripts/test/toolchain-contract-test.sh
  bash scripts/test/ci-scan-adversarial-test.sh
  bash scripts/test/makefile-safety-test.sh
  make route-table-test
  ```

## Executable RED → GREEN checkpoints

Do not batch the two sections above. Land each checkpoint only after its narrow
GREEN passes.

- [ ] Registry RED: add `TestReservedAPI` with
      `if !Reserved("api") { t.Fatal("api must be reserved") }`; run
      `(cd apps/server && go test ./internal/publicroots -run TestReservedAPI -count=1)`
      and observe the missing package/symbol. GREEN: generate
      `var reserved = map[string]struct{}{ "api": {} }` plus the other 13
      authority rows and
      `func Reserved(root string) bool { _, ok := reserved[root]; return ok }`;
      rerun the same command, then
      `node scripts/generate-public-roots.mjs --check`.
- [ ] Topology RED: add the `render-topology-test.sh` assertions and run
      `bash scripts/test/render-topology-test.sh`; observe missing `render` and
      digest/origin variables. GREEN: add
      `render: {internal: true, ipam: {config: [{subnet: 10.91.0.0/28}]}}`,
      server/web memberships, fixed edge bind, and the validated native
      state/media-root overrides; rerun the same command and
      `bash scripts/dev-https-test.sh --static`.
- [ ] Edge RED: add a Caddy request for `POST /internal-render/public` whose
      fallback backend records every request; run `make route-table-test` and
      observe the backend hit. GREEN: put an exact internal-render rejection
      matcher before the Nuxt fallback and place the generated viewer matcher at
      the single verified marker; rerun `make route-table-test` and the real
      gzip/zstd ETag cases.

## Adversarial cases and completion

- [ ] Prove web-to-Go on render is refused because Go has no render listener;
      Caddy rejects both internal-render paths before Nuxt fallback and no
      backend receives them; web cannot forge the canonical proxy header;
      fragment drift stops launch; encoded ETags preserve parity. Caddy and web
      share `frontend`, so the proof is route denial, not network impossibility.
- [ ] Return the exact handoff report and generated path list.
- [ ] Suggest commit: `feat(routing): isolate public render topology`.
