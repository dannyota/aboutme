import {
  expect,
  test,
  type Download,
  type Page,
  type Request,
} from "@playwright/test";
import { writeFile } from "node:fs/promises";

import {
  createBlankResume,
  deleteRecordedResume,
  loginAsDevelopmentUser,
  uniqueTitle,
} from "./editor-fixtures";
import {
  installExternalRequestFirewall,
  installExternalWebSocketFirewall,
  newDiagnosticCounters,
  pageDiagnosticsAttacher,
  pinEnglish,
} from "./harness-lib";
import { ALLOWED_ORIGIN, httpFailureStatus } from "./network-policy";

const ORIGIN = ALLOWED_ORIGIN;
const EVIDENCE_PATH = "/evidence/exports-proof.json";
const FULL_NAME = "Export proof resume";
const PDF_NAME = "Export-proof-resume-Resume.pdf";
const PDF_MAX_BYTES = 16_777_216;
const PNG_MAX_BYTES = 4_194_304;
const recordedResumeIDs = new Set<string>();

test.use({ acceptDownloads: true });

test.afterEach(async ({ browser }) => {
  if (recordedResumeIDs.size === 0) return;
  const context = await browser.newContext();
  const counters = newDiagnosticCounters();
  try {
    await installExternalRequestFirewall(context, counters);
    await installExternalWebSocketFirewall(context, counters);
    const page = await context.newPage();
    await loginAsDevelopmentUser(page);
    for (const id of recordedResumeIDs) await deleteRecordedResume(page, id);
    recordedResumeIDs.clear();
  } catch {
    stage("cleanup-hook-failed");
    throw new Error("exports cleanup hook failed");
  } finally {
    await context.close();
  }
});

interface FetchedArtifact {
  readonly body: number[];
  readonly headers: Record<string, string>;
  readonly status: number;
}

interface FetchedText {
  readonly headers: Record<string, string>;
  readonly status: number;
  readonly text: string;
}

function stage(name: string): void {
  console.log(`exports-stage:${name}`);
}

async function fetchArtifact(
  page: Page,
  path: string,
  init: RequestInit = {},
): Promise<FetchedArtifact> {
  return page.evaluate(
    async ({ init, path }) => {
      const response = await fetch(path, {
        cache: "no-store",
        credentials: "omit",
        ...init,
      });
      return {
        body: Array.from(new Uint8Array(await response.arrayBuffer())),
        headers: Object.fromEntries(response.headers.entries()),
        status: response.status,
      };
    },
    { init, path },
  );
}

async function fetchText(page: Page, path: string): Promise<FetchedText> {
  return page.evaluate(async (requestPath) => {
    const response = await fetch(requestPath, {
      cache: "no-store",
      credentials: "omit",
    });
    return {
      headers: Object.fromEntries(response.headers.entries()),
      status: response.status,
      text: await response.text(),
    };
  }, path);
}

async function readDownload(download: Download): Promise<Buffer> {
  const stream = await download.createReadStream();
  expect(stream).not.toBeNull();
  const chunks: Buffer[] = [];
  for await (const chunk of stream!) chunks.push(Buffer.from(chunk));
  return Buffer.concat(chunks);
}

function expectPDF(bytes: Uint8Array): void {
  expect(bytes.byteLength).toBeGreaterThan(5);
  expect(bytes.byteLength).toBeLessThanOrEqual(PDF_MAX_BYTES);
  expect(Buffer.from(bytes.subarray(0, 5)).toString("ascii")).toBe("%PDF-");
}

// The PDF Title is the print page title, and its dates are the saved
// revision's time, never the 1970 epoch (ADR 0045).
function expectRevisionMetadata(bytes: Uint8Array): void {
  const text = Buffer.from(bytes).toString("latin1");
  stage("owner-metadata-title");
  expect(text).toContain(`/Title (${FULL_NAME} - Resume)`);
  stage("owner-metadata-date");
  expect(text).toMatch(/\/CreationDate \(D:20\d{12}\+00'00'\)/u);
  stage("owner-metadata-epoch");
  expect(text).not.toContain("D:19700101000000");
}

// The stored preview card's byte cap (docs/design/link-previews.md, "Preview
// card"), stricter than PNG_MAX_BYTES's general download sanity cap.
const CARD_MAX_BYTES = 524_288;

// Reads one head meta element's content by its naming attribute, matching
// public.spec.ts's headMetaTags pattern.
function extractMetaContent(
  html: string,
  attribute: "name" | "property",
  key: string,
): string {
  const pattern = new RegExp(
    `<meta\\s+${attribute}="${key}"\\s+content="([^"]*)"\\s*\\/?>`,
    "u",
  );
  const match = pattern.exec(html);
  expect(match, `missing meta ${attribute}=${key}`).not.toBeNull();
  return match![1]!;
}

function expectPNG(bytes: Uint8Array): void {
  expect(bytes.byteLength).toBeGreaterThan(24);
  expect(bytes.byteLength).toBeLessThanOrEqual(PNG_MAX_BYTES);
  expect([...bytes.subarray(0, 8)]).toEqual([137, 80, 78, 71, 13, 10, 26, 10]);
  expect(Buffer.from(bytes.subarray(12, 16)).toString("ascii")).toBe("IHDR");
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  expect(view.getUint32(16)).toBe(1200);
  expect(view.getUint32(20)).toBe(630);
}

function isSaveRequest(request: Request, resumeID: string): boolean {
  const url = new URL(request.url());
  return (
    request.method() === "PATCH" &&
    url.origin === ORIGIN &&
    url.pathname === `/api/v1/resumes/${resumeID}/personal-details`
  );
}

function isOwnerPDFRequest(request: Request, resumeID: string): boolean {
  const url = new URL(request.url());
  return (
    request.method() === "GET" &&
    url.origin === ORIGIN &&
    url.pathname === `/api/v1/resumes/${resumeID}/pdf`
  );
}

function isExpectedExportConsole(
  message: string,
  value: string,
  expectedFailure: "owner-denial" | "download-disabled" | "revoked" | null,
  resumeID: string | undefined,
  slug: string | undefined,
): boolean {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return false;
  }
  if (url.origin !== ORIGIN || url.search !== "") return false;
  const status = httpFailureStatus(message);
  return (
    (expectedFailure === "owner-denial" &&
      status === 401 &&
      resumeID !== undefined &&
      url.pathname === `/api/v1/resumes/${resumeID}/pdf`) ||
    (expectedFailure === "download-disabled" &&
      status === 404 &&
      slug !== undefined &&
      url.pathname === `/api/v1/public/resumes/${slug}/pdf`) ||
    (expectedFailure === "revoked" &&
      status === 404 &&
      slug !== undefined &&
      ([
        `/api/v1/public/resumes/${slug}/pdf`,
        `/api/v1/public/resumes/${slug}/og.png`,
      ].includes(url.pathname) ||
        new RegExp(
          `^/api/v1/public/resumes/${slug}/og/[0-9a-f]{16}\\.png$`,
        ).test(url.pathname)))
  );
}

function isAnonymousMeConsole(message: string, value: string): boolean {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return false;
  }
  return (
    httpFailureStatus(message) === 401 &&
    url.origin === ORIGIN &&
    url.pathname === "/api/v1/me" &&
    url.search === ""
  );
}

test("proves owner and public export gates through native HTTPS", async ({
  browser,
  context,
  page,
}) => {
  const counters = newDiagnosticCounters();
  let createdID: string | undefined;
  let slug: string | undefined;
  let expectedFailure: "owner-denial" | "download-disabled" | "revoked" | null =
    null;
  let loginMeReadExpected = false;
  await pinEnglish(context);
  await installExternalRequestFirewall(context, counters);
  await installExternalWebSocketFirewall(context, counters);
  pageDiagnosticsAttacher(counters, {
    countConsoleError: (message) =>
      !isExpectedExportConsole(
        message.text(),
        message.location().url,
        expectedFailure,
        createdID,
        slug,
      ) &&
      !(
        loginMeReadExpected &&
        isAnonymousMeConsole(message.text(), message.location().url)
      ),
  })(page);

  let publicContext: Awaited<ReturnType<typeof browser.newContext>> | undefined;
  const steps = {
    auth: false,
    ownerSaveFirst: false,
    ownerDownload: false,
    ownerPrivacy: false,
    publicPDF: false,
    shareImage: false,
    conditional: false,
    downloadGate: false,
    discoveryIndependent: false,
    revocation: false,
    cleanup: false,
  };

  try {
    stage("sign-in");
    loginMeReadExpected = true;
    await loginAsDevelopmentUser(page);
    loginMeReadExpected = false;
    steps.auth = true;
    const created = await createBlankResume(
      page,
      uniqueTitle(),
      undefined,
      (accepted) => {
        createdID = accepted.metadata.id;
        recordedResumeIDs.add(createdID);
      },
    );
    createdID = created.metadata.id;
    await page.getByTestId("workspace-locale-vi").press("Enter");
    await expect(page.locator("html")).toHaveAttribute("lang", "vi");
    await page.setViewportSize({ width: 390, height: 844 });
    stage("vietnamese-owner-export");

    const requestOrder: string[] = [];
    let acceptedSaveResponseSeen = false;
    let pdfRequestedAfterAcceptedSave: boolean | undefined;
    page.on("request", (request) => {
      if (isSaveRequest(request, createdID!)) requestOrder.push("save");
      if (isOwnerPDFRequest(request, createdID!)) {
        requestOrder.push("owner-pdf");
        pdfRequestedAfterAcceptedSave = acceptedSaveResponseSeen;
      }
    });
    page.on("response", (response) => {
      if (
        isSaveRequest(response.request(), createdID!) &&
        response.status() === 200
      ) {
        acceptedSaveResponseSeen = true;
      }
    });
    stage("owner-pending-save");
    const fullName = page.getByLabel("Họ và tên", { exact: true });
    const savedPatch = page.waitForResponse((response) =>
      isSaveRequest(response.request(), createdID!),
    );
    await fullName.fill(FULL_NAME);
    const ownerPDFResponse = page.waitForResponse((response) =>
      isOwnerPDFRequest(response.request(), createdID!),
    );
    const ownerDownload = page.waitForEvent("download");
    await page
      // The accessible name carries the page size the PDF uses.
      .getByRole("button", { name: /^Tải PDF, (?:A4|Letter)$/ })
      .click();
    const saveResponse = await savedPatch;
    expect(saveResponse.status()).toBe(200);
    stage("owner-save-accepted");
    const ownerResponse = await ownerPDFResponse;
    expect(requestOrder.indexOf("save")).toBeGreaterThanOrEqual(0);
    expect(requestOrder.indexOf("owner-pdf")).toBeGreaterThan(
      requestOrder.indexOf("save"),
    );
    expect(pdfRequestedAfterAcceptedSave).toBe(true);
    stage("owner-pdf-response");
    stage(
      ownerResponse.status() === 200
        ? "owner-pdf-success"
        : "owner-pdf-unavailable",
    );
    expect(ownerResponse.status()).toBe(200);
    expect(ownerResponse.headers()["content-type"]).toBe("application/pdf");
    expect(ownerResponse.headers()["cache-control"]).toBe(
      "no-store, no-transform",
    );
    expect(ownerResponse.headers()["content-disposition"]).toBe(
      `attachment; filename="${PDF_NAME}"; filename*=UTF-8''${PDF_NAME}`,
    );
    stage("owner-pdf-accepted");
    const download = await ownerDownload;
    stage("owner-download-ready");
    expect(download.suggestedFilename()).toBe(PDF_NAME);
    stage("owner-download-name");
    const ownerBytes = await readDownload(download);
    stage("owner-download-read");
    expectPDF(ownerBytes);
    stage("owner-download-pdf");
    expectRevisionMetadata(ownerBytes);
    stage("owner-download-captured");
    steps.ownerSaveFirst = true;
    steps.ownerDownload = true;

    await page.setViewportSize({ width: 1440, height: 900 });
    await expect(
      page.getByRole("button", { name: /^Tải PDF, (?:A4|Letter)$/ }),
    ).toBeVisible();
    stage("vietnamese-desktop-control");

    await page.getByTestId("workspace-locale-en").press("Enter");
    await expect(page.locator("html")).toHaveAttribute("lang", "en");
    await page.setViewportSize({ width: 1440, height: 900 });
    const englishOwnerPDFResponse = page.waitForResponse((response) =>
      isOwnerPDFRequest(response.request(), createdID!),
    );
    const englishOwnerDownload = page.waitForEvent("download");
    await page
      .getByRole("button", { name: /^Download PDF, (?:A4|Letter)$/ })
      .click();
    expect((await englishOwnerPDFResponse).status()).toBe(200);
    const englishOwnerBytes = await readDownload(await englishOwnerDownload);
    expectPDF(englishOwnerBytes);
    expect(englishOwnerBytes).toEqual(ownerBytes);
    stage("english-owner-export-same-bytes");
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(
      page.getByRole("button", { name: /^Download PDF, (?:A4|Letter)$/ }),
    ).toBeVisible();
    stage("english-phone-control");

    stage("anonymous-owner-denial");
    publicContext = await browser.newContext({ acceptDownloads: true });
    await installExternalRequestFirewall(publicContext, counters);
    await installExternalWebSocketFirewall(publicContext, counters);
    const publicPage = await publicContext.newPage();
    pageDiagnosticsAttacher(counters, {
      countConsoleError: (message) =>
        !isExpectedExportConsole(
          message.text(),
          message.location().url,
          expectedFailure,
          createdID,
          slug,
        ) && !isAnonymousMeConsole(message.text(), message.location().url),
    })(publicPage);
    const anonymousOrigin = await publicPage.goto(`${ORIGIN}/`);
    expect(anonymousOrigin?.status()).toBe(200);
    expectedFailure = "owner-denial";
    const ownerDenied = await fetchArtifact(
      publicPage,
      `/api/v1/resumes/${createdID}/pdf`,
    );
    expect(ownerDenied.status).toBe(401);
    expect(ownerDenied.headers["cache-control"]).toBe("no-store, no-transform");
    steps.ownerPrivacy = true;
    expectedFailure = null;

    stage("complete-resume");
    await page.setViewportSize({ width: 1440, height: 900 });
    stage("complete-open-structure");
    await page.locator('[data-action="open-structure"]').click();
    stage("complete-section-type");
    await page.locator('[data-action="section-type"]').selectOption("work");
    await page
      .getByTestId("section-create-form")
      .locator('[data-action="create"]')
      .click();
    stage("complete-section-created");
    await expect(page.getByTestId("save-status")).toContainText("Saved");
    stage("complete-section-saved");
    await page.locator('[data-outline-key="work"]').click();
    stage("complete-work-open");
    await page.locator('[data-action="add-entry"]').click();
    stage("complete-entry-added");
    const entry = page.locator("[data-entry-id]");
    await expect(entry).toHaveCount(1);
    stage("complete-entry-visible");
    await entry.getByLabel("Job title", { exact: true }).fill("Engineer");
    await entry.getByLabel("Job title", { exact: true }).press("Tab");
    await expect(page.getByTestId("save-status")).toContainText("Saved");
    stage("complete-title-saved");
    await entry.getByLabel("Employer", { exact: true }).fill("Example Corp");
    await entry.getByLabel("Employer", { exact: true }).press("Tab");
    await expect(page.getByTestId("save-status")).toContainText("Saved");
    stage("complete-employer-saved");

    stage("publish");
    slug = `exports-${crypto.randomUUID().slice(0, 8)}`;
    await page.getByRole("button", { name: "Publish", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "Publish resume" });
    await expect(dialog).toBeVisible();
    await dialog.getByLabel("Slug", { exact: true }).fill(slug);
    await dialog.getByLabel("Public resume", { exact: true }).check();
    await dialog.getByLabel("PDF download", { exact: true }).check();
    const publishResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return (
        response.request().method() === "POST" &&
        url.origin === ORIGIN &&
        url.pathname === `/api/v1/resumes/${createdID}/publish`
      );
    });
    await dialog.getByRole("button", { name: "Publish", exact: true }).click();
    expect((await publishResponse).status()).toBe(200);
    await expect(dialog.getByRole("status")).toHaveText(
      "Published successfully.",
    );

    stage("public-artifacts");
    const publicHTML = await fetchText(publicPage, `/${slug}`);
    expect(publicHTML.status).toBe(200);
    expect(publicHTML.headers["x-robots-tag"]).toBe("noindex, noarchive");
    // The stored card's versioned URL (docs/design/link-previews.md, "Build,
    // storage, and serving"); og:image and twitter:image must agree on it.
    const ogImageURL = extractMetaContent(publicHTML.text, "property", "og:image");
    expect(extractMetaContent(publicHTML.text, "name", "twitter:image")).toBe(
      ogImageURL,
    );
    const ogImagePath = new URL(ogImageURL).pathname;
    expect(ogImagePath).toMatch(
      new RegExp(`^/api/v1/public/resumes/${slug}/og/[0-9a-f]{16}\\.png$`),
    );
    const publicPDF = await fetchArtifact(
      publicPage,
      `/api/v1/public/resumes/${slug}/pdf`,
    );
    expect(publicPDF.status).toBe(200);
    expect(publicPDF.headers["cache-control"]).toBe(
      "no-cache, must-revalidate",
    );
    expect(publicPDF.headers["content-type"]).toBe("application/pdf");
    expect(publicPDF.headers["content-disposition"]).toBe(
      `attachment; filename="${PDF_NAME}"; filename*=UTF-8''${PDF_NAME}`,
    );
    const publicPDFETag = publicPDF.headers.etag;
    expect(publicPDFETag).toMatch(/^"[^\"]+"$/);
    expectPDF(Uint8Array.from(publicPDF.body));
    const publicPNG = await fetchArtifact(
      publicPage,
      `/api/v1/public/resumes/${slug}/og.png`,
    );
    expect(publicPNG.status).toBe(200);
    expect(publicPNG.headers["cache-control"]).toBe(
      "no-cache, must-revalidate",
    );
    expect(publicPNG.headers["content-type"]).toBe("image/png");
    const publicPNGETag = publicPNG.headers.etag;
    expect(publicPNGETag).toMatch(/^"[^\"]+"$/);
    expectPNG(Uint8Array.from(publicPNG.body));
    expect(publicPNG.body.length).toBeLessThanOrEqual(CARD_MAX_BYTES);

    stage("public-share-image-versioned");
    const versionedPNG = await fetchArtifact(publicPage, ogImagePath);
    expect(versionedPNG.status).toBe(200);
    expect(versionedPNG.headers["content-type"]).toBe("image/png");
    expectPNG(Uint8Array.from(versionedPNG.body));
    expect(versionedPNG.body).toEqual(publicPNG.body);
    steps.publicPDF = true;
    steps.shareImage = true;
    steps.discoveryIndependent = true;

    stage("conditional-cache");
    for (const [path, etag] of [
      [`/api/v1/public/resumes/${slug}/pdf`, publicPDFETag],
      [`/api/v1/public/resumes/${slug}/og.png`, publicPNGETag],
    ] as const) {
      const head = await fetchArtifact(publicPage, path, { method: "HEAD" });
      expect(head.status).toBe(200);
      expect(head.headers.etag).toBe(etag);
      expect(head.body).toEqual([]);
      const conditional = await fetchArtifact(publicPage, path, {
        headers: { "If-None-Match": etag },
      });
      expect(conditional.status).toBe(304);
      expect(conditional.headers.etag).toBe(etag);
      expect(conditional.body).toEqual([]);
    }
    steps.conditional = true;

    stage("disable-download");
    await dialog.getByLabel("PDF download", { exact: true }).uncheck();
    const updateResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return (
        response.request().method() === "POST" &&
        url.origin === ORIGIN &&
        url.pathname === `/api/v1/resumes/${createdID}/publish`
      );
    });
    await dialog
      .getByRole("button", { name: "Update publication", exact: true })
      .click();
    expect((await updateResponse).status()).toBe(200);
    expectedFailure = "download-disabled";
    const disabledPDF = await fetchArtifact(
      publicPage,
      `/api/v1/public/resumes/${slug}/pdf`,
      { headers: { "If-None-Match": publicPDFETag } },
    );
    expect(disabledPDF.status).toBe(404);
    const livePNG = await fetchArtifact(
      publicPage,
      `/api/v1/public/resumes/${slug}/og.png`,
      { headers: { "If-None-Match": publicPNGETag } },
    );
    expect(livePNG.status).toBe(304);
    expect(livePNG.headers.etag).toBe(publicPNGETag);
    steps.downloadGate = true;
    expectedFailure = null;

    stage("delete-revocation");
    await page.goto("/app/resumes");
    await expect(page.getByRole("heading", { name: "Resumes" })).toBeVisible();
    stage("delete-list-open");
    expectedFailure = "revoked";
    await deleteRecordedResume(page, createdID);
    createdID = undefined;
    recordedResumeIDs.delete(created.metadata.id);
    stage("delete-accepted");
    const [revokedPDF, revokedPNG, revokedVersionedPNG] = await Promise.all([
      fetchArtifact(publicPage, `/api/v1/public/resumes/${slug}/pdf`, {
        headers: { "If-None-Match": publicPDFETag },
      }),
      fetchArtifact(publicPage, `/api/v1/public/resumes/${slug}/og.png`, {
        headers: { "If-None-Match": publicPNGETag },
      }),
      fetchArtifact(publicPage, ogImagePath),
    ]);
    stage(
      revokedPDF.status === 404 &&
        revokedPNG.status === 404 &&
        revokedVersionedPNG.status === 404
        ? "revocation-read-denied"
        : "revocation-read-unexpected",
    );
    expect(revokedPDF.status).toBe(404);
    expect(revokedPNG.status).toBe(404);
    expect(revokedVersionedPNG.status).toBe(404);
    steps.revocation = true;
    steps.cleanup = true;
  } catch (error) {
    const sourceLine =
      error instanceof Error
        ? /exports\.spec\.ts:([0-9]{1,4}):/u.exec(error.stack ?? "")?.[1]
        : undefined;
    if (sourceLine !== undefined) stage(`failure-at-line-${sourceLine}`);
    throw error;
  } finally {
    await publicContext?.close();
    if (createdID !== undefined) await deleteRecordedResume(page, createdID);
  }

  const expectedCounters = {
    certificateErrors: 0,
    consoleErrors: 0,
    externalRequests: 0,
    pageErrors: 0,
  };
  stage(
    JSON.stringify(counters) === JSON.stringify(expectedCounters)
      ? "diagnostics-clean"
      : "diagnostics-unexpected",
  );
  expect(counters).toEqual(expectedCounters);
  await writeFile(
    EVIDENCE_PATH,
    `${JSON.stringify({
      errors: { certificate: 0, console: 0, externalRequest: 0, page: 0 },
      origin: ORIGIN,
      scenario: "resume-exports",
      schemaVersion: 1,
      steps,
    })}\n`,
    { flag: "wx", mode: 0o600 },
  );
});
