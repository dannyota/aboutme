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
} from "./harness-lib";
import { ALLOWED_ORIGIN, httpFailureStatus } from "./network-policy";

const ORIGIN = ALLOWED_ORIGIN;
const EVIDENCE_PATH = "/evidence/exports-proof.json";
const PDF_MAX_BYTES = 16_777_216;
const PNG_MAX_BYTES = 4_194_304;

test.use({ acceptDownloads: true });

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
      [
        `/api/v1/public/resumes/${slug}/pdf`,
        `/api/v1/public/resumes/${slug}/og.png`,
      ].includes(url.pathname))
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
    const created = await createBlankResume(page, uniqueTitle());
    createdID = created.metadata.id;

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
    const fullName = page.getByLabel("Full name", { exact: true });
    const savedPatch = page.waitForResponse((response) =>
      isSaveRequest(response.request(), createdID!),
    );
    await fullName.fill("Export proof resume");
    const ownerPDFResponse = page.waitForResponse((response) =>
      isOwnerPDFRequest(response.request(), createdID!),
    );
    const ownerDownload = page.waitForEvent("download");
    await page
      .getByRole("button", { name: "Download PDF", exact: true })
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
      'attachment; filename="resume.pdf"',
    );
    stage("owner-pdf-accepted");
    const download = await ownerDownload;
    expect(download.suggestedFilename()).toBe("resume.pdf");
    const ownerBytes = await readDownload(download);
    expectPDF(ownerBytes);
    stage("owner-download-captured");
    steps.ownerSaveFirst = true;
    steps.ownerDownload = true;

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
    await page
      .getByRole("button", { name: "+ Add section", exact: true })
      .click();
    await page.getByLabel("Section type").selectOption("work");
    await page
      .getByTestId("section-create-form")
      .getByRole("button", { name: "Add section", exact: true })
      .click();
    await expect(page.getByTestId("save-status")).toContainText("Saved");
    await page
      .getByRole("navigation", { name: "Resume outline" })
      .getByRole("button", { name: "Experience", exact: true })
      .click();
    await page.getByRole("button", { name: "Add entry", exact: true }).click();
    const entry = page.locator("[data-entry-id]");
    await expect(entry).toHaveCount(1);
    await entry.getByLabel("Job title", { exact: true }).fill("Engineer");
    await entry.getByLabel("Job title", { exact: true }).press("Tab");
    await expect(page.getByTestId("save-status")).toContainText("Saved");
    await entry.getByLabel("Employer", { exact: true }).fill("Example Corp");
    await entry.getByLabel("Employer", { exact: true }).press("Tab");
    await expect(page.getByTestId("save-status")).toContainText("Saved");

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
    expect(publicHTML.text).toContain('property="og:image"');
    expect(publicHTML.text).toContain(`/api/v1/public/resumes/${slug}/og.png`);
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
      'attachment; filename="resume.pdf"',
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
    stage("delete-accepted");
    const [revokedPDF, revokedPNG] = await Promise.all([
      fetchArtifact(publicPage, `/api/v1/public/resumes/${slug}/pdf`, {
        headers: { "If-None-Match": publicPDFETag },
      }),
      fetchArtifact(publicPage, `/api/v1/public/resumes/${slug}/og.png`, {
        headers: { "If-None-Match": publicPNGETag },
      }),
    ]);
    stage(
      revokedPDF.status === 404 && revokedPNG.status === 404
        ? "revocation-read-denied"
        : "revocation-read-unexpected",
    );
    expect(revokedPDF.status).toBe(404);
    expect(revokedPNG.status).toBe(404);
    steps.revocation = true;
    steps.cleanup = true;
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
