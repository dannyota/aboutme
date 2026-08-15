# Task 15: Native HTTPS editor browser proof

**Owner:** Phase 4 integration owner. This is a serialized harness/root window
after all component gates pass.

**Authorities:** `editor-contract.md` Native HTTPS browser proof, native HTTPS
design/current implementation, `design.md` Dependencies, D18/D19, and ADR 0024.

**Acceptance:** AC-EDITOR-001, AC-EDITOR-002, AC-EDITOR-004, AC-EDITOR-006, and
AC-EDITOR-012 through AC-EDITOR-017. Phase exit, not this task, aggregates
AC-EDITOR-001 through AC-EDITOR-017. The keyboard section-move scenario is one
native slice; Task 10 owns the complete AC-EDITOR-009 structure matrix.

**Files:**

- Create: `deploy/dev-https-browser/editor.spec.ts`
- Create: `deploy/dev-https-browser/editor-fixtures.ts`
- Modify: `deploy/dev-https-browser/package.json`
- Modify: `deploy/dev-https-browser/package-lock.json`
- Modify: `deploy/dev-https-browser/Dockerfile`
- Modify: `deploy/dev-https-browser/playwright.config.ts`
- Modify: `deploy/dev-https-browser/run.sh`
- Modify: `deploy/dev-https-browser/static-test.sh`
- Modify: root `Makefile`
- Modify: `scripts/test/makefile-safety-test.sh`
- Modify: `deploy/dev-https-browser/network-policy.ts` only for exact expected
  forced-failure console classification

**Interfaces:** Auth and transport targets remain independently runnable. Add
`make dev-https-editor-check`. The optional runner selector accepts only `auth`,
`transport`, or `editor`; omitted remains auth. Editor mode alone selects
`editor.spec.ts` and writes `editor-proof.json`.

`editor-fixtures.ts` exports only typed test helpers, including:

```ts
export interface PersistenceCounts {
  readonly localStorage: number;
  readonly sessionStorage: number;
  readonly indexedDB: number;
  readonly history: number;
  readonly sendBeacon: number;
}
export interface BrowserPersistenceProbe {
  read(): Promise<PersistenceCounts>;
  reset(): Promise<void>;
}
export function installBrowserPersistenceProbes(
  page: Page,
): Promise<BrowserPersistenceProbe>;
export function settledVisiblePageCount(page: Page): Promise<number>;
```

- [ ] **Step 1: Write harness/static RED tests**

Require editor sources and exact axe package in immutable source hash/image;
closed runner selection; unchanged auth/transport schemas; CA-only read mount;
new empty evidence output; non-root/read-only/sandboxed Chromium; host network;
trusted NSS CA; no repo/home/socket mount or TLS bypass; zero retry; and bounded
new-only mode-0600 editor evidence.

- [ ] **Step 2: Run the harness/static tests RED**

Run:

```sh
bash deploy/dev-https-browser/static-test.sh
bash scripts/test/makefile-safety-test.sh
```

Expected RED: FAIL because editor mode/target do not exist.

- [ ] **Step 3: Add the pinned dependency and closed runner mode**

Run the exact install command in `integration-handoffs.md`. Extend image copies,
config, runner, source hash, and root target without changing Caddy 2.11.4, base
image, Chromium, trust, origin, retries, workers, artifacts, or sandbox.
Preserve old three-argument auth and Task 00 transport calls.

```ts
const mode = process.env.ABOUTME_BROWSER_MODE ?? "auth";
if (!(["auth", "transport", "editor"] as const).includes(mode as never))
  throw new Error("invalid browser mode");
const testMatch = [`${mode}.spec.ts`];
```

- [ ] **Step 4: Rerun the harness/static tests GREEN**

Run the Step 2 commands. Expected GREEN: PASS.

- [ ] **Step 5: Write the list/load/ETag/autosave scenario**

Using roles and fixed Google: login; create one unique blank resume; retain only
its ID in memory; return to list; rename; open; assert current-v2 blank preview;
require exact `Estimated pages` and a count equal to the settled visible
contiguous `data-page-index` elements while excluding `.pagination-measurement`;
while opening the editor, capture its authenticated `GET /api/v1/resumes/{id}`
response at `https://localhost:20443` through Caddy (not the Nuxt HTML response
from `/app/resumes/{id}`). Require that browser request to carry
`Accept-Encoding`, exact `no-store, no-transform`, and exact strong `"rN"`;
edit; use Chromium's monotonic input and Resource Timing clocks to require that
the mutation starts no earlier than 1000 ms after the last input event; require
exact observed ETag bytes as `If-Match`; load the same editor URL in a fresh
page in the authenticated context and prove server persistence without retained
client state; logout.

```ts
async function proveListLoadAutosave(
  page: Page,
  context: BrowserContext,
  createdIds: Set<string>,
): Promise<AcceptedResume> {
  const accepted = await createBlankResume(page, uniqueTitle());
  createdIds.add(accepted.metadata.id);
  const ownerRead = page.waitForResponse((response) => {
    const request = response.request();
    const url = new URL(response.url());
    return (
      request.method() === "GET" &&
      url.origin === "https://localhost:20443" &&
      url.pathname === `/api/v1/resumes/${accepted.metadata.id}`
    );
  });
  await page.goto(`/app/resumes/${accepted.metadata.id}`);
  const response = await ownerRead;
  expect(response.request().headers()["accept-encoding"]).toBeTruthy();
  expect(response.headers()["cache-control"]).toBe("no-store, no-transform");
  const observedETag = response.headers()["etag"];
  expect(observedETag).toMatch(/^"r[1-9][0-9]*"$/);
  await expect(
    page.getByText("Estimated pages", { exact: true }),
  ).toBeVisible();
  await expect
    .poll(async () => {
      const settledCount = await settledVisiblePageCount(page);
      const displayedCount = await page
        .getByLabel("Estimated pages")
        .textContent();
      return displayedCount === String(settledCount) ? settledCount : null;
    })
    .not.toBeNull();
  await installLastInputClock(page.getByLabel("Resume title"));
  await page.getByLabel("Resume title").fill("Changed title");
  await page.getByLabel("Resume title").press("Tab");
  const mutation = await capturedMutation(page);
  expect(await browserMonotonicDelay(mutation)).toBeGreaterThanOrEqual(1000);
  expect(mutation.headers()["if-match"]).toBe(observedETag);
  const verificationPage = await context.newPage();
  await verificationPage.goto(page.url());
  await expect(verificationPage.getByLabel("Resume title")).toHaveValue(
    "Changed title",
  );
  await verificationPage.close();
  return accepted;
}
```

- [ ] **Step 6: Compile-list the list/autosave scenario GREEN**

```sh
(cd deploy/dev-https-browser && ABOUTME_BROWSER_MODE=editor npx playwright test --list)
```

Expected: PASS with one editor scenario listed.

- [ ] **Step 7: Add the stale/conflict/template slice**

Use same-origin page fetch with fresh `/me` CSRF to create controlled winners.
Prove unrelated stale safe rebase, same-target conflict, Accept latest, valid
field Apply mine, missing-entry action, changed-membership reorder, and
changed-photo action. Apply one preset; saved only after one complete exact
helper result; undo it. Route the second child to a stable local failure and
assert partial state plus exactly three recovery actions.

```ts
async function proveConflictAndTemplate(
  page: Page,
  accepted: AcceptedResume,
): Promise<void> {
  const firstPatch = page.waitForResponse(isResumePatch);
  await mutateWinnerFromPage(page, accepted.metadata.id, "remote");
  await page.getByLabel("Headline").fill("local");
  expect((await firstPatch).status()).toBe(200);
  await forceSameTargetWinner(page, accepted.metadata.id);
  await expect(
    page.getByRole("dialog", { name: "Resolve conflict" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Accept latest" }).click();
  await page.getByRole("button", { name: "Apply template" }).first().click();
  await expect(page.getByRole("status")).toHaveText("Saved");
  await forceTemplateChildFailure(page);
  await expect(
    page
      .getByRole("dialog", { name: "Template partially applied" })
      .getByRole("button"),
  ).toHaveText(["Retry remaining", "Restore pre-apply", "Keep partial"]);
}
```

- [ ] **Step 8: Compile-list the conflict/template slice GREEN**

Run the Step 6 command. Expected: PASS.

- [ ] **Step 9: Add the photo/session/persistence slice**

Upload fixed valid bytes and prove no source preview/decode before acceptance.
Assert normalized owner read, preview, numeric crop, replacement crop clear, and
read-failure suspension with usable forms/replace/delete. Destroy session in a
second tab; retain first-tab edit; reauthenticate; resume and save.

Before editor navigation, inject counters for local/session Storage get/set/
remove/clear, IndexedDB open/delete, history push/replace, and sendBeacon. Nuxt
development bootstrap performs framework-owned storage reads and History API
normalization before hydration settles, so reset the counters only after the
editor is interactive and the full URL/query/hash baseline is recorded. Require
zero calls and the unchanged baseline before edit, after edit, after session
loss, after reauthentication, and after save. For the fresh-page persistence
read, install the same probes before navigation, wait for restored editor state,
then reset the bootstrap counters before its zero-call assertion. Never put
counter details or resume content in evidence. Component tests remain the direct
proof that editor-owned mount code does not use these persistence APIs.

```ts
async function provePhotoSessionPersistence(
  page: Page,
  context: BrowserContext,
): Promise<void> {
  const probes = await installBrowserPersistenceProbes(page);
  const before = await page.evaluate(() => ({
    href: location.href,
    search: location.search,
    hash: location.hash,
  }));
  await probes.reset();
  const expectURLUnchanged = async () => {
    expect(
      await page.evaluate(() => ({
        href: location.href,
        search: location.search,
        hash: location.hash,
      })),
    ).toEqual(before);
  };
  await expectURLUnchanged();
  await page.getByLabel("Photo").setInputFiles(validPhotoBytes);
  await expect(page.getByTestId("accepted-photo")).toBeVisible();
  await expectURLUnchanged();
  await page.getByLabel("Crop width").fill("0.75");
  await destroySessionInSecondPage(context);
  await page.getByLabel("Headline").fill("retained while signed out");
  await expectURLUnchanged();
  await expect(
    page.getByRole("button", { name: "Resume after sign-in" }),
  ).toBeVisible();
  await reauthenticateInSecondPage(context);
  await expectURLUnchanged();
  await page.getByRole("button", { name: "Resume after sign-in" }).click();
  await expectURLUnchanged();
  expect(await probes.read()).toEqual({
    localStorage: 0,
    sessionStorage: 0,
    indexedDB: 0,
    history: 0,
    sendBeacon: 0,
  });
}
```

- [ ] **Step 10: Compile-list the photo/session slice GREEN**

Run the Step 6 command. Expected: PASS.

- [ ] **Step 11: Add keyboard/axe/network/teardown**

Complete create, edit, section move, template choice, numeric crop, conflict
decision, and final list delete without pointer-only action. Run `AxeBuilder` on
list/editor; fail serious/critical findings. Block all HTTP/WebSocket origins
except `https://localhost:20443`; fail certificate, page, unexpected console,
download, service worker, external request, or sendBeacon. In `finally`,
reauthenticate if needed and delete only IDs retained by this run.

```ts
test("authenticated editor", async ({ page, context }) => {
  const createdIds = new Set<string>();
  try {
    const accepted = await proveListLoadAutosave(page, createdIds);
    await proveConflictAndTemplate(page, accepted);
    await provePhotoSessionPersistence(page, context);
    await keyboardOnlyLifecycle(page);
    const findings = await new AxeBuilder({ page }).analyze();
    expect(
      findings.violations.filter(
        ({ impact }) => impact === "serious" || impact === "critical",
      ),
    ).toEqual([]);
  } finally {
    await ensureAuthenticated(page);
    for (const id of createdIds) await deleteRecordedResume(page, id);
  }
});
```

If create returns `resume_cap_exceeded`, fail safely. Never delete a preexisting
or unrecorded resume to make room; cleanup uses only IDs recorded from this
run's validated `201` responses.

- [ ] **Step 12: Compile-list the complete scenario GREEN**

Run the Step 6 command. Expected: PASS.

- [ ] **Step 13: Write bounded verdict evidence**

Write one mode-0600 JSON file, maximum 8 KiB, with exact closed keys:

```json
{
  "schemaVersion": 1,
  "scenario": "authenticated-editor",
  "origin": "https://localhost:20443",
  "errors": { "certificate": 0, "console": 0, "externalRequest": 0, "page": 0 },
  "steps": {
    "auth": true,
    "cache": true,
    "etag": true,
    "ifMatch": true,
    "autosave": true,
    "conflict": true,
    "template": true,
    "photo": true,
    "session": true,
    "persistence": true,
    "accessibility": true,
    "teardown": true
  }
}
```

`cache` records the Caddy-routed owner `GET` with browser `Accept-Encoding` and
the exact authenticated cache value. `etag` records its exact strong ETag, and
`ifMatch` records byte equality between that observed ETag and the next
mutation's outbound header.

Reject extra keys and scan for cookie names, CSRF/idempotency values, OAuth
codes, resume content, filenames, object keys, email, and raw bodies.

- [ ] **Step 14: Run static and live GREEN gates**

With every other heavy worker idle:

```sh
bash deploy/dev-https-browser/static-test.sh
bash scripts/test/makefile-safety-test.sh
make operational-test route-table-test
make dev-native-down
make dev-https
make dev-https-status
make dev-https-browser-image
make dev-https-auth-check
make dev-https-transport-check
make dev-https-editor-check
make dev-https-down
```

Expected: all three proofs pass once; ports 20440–20443 are free; shared DB
remains running.

- [ ] **Step 15: Report and suggest commit**

Report image ID, source hash, evidence path, scenario count, cleanup result, and
unrun commands. Suggested commit:
`test(editor): add trusted HTTPS editor proof`.
