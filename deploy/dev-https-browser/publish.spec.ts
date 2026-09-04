import { AxeBuilder } from "@axe-core/playwright";
import {
  expect,
  test,
  type BrowserContext,
  type ConsoleMessage,
  type Page,
  type Request,
  type Response,
} from "@playwright/test";
import { writeFile } from "node:fs/promises";

import { createBlankResume, deleteRecordedResume } from "./editor-fixtures";
import {
  installExternalRequestFirewall,
  installExternalWebSocketFirewall,
  newDiagnosticCounters,
  pageDiagnosticsAttacher,
  waitForHydration,
} from "./harness-lib";
import {
  ALLOWED_ORIGIN,
  httpFailureStatus,
  isExpectedAnonymousMeConsole,
} from "./network-policy";

const ORIGIN = ALLOWED_ORIGIN;
const EVIDENCE_PATH = "/evidence/publish-proof.json";
const SCHEMA_VERSION = "2";
const SEED_EMAIL = "dev@aboutme.invalid";
const SEED_PASSWORD = "aboutme-dev-password-1";

const steps = {
  auth: false,
  complete: false,
  published: false,
  saveFirst: false,
  headers: false,
  noindex: false,
  discovery: false,
  unpublish: false,
  revocation: false,
  cleanup: false,
  signOut: false,
  accessibility: false,
  keyboard: false,
  longInvalidLayout: false,
};

const statuses = {
  publish: 0,
  publicPrivate: 0,
  publicDiscoverable: 0,
  unpublish: 0,
  revoked: 0,
};
const headerPresence = {
  publishContentType: false,
  publishCSRF: false,
  publishIfMatch: false,
  publishIdempotency: false,
  publishSchema: false,
  unpublishContentType: false,
  unpublishCSRF: false,
  unpublishIfMatch: false,
  unpublishIdempotency: false,
  unpublishSchema: false,
};

function mutationHeaderPresence(request: Request) {
  const headers = request.headers();
  return {
    contentType: headers["content-type"] === "application/json",
    csrf:
      headers["x-csrf-token"] !== undefined && headers["x-csrf-token"] !== "",
    ifMatch: headers["if-match"] !== undefined && headers["if-match"] !== "",
    idempotency:
      headers["idempotency-key"] !== undefined &&
      headers["idempotency-key"] !== "",
    schema: headers["x-resume-schema-version"] === SCHEMA_VERSION,
  };
}

function stage(name: string): void {
  console.log(`publish-stage:${name}`);
}

function stageNavigationFailure(error: unknown): void {
  const message = error instanceof Error ? error.message : "";
  if (message.includes("ERR_CERT")) stage("navigation-certificate-error");
  else if (message.includes("ERR_CONNECTION_REFUSED")) {
    stage("navigation-connection-refused");
  } else if (message.includes("Timeout")) stage("navigation-timeout");
  else if (message.includes("ERR_FAILED")) stage("navigation-failed");
  else stage("navigation-error");
}

function isExpectedRevokedPublicConsole(
  message: ConsoleMessage,
  slug: string,
): boolean {
  let location: URL;
  try {
    location = new URL(message.location().url);
  } catch {
    return false;
  }
  return (
    message.text() ===
      "Failed to load resource: the server responded with a status of 404 ()" &&
    location.origin === ORIGIN &&
    location.pathname === `/${slug}` &&
    location.search === ""
  );
}

function isExpectedSyntheticPublishInvalidConsole(
  message: ConsoleMessage,
  resumeID: string | undefined,
): boolean {
  let location: URL;
  try {
    location = new URL(message.location().url);
  } catch {
    return false;
  }
  return (
    resumeID !== undefined &&
    httpFailureStatus(message.text()) === 422 &&
    location.origin === ORIGIN &&
    location.pathname === `/api/v1/resumes/${resumeID}/publish` &&
    location.search === ""
  );
}

async function installPublicGuards(
  context: BrowserContext,
  counters: ReturnType<typeof newDiagnosticCounters>,
): Promise<void> {
  await installExternalRequestFirewall(context, counters);
  await installExternalWebSocketFirewall(context, counters);
}

async function auditDialog(page: Page): Promise<void> {
  for (const viewport of [
    { width: 1280, height: 900 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    const findings = await new AxeBuilder({ page }).analyze();
    expect(
      findings.violations.filter(
        ({ impact }) => impact === "serious" || impact === "critical",
      ),
    ).toEqual([]);
  }
}

test("proves native HTTPS publish, discovery, and revocation", async ({
  browser,
  context,
  page,
}) => {
  const counters = newDiagnosticCounters();
  const events: string[] = [];
  let resumeID: string | undefined;
  const slug = `publish-${crypto.randomUUID().slice(0, 8)}`;
  let loggedIn = false;
  let publicContext: BrowserContext | undefined;
  let revocationElapsed = 0;
  let publishMutationCount = 0;
  let syntheticInvalidRequest = false;
  let syntheticInvalidConsoleBudget = 0;
  const requestListener = (request: Request): void => {
    const url = new URL(request.url());
    if (url.origin !== ORIGIN) return;
    if (request.method() === "POST" && url.pathname.endsWith("/publish")) {
      if (syntheticInvalidRequest) return;
      events.push("publish-request");
      const present = mutationHeaderPresence(request);
      publishMutationCount += 1;
      if (publishMutationCount === 1) {
        headerPresence.publishContentType = present.contentType;
        headerPresence.publishCSRF = present.csrf;
        headerPresence.publishIfMatch = present.ifMatch;
        headerPresence.publishIdempotency = present.idempotency;
        headerPresence.publishSchema = present.schema;
      } else if (publishMutationCount === 2) {
        headerPresence.publishContentType &&= present.contentType;
        headerPresence.publishCSRF &&= present.csrf;
        headerPresence.publishIfMatch &&= present.ifMatch;
        headerPresence.publishIdempotency &&= present.idempotency;
        headerPresence.publishSchema &&= present.schema;
      } else {
        headerPresence.unpublishContentType = present.contentType;
        headerPresence.unpublishCSRF = present.csrf;
        headerPresence.unpublishIfMatch = present.ifMatch;
        headerPresence.unpublishIdempotency = present.idempotency;
        headerPresence.unpublishSchema = present.schema;
      }
    }
    if (
      request.method() === "PATCH" &&
      url.pathname.endsWith("/personal-details")
    ) {
      events.push("edit-request");
    }
  };
  const responseListener = (response: Response): void => {
    const request = response.request();
    const url = new URL(response.url());
    if (
      request.method() === "PATCH" &&
      url.pathname.endsWith("/personal-details") &&
      response.status() === 200
    ) {
      events.push("edit-accepted");
    }
  };
  await installPublicGuards(context, counters);
  await context.grantPermissions(["clipboard-read", "clipboard-write"], {
    origin: ORIGIN,
  });
  await page.emulateMedia({ reducedMotion: "no-preference" });
  pageDiagnosticsAttacher(counters, {
    countConsoleError: (message) => {
      if (
        syntheticInvalidConsoleBudget > 0 &&
        isExpectedSyntheticPublishInvalidConsole(message, resumeID)
      ) {
        syntheticInvalidConsoleBudget -= 1;
        return false;
      }
      return !isExpectedAnonymousMeConsole(
        message.text(),
        message.location().url,
      );
    },
  })(page);
  page.on("request", requestListener);
  page.on("response", responseListener);

  try {
    stage("open-login");
    try {
      await page.goto("/login");
    } catch (error) {
      stageNavigationFailure(error);
      throw error;
    }
    stage("login-response");
    await waitForHydration(page);
    await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
    stage("login-form");
    await page.getByRole("textbox", { name: "Email" }).fill(SEED_EMAIL);
    await page
      .getByRole("textbox", { name: "Password", exact: true })
      .fill(SEED_PASSWORD);
    stage("credentials-filled");
    await page.getByRole("button", { name: "Sign in" }).click();
    stage("sign-in-submitted");
    await page.waitForURL(
      (url) => url.origin === ORIGIN && url.pathname === "/app/resumes",
    );
    loggedIn = true;
    steps.auth = true;
    stage("signed-in");

    const created = await createBlankResume(
      page,
      `Publish proof ${crypto.randomUUID()}`,
    );
    resumeID = created.metadata.id;
    stage("resume-created");
    await page
      .getByRole("navigation", { name: "Resume outline" })
      .getByRole("button", { name: "Personal details", exact: true })
      .press("Enter");
    await page.getByLabel("Full name").fill("Publish proof resume");
    await page.getByLabel("Full name").press("Tab");
    await expect(page.getByTestId("save-status")).toContainText("Saved");
    await page.getByRole("button", { name: "Structure" }).press("Enter");
    await page.getByLabel("Section type").selectOption("work");
    await page.locator('[data-action="create"]').press("Enter");
    await expect(page.getByTestId("save-status")).toContainText("Saved");
    await page.getByRole("button", { name: "Document" }).press("Enter");
    await page
      .getByRole("navigation", { name: "Resume outline" })
      .getByRole("button", { name: "Experience" })
      .press("Enter");
    await page.getByRole("button", { name: "Add entry" }).press("Enter");
    const entry = page.locator("[data-entry-id]");
    await expect(entry).toHaveCount(1);
    await entry.getByLabel("Job title").fill("Engineer");
    await entry.getByLabel("Job title").press("Tab");
    await entry.getByLabel("Employer", { exact: true }).fill("Example Corp");
    await entry.getByLabel("Employer", { exact: true }).press("Tab");
    await expect(page.getByTestId("save-status")).toContainText("Saved");
    steps.complete = true;
    stage("resume-complete");

    events.length = 0;
    await page
      .getByRole("navigation", { name: "Resume outline" })
      .getByRole("button", { name: "Personal details", exact: true })
      .press("Enter");
    await page.getByLabel("Headline").fill("Accepted before publication");
    await expect(page.getByTestId("public-mark")).toHaveCount(0);
    await expect(page.getByTestId("preview-stamp")).toHaveCount(0);
    await page.locator('[data-action="publish"]').press("Enter");
    const dialog = page.getByRole("dialog", { name: "Publish resume" });
    await expect(dialog).toBeVisible();
    await expect(page.getByLabel("Slug")).toBeFocused();
    steps.keyboard = true;
    stage("dialog-open");
    await auditDialog(page);
    steps.accessibility = true;
    stage("dialog-audited");
    await page.setViewportSize({ width: 1280, height: 900 });
    stage("dialog-sized");
    await page.getByLabel("Slug").fill(slug);
    const live = page.getByLabel("Public resume");
    const download = page.getByLabel("PDF download");
    const seo = page.getByLabel("SEO and GEO");
    await expect(live).not.toBeChecked();
    await expect(download).not.toBeChecked();
    await expect(seo).not.toBeChecked();
    stage("dialog-defaults");
    await live.press("Space");
    await download.press("Space");
    await expect(seo).not.toBeChecked();
    stage("dialog-options");
    const publishResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return (
        response.request().method() === "POST" &&
        url.pathname === `/api/v1/resumes/${resumeID}/publish`
      );
    });
    await dialog
      .getByRole("button", { name: "Publish", exact: true })
      .press("Enter");
    stage("publish-requested");
    const acceptedPublish = await publishResponse;
    statuses.publish = acceptedPublish.status();
    expect(statuses.publish).toBe(200);
    const publicMark = page.getByTestId("public-mark");
    const previewStamp = page.getByTestId("preview-stamp");
    stage("publish-marks-landing");
    await Promise.all([
      expect(publicMark).toHaveAttribute("data-stamp", "landing"),
      expect(previewStamp).toHaveAttribute("data-stamp", "landing"),
    ]);
    await expect(dialog.getByRole("status")).toHaveText(
      "Published successfully.",
    );
    stage("publish-status");
    const editAccepted = events.indexOf("edit-accepted");
    const publishRequested = events.indexOf("publish-request");
    expect(editAccepted).toBeGreaterThanOrEqual(0);
    expect(publishRequested).toBeGreaterThan(editAccepted);
    steps.saveFirst = true;
    steps.headers = Object.values(headerPresence).slice(0, 5).every(Boolean);

    stage("publish-marks");
    await Promise.all([
      expect(publicMark).toBeVisible(),
      expect(previewStamp).toBeVisible(),
    ]);
    stage("publish-marks-visible");
    const publicMarkLink = publicMark.locator("[data-public-link]");
    await expect(publicMarkLink).toHaveCount(1);
    stage("publish-mark-link-count");
    await expect(publicMarkLink).toHaveText(`aboutme.vn/${slug}`);
    stage("publish-mark-link-text");
    await expect(publicMarkLink).toHaveAttribute("href", `/${slug}`);
    stage("publish-mark-link");
    await expect(previewStamp).toHaveAttribute(
      "aria-label",
      `Public at aboutme.vn/${slug}`,
    );
    stage("publish-mark-label");
    stage("publish-marks-idle");
    await Promise.all([
      expect(publicMark).not.toHaveAttribute("data-stamp"),
      expect(previewStamp).not.toHaveAttribute("data-stamp"),
    ]);

    const link = dialog.getByRole("link", {
      name: `aboutme.vn/${slug}`,
      exact: true,
    });
    await expect(link).toHaveAttribute("href", `/${slug}`);
    await dialog
      .getByRole("button", { name: "Copy link", exact: true })
      .press("Enter");
    await expect(
      dialog.getByRole("button", { name: "Copied", exact: true }),
    ).toBeVisible();
    await expect
      .poll(() => page.evaluate(() => navigator.clipboard.readText()))
      .toBe(`${ORIGIN}/${slug}`);
    steps.published = true;
    stage("published");
    publicContext = await browser.newContext();
    const publicCounters = counters;
    await installPublicGuards(publicContext, publicCounters);
    const publicPage = await publicContext.newPage();
    pageDiagnosticsAttacher(counters, {
      countConsoleError: (message) =>
        !isExpectedAnonymousMeConsole(message.text(), message.location().url) &&
        !isExpectedRevokedPublicConsole(message, slug),
    })(publicPage);
    const privateResponse = await publicPage.goto(`${ORIGIN}/${slug}`);
    statuses.publicPrivate = privateResponse?.status() ?? 0;
    expect(statuses.publicPrivate).toBe(200);
    expect(privateResponse?.headers()["x-robots-tag"]).toBe(
      "noindex, noarchive",
    );
    expect(await publicPage.getByRole("main").count()).toBe(1);
    await waitForHydration(publicPage, "public-resume");
    steps.noindex = true;
    stage("noindex");

    await dialog
      .getByRole("button", { name: "Cancel", exact: true })
      .press("Enter");
    stage("published-close");
    await expect(page.locator('[data-action="publish"]')).toBeFocused();
    stage("published-focus-restored");
    await page.locator('[data-action="publish"]').press("Enter");
    stage("update-reopen");
    let updateDialog = page.getByRole("dialog", { name: "Publish resume" });
    await expect(updateDialog).toBeVisible();
    stage("update-visible");

    await page.setViewportSize({ width: 390, height: 844 });
    syntheticInvalidRequest = true;
    syntheticInvalidConsoleBudget = 1;
    await page.route(
      `**/api/v1/resumes/${resumeID}/publish`,
      async (route) => {
        await route.fulfill({
          status: 422,
          contentType: "application/json",
          headers: { "cache-control": "no-store, no-transform" },
          body: JSON.stringify({
            error: {
              code: "publish_invalid",
              message: "resume cannot be published",
              details: {
                issues: Array.from({ length: 24 }, (_, index) => ({
                  path: `content.work.entries.${index}.title`,
                  code: "required",
                })),
              },
            },
          }),
        });
      },
      { times: 1 },
    );
    await updateDialog
      .getByRole("button", { name: "Update publication", exact: true })
      .press("Enter");
    const issueActions = updateDialog.locator(
      '[data-action="focus-publish-issue"]',
    );
    await expect(issueActions).toHaveCount(24);
    syntheticInvalidRequest = false;
    const dialogLayout = await updateDialog.evaluate((element) => {
      const bounds = element.getBoundingClientRect();
      return {
        bottom: bounds.bottom,
        clientHeight: element.clientHeight,
        scrollHeight: element.scrollHeight,
        top: bounds.top,
      };
    });
    expect(dialogLayout.top).toBeGreaterThanOrEqual(0);
    expect(dialogLayout.bottom).toBeLessThanOrEqual(844);
    expect(dialogLayout.scrollHeight).toBeGreaterThan(
      dialogLayout.clientHeight,
    );
    const invalidClose = updateDialog.getByRole("button", {
      name: "Cancel",
      exact: true,
    });
    await invalidClose.scrollIntoViewIfNeeded();
    await expect(invalidClose).toBeInViewport();
    await invalidClose.press("Enter");
    steps.longInvalidLayout = true;
    stage("long-invalid-layout");

    await page.setViewportSize({ width: 1280, height: 900 });
    await page.locator('[data-action="publish"]').press("Enter");
    updateDialog = page.getByRole("dialog", { name: "Publish resume" });
    await expect(updateDialog).toBeVisible();
    await updateDialog.getByLabel("SEO and GEO").press("Space");
    const discoveryResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return (
        response.request().method() === "POST" &&
        url.pathname === `/api/v1/resumes/${resumeID}/publish`
      );
    });
    await updateDialog
      .getByRole("button", { name: "Update publication", exact: true })
      .press("Enter");
    expect((await discoveryResponse).status()).toBe(200);
    const discoverableResponse = await publicPage.reload();
    statuses.publicDiscoverable = discoverableResponse?.status() ?? 0;
    expect(statuses.publicDiscoverable).toBe(200);
    expect(discoverableResponse?.headers()["x-robots-tag"] ?? "").toBe("");
    expect(await publicPage.content()).toContain(
      '<script type="application/ld+json">',
    );
    steps.discovery = true;
    stage("discovery");

    await updateDialog
      .getByRole("button", { name: "Cancel", exact: true })
      .press("Enter");
    await page.locator('[data-action="publish"]').press("Enter");
    const unpublishDialog = page.getByRole("dialog", {
      name: "Publish resume",
    });
    await expect(unpublishDialog).toBeVisible();
    await unpublishDialog.getByLabel("Public resume").press("Space");
    await page.emulateMedia({ reducedMotion: "reduce" });
    const unpublishResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return (
        response.request().method() === "POST" &&
        url.pathname === `/api/v1/resumes/${resumeID}/publish`
      );
    });
    await unpublishDialog
      .getByRole("button", { name: "Unpublish", exact: true })
      .press("Enter");
    statuses.unpublish = (await unpublishResponse).status();
    expect(statuses.unpublish).toBe(200);
    await expect(unpublishDialog.getByLabel("Slug")).toHaveValue(slug);
    await expect(publicMark).toHaveCount(0);
    await expect(previewStamp).toHaveCount(0);
    const started = Date.now();
    const revokedResponse = await publicPage.goto(`${ORIGIN}/${slug}`);
    const elapsed = Date.now() - started;
    revocationElapsed = elapsed;
    statuses.revoked = revokedResponse?.status() ?? 0;
    expect(statuses.revoked).toBe(404);
    expect(elapsed).toBeLessThanOrEqual(5_000);
    steps.unpublish = true;
    steps.revocation = true;
    steps.headers = Object.values(headerPresence).every(Boolean);
    stage("revoked");
    await publicContext.close();
    publicContext = undefined;
    await unpublishDialog
      .getByRole("button", { name: "Cancel", exact: true })
      .press("Enter");
    await deleteRecordedResume(page, resumeID);
    steps.cleanup = true;
    stage("cleanup");
    await page.goto(`${ORIGIN}/app/settings/sessions`);
    await page
      .getByTestId("settings-page")
      .getByRole("button", { name: "Log out", exact: true })
      .press("Enter");
    await expect(page).toHaveURL(`${ORIGIN}/login`);
    steps.signOut = true;
    loggedIn = false;
    stage("signed-out");
  } finally {
    if (publicContext !== undefined) await publicContext.close();
    if (resumeID !== undefined && loggedIn && !steps.cleanup) {
      await deleteRecordedResume(page, resumeID);
    }
  }

  if (counters.certificateErrors !== 0) stage("certificate-errors");
  if (counters.consoleErrors !== 0) stage("console-errors");
  if (counters.externalRequests !== 0) stage("external-requests");
  if (counters.pageErrors !== 0) stage("page-errors");
  for (const [name, present] of [
    ["publish-content-type", headerPresence.publishContentType],
    ["publish-csrf", headerPresence.publishCSRF],
    ["publish-if-match", headerPresence.publishIfMatch],
    ["publish-idempotency", headerPresence.publishIdempotency],
    ["publish-schema", headerPresence.publishSchema],
    ["unpublish-content-type", headerPresence.unpublishContentType],
    ["unpublish-csrf", headerPresence.unpublishCSRF],
    ["unpublish-if-match", headerPresence.unpublishIfMatch],
    ["unpublish-idempotency", headerPresence.unpublishIdempotency],
    ["unpublish-schema", headerPresence.unpublishSchema],
  ] as const) {
    if (!present) stage(`missing-${name}`);
  }
  expect(publishMutationCount).toBe(3);
  expect(counters.certificateErrors).toBe(0);
  expect(counters.consoleErrors).toBe(0);
  expect(counters.externalRequests).toBe(0);
  expect(counters.pageErrors).toBe(0);
  expect(steps).toEqual(
    Object.fromEntries(Object.keys(steps).map((key) => [key, true])),
  );
  await writeFile(
    EVIDENCE_PATH,
    `${JSON.stringify({
      errors: {
        certificate: counters.certificateErrors,
        console: counters.consoleErrors,
        externalRequest: counters.externalRequests,
        page: counters.pageErrors,
      },
      origin: ORIGIN,
      scenario: "native-https-publish",
      schemaVersion: 1,
      steps,
      statuses,
      elapsedMs: { revocation: revocationElapsed },
      headers: headerPresence,
    })}\n`,
    { flag: "wx", mode: 0o600 },
  );
});
