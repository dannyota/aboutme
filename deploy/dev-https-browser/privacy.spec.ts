import {
  expect,
  test,
  type BrowserContext,
  type ConsoleMessage,
  type Download,
  type Page,
  type Route,
} from "@playwright/test";
import { createHash, randomBytes, randomUUID } from "node:crypto";
import { readFile, writeFile } from "node:fs/promises";

import { createBlankResume, freshCSRF, uniqueTitle } from "./editor-fixtures";
import {
  installExternalRequestFirewall,
  installExternalWebSocketFirewall,
  newDiagnosticCounters,
  pageDiagnosticsAttacher,
  signInWithGoogle,
  waitForHydration,
} from "./harness-lib";
import { ALLOWED_ORIGIN, httpFailureStatus } from "./network-policy";

const ORIGIN = ALLOWED_ORIGIN;
const EVIDENCE_PATH = "/evidence/privacy-proof.json";
const SCHEMA_VERSION = "2";
const ACCOUNT_LABEL = "Alice Local — alice@example.invalid";
const CLIENT_NAME_PATH = "/uat-input/mcp-client-name";
const REDIRECT_URI = "http://127.0.0.1:20090/callback";
const VALID_PNG_BASE64 =
  "iVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAIAAAD8GO2jAAAANElEQVR4nOzNsQkA" +
  "MAwDQRWGrJn9pwjZweruEWpvknuS3uZfMwAAAAAAAAAAALDVCwAA///3/wKTiM0y" +
  "DAAAAABJRU5ErkJggg==";

test.use({ acceptDownloads: true });

interface OAuthGrant {
  readonly accessToken: string;
  readonly clientID: string;
  readonly refreshToken: string;
}

interface PreparedResume {
  readonly id: string;
  readonly revision: string;
  readonly slug: string;
}

interface ValidResume {
  readonly id: string;
  readonly revision: string;
}

function stage(name: string): void {
  console.log(`privacy-stage:${name}`);
}

function object(value: unknown): Record<string, unknown> {
  expect(value).not.toBeNull();
  expect(typeof value).toBe("object");
  expect(Array.isArray(value)).toBe(false);
  return value as Record<string, unknown>;
}

async function readDownload(download: Download): Promise<Buffer> {
  const stream = await download.createReadStream();
  expect(stream).not.toBeNull();
  const chunks: Buffer[] = [];
  let size = 0;
  for await (const chunk of stream!) {
    const bytes = Buffer.from(chunk);
    size += bytes.byteLength;
    expect(size).toBeLessThanOrEqual(12_582_912);
    chunks.push(bytes);
  }
  return Buffer.concat(chunks);
}

function assertPortableExport(value: unknown, resumeID: string): void {
  const envelope = object(value);
  expect(Object.keys(envelope).sort()).toEqual(["data"]);
  const data = object(envelope.data);
  expect(Object.keys(data).sort()).toEqual([
    "account",
    "exportVersion",
    "exportedAt",
    "resumes",
  ]);
  expect(data.exportVersion).toBe(1);
  expect(Number.isNaN(Date.parse(data.exportedAt as string))).toBe(false);
  const account = object(data.account);
  expect(Object.keys(account).sort()).toEqual([
    "createdAt",
    "email",
    "id",
    "linkedProviders",
    "name",
    "updatedAt",
  ]);
  expect(account.email).toBe("alice@example.invalid");
  expect(account.linkedProviders).toEqual(["google"]);
  expect(Array.isArray(data.resumes)).toBe(true);
  const exportedResume = (data.resumes as unknown[])
    .map(object)
    .find((resume) => resume.id === resumeID);
  expect(exportedResume).toBeDefined();
  const document = object(exportedResume!.document);
  expect(document.schemaVersion).toBe(Number(SCHEMA_VERSION));
  expect(object(document.personalDetails)).not.toHaveProperty("photo");
  const photo = object(exportedResume!.photo);
  expect(photo.mediaType).toMatch(/^image\/(?:jpeg|png)$/);
  expect(typeof photo.data).toBe("string");
  expect(
    Buffer.from(photo.data as string, "base64").byteLength,
  ).toBeGreaterThan(0);

  const forbidden = new Set([
    "authorizationCode",
    "cleanup",
    "csrfToken",
    "objectKey",
    "passwordHash",
    "photoKey",
    "providerSubject",
    "sessionToken",
    "storageKey",
    "accessToken",
    "refreshToken",
  ]);
  const pending: unknown[] = [value];
  while (pending.length > 0) {
    const next = pending.pop();
    if (Array.isArray(next)) {
      pending.push(...next);
      continue;
    }
    if (typeof next !== "object" || next === null) continue;
    for (const [key, child] of Object.entries(next)) {
      expect(forbidden.has(key), `forbidden export field ${key}`).toBe(false);
      pending.push(child);
    }
  }
}

function expectedNegativeConsole(
  message: ConsoleMessage,
  deleting: boolean,
  revoking: boolean,
  revokedSlug: string | undefined,
  tombstoneResumeID: string | undefined,
): boolean {
  let url: URL;
  try {
    url = new URL(message.location().url);
  } catch {
    return false;
  }
  const status = httpFailureStatus(message.text());
  return (
    url.origin === ORIGIN &&
    ((deleting && status === 403 && url.pathname === "/api/v1/me") ||
      (status === 401 && url.pathname === "/api/v1/me") ||
      (revoking &&
        status === 401 &&
        url.pathname === "/mcp" &&
        url.search === "") ||
      (revoking &&
        status === 400 &&
        url.pathname === "/oauth/token" &&
        url.search === "") ||
      (status === 404 &&
        revokedSlug !== undefined &&
        [
          `/${revokedSlug}`,
          `/api/v1/public/resumes/${revokedSlug}`,
          `/api/v1/public/resumes/${revokedSlug}/photo`,
          `/api/v1/public/resumes/${revokedSlug}/pdf`,
          `/api/v1/public/resumes/${revokedSlug}/og.png`,
        ].includes(url.pathname)) ||
      (status === 409 &&
        tombstoneResumeID !== undefined &&
        url.pathname === `/api/v1/resumes/${tombstoneResumeID}/publish`))
  );
}

async function installGuards(
  context: BrowserContext,
  counters: ReturnType<typeof newDiagnosticCounters>,
): Promise<void> {
  await installExternalRequestFirewall(context, counters);
  await installExternalWebSocketFirewall(context, counters);
}

async function forceNextDeleteReauth(page: Page): Promise<void> {
  const pattern = `${ORIGIN}/api/v1/me`;
  const handler = async (route: Route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (
      request.method() !== "DELETE" ||
      url.origin !== ORIGIN ||
      url.pathname !== "/api/v1/me" ||
      url.search !== ""
    ) {
      await route.fallback();
      return;
    }
    await route.fulfill({
      body: JSON.stringify({
        error: {
          code: "reauth_required",
          message: "recent reauthentication is required",
        },
      }),
      contentType: "application/json",
      headers: { "Cache-Control": "no-store, no-transform" },
      status: 403,
    });
  };
  await page.route(pattern, handler, { times: 1 });
}

async function prepareResume(page: Page): Promise<ValidResume> {
  const created = await createBlankResume(page, uniqueTitle());
  await page
    .getByRole("navigation", { name: "Resume outline" })
    .getByRole("button", { name: "Personal details", exact: true })
    .press("Enter");
  await page
    .getByLabel("Full name", { exact: true })
    .fill("Privacy proof resume");
  await page.getByLabel("Full name", { exact: true }).press("Tab");
  await expect(page.locator('[data-state="saved"]')).toBeVisible();

  await page
    .getByRole("button", { name: "+ Add section", exact: true })
    .press("Enter");
  await page.getByLabel("Section type").selectOption("work");
  await page
    .getByTestId("section-create-form")
    .getByRole("button", { name: "Add section", exact: true })
    .press("Enter");
  await expect(page.locator('[data-state="saved"]')).toBeVisible();
  await page
    .getByRole("navigation", { name: "Resume outline" })
    .getByRole("button", { name: "Experience" })
    .press("Enter");
  await page.getByRole("button", { name: "Add entry" }).press("Enter");
  const entry = page.locator("[data-entry-id]").first();
  await entry.getByLabel("Job title").fill("Engineer");
  await entry.getByLabel("Employer", { exact: true }).fill("Local proof");
  await entry.getByLabel("Employer", { exact: true }).press("Tab");
  await expect(page.locator('[data-state="saved"]')).toBeVisible();

  await page.getByRole("button", { name: "Photo" }).press("Enter");
  const uploaded = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return (
      response.request().method() === "POST" &&
      url.pathname === `/api/v1/resumes/${created.metadata.id}/photo`
    );
  });
  await page.getByLabel("Upload photo").setInputFiles({
    buffer: Buffer.from(VALID_PNG_BASE64, "base64"),
    mimeType: "image/png",
    name: "privacy-proof.png",
  });
  expect((await uploaded).status()).toBe(200);
  await expect(page.locator("[data-photo-preview] img")).toBeVisible();

  const owner = await page.evaluate(async (id) => {
    const response = await fetch(`/api/v1/resumes/${id}`, {
      cache: "no-store",
      credentials: "include",
    });
    const body = (await response.json()) as { data?: { revision?: unknown } };
    return { revision: body.data?.revision, status: response.status };
  }, created.metadata.id);
  expect(owner.status).toBe(200);
  expect(owner.revision).toMatch(/^[1-9][0-9]*$/);
  return { id: created.metadata.id, revision: owner.revision as string };
}

async function preparePublishedResume(page: Page): Promise<PreparedResume> {
  const prepared = await prepareResume(page);
  const slug = `privacy-${randomUUID().slice(0, 8)}`;
  const csrf = await freshCSRF(page);
  const published = await publish(
    page,
    prepared.id,
    prepared.revision,
    slug,
    csrf,
  );
  expect(published).toBe(200);
  return { ...prepared, slug };
}

async function publish(
  page: Page,
  id: string,
  revision: string,
  slug: string,
  csrf: string,
): Promise<number> {
  return page.evaluate(
    async ({ csrf, id, revision, schemaVersion, slug }) => {
      const response = await fetch(`/api/v1/resumes/${id}/publish`, {
        body: JSON.stringify({
          downloadEnabled: true,
          live: true,
          seoGeoEnabled: true,
          slug,
        }),
        credentials: "include",
        headers: {
          "Content-Type": "application/json",
          "Idempotency-Key": crypto.randomUUID(),
          "If-Match": `"r${revision}"`,
          "X-CSRF-Token": csrf,
          "X-Resume-Schema-Version": schemaVersion,
        },
        method: "POST",
      });
      return response.status;
    },
    { csrf, id, revision, schemaVersion: SCHEMA_VERSION, slug },
  );
}

async function createOAuthGrant(
  context: BrowserContext,
  page: Page,
): Promise<OAuthGrant> {
  const clientName = (await readFile(CLIENT_NAME_PATH, "utf8")).trim();
  expect(clientName).toMatch(
    /^aboutme MCP UAT [0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
  );
  const clientID = await page.evaluate(
    async ({ clientName, redirectURI }) => {
      const response = await fetch("/oauth/register", {
        body: JSON.stringify({
          client_name: clientName,
          redirect_uris: [redirectURI],
          token_endpoint_auth_method: "none",
        }),
        headers: { "Content-Type": "application/json" },
        method: "POST",
      });
      const body = (await response.json()) as { client_id?: unknown };
      if (response.status !== 201 || typeof body.client_id !== "string") {
        throw new Error("OAuth registration failed");
      }
      return body.client_id;
    },
    { clientName, redirectURI: REDIRECT_URI },
  );
  const verifier = randomBytes(48).toString("base64url");
  const challenge = createHash("sha256").update(verifier).digest("base64url");
  const oauthState = randomBytes(24).toString("base64url");
  const query = new URLSearchParams({
    client_id: clientID,
    code_challenge: challenge,
    code_challenge_method: "S256",
    redirect_uri: REDIRECT_URI,
    response_type: "code",
    scope: "resumes:read resumes:write",
    state: oauthState,
  });
  let callback: URL | undefined;
  await context.route(`${REDIRECT_URI}**`, async (route) => {
    callback = new URL(route.request().url());
    await route.fulfill({
      body: "Complete",
      contentType: "text/plain",
      status: 200,
    });
  });
  await page.goto(`/oauth/authorize?${query.toString()}`);
  await waitForHydration(page);
  await Promise.all([
    page.waitForURL(`${REDIRECT_URI}**`),
    page.getByRole("button", { name: "Approve" }).click(),
  ]);
  expect(callback?.searchParams.get("state")).toBe(oauthState);
  const code = callback?.searchParams.get("code");
  expect(code).toMatch(/^[A-Za-z0-9_-]+$/);
  await page.goto("/app/resumes");
  const tokens = await page.evaluate(
    async ({ clientID, code, redirectURI, verifier }) => {
      const response = await fetch("/oauth/token", {
        body: new URLSearchParams({
          client_id: clientID,
          code,
          code_verifier: verifier,
          grant_type: "authorization_code",
          redirect_uri: redirectURI,
        }),
        headers: { "Content-Type": "application/x-www-form-urlencoded" },
        method: "POST",
      });
      const body = (await response.json()) as {
        access_token?: unknown;
        refresh_token?: unknown;
      };
      if (
        response.status !== 200 ||
        typeof body.access_token !== "string" ||
        typeof body.refresh_token !== "string"
      ) {
        throw new Error("OAuth token exchange failed");
      }
      return {
        accessToken: body.access_token,
        refreshToken: body.refresh_token,
      };
    },
    { clientID, code: code!, redirectURI: REDIRECT_URI, verifier },
  );
  return { clientID, ...tokens };
}

async function confirmAccountDeletion(
  page: Page,
  expectedStatus: 204 | 403,
): Promise<void> {
  await page.getByTestId("account-delete-action").click();
  const dialog = page.getByRole("alertdialog", {
    name: "Delete your account?",
  });
  await expect(dialog).toContainText("Access ends immediately.");
  await expect(dialog).toContainText("Private-media removal targets 24 hours.");
  await expect(dialog).toContainText(
    "Backup copies expire on the 30-day schedule.",
  );
  await dialog
    .getByLabel("Type DELETE to permanently delete your account")
    .fill("DELETE");
  const deleted = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return (
      response.request().method() === "DELETE" && url.pathname === "/api/v1/me"
    );
  });
  await dialog.getByRole("button", { name: "Delete account" }).click();
  expect((await deleted).status()).toBe(expectedStatus);
}

async function reauthenticateProvider(page: Page): Promise<void> {
  const prompt = page.getByTestId("account-delete-reauth-provider");
  await expect(prompt).toContainText("then confirm deletion again");
  const started = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return (
      response.request().method() === "POST" &&
      url.origin === ORIGIN &&
      url.pathname === "/api/v1/auth/google/start" &&
      url.search === "?purpose=reauth"
    );
  });
  await Promise.all([
    page.waitForURL("**/__uat/oauth/google/authorize**"),
    prompt.getByRole("button", { name: "Continue with google" }).click(),
  ]);
  expect((await started).status()).toBe(200);
  stage("reauth-provider-authorize");
  await page.getByLabel(ACCOUNT_LABEL).check();
  await expect(page.getByLabel(ACCOUNT_LABEL)).toBeChecked();
  stage("reauth-provider-selected");
  await Promise.all([
    page.waitForURL(
      (url) =>
        url.origin === ORIGIN &&
        url.pathname === "/app/settings/sessions" &&
        url.search === "",
    ),
    page.getByRole("button", { name: "Continue with Google" }).click(),
  ]);
  stage("reauth-provider-callback");
  await waitForHydration(page);
}

async function deleteThroughReauth(page: Page): Promise<void> {
  await page.goto("/app/settings/sessions");
  await waitForHydration(page);
  await forceNextDeleteReauth(page);
  await confirmAccountDeletion(page, 403);
  await reauthenticateProvider(page);
  await expect(page.getByRole("alertdialog")).toHaveCount(0);
  await Promise.all([
    page.waitForURL(
      (url) => url.origin === ORIGIN && url.pathname === "/login",
    ),
    confirmAccountDeletion(page, 204),
  ]);
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
}

async function publicReads(
  page: Page,
  slug: string,
): Promise<Record<string, number | string>> {
  return page.evaluate(async (value) => {
    const paths = {
      html: `/${value}`,
      json: `/api/v1/public/resumes/${value}`,
      photo: `/api/v1/public/resumes/${value}/photo`,
      pdf: `/api/v1/public/resumes/${value}/pdf`,
      share: `/api/v1/public/resumes/${value}/og.png`,
    };
    const result: Record<string, number | string> = {};
    for (const [name, path] of Object.entries(paths)) {
      result[name] = (await fetch(path, { cache: "no-store" })).status;
    }
    const sitemap = await fetch("/sitemap.xml", { cache: "no-store" });
    result.discovery = await sitemap.text();
    result.discoveryStatus = sitemap.status;
    return result;
  }, slug);
}

test("proves account export, reauthentication, and deletion", async ({
  browser,
  context,
  page,
}) => {
  const counters = newDiagnosticCounters();
  let deleting = false;
  let revoking = false;
  let revokedSlug: string | undefined;
  let tombstoneResumeID: string | undefined;
  const attach = pageDiagnosticsAttacher(counters, {
    countConsoleError: (message) =>
      !expectedNegativeConsole(
        message,
        deleting,
        revoking,
        revokedSlug,
        tombstoneResumeID,
      ),
  });
  attach(page);
  context.on("page", attach);
  await installGuards(context, counters);
  let publicContext: BrowserContext | undefined;
  let accountExists = false;
  const steps = {
    auth: false,
    export: false,
    cancel: false,
    reauth: false,
    explicitConfirmation: false,
    deletion: false,
    sessionRevoked: false,
    grantRevoked: false,
    publicRevoked: false,
    tombstone: false,
    cleanup: false,
  };

  try {
    stage("auth");
    accountExists = true;
    await signInWithGoogle(page, {
      accountLabel: ACCOUNT_LABEL,
      fromLoginPage: true,
      keyboard: true,
    });
    steps.auth = true;
    const prepared = await preparePublishedResume(page);
    const grant = await createOAuthGrant(context, page);
    const oldSession = (await context.cookies()).find(
      (cookie) => cookie.name === "__Host-session",
    );
    expect(oldSession).toBeDefined();

    publicContext = await browser.newContext();
    await installGuards(publicContext, counters);
    const publicPage = await publicContext.newPage();
    attach(publicPage);
    await publicPage.goto(`${ORIGIN}/login`);
    await waitForHydration(publicPage);
    const live = await publicReads(publicPage, prepared.slug);
    expect(live).toMatchObject({
      discoveryStatus: 200,
      html: 200,
      json: 200,
      pdf: 200,
      photo: 200,
      share: 200,
    });
    expect(live.discovery).toContain(`/${prepared.slug}`);

    stage("export");
    await page.goto("/app/settings/sessions");
    await waitForHydration(page);
    let exportSchema: string | undefined;
    page.on("response", (response) => {
      const url = new URL(response.url());
      if (url.pathname === "/api/v1/me/export" && response.status() === 200) {
        exportSchema = response.headers()["x-resume-schema-version"];
      }
    });
    const download = page.waitForEvent("download");
    await page.getByTestId("account-export-action").click();
    const accountExport = await download;
    expect(accountExport.suggestedFilename()).toBe("aboutme-export.json");
    const bytes = await readDownload(accountExport);
    expect(exportSchema).toBe(SCHEMA_VERSION);
    assertPortableExport(JSON.parse(bytes.toString("utf8")), prepared.id);
    steps.export = true;

    stage("cancel");
    let deleteRequests = 0;
    const countDeletes = (request: {
      method(): string;
      url(): string;
    }): void => {
      if (
        request.method() === "DELETE" &&
        new URL(request.url()).pathname === "/api/v1/me"
      ) {
        deleteRequests += 1;
      }
    };
    page.on("request", countDeletes);
    await page.getByTestId("account-delete-action").click();
    const cancelled = page.getByRole("alertdialog", {
      name: "Delete your account?",
    });
    await cancelled
      .getByLabel("Type DELETE to permanently delete your account")
      .fill("DELETE");
    await cancelled.getByRole("button", { name: "Cancel" }).click();
    expect(deleteRequests).toBe(0);
    steps.cancel = true;

    stage("reauth");
    deleting = true;
    await forceNextDeleteReauth(page);
    stage("reauth-forced-rejection");
    await confirmAccountDeletion(page, 403);
    stage("reauth-provider-prompt");
    await reauthenticateProvider(page);
    stage("reauth-provider-return");
    steps.reauth = true;
    await expect(page.getByRole("alertdialog")).toHaveCount(0);
    steps.explicitConfirmation = true;

    stage("delete");
    await Promise.all([
      page.waitForURL(
        (url) => url.origin === ORIGIN && url.pathname === "/login",
      ),
      confirmAccountDeletion(page, 204),
    ]);
    steps.deletion = true;
    accountExists = false;
    deleting = false;

    stage("revocation");
    const oldContext = await browser.newContext();
    try {
      await installGuards(oldContext, counters);
      const oldPage = await oldContext.newPage();
      attach(oldPage);
      await oldPage.goto(`${ORIGIN}/login`);
      await waitForHydration(oldPage);
      await oldContext.addCookies([oldSession!]);
      revoking = true;
      const revokedAccess = await oldPage.evaluate(
        async ({ accessToken, clientID, refreshToken }) => {
          const me = await fetch("/api/v1/me", {
            cache: "no-store",
            credentials: "include",
          });
          const mcp = await fetch("/mcp", {
            body: JSON.stringify({
              id: 1,
              jsonrpc: "2.0",
              method: "tools/list",
              params: {},
            }),
            cache: "no-store",
            credentials: "omit",
            headers: {
              Accept: "application/json, text/event-stream",
              Authorization: `Bearer ${accessToken}`,
              "Content-Type": "application/json",
            },
            method: "POST",
          });
          const refresh = await fetch("/oauth/token", {
            body: new URLSearchParams({
              client_id: clientID,
              grant_type: "refresh_token",
              refresh_token: refreshToken,
            }),
            cache: "no-store",
            credentials: "omit",
            headers: { "Content-Type": "application/x-www-form-urlencoded" },
            method: "POST",
          });
          return {
            mcp: mcp.status,
            me: me.status,
            refresh: refresh.status,
          };
        },
        {
          accessToken: grant.accessToken,
          clientID: grant.clientID,
          refreshToken: grant.refreshToken,
        },
      );
      expect(revokedAccess.me).toBe(401);
      stage("revocation-session");
      steps.sessionRevoked = true;
      expect(revokedAccess.mcp).toBe(401);
      stage("revocation-token");
      expect(revokedAccess.refresh).toBe(400);
      stage("revocation-grant");
      steps.grantRevoked = true;
      revoking = false;
    } finally {
      revoking = false;
      await oldContext.close();
    }

    revokedSlug = prepared.slug;
    const revoked = await publicReads(publicPage, prepared.slug);
    expect(revoked).toMatchObject({
      discoveryStatus: 200,
      html: 404,
      json: 404,
      pdf: 404,
      photo: 404,
      share: 404,
    });
    expect(revoked.discovery).not.toContain(`/${prepared.slug}`);
    steps.publicRevoked = true;

    stage("tombstone");
    accountExists = true;
    await signInWithGoogle(page, {
      accountLabel: ACCOUNT_LABEL,
      fromLoginPage: true,
      keyboard: true,
    });
    const replacement = await prepareResume(page);
    await page.goto("/app/settings/sessions");
    await waitForHydration(page);
    deleting = true;
    await forceNextDeleteReauth(page);
    await confirmAccountDeletion(page, 403);
    await reauthenticateProvider(page);
    const csrf = await freshCSRF(page);
    tombstoneResumeID = replacement.id;
    expect(
      await publish(
        page,
        replacement.id,
        replacement.revision,
        prepared.slug,
        csrf,
      ),
    ).toBe(409);
    steps.tombstone = true;

    stage("cleanup");
    await Promise.all([
      page.waitForURL(
        (url) => url.origin === ORIGIN && url.pathname === "/login",
      ),
      confirmAccountDeletion(page, 204),
    ]);
    deleting = false;
    accountExists = false;
    steps.cleanup = true;
  } finally {
    try {
      await publicContext?.close();
    } finally {
      if (accountExists) {
        try {
          await deleteThroughReauth(page);
        } catch {
          // The final assertion reports incomplete cleanup without exposing state.
        }
      }
    }
  }

  expect(counters).toEqual({
    certificateErrors: 0,
    consoleErrors: 0,
    externalRequests: 0,
    pageErrors: 0,
  });
  expect(steps).toEqual({
    auth: true,
    export: true,
    cancel: true,
    reauth: true,
    explicitConfirmation: true,
    deletion: true,
    sessionRevoked: true,
    grantRevoked: true,
    publicRevoked: true,
    tombstone: true,
    cleanup: true,
  });
  await writeFile(
    EVIDENCE_PATH,
    `${JSON.stringify({
      errors: { certificate: 0, console: 0, externalRequest: 0, page: 0 },
      origin: ORIGIN,
      scenario: "account-privacy",
      schemaVersion: 1,
      steps,
    })}\n`,
    { flag: "wx", mode: 0o600 },
  );
});
