# Task 09 — Closed Nuxt SSR worker and hydration

**Owner:** Web author in W4. The integration owner alone performs manifest and
lockfile window W4b after RED proves the direct build dependencies.

**Acceptance:** AC-PUB-003 and AC-OPS-005 renderer prerequisites. This task has
no revocation-row test ownership.

**Authorities:** `public-formats.md`, `public-contract.md`, Phase 3 renderer
Tasks 06/07/09/11, ADR 0005, ADR 0017, and ADR 0022.

**Files:** Task 09 Web paths in `file-structure.md`. Do not edit Go,
Caddy/topology, shared golden expected bytes, or manifests outside W4b.

**Interfaces:** Consumes Task 04 generated `PublicResume` and Task 08 goldens.
Produces the exact Step 1 request/runner, emitted worker URL/artifact, and
external public assets consumed by Task 10.

## Step 1 — RED the real build and noncooperative worker boundary

- [ ] Add strict handler tests: POST-only, `application/json`, stream cap at
      532,480 bytes before parse, duplicate/unknown/trailing/shape rejection,
      four fields only, mode, current schema, normalized 512-byte ASCII origin,
      photo context, and 2,097,152-byte HTML cap.
- [ ] Repeat Task 04's generated `PublicResume` wire and Task 08's JSON-LD
      bytes. Compile this exact interface:

  ```ts
  export interface PublicRenderRequest {
    publicResume: PublicResume;
    mode: "continuous";
    canonicalOrigin: string;
    discoveryEnabled: boolean;
  }
  export function runPublicRenderWorker(
    request: PublicRenderRequest,
    options: { signal: AbortSignal; deadlineMs: 5000 },
  ): Promise<string>;
  ```

- [ ] Build an actual Vue worker and assert repeat bytes plus worker exit before
      promise resolution. Use a separate test worker that spins forever without
      observing AbortSignal. Abort it and advance an injected monotonic timer to
      exactly 5,000 ms; assert `terminate()` once, observed worker exit/join,
      late message rejection, then generic rejection. Do not fake cooperation.
- [ ] Cover malformed message, worker error, nonzero exit, no result, oversize
      result, and natural exit. Production build must contain the emitted
      worker.
- [ ] Run RED:

  ```sh
  (cd apps/web && npx vitest run test/public-render)
  make web-build
  ```

  Expected: the route, worker artifact, and virtual URL module are absent.

## Step 2 — GREEN a separate joined worker build

- [ ] In W4b pin direct dev dependencies exactly:

  ```sh
  cd apps/web && npm install --save-dev --save-exact vite@8.2.0 @vitejs/plugin-vue@6.0.8
  ```

- [ ] Add Vite SSR build of `worker-entry.ts` with the Vue plugin into Nuxt's
      build directory. A Nitro Rollup hook emits `workers/public-render.mjs`;
      virtual `#public-render-worker-url` resolves with `ROLLUP_FILE_URL_*`.
      Declare that module and assert production output.
- [ ] Spawn one Worker per render with immutable four-field `workerData`. Retain
      success but resolve only after clean exit. On abort/deadline, call and
      await `worker.terminate()` exactly once and also observe exit before
      return. Reject all late messages and every malformed/error/nonzero/no-
      result/oversize outcome with one generic failure.
- [ ] Worker validates input, renders one deterministic complete document,
      checks cap, posts one closed result, closes its parent port, and exits. It
      receives no request object, cookies, headers, session, IDs, fetch, API, DB
      capability, clock, or randomness.
- [ ] Add `PublicResumeApp.vue` continuous rendering and external hydration.
      Hydration derives the validated path slug and fetches only public JSON
      with credentials omitted. When the fetched revision equals the SSR DOM
      revision, call
      `createSSRApp(PublicResumeApp, { publicResume }).mount(root)` to hydrate
      and preserve the nodes. When it differs, call `root.replaceChildren()`
      then `createApp(PublicResumeApp, { publicResume }).mount(root)` to replace
      stale markup. Fetch/parse/mount failure leaves the accessible SSR document
      and its links/text unchanged and records only a secret-free client
      diagnostic; it never clears the root or inserts an error page.
- [ ] Test matching-revision hydration without duplicate nodes, differing-
      revision replacement, and network/shape/mount failure with the original
      SSR title, main landmark, text, and links still accessible.
- [ ] Run GREEN:

  ```sh
  (cd apps/web && npx vitest run test/public-render)
  make web-lint web-typecheck web-test web-build
  ```

## Executable RED → GREEN checkpoints

- [ ] Handler RED: POST a duplicate-key 532,481-byte stream and assert no worker
      starts; run
      `(cd apps/web && npx vitest run test/public-render/handler.test.ts)` and
      observe the route is absent. GREEN: stream-cap at 532,480 before strict
      four-field decoding, then call
      `runPublicRenderWorker(request, { signal, deadlineMs: 5000 })`; rerun the
      command.
- [ ] Build RED: run
      `(cd apps/web && npx vitest run test/public-render/build.test.ts)` and
      `make web-build`; observe no emitted worker or virtual URL. GREEN: after
      W4b pins the two exact packages, build `worker-entry.ts` as Vite SSR with
      Vue, emit `workers/public-render.mjs` through the Nitro Rollup hook, and
      resolve the emitted file through `#public-render-worker-url`; rerun both
      commands.
- [ ] Lifecycle RED: run the real infinite-loop worker test with
      `(cd apps/web && npx vitest run test/public-render/worker-lifecycle.test.ts)`
      and observe abort/deadline returns before exit. GREEN: create one `Worker`
      with immutable `workerData`, retain its result until clean `exit`, and on
      abort/deadline execute `await worker.terminate()` once before rejecting;
      rerun the command, including exact 5,000 ms and late-message cases.
- [ ] Hydration RED: run
      `(cd apps/web && npx vitest run test/public-render/hydration.test.ts)`
      with matching, differing, and failed JSON. GREEN: call
      `createSSRApp(PublicResumeApp, { publicResume }).mount(root)` for matching
      revision, call `root.replaceChildren()` then
      `createApp(PublicResumeApp, { publicResume }).mount(root)` for differing
      revision, and leave `root` untouched on failure; rerun the command, then
      `make web-lint web-typecheck web-test web-build`.

## Completion

- [ ] Return emitted worker/asset paths, observed join evidence, measured caps,
      dependency resolution, and exact report.
- [ ] Suggest commit: `feat(web): isolate public SSR in joined workers`.
