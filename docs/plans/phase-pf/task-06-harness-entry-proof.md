# Task 06 — Harness flag and the entry proof

**Acceptance:** AC-OPS-022; AC-AUTH-017 (harness flag-on branch).

**Depends on:** T01, T03, T04, T05.

**Owned paths (integration owner):** `scripts/dev-https.sh`,
`scripts/dev-https-test.sh`, `scripts/dev-https-check.sh`,
`deploy/dev-https-browser/run.sh`, `deploy/dev-https-browser/static-test.sh`,
`deploy/dev-https-browser/playwright.config.ts`,
`deploy/dev-https-browser/entry.spec.ts`, root `Makefile`,
`docs/runbooks/local-uat.md`.

## Interfaces

- Consumes: the seed credentials from T03; `AppChrome` link texts from T04;
  provider links on `/login` from T05 (the harness runs with the flag on);
  `harness-lib.ts` helpers `newDiagnosticCounters`, `pageDiagnosticsAttacher`,
  `installExternalRequestFirewall`, `installExternalWebSocketFirewall`,
  `isUnexpectedConsoleError`, `waitForHydration`; `ALLOWED_ORIGIN` from
  `network-policy.ts`.
- Produces: `make dev-https-entry-check`, evidence `entry-proof.json`.

## Contract

`run.sh` is an image source, so this task ends with
`make dev-https-browser-image`. The evidence file is at most 4,096 bytes and
contains `schemaVersion`, `scenario`, `origin`, four error counters, and five
step booleans. The proof creates no rows: it signs in as the seed user and signs
out. The check runs `dev-seed seed` first and never `cleanup`.

## Steps

- [ ] **Step 1: Turn the flag on in the harness and assert it**

In `scripts/dev-https.sh`, in the `server)` case of the environment-file writer,
append `PROVIDER_LOGIN_ENABLED=%q\n` to the format string and `true` as the last
argument. In `scripts/dev-https-test.sh`, after
`assert_contains "$server_env" 'MCP_ENABLED=true'`, add
`assert_contains "$server_env" 'PROVIDER_LOGIN_ENABLED=true'`.

```sh
bash scripts/dev-https-test.sh --static
```

Expected: pass.

- [ ] **Step 2: Register the new mode everywhere the harness enumerates modes**

- `deploy/dev-https-browser/run.sh`: add `entry.spec.ts` to `SPEC_SOURCES` after
  `mcp.spec.ts`; extend the mode guard to
  `auth | transport | editor | public | password-auth | mcp | entry) ;;` and its
  failure text; add to the `case $mode in` mapping:

  ```sh
  entry)
    evidence_name=entry-proof.json
    evidence_limit=4096
    proof_name='entry flow'
    spec=entry.spec.ts
    ;;
  ```

  and to the `VERIFY_EVIDENCE` node script, before the final `: null` (or
  whatever the last branch is), add:

  ```js
  : mode === 'entry' ? {
    ...common,
    scenario: 'entry-flow',
    schemaVersion: 1,
    steps: {
      landing: true,
      providerLinks: true,
      resumeList: true,
      signIn: true,
      signedInShell: true,
    },
  }
  ```

- `deploy/dev-https-browser/static-test.sh`: add `entry.spec.ts` to its spec
  list and `entry` to the fake podman's accepted mode list
  (`transport | editor | public | password-auth | mcp | entry)`), and to any
  other mode enumeration in the file.
- `deploy/dev-https-browser/playwright.config.ts`: add `'entry'` to
  `browserModes`.
- `scripts/dev-https-check.sh`: add `entry.spec.ts` to `SPEC_SOURCES`; add
  `entry) evidence_prefix=entry ;;` to the mode case and `entry` to the usage
  string; after the `mcp` seeding branch add:

  ```sh
  elif [ "$MODE" = entry ]; then
    install -d -m 0700 "$REPO/.dev/bin"
    (cd "$REPO/apps/server" &&
      go build -o "$REPO/.dev/bin/dev-seed" ./cmd/dev-seed) ||
      fail 'dev-seed build failed'
    "$REPO/.dev/bin/dev-seed" seed --database-url "$NATIVE_DSN"
  ```

- Root `Makefile`, after `dev-https-mcp-check`:

  ```make
  dev-https-entry-check: dev-https-status ## Prove the landing, sign-in, and signed-in shell over native HTTPS and retain only bounded local evidence
      @bash scripts/dev-https-check.sh entry
  ```

  The recipe line is indented with one tab.

```sh
make operational-test
```

Expected: pass (the static browser test and the Makefile safety test both see
the new mode).

- [ ] **Step 3: Write `entry.spec.ts`**

```ts
import { expect, test, type Page } from "@playwright/test";
import { writeFile } from "node:fs/promises";
import {
  installExternalRequestFirewall,
  installExternalWebSocketFirewall,
  isUnexpectedConsoleError,
  newDiagnosticCounters,
  pageDiagnosticsAttacher,
  waitForHydration,
} from "./harness-lib";
import { ALLOWED_ORIGIN } from "./network-policy";

const ORIGIN = ALLOWED_ORIGIN;
const EVIDENCE_PATH = "/evidence/entry-proof.json";

// The seed identities are frozen by docs/plans/phase-pf/design.md D7 and
// pinned by apps/server/cmd/dev-seed/seed_test.go.
const SEED_EMAIL = "dev@aboutme.invalid";
const SEED_PASSWORD = "aboutme-dev-password-1";

const steps = {
  landing: false,
  providerLinks: false,
  resumeList: false,
  signIn: false,
  signedInShell: false,
};

async function expectSignedOutShell(page: Page): Promise<void> {
  const header = page.getByRole("banner");
  await expect(header.getByRole("link", { name: "Sign in" })).toBeVisible();
  await expect(
    header.getByRole("link", { name: "Create account" }),
  ).toBeVisible();
  await expect(header.getByRole("link", { name: "Resumes" })).toHaveCount(0);
  await expect(header.getByRole("link", { name: "Settings" })).toHaveCount(0);
}

test("landing, sign-in, and the signed-in shell", async ({ browser }) => {
  const counters = newDiagnosticCounters();
  const context = await browser.newContext();
  await installExternalRequestFirewall(context, counters);
  await installExternalWebSocketFirewall(context, counters);
  const page = await context.newPage();
  pageDiagnosticsAttacher(counters, isUnexpectedConsoleError)(page);

  try {
    await page.goto(`${ORIGIN}/`);
    await waitForHydration(page);
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(
      "Build your resume. Publish it at its own link.",
    );
    const main = page.getByRole("main");
    await expect(main.getByRole("link", { name: "Sign in" })).toHaveAttribute(
      "href",
      "/login",
    );
    await expect(
      main.getByRole("link", { name: "Create account" }),
    ).toHaveAttribute("href", "/register");
    await expectSignedOutShell(page);
    steps.landing = true;

    await page.getByRole("main").getByRole("link", { name: "Sign in" }).click();
    await expect(page).toHaveURL(`${ORIGIN}/login`);
    await waitForHydration(page);
    // The harness runs with PROVIDER_LOGIN_ENABLED=true, so the capabilities
    // read must surface all three provider links.
    await expect(page.locator('a[href^="/api/v1/auth/"]')).toHaveCount(3);
    steps.providerLinks = true;

    await page.getByRole("textbox", { name: "Email" }).fill(SEED_EMAIL);
    await page
      .getByRole("textbox", { name: "Password", exact: true })
      .fill(SEED_PASSWORD);
    await page.getByRole("button", { name: "Sign in" }).click();
    await expect(page).toHaveURL(`${ORIGIN}/app/resumes`);
    steps.signIn = true;

    await expect(
      page.getByRole("heading", { level: 1, name: "Resumes" }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Create resume" }),
    ).toBeVisible();
    steps.resumeList = true;

    const header = page.getByRole("banner");
    await expect(header.getByRole("link", { name: "Resumes" })).toBeVisible();
    await expect(header.getByRole("link", { name: "Settings" })).toBeVisible();
    await expect(
      header.getByRole("link", { name: /Account settings for Dev User/ }),
    ).toBeVisible();
    await expect(header.getByRole("link", { name: "Sign in" })).toHaveCount(0);
    steps.signedInShell = true;

    // Sign out through the settings page so the proof leaves no session.
    await page.goto(`${ORIGIN}/app/settings/sessions`);
    await waitForHydration(page);
    await page
      .getByRole("button", { name: "Log out", exact: true })
      .first()
      .click();
    await expect(page).toHaveURL(`${ORIGIN}/login`);
  } finally {
    const evidence = {
      schemaVersion: 1,
      scenario: "entry-flow",
      origin: ORIGIN,
      errors: {
        certificate: counters.certificate,
        console: counters.console,
        externalRequest: counters.externalRequest,
        page: counters.page,
      },
      steps,
    };
    await writeFile(EVIDENCE_PATH, JSON.stringify(evidence), { mode: 0o600 });
    await context.close();
  }

  expect(counters.certificate).toBe(0);
  expect(counters.console).toBe(0);
  expect(counters.externalRequest).toBe(0);
  expect(counters.page).toBe(0);
});
```

Match the counter field names and the `pageDiagnosticsAttacher` and
`waitForHydration` signatures to `harness-lib.ts` exactly;
`password-auth.spec.ts` is the reference for how the other proofs wire them. If
the current-session "Log out" button has a different accessible name in
`sessions.vue`, use that name.

- [ ] **Step 4: Rebuild the browser image and run the proof**

```sh
make dev-native-down
make dev-https
make dev-https-status
make dev-https-browser-image
make dev-https-entry-check
```

Expected: `dev-https-entry-check` prints the evidence path; the file is mode
0600, at most 4,096 bytes, and every step is `true` with all counters `0`. Read
it back:

```sh
cat "$(ls -t .dev/native-https/evidence/entry.*/entry-proof.json | head -1)"
```

- [ ] **Step 5: Re-run the neighbors**

```sh
make dev-https-auth-check
make dev-https-password-check
make dev-https-mcp-check
make dev-https-down
make dev-native
```

Expected: all pass with the flag on; the native stack is restored.

- [ ] **Step 6: Document the check**

In `docs/runbooks/local-uat.md`, add `make dev-https-entry-check` to the "Native
HTTPS feature checks" command sequence after `make dev-https-mcp-check`, and one
sentence: "The entry check seeds the development account, proves the landing
page, sign-in, and signed-in shell, and signs out; it deletes nothing." The
owner adds the check to the AGENTS.md authenticated-UI row locally (never
staged).

## Adversarial checklist

- The proof asserts the absence of `Resumes` and `Settings` in the signed-out
  banner, not only the presence of Sign in.
- Evidence contains no email, password, cookie, or token; the static test's
  secret-literal scan runs over the spec.
- A failed step still writes evidence with that step `false`, so a partial run
  is visible.

## Handoff

Report the `operational-test`, image build, entry-check, and neighbor-check
outputs and the evidence JSON. Suggested commit:
`test(harness): prove the entry flow over native HTTPS`.
