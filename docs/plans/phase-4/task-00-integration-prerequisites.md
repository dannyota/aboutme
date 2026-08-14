# Task 00: Authenticated transport and dependency windows

**Owner:** Phase 4 integration owner. This task is serialized because every path
is a manifest, lockfile, generated artifact, Caddy source, or shared transport
policy.

**Authorities:** `design.md` Dependencies, Design `api.md` Conventions, ADR
0016, `editor-contract.md` Native HTTPS browser proof, and D1/D11/D18/D19 in
`decisions.md`.

**Acceptance:** AC-EDITOR-001 and AC-EDITOR-017.

**Files:**

- Modify: `apps/server/internal/api/cache_policy.go`
- Modify: `apps/server/internal/api/cache_policy_test.go`
- Modify: `apps/server/internal/api/router.go`
- Modify: `apps/server/internal/resumeapi/photo.go`
- Modify: `apps/server/internal/resumeapi/adversarial_remaining_exit_test.go`
- Modify: `apps/server/internal/resumeapi/adversarial_security_exit_test.go`
- Modify: `docs/api/openapi.yaml`
- Regenerate: `apps/web/app/api/generated/openapi.ts`
- Create: `deploy/dev-https-browser/transport.spec.ts`
- Modify: `deploy/dev-https-browser/Dockerfile`
- Modify: `deploy/dev-https-browser/playwright.config.ts`
- Modify: `deploy/dev-https-browser/run.sh`
- Modify: `deploy/dev-https-browser/static-test.sh`
- Modify: root `Makefile`
- Modify: `scripts/test/makefile-safety-test.sh`
- Create: `apps/web/test/nuxt/editor-config.test.ts`
- Modify: `packages/schema/package.json`
- Modify: `apps/web/package.json`
- Modify: `apps/web/package-lock.json`
- Modify: `apps/web/nuxt.config.ts`
- Conditional owner-only correction: `deploy/caddy/Caddyfile`,
  `scripts/dev-https-test.sh`, and
  `apps/server/internal/routetable/route_table_test.go`, only if transport RED
  proves Caddy changes a no-transform response

**Interfaces:**

- `api.CacheControlNoStore` is exact `no-store, no-transform` and every
  authenticated response, including binary photo success/failure and `304`, uses
  it.
- `make dev-https-auth-check` remains unchanged and independently runnable.
  `make dev-https-transport-check` selects `transport.spec.ts` through the
  runner's closed optional `auth|transport` fourth argument; an omitted fourth
  argument remains auth mode.
- `@aboutme/schema/current-schema` exports `resume.schema.json` and
  `@aboutme/schema/validation` exports `validation/store.ts`.
- Nuxt installs `@pinia/nuxt` and configures `/app/resumes/**` with
  `{ ssr: false }`; existing `/_harness/**` behavior remains unchanged.
- Exact install commands are in `integration-handoffs.md#dependency-commands`.

- [ ] **Step 1: Write cache-policy RED tests**

Change the shared and route tests to require the exact value and prove it on a
successful resume read, a `401`, a `412`, photo `200`, photo `304`, and photo
error:

```go
const want = "no-store, no-transform"
if got := response.Header().Get("Cache-Control"); got != want {
    t.Fatalf("Cache-Control = %q, want %q", got, want)
}
```

Add an assertion that none of those owner responses has `Content-Encoding` when
the request carries `Accept-Encoding: gzip, zstd`.

- [ ] **Step 2: Run cache RED**

Run:

```sh
(cd apps/server && REQUIRE_TEST_DB=1 go test ./internal/api ./internal/resumeapi/... \
  -run 'Cache|NoStore|Photo.*Conditional' -count=1)
```

Expected: FAIL because the current constant and direct photo headers are
`no-store`.

- [ ] **Step 3: Implement exact cache policy and API contract**

Set the shared constant, replace direct photo literals with that constant, and
change OpenAPI's authenticated-response convention to the exact value. Run
`make api-gen` so only source-generated client output changes. Do not change an
endpoint, status, request body, or document schema.

```go
const CacheControlNoStore = "no-store, no-transform"

w.Header().Set("Cache-Control", api.CacheControlNoStore)
```

- [ ] **Step 4: Run server/API GREEN**

Run:

```sh
make api-check server-build server-vet server-test route-table-test
```

Expected: PASS before native transport work starts.

- [ ] **Step 5: Write native transport harness RED tests**

Extend the static harness and Make safety tests to require:

- `transport.spec.ts` is an immutable image input included in the recorded
  source hash;
- the old three-argument runner selects auth and remains byte-compatible;
- the optional fourth argument accepts only `auth` or `transport`;
- the transport target uses a new mode-0700, new-only evidence directory and
  never reads caller-supplied origin, account, cookie, token, or resume ID;
- auth and transport evidence schemas, filenames, sizes, and mode-0600 checks
  are separate; and
- the current non-root, read-only, sandbox, trusted-CA, closed-mount, host
  network, no-TLS-bypass, and zero-retry invariants still apply in both modes.

- [ ] **Step 6: Run the native transport harness tests RED**

Run:

```sh
bash deploy/dev-https-browser/static-test.sh
bash scripts/test/makefile-safety-test.sh
```

Expected: FAIL because the transport spec, runner mode, source-hash input, and
root target do not exist.

- [ ] **Step 7: Implement the trusted transport mode**

Add `transport.spec.ts`. It logs in through the fixed Google flow, then uses
same-origin page `fetch` and fresh `/me` CSRF state to:

1. create one uniquely titled blank resume and retain its ID only in page/test
   memory;
2. read that owner resume with the current schema header and verify the browser
   request carried `Accept-Encoding`;
3. require exact `Cache-Control: no-store, no-transform` and an exact strong
   parent ETag matching `^"r[1-9][0-9]*"$`;
4. patch the title with that exact observed byte string as `If-Match`, capture
   the outbound header through Playwright, and require byte equality plus a
   successful validated response; and
5. delete only that recorded ID in `finally`, using the latest accepted ETag.

If the fixed development account is already at its resume cap, fail with the
safe `resume_cap_exceeded` code. Never delete an existing or unrecorded resume
to make room; cleanup is limited to IDs this run recorded from validated `201`
responses.

The successful patch proves Go received an acceptable unchanged precondition;
the test never removes quotes or parses the revision as a number. Emit only a
bounded `transport-proof.json` with booleans for auth, cache, ETag, If-Match,
and teardown plus the existing zeroed error counters. Do not change
`auth.spec.ts` or its evidence schema.

```ts
const createdIds = new Set<string>();
const created = await ownerFetch("/api/v1/resumes", createRequest);
if (
  created.status === 422 &&
  (await errorCode(created)) === "resume_cap_exceeded"
)
  throw new Error("resume_cap_exceeded");
const accepted = await validatedAcceptedResume(created);
createdIds.add(accepted.metadata.id);
const read = await ownerFetch(
  `/api/v1/resumes/${accepted.metadata.id}`,
  readRequest,
);
const observedETag = requireStrongParentETag(read.headers.get("ETag"));
const patched = await ownerFetch(`/api/v1/resumes/${accepted.metadata.id}`, {
  ...patchRequest,
  headers: { ...patchRequest.headers, "If-Match": observedETag },
});
expect(capturedIfMatch).toBe(observedETag);
```

- [ ] **Step 8: Rerun the harness/static tests GREEN**

Run the Step 6 commands. Expected GREEN: PASS after the closed runner, source
hash, Make target, and evidence validation are implemented.

- [ ] **Step 9: Run the live auth and transport proofs GREEN**

Run with the browser target's exclusive heavy slot:

```sh
make dev-native-down
make dev-https
make dev-https-status
make dev-https-browser-image
make dev-https-auth-check
make dev-https-transport-check
make dev-https-down
```

Expected: both independent proofs pass once and ports 20440–20443 are free after
teardown. If transport fails because Caddy changes the cache policy or
ETag/precondition, correct Caddy and native route tests in this window, rebuild
the immutable browser image, and rerun both proofs before W1.

- [ ] **Step 10: Write the dependency/config RED test**

Create `apps/web/test/nuxt/editor-config.test.ts`. Import
`@aboutme/schema/current-schema`, `@aboutme/schema/validation`, and `pinia`. Use
`loadNuxtConfig` from `nuxt/kit` with `NUXT_HARNESS=1` to prove both route rules
are present in one loaded config:

```ts
expect(config.routeRules?.["/app/resumes/**"]).toEqual({ ssr: false });
expect(config.modules).toContain("@pinia/nuxt");
expect(config.routeRules?.["/_harness/**"]?.headers).toHaveProperty(
  "Content-Security-Policy",
);
```

Restore `NUXT_HARNESS` in `finally` so the config test cannot affect another
test file.

- [ ] **Step 11: Run the dependency/config test RED**

Run:

```sh
(cd apps/web && npx vitest run test/nuxt/editor-config.test.ts)
```

Expected: FAIL because the exports, Pinia dependency, and resume route rule are
absent.

- [ ] **Step 12: Add runtime schema exports and pinned dependencies**

Add these package exports:

```json
"./current-schema": "./resume.schema.json",
"./validation": {
  "types": "./validation/store.ts",
  "default": "./validation/store.ts"
}
```

Run the Task 00 web install command from
`integration-handoffs.md#dependency-commands`. Add `@pinia/nuxt` to `modules`
and make route rules an unconditional object with this entry plus a conditional
harness spread:

```ts
'/app/resumes/**': { ssr: false }
```

- [ ] **Step 13: Rerun the dependency/config test GREEN**

Run the Step 11 command. Expected GREEN: PASS.

- [ ] **Step 14: Run the final dependency/config GREEN gate and report**

Rerun every Task 00 RED-owned suite and both live proofs with no other heavy
worker active:

```sh
(cd apps/server && REQUIRE_TEST_DB=1 go test ./internal/api ./internal/resumeapi/... \
  -run 'Cache|NoStore|Photo.*Conditional' -count=1)
make api-check server-build server-vet server-test route-table-test
bash deploy/dev-https-browser/static-test.sh
bash scripts/test/makefile-safety-test.sh
(cd apps/web && npx vitest run test/nuxt/editor-config.test.ts)
make schema-check web-lint web-typecheck web-test web-build
make dev-native-down
make dev-https
make dev-https-status
make dev-https-browser-image
make dev-https-auth-check
make dev-https-transport-check
make dev-https-down
```

Expected: PASS once with exact resolved dependency versions recorded, both
proofs successful, and ports 20440–20443 free after teardown. Use the phase
report format and list any Caddy correction explicitly. Suggested commit:
`build(editor): add authenticated editor prerequisites`.
