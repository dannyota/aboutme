import { AxeBuilder } from "@axe-core/playwright";
import {
  expect,
  test,
  type Browser,
  type BrowserContext,
  type ConsoleMessage,
  type Locator,
  type Page,
  type Request,
  type Response,
  type Route,
} from "@playwright/test";
import { writeFile } from "node:fs/promises";

import {
  createBlankResume,
  deleteRecordedResume,
  freshCSRF,
  readRemoteResumeRevision,
} from "./editor-fixtures";
import {
  installExternalRequestFirewall,
  installExternalWebSocketFirewall,
  newDiagnosticCounters,
  pageDiagnosticsAttacher,
  signInWithGoogle,
  waitForHydration,
  pinEnglish,
  WAIT_WARM_MS,
} from "./harness-lib";
import {
  ALLOWED_ORIGIN,
  httpFailureStatus,
  isExpectedAnonymousMeConsole,
} from "./network-policy";
import {
  entryPointFacts,
  fetchPDF,
  isPDF,
  markPublicRoot,
  pdfLinkURIs,
  serverEntryPoints,
  sortedTitles,
  withoutField,
  type EntryPointFacts,
} from "./entry-points";
import {
  deleteRecordedProject,
  gateFacts,
  gateProviderLink,
  hasViewPass,
  isExpectedNotFoundConsole,
  openPublic,
  ownerCreateProject,
  ownerLinkProject,
  ownerRenameProject,
  ownerSetEntryHidden,
  ownerSetPublishState,
  ownerWriteEntry,
  projectDocument,
  projectPageFacts,
  readNotFoundShape,
  type NotFoundShape,
} from "./project-pages";

const ORIGIN = ALLOWED_ORIGIN;
const EVIDENCE_PATH = "/evidence/publish-proof.json";
const SCHEMA_VERSION = "6";
const SEED_EMAIL = "dev@aboutme.invalid";
const SEED_PASSWORD = "aboutme-dev-password-1";
const REVOKED_PUBLIC_STATUSES: readonly number[] = [401, 404];
const REVOCATION_BOUND_MS = 30_000;

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
  publicRealtime: false,
  ownerRealtime: false,
  scroll: false,
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

let currentStage = "setup";
let failedStage: string | undefined;
let countedConsoleFailure: string | undefined;
let createdID: string | undefined;
let createdProjectIDs: string[] = [];
let writePublishEvidence: (() => Promise<void>) | undefined;

function stage(name: string): void {
  currentStage = name;
  console.log(`publish-stage:${name}`);
}

test.beforeEach(() => {
  currentStage = "setup";
  failedStage = undefined;
  countedConsoleFailure = undefined;
  writePublishEvidence = undefined;
});

test.afterEach(async ({ browser }, info) => {
  if (info.status !== info.expectedStatus) failedStage ??= currentStage;
  if (createdID === undefined) return;
  info.setTimeout(info.timeout + 30_000);
  let cleaning = true;
  try {
    for (const context of browser.contexts()) await context.close();
    await deleteOutsideCounters(browser, createdID, createdProjectIDs);
    createdID = undefined;
    createdProjectIDs = [];
    cleaning = false;
    if (info.title === "proves native HTTPS publish, discovery, and revocation") {
      steps.cleanup = true;
      stage("cleanup");
      if (info.status === info.expectedStatus) {
        if (writePublishEvidence === undefined) {
          throw new Error("publish evidence is missing");
        }
        await writePublishEvidence();
      }
    }
    if (info.title === PROJECT_PAGES_TEST_TITLE) {
      projectSteps.cleanup = true;
      stage("projects-cleanup");
    }
    if (info.title === ENTRY_POINTS_TEST_TITLE) {
      entryPointSteps.cleanup = true;
      stage("entry-points-cleanup");
    }
  } catch (error) {
    if (info.status === info.expectedStatus) {
      failedStage = cleaning ? "cleanup-failed" : currentStage;
    }
    throw error;
  }
});

test.afterEach(async ({}, info) => {
  if (info.status === info.expectedStatus) return;
  const messages = info.errors.map((error) => error.message ?? "").join("\n");
  const failureStage = failedStage ?? currentStage;
  const reason =
    info.status === "timedOut" ||
    /timed out|timeout of \d+ms exceeded|timeout \d+ms exceeded/iu.test(messages)
      ? "timeout"
      : /expect\(/u.test(messages)
        ? failureStage.endsWith("-status")
          ? "wrong-status"
          : "wrong-content"
        : "error";
  const marker = `fail-${failureStage}-${reason}${countedConsoleFailure === undefined ? "" : `-${countedConsoleFailure}`}`;
  stage(marker);
  const journey =
    info.title === "proves native HTTPS publish, discovery, and revocation"
      ? "publish"
      : info.title === DELETION_TEST_TITLE
        ? "deletion"
        : info.title === PROJECT_PAGES_TEST_TITLE
          ? "projects"
          : info.title === ENTRY_POINTS_TEST_TITLE
            ? "entry-points"
            : "showcase";
  await writeFile(
    `/evidence/${journey}-diagnostic.json`,
    `${JSON.stringify({ marker })}\n`,
    { flag: "wx", mode: 0o600 },
  );
});

function stageNavigationFailure(error: unknown): void {
  const message = error instanceof Error ? error.message : "";
  if (message.includes("ERR_CERT")) stage("navigation-certificate-error");
  else if (message.includes("ERR_CONNECTION_REFUSED")) {
    stage("navigation-connection-refused");
  } else if (message.includes("Timeout")) stage("navigation-timeout");
  else if (message.includes("ERR_FAILED")) stage("navigation-failed");
  else stage("navigation-error");
}

function isPublicRouteOf(path: string, slug: string): boolean {
  const card = `/api/v1/public/resumes/${slug}/og/`;
  return (
    [
      `/${slug}`,
      `/api/v1/public/resumes/${slug}`,
      `/api/v1/live/${slug}`,
    ].includes(path) ||
    (path.startsWith(card) &&
      /^[0-9a-f]{16}\.png$/u.test(path.slice(card.length)))
  );
}

function isExpectedRevokedPublicConsole(
  message: ConsoleMessage,
  slug: string,
  statuses: readonly number[] = REVOKED_PUBLIC_STATUSES,
): boolean {
  let location: URL;
  try {
    location = new URL(message.location().url);
  } catch {
    return false;
  }
  const status = httpFailureStatus(message.text());
  return (
    status !== null &&
    statuses.includes(status) &&
    location.origin === ORIGIN &&
    isPublicRouteOf(location.pathname, slug) &&
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

function isExpectedRevokingPublicConsole(
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
    httpFailureStatus(message.text()) === 503 &&
    location.origin === ORIGIN &&
    [`/api/v1/public/resumes/${slug}`, `/api/v1/live/${slug}`].includes(
      location.pathname,
    ) &&
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

async function signInWithPassword(page: Page): Promise<void> {
  await page.goto("/login");
  await waitForHydration(page);
  await page.getByRole("textbox", { name: "Email" }).fill(SEED_EMAIL);
  await page
    .getByRole("textbox", { name: "Password", exact: true })
    .fill(SEED_PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await page.waitForURL(
    (url) => url.origin === ORIGIN && url.pathname === "/app/resumes",
  );
}

async function deleteOutsideCounters(
  browser: Browser,
  resumeID: string,
  projectIDs: readonly string[],
): Promise<void> {
  const cleanupCounters = newDiagnosticCounters();
  const cleanupContext = await browser.newContext();
  try {
    await pinEnglish(cleanupContext);
    await installPublicGuards(cleanupContext, cleanupCounters);
    const cleanupPage = await cleanupContext.newPage();
    await signInWithPassword(cleanupPage);
    await deleteRecordedResume(cleanupPage, resumeID);
    for (const projectID of projectIDs) {
      await deleteRecordedProject(cleanupPage, projectID);
    }
    expect(cleanupCounters.externalRequests).toBe(0);
  } finally {
    await cleanupContext.close();
  }
}

async function auditDialog(page: Page): Promise<void> {
  for (const viewport of [
    { width: 1440, height: 900 },
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

const PROJECT_PAGES_TEST_TITLE =
  "proves the public project pages, their gate, and their revocations";
const projectSteps = {
  linked: false,
  served: false,
  unknownUniform: false,
  hiddenUniform: false,
  gate: false,
  pass: false,
  unpublishedUniform: false,
  renamed: false,
  cleanup: false,
};
const projectCounts = {
  stylesheets: 0,
  styleAttributes: 0,
  uniformNotFound: 0,
  gatedAddresses: 0,
};
const projectErrors = {
  certificate: -1,
  console: -1,
  externalRequest: -1,
  page: -1,
};
const SERVED_PROJECT_PAGE = {
  styleSources: ["style-src 'self'"],
  unsafeInline: false,
  robotsHeader: "noindex, noarchive",
  stylesheets: ["print-fonts", "project-pages"],
  styleVersions: 1,
  loadedStylesheets: 2,
  styleAttributes: 0,
  styleElements: 0,
  scripts: 0,
  projectMains: 1,
  gates: 0,
};
const GATE_FOR_PROJECT = {
  noStore: true,
  robotsHeader: "noindex, noarchive",
  gates: 1,
  projectMains: 0,
  providers: 2,
  providersWithPage: 2,
  revealsName: false,
};

test(PROJECT_PAGES_TEST_TITLE, async ({ browser, context, page }) => {
  test.setTimeout(180_000);
  stage("projects-setup");
  await pinEnglish(context);
  const counters = newDiagnosticCounters();
  const token = crypto.randomUUID().slice(0, 8);
  const slug = `projects-${token}`;
  const projectSlug = `harbor-${token}`;
  const renamedSlug = `harbor-renamed-${token}`;
  const unknownSlug = `missing-${token}`;
  const projectName = `Harbor map ${token}`;
  const content = projectDocument(projectName);
  const listPath = `/${slug}/projects`;
  const pagePath = `${listPath}/${projectSlug}`;
  const unknownPath = `${listPath}/${unknownSlug}`;
  let resumeID: string | undefined;
  let viewerContext: BrowserContext | undefined;
  const notFoundPaths = new Set<string>();
  const hooks = {
    countConsoleError: (message: ConsoleMessage): boolean =>
      !isExpectedAnonymousMeConsole(message.text(), message.location().url) &&
      !isExpectedNotFoundConsole(message, notFoundPaths),
    onCountedConsoleError: (message: ConsoleMessage): void => {
      recordCountedConsole(message, resumeID, slug);
    },
  };
  await installPublicGuards(context, counters);
  pageDiagnosticsAttacher(counters, hooks)(page);
  let uniformNotFound: NotFoundShape | undefined;
  const expectUniformNotFound = async (
    target: Page,
    path: string,
  ): Promise<void> => {
    notFoundPaths.add(path);
    expect(await readNotFoundShape(target, path)).toEqual(uniformNotFound);
    projectCounts.uniformNotFound += 1;
  };
  const projectHeading = (target: Page): Locator =>
    target.getByRole("heading", { level: 1, name: projectName, exact: true });

  try {
    stage("projects-login");
    await signInWithPassword(page);
    stage("projects-create-resume-status");
    const created = await ownerCreate(
      page,
      `Project pages proof ${crypto.randomUUID()}`,
      "Project pages proof resume",
    );
    createdID = resumeID = created.id;
    expect(created.status).toBe(201);
    const id = created.id ?? "";
    stage("projects-create-project-status");
    const project = await ownerCreateProject(page, projectSlug, content);
    if (project.id !== undefined) createdProjectIDs.push(project.id);
    expect(project.status).toBe(201);
    const projectID = project.id ?? "";
    stage("projects-link-status");
    const linked = await ownerLinkProject(page, id, projectID);
    expect(linked.status).toBe(200);
    stage("projects-link-entry");
    expect(linked.entry).toBeDefined();
    stage("projects-publish-status");
    expect(
      await ownerSetPublishState(page, id, {
        slug,
        live: true,
        signInToView: false,
      }),
    ).toBe(200);
    projectSteps.linked = true;

    viewerContext = await browser.newContext();
    await installPublicGuards(viewerContext, counters);
    const viewer = await viewerContext.newPage();
    pageDiagnosticsAttacher(counters, hooks)(viewer);

    stage("projects-list-status");
    const list = await openPublic(viewer, listPath);
    expect(list.status()).toBe(200);
    stage("projects-list-content");
    const listFacts = await projectPageFacts(viewer, list);
    expect(listFacts).toEqual(SERVED_PROJECT_PAGE);
    await expect(
      viewer.getByRole("link", { name: projectName, exact: true }),
    ).toBeVisible();
    stage("projects-page-status");
    const served = await openPublic(viewer, pagePath);
    expect(served.status()).toBe(200);
    stage("projects-page-content");
    const pageFacts = await projectPageFacts(viewer, served);
    expect(pageFacts).toEqual(SERVED_PROJECT_PAGE);
    await expect(projectHeading(viewer)).toBeVisible();
    projectCounts.stylesheets =
      listFacts.loadedStylesheets + pageFacts.loadedStylesheets;
    projectCounts.styleAttributes =
      listFacts.styleAttributes + pageFacts.styleAttributes;
    projectSteps.served = true;

    stage("projects-unknown-resume-status");
    notFoundPaths.add(`/${unknownSlug}`);
    uniformNotFound = await readNotFoundShape(viewer, `/${unknownSlug}`);
    expect(uniformNotFound.status).toBe(404);
    stage("projects-unknown-project-uniform");
    await expectUniformNotFound(viewer, unknownPath);
    projectSteps.unknownUniform = true;

    stage("projects-hide-status");
    expect(await ownerSetEntryHidden(page, id, linked, true)).toBe(200);
    stage("projects-hidden-page-uniform");
    await expectUniformNotFound(viewer, pagePath);
    stage("projects-hidden-list-uniform");
    await expectUniformNotFound(viewer, listPath);
    projectSteps.hiddenUniform = true;
    stage("projects-show-status");
    expect(await ownerSetEntryHidden(page, id, linked, false)).toBe(200);
    stage("projects-shown-page-status");
    expect((await openPublic(viewer, pagePath)).status()).toBe(200);

    stage("projects-gate-on-status");
    expect(
      await ownerSetPublishState(page, id, {
        slug,
        live: true,
        signInToView: true,
      }),
    ).toBe(200);
    const gateTexts = new Map<string, string>();
    for (const [name, path, gatePage] of [
      ["list", listPath, "projects"],
      ["unknown", unknownPath, `projects/${unknownSlug}`],
      ["page", pagePath, `projects/${projectSlug}`],
    ] as const) {
      stage(`projects-gate-${name}-status`);
      const gate = await openPublic(viewer, path);
      expect(gate.status()).toBe(200);
      stage(`projects-gate-${name}-content`);
      expect(
        await gateFacts(viewer, gate, slug, gatePage, projectName),
      ).toEqual(GATE_FOR_PROJECT);
      gateTexts.set(name, await gate.text());
      projectCounts.gatedAddresses += 1;
    }
    stage("projects-gate-unknown-same");
    expect(gateTexts.get("unknown")).toBe(
      gateTexts
        .get("page")
        ?.replaceAll(`page=projects/${projectSlug}`, `page=projects/${unknownSlug}`),
    );
    projectSteps.gate = true;

    stage("projects-pass-sign-in");
    await signInWithGoogle(viewer, {
      activator: gateProviderLink(viewer, slug, `projects/${projectSlug}`),
      returnPath: pagePath,
    });
    stage("projects-pass-cookie");
    expect(await hasViewPass(viewer)).toBe(true);
    stage("projects-pass-page-status");
    const passed = await openPublic(viewer, pagePath);
    expect(passed.status()).toBe(200);
    stage("projects-pass-page-content");
    expect(await projectPageFacts(viewer, passed)).toEqual(SERVED_PROJECT_PAGE);
    await expect(projectHeading(viewer)).toBeVisible();
    stage("projects-pass-list-status");
    expect((await openPublic(viewer, listPath)).status()).toBe(200);
    projectSteps.pass = true;

    stage("projects-unpublish-status");
    expect(
      await ownerSetPublishState(page, id, {
        slug,
        live: false,
        signInToView: true,
      }),
    ).toBe(200);
    stage("projects-unpublished-page-uniform");
    await expectUniformNotFound(viewer, pagePath);
    stage("projects-unpublished-list-uniform");
    await expectUniformNotFound(viewer, listPath);
    projectSteps.unpublishedUniform = true;

    stage("projects-republish-status");
    expect(
      await ownerSetPublishState(page, id, {
        slug,
        live: true,
        signInToView: false,
      }),
    ).toBe(200);
    stage("projects-republished-page-status");
    expect((await openPublic(viewer, pagePath)).status()).toBe(200);
    stage("projects-rename-status");
    expect(
      await ownerRenameProject(page, projectID, renamedSlug, content),
    ).toBe(200);
    stage("projects-renamed-old-uniform");
    await expectUniformNotFound(viewer, pagePath);
    stage("projects-renamed-new-status");
    const renamed = await openPublic(viewer, `${listPath}/${renamedSlug}`);
    expect(renamed.status()).toBe(200);
    stage("projects-renamed-new-content");
    await expect(projectHeading(viewer)).toBeVisible();
    projectSteps.renamed = true;
  } catch (error) {
    failedStage = currentStage;
    throw error;
  } finally {
    stage("projects-close-contexts");
    if (viewerContext !== undefined) await viewerContext.close();
    if (resumeID !== undefined) {
      await page.close();
    }
  }

  stage("projects-final-certificate-errors");
  expect(counters.certificateErrors).toBe(0);
  stage("projects-final-console-errors");
  expect(counters.consoleErrors).toBe(0);
  stage("projects-final-external-requests");
  expect(counters.externalRequests).toBe(0);
  stage("projects-final-page-errors");
  expect(counters.pageErrors).toBe(0);
  projectErrors.certificate = counters.certificateErrors;
  projectErrors.console = counters.consoleErrors;
  projectErrors.externalRequest = counters.externalRequests;
  projectErrors.page = counters.pageErrors;
});

const ENTRY_POINTS_TEST_TITLE =
  "proves the project entry points on the resume page and in its PDF";
const ENTRY_POINT_REFETCH_MS = 30_000;
const entryPointSteps = {
  firstLink: false,
  rendered: false,
  opened: false,
  publicPDF: false,
  hidden: false,
  unlinked: false,
  untitled: false,
  lastRemoval: false,
  preview: false,
  unpublishedPDF: false,
  cleanup: false,
};
const entryPointCounts = {
  liveStates: 0,
  renderedTitleLinks: 0,
  openedPages: 0,
  publicPDFLinks: -1,
  unpublishedPDFLinks: -1,
  previewLinks: -1,
};
const entryPointErrors = {
  certificate: -1,
  console: -1,
  externalRequest: -1,
  page: -1,
};

interface Titled {
  readonly entry: string;
  readonly slug: string;
}

test(ENTRY_POINTS_TEST_TITLE, async ({ browser, context, page }) => {
  test.setTimeout(240_000);
  stage("entry-points-setup");
  await pinEnglish(context);
  const counters = newDiagnosticCounters();
  const token = crypto.randomUUID().slice(0, 8);
  const slug = `entry-points-${token}`;
  const projectsURL = `${ORIGIN}/${slug}/projects`;
  const projects = [
    { slug: `atlas-${token}`, name: `Atlas route ${token}` },
    { slug: `beacon-${token}`, name: `Beacon log ${token}` },
    { slug: `cinder-${token}`, name: `Cinder notes ${token}` },
  ];
  let resumeID: string | undefined;
  let viewerContext: BrowserContext | undefined;
  const hooks = {
    countConsoleError: (message: ConsoleMessage): boolean =>
      !isExpectedAnonymousMeConsole(message.text(), message.location().url),
    onCountedConsoleError: (message: ConsoleMessage): void => {
      recordCountedConsole(message, resumeID, slug);
    },
  };
  await installPublicGuards(context, counters);
  pageDiagnosticsAttacher(counters, hooks)(page);
  const expected = (
    titled: readonly Titled[],
    bar: boolean,
    kept: boolean,
  ): EntryPointFacts => ({
    titles: sortedTitles(
      titled.map((link) => ({
        entry: link.entry,
        href: `${projectsURL}/${link.slug}`,
      })),
    ),
    bars: bar ? [{ label: "Projects", href: projectsURL }] : [],
    kept,
  });
  const projectURIs = (bytes: readonly number[]): string[] =>
    pdfLinkURIs(bytes)
      .filter((uri) => uri.includes("/projects/"))
      .sort();

  try {
    stage("entry-points-login");
    await signInWithPassword(page);
    stage("entry-points-create-resume-status");
    const created = await ownerCreate(
      page,
      `Entry points proof ${crypto.randomUUID()}`,
      "Entry points proof resume",
    );
    createdID = resumeID = created.id;
    expect(created.status).toBe(201);
    const id = created.id ?? "";
    stage("entry-points-create-projects-status");
    const projectIDs: string[] = [];
    for (const project of projects) {
      const made = await ownerCreateProject(
        page,
        project.slug,
        projectDocument(project.name),
      );
      if (made.id !== undefined) createdProjectIDs.push(made.id);
      expect(made.status).toBe(201);
      projectIDs.push(made.id ?? "");
    }
    stage("entry-points-publish-status");
    expect(
      await ownerSetPublishState(page, id, {
        slug,
        live: true,
        signInToView: false,
        download: true,
      }),
    ).toBe(200);

    viewerContext = await browser.newContext();
    await installPublicGuards(viewerContext, counters);
    const viewer = await viewerContext.newPage();
    pageDiagnosticsAttacher(counters, hooks)(viewer);
    stage("entry-points-open-status");
    expect((await openPublic(viewer, `/${slug}`)).status()).toBe(200);
    stage("entry-points-open-hydrated");
    await waitForHydration(viewer, "public-resume");
    await markPublicRoot(viewer);
    const expectLive = async (target: EntryPointFacts): Promise<void> => {
      await expect
        .poll(() => entryPointFacts(viewer), {
          timeout: ENTRY_POINT_REFETCH_MS,
        })
        .toEqual(target);
      entryPointCounts.liveStates += 1;
    };
    stage("entry-points-zero-links");
    await expectLive(expected([], false, true));

    const linkProject = async (index: number, name: string) => {
      stage(`entry-points-${name}-status`);
      const linked = await ownerLinkProject(page, id, projectIDs[index] ?? "");
      expect(linked.status).toBe(200);
      stage(`entry-points-${name}-entry`);
      expect(linked.sectionKey).toBeDefined();
      expect(linked.entry?.id).toEqual(expect.any(String));
      return {
        sectionKey: linked.sectionKey ?? "",
        entry: linked.entry ?? {},
        titled: {
          entry: String(linked.entry?.id),
          slug: projects[index]?.slug ?? "",
        },
      };
    };
    const atlas = await linkProject(0, "first-link");
    stage("entry-points-first-link");
    await expectLive(expected([atlas.titled], true, true));
    entryPointSteps.firstLink = true;

    const beacon = await linkProject(1, "second-link");
    stage("entry-points-second-link");
    await expectLive(expected([atlas.titled, beacon.titled], true, true));
    const cinder = await linkProject(2, "untitled-link");
    stage("entry-points-untitled-link");
    await expectLive(
      expected([atlas.titled, beacon.titled, cinder.titled], true, true),
    );
    stage("entry-points-untitled-title-status");
    const untitled = withoutField(cinder.entry, "title");
    expect(
      await ownerWriteEntry(page, id, cinder.sectionKey, untitled),
    ).toBe(200);
    stage("entry-points-untitled-title");
    await expectLive(expected([atlas.titled, beacon.titled], true, true));

    stage("entry-points-rendered-status");
    const reader = await viewerContext.newPage();
    pageDiagnosticsAttacher(counters, hooks)(reader);
    const rendered = await openPublic(reader, `/${slug}`);
    expect(rendered.status()).toBe(200);
    stage("entry-points-rendered-server");
    expect(serverEntryPoints(await rendered.text())).toEqual({
      marker: true,
      titles: 2,
      bars: 1,
    });
    stage("entry-points-rendered-content");
    await waitForHydration(reader, "public-resume");
    const renderedFacts = await entryPointFacts(reader);
    expect(renderedFacts).toEqual(
      expected([atlas.titled, beacon.titled], true, false),
    );
    entryPointCounts.renderedTitleLinks = renderedFacts.titles.length;
    entryPointSteps.rendered = true;

    for (const [name, href] of [
      ...renderedFacts.titles.map(({ href }) => ["title", href] as const),
      ...renderedFacts.bars.map(({ href }) => ["bar", href] as const),
    ]) {
      stage(`entry-points-open-${name}-status`);
      expect((await reader.goto(href))?.status()).toBe(200);
      stage(`entry-points-open-${name}-content`);
      await expect(reader.locator("main#public-project")).toHaveCount(1);
      entryPointCounts.openedPages += 1;
    }
    entryPointSteps.opened = true;

    stage("entry-points-public-pdf-status");
    const publicPDF = await fetchPDF(
      reader,
      `/api/v1/public/resumes/${slug}/pdf`,
      "omit",
    );
    expect(publicPDF.status).toBe(200);
    stage("entry-points-public-pdf-links");
    expect(publicPDF.contentType).toBe("application/pdf");
    expect(isPDF(publicPDF.bytes)).toBe(true);
    const publicLinks = projectURIs(publicPDF.bytes);
    expect(publicLinks).toEqual(
      renderedFacts.titles.map(({ href }) => href).sort(),
    );
    entryPointCounts.publicPDFLinks = publicLinks.length;
    entryPointSteps.publicPDF = true;

    stage("entry-points-hide-status");
    expect(
      await ownerWriteEntry(page, id, atlas.sectionKey, {
        ...atlas.entry,
        isHidden: true,
      }),
    ).toBe(200);
    stage("entry-points-hidden-link");
    await expectLive(expected([beacon.titled], true, true));
    entryPointSteps.hidden = true;
    stage("entry-points-unlink-status");
    expect(
      await ownerWriteEntry(
        page,
        id,
        beacon.sectionKey,
        withoutField(beacon.entry, "projectId"),
      ),
    ).toBe(200);
    stage("entry-points-unlinked-untitled-bar");
    await expectLive(expected([], true, true));
    entryPointSteps.unlinked = true;
    entryPointSteps.untitled = true;
    stage("entry-points-last-unlink-status");
    expect(
      await ownerWriteEntry(
        page,
        id,
        cinder.sectionKey,
        withoutField(untitled, "projectId"),
      ),
    ).toBe(200);
    stage("entry-points-last-unlinked-bar");
    await expectLive(expected([], false, true));
    entryPointSteps.lastRemoval = true;
    stage("entry-points-show-status");
    expect(
      await ownerWriteEntry(page, id, atlas.sectionKey, {
        ...atlas.entry,
        isHidden: false,
      }),
    ).toBe(200);
    stage("entry-points-shown-link");
    await expectLive(expected([atlas.titled], true, true));

    stage("entry-points-preview-open");
    await page.goto(`${ORIGIN}/app/resumes/${id}`);
    await waitForHydration(page, "__nuxt", WAIT_WARM_MS);
    stage("entry-points-preview-content");
    await expect(
      page
        .getByTestId("preview-sheet")
        .getByText(projects[0]?.name ?? "", { exact: true })
        .filter({ visible: true })
        .first(),
    ).toBeVisible({ timeout: ENTRY_POINT_REFETCH_MS });
    entryPointCounts.previewLinks = await page
      .locator("a[data-project-entry]")
      .count();
    expect(entryPointCounts.previewLinks).toBe(0);
    entryPointSteps.preview = true;

    stage("entry-points-close-viewer");
    await viewerContext.close();
    viewerContext = undefined;
    stage("entry-points-unpublish-status");
    expect(
      await ownerSetPublishState(page, id, {
        slug,
        live: false,
        signInToView: false,
      }),
    ).toBe(200);
    stage("entry-points-unpublished-pdf-status");
    const ownerPDF = await fetchPDF(
      page,
      `/api/v1/resumes/${id}/pdf`,
      "include",
    );
    expect(ownerPDF.status).toBe(200);
    stage("entry-points-unpublished-pdf-links");
    expect(ownerPDF.contentType).toBe("application/pdf");
    expect(isPDF(ownerPDF.bytes)).toBe(true);
    entryPointCounts.unpublishedPDFLinks = projectURIs(ownerPDF.bytes).length;
    expect(entryPointCounts.unpublishedPDFLinks).toBe(0);
    entryPointSteps.unpublishedPDF = true;
  } catch (error) {
    failedStage = currentStage;
    throw error;
  } finally {
    stage("entry-points-close-contexts");
    if (viewerContext !== undefined) await viewerContext.close();
    if (resumeID !== undefined) {
      await page.close();
    }
  }

  stage("entry-points-final-certificate-errors");
  expect(counters.certificateErrors).toBe(0);
  stage("entry-points-final-console-errors");
  expect(counters.consoleErrors).toBe(0);
  stage("entry-points-final-external-requests");
  expect(counters.externalRequests).toBe(0);
  stage("entry-points-final-page-errors");
  expect(counters.pageErrors).toBe(0);
  entryPointErrors.certificate = counters.certificateErrors;
  entryPointErrors.console = counters.consoleErrors;
  entryPointErrors.externalRequest = counters.externalRequests;
  entryPointErrors.page = counters.pageErrors;
});

test("proves native HTTPS publish, discovery, and revocation", async ({
  browser,
  context,
  page,
}) => {
  await pinEnglish(context);
  const counters = newDiagnosticCounters();
  const events: string[] = [];
  let resumeID: string | undefined;
  const slug = `publish-${crypto.randomUUID().slice(0, 8)}`;
  let publicContext: BrowserContext | undefined;
  let secondEditor: Page | undefined;
  let revocationElapsed = 0;
  let publishMutationCount = 0;
  let localeMutationCount = 0;
  let revokingPublic = false;
  let syntheticInvalidRequest = false;
  let syntheticInvalidConsoleBudget = 0;
  const recordConsoleFailure = (message: ConsoleMessage): void => {
    recordCountedConsole(message, resumeID, slug);
  };
  const requestListener = (request: Request): void => {
    const url = new URL(request.url());
    if (url.origin !== ORIGIN) return;
    if (
      resumeID !== undefined &&
      ["POST", "PATCH", "DELETE"].includes(request.method()) &&
      url.pathname.startsWith(`/api/v1/resumes/${resumeID}`)
    ) {
      localeMutationCount += 1;
    }
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
    onCountedConsoleError: recordConsoleFailure,
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
    steps.auth = true;
    stage("signed-in");

    const created = await createBlankResume(
      page,
      `Publish proof ${crypto.randomUUID()}`,
      undefined,
      (accepted) => {
        createdID = resumeID = accepted.metadata.id;
      },
    );
    resumeID = created.metadata.id;
    stage("resume-created");
    await page.getByTestId("workspace-locale-vi").press("Enter");
    await expect(page.locator("html")).toHaveAttribute("lang", "vi");
    stage("vietnamese-editor");
    await page.locator('[data-action="open-document"]').press("Enter");
    await page.getByLabel("Họ và tên").fill("Publish proof resume");
    await page.getByLabel("Họ và tên").press("Tab");
    await expect(page.getByTestId("save-status")).toContainText("Đã lưu");
    await page.locator('[data-action="open-structure"]').press("Enter");
    await page.locator('[data-action="section-type"]').selectOption("work");
    await page
      .getByTestId("section-create-form")
      .locator('[data-action="create"]')
      .press("Enter");
    await expect(page.getByTestId("save-status")).toContainText("Đã lưu");
    await page.locator('[data-outline-key="work"]').press("Enter");
    await page.locator('[data-action="add-entry"]').press("Enter");
    const entry = page.locator("[data-entry-id]");
    await expect(entry).toHaveCount(1);
    await entry.getByLabel("Chức danh").fill("Engineer");
    await entry.getByLabel("Chức danh").press("Tab");
    await entry.getByLabel("Công ty", { exact: true }).fill("Example Corp");
    await entry.getByLabel("Công ty", { exact: true }).press("Tab");
    await expect(page.getByTestId("save-status")).toContainText("Đã lưu");
    steps.complete = true;
    stage("resume-complete");

    events.length = 0;
    await page.locator('[data-action="open-document"]').press("Enter");
    await page.getByLabel("Tiêu đề").fill("Accepted before publication");
    await expect(page.getByTestId("public-mark")).toHaveCount(0);
    await expect(page.getByTestId("preview-stamp")).toHaveCount(0);
    await page.locator('[data-action="publish"]').press("Enter");
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(page.getByLabel("Đường dẫn")).toBeFocused();
    steps.keyboard = true;
    stage("dialog-open");
    await auditDialog(page);
    steps.accessibility = true;
    stage("dialog-audited");
    await page.setViewportSize({ width: 1440, height: 900 });
    stage("dialog-sized");
    await dialog.getByLabel("Đường dẫn", { exact: true }).fill(slug);
    const live = dialog.getByLabel("CV công khai", { exact: true });
    const download = dialog.getByLabel("Tải PDF", { exact: true });
    const seo = dialog.getByLabel("SEO và GEO", { exact: true });
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
    await dialog.locator('[data-action="publish-submit"]').press("Enter");
    stage("publish-requested");
    const acceptedPublish = await publishResponse;
    statuses.publish = acceptedPublish.status();
    expect(statuses.publish).toBe(200);
    const publicMark = page.getByTestId("public-mark");
    const previewStamp = page.getByTestId("preview-stamp");
    stage("publish-marks-landing");
    await expect(publicMark).toHaveAttribute("data-stamp", "landing");
    await expect(previewStamp).toHaveCount(0);
    await expect(dialog.getByRole("status")).toHaveText(
      "Đã xuất bản thành công.",
    );
    stage("publish-status");
    const editAccepted = events.indexOf("edit-accepted");
    const publishRequested = events.indexOf("publish-request");
    expect(editAccepted).toBeGreaterThanOrEqual(0);
    expect(publishRequested).toBeGreaterThan(editAccepted);
    steps.saveFirst = true;
    steps.headers = Object.values(headerPresence).slice(0, 5).every(Boolean);

    stage("publish-marks");
    await expect(publicMark).toBeVisible();
    await expect(previewStamp).toHaveCount(0);
    stage("publish-marks-visible");
    const publicMarkLink = publicMark.locator("[data-public-link]");
    await expect(publicMarkLink).toHaveCount(1);
    stage("publish-mark-link-count");
    await expect(publicMarkLink).toHaveText(`aboutme.vn/${slug}`);
    stage("publish-mark-link-text");
    await expect(publicMarkLink).toHaveAttribute("href", `/${slug}`);
    stage("publish-mark-link");
    await expect(
      publicMark.getByRole("img", { includeHidden: true }),
    ).toHaveAttribute(
      "aria-label",
      `Công khai tại aboutme.vn/${slug}`,
    );
    stage("publish-mark-label");
    stage("publish-marks-idle");
    await expect(publicMark).not.toHaveAttribute("data-stamp");
    await expect(previewStamp).toHaveCount(0);

    const link = dialog.getByRole("link", {
      name: `aboutme.vn/${slug}`,
      exact: true,
    });
    await expect(link).toHaveAttribute("href", `/${slug}`);
    await dialog.locator('[data-action="copy-link"]').press("Enter");
    await expect(
      dialog.getByRole("button", { name: "Đã sao chép", exact: true }),
    ).toBeVisible();
    await expect
      .poll(() => page.evaluate(() => navigator.clipboard.readText()))
      .toBe(`${ORIGIN}/${slug}`);
    steps.published = true;
    stage("published");
    const revisionBeforeLocaleToggle = await readRemoteResumeRevision(
      page,
      resumeID,
    );
    const dialogNode = await dialog.elementHandle();
    if (dialogNode === null) throw new Error("publish dialog is unavailable");
    const slugField = dialog.locator('[data-action="publish-slug"]');
    await slugField.focus();
    await slugField.evaluate((input) => input.setSelectionRange(1, 3));
    localeMutationCount = 0;
    await dialog.getByRole("button", { name: "English", exact: true }).click();
    await expect(page.locator("html")).toHaveAttribute("lang", "en");
    expect(
      await dialog.evaluate((node, original) => node === original, dialogNode),
    ).toBe(true);
    await expect(dialog.getByLabel("Slug", { exact: true })).toHaveValue(slug);
    await expect(slugField).toBeFocused();
    await expect(slugField).toHaveJSProperty("selectionStart", 1);
    await expect(slugField).toHaveJSProperty("selectionEnd", 3);
    expect(localeMutationCount).toBe(0);
    await expect(readRemoteResumeRevision(page, resumeID)).resolves.toBe(
      revisionBeforeLocaleToggle,
    );
    const vietnamese = dialog.getByRole("button", {
      name: "Tiếng Việt",
      exact: true,
    });
    await vietnamese.focus();
    await vietnamese.press("Enter");
    await expect(page.locator("html")).toHaveAttribute("lang", "vi");
    await expect(vietnamese).toBeFocused();
    await dialog
      .getByRole("button", { name: "English", exact: true })
      .press("Enter");
    await expect(page.locator("html")).toHaveAttribute("lang", "en");
    expect(localeMutationCount).toBe(0);
    await expect(readRemoteResumeRevision(page, resumeID)).resolves.toBe(
      revisionBeforeLocaleToggle,
    );
    await expect(dialog.getByRole("status")).toHaveText(
      "Published successfully.",
    );
    await expect(dialog.locator('[data-action="copy-link"]')).toHaveText(
      "Copied",
    );
    await dialog.locator('[data-action="copy-link"]').press("Enter");
    await expect(
      dialog.getByRole("button", { name: "Copied", exact: true }),
    ).toBeVisible();
    stage("english-publication-state");
    publicContext = await browser.newContext();
    const publicCounters = counters;
    await installPublicGuards(publicContext, publicCounters);
    const publicPage = await publicContext.newPage();
    pageDiagnosticsAttacher(counters, {
      onCountedConsoleError: recordConsoleFailure,
      countConsoleError: (message) =>
        !isExpectedAnonymousMeConsole(message.text(), message.location().url) &&
        !(
          revokingPublic &&
          (isExpectedRevokedPublicConsole(message, slug) ||
            isExpectedRevokingPublicConsole(message, slug))
        ),
    })(publicPage);
    const publicStream = publicPage.waitForResponse(
      (response) =>
        new URL(response.url()).pathname === `/api/v1/live/${slug}` &&
        response.status() === 200,
    );
    const privateResponse = await publicPage.goto(`${ORIGIN}/${slug}`);
    statuses.publicPrivate = privateResponse?.status() ?? 0;
    expect(statuses.publicPrivate).toBe(200);
    expect(privateResponse?.headers()["x-robots-tag"]).toBe(
      "noindex, noarchive",
    );
    expect(await publicPage.getByRole("main").count()).toBe(1);
    await waitForHydration(publicPage, "public-resume");
    expect((await publicStream).headers()["content-type"]).toContain(
      "text/event-stream",
    );
    steps.noindex = true;
    stage("noindex");

    await dialog
      .getByRole("button", { name: "Cancel", exact: true })
      .press("Enter");
    stage("published-close");
    await expect(page.locator('[data-action="publish"]')).toBeFocused();
    stage("published-focus-restored");

    secondEditor = await context.newPage();
    stage("realtime-second-tab-created");
    pageDiagnosticsAttacher(counters, {
      onCountedConsoleError: recordConsoleFailure,
    })(secondEditor);
    const ownerStream = secondEditor.waitForResponse(
      (response) =>
        new URL(response.url()).pathname === "/api/v1/events" &&
        response.status() === 200,
    );
    await secondEditor.goto(`${ORIGIN}/app/resumes/${resumeID}`);
    stage("realtime-second-tab-loaded");
    await waitForHydration(secondEditor);
    stage("realtime-second-tab-hydrated");
    expect((await ownerStream).headers()["content-type"]).toContain(
      "text/event-stream",
    );
    stage("realtime-owner-stream");
    await secondEditor
      .getByRole("navigation", { name: "Resume outline" })
      .getByRole("button", { name: "Personal details", exact: true })
      .click();
    stage("realtime-second-tab-panel");
    await publicPage.setViewportSize({ width: 900, height: 100 });
    const scrollBefore = await publicPage.evaluate(() => {
      document
        .querySelector("#public-resume")
        ?.setAttribute("data-realtime-proof", "kept");
      window.scrollTo(0, 180);
      return window.scrollY;
    });
    expect(scrollBefore).toBeGreaterThan(0);
    stage("realtime-scrolled");
    const ownerRefresh = page.waitForResponse(
      (response) =>
        response.request().method() === "GET" &&
        new URL(response.url()).pathname === `/api/v1/resumes/${resumeID}` &&
        response.status() === 200 &&
        response.request().headers()["if-none-match"] === undefined,
    );
    const publicRefresh = publicPage.waitForResponse(
      (response) =>
        new URL(response.url()).pathname === `/api/v1/public/resumes/${slug}` &&
        response.status() === 200 &&
        response.request().headers()["if-none-match"] === undefined,
    );
    await secondEditor.getByLabel("Headline").fill("Updated in another editor");
    await secondEditor.getByLabel("Headline").press("Tab");
    stage("realtime-edit-submitted");
    await expect(secondEditor.getByTestId("save-status")).toContainText(
      "Saved",
    );
    await Promise.all([ownerRefresh, publicRefresh]);
    stage("realtime-refetched");
    await expect(page.getByLabel("Headline")).toHaveValue(
      "Updated in another editor",
    );
    await expect(publicPage.getByRole("main")).toContainText(
      "Updated in another editor",
    );
    await expect(publicPage.locator("#public-resume")).toHaveAttribute(
      "data-realtime-proof",
      "kept",
    );
    expect(await publicPage.evaluate(() => window.scrollY)).toBe(scrollBefore);
    steps.ownerRealtime = true;
    steps.publicRealtime = true;
    steps.scroll = true;
    stage("realtime-refresh");
    await secondEditor.close();
    secondEditor = undefined;
    await page.bringToFront();

    await page.locator('[data-action="publish"]').press("Enter");
    stage("update-reopen");
    let updateDialog = page.getByRole("dialog");
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
    await updateDialog
      .getByRole("button", { name: "Tiếng Việt", exact: true })
      .press("Enter");
    await expect(page.locator("html")).toHaveAttribute("lang", "vi");
    await expect(updateDialog).toContainText("Không thể xuất bản CV.");
    await expect(issueActions).toHaveCount(24);
    await updateDialog
      .getByRole("button", { name: "English", exact: true })
      .press("Enter");
    await expect(page.locator("html")).toHaveAttribute("lang", "en");
    await expect(updateDialog).toContainText("The resume cannot be published.");
    await expect(issueActions).toHaveCount(24);
    stage("locale-error-preserved");
    syntheticInvalidRequest = false;
    await updateDialog.evaluate(async (element) => {
      await Promise.all(
        element.getAnimations().map((animation) => animation.finished),
      );
    });
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

    await page.setViewportSize({ width: 1440, height: 900 });
    await page.locator('[data-action="publish"]').press("Enter");
    updateDialog = page.getByRole("dialog");
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
    const unpublishDialog = page.getByRole("dialog");
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
    const revokedNavigation = publicPage.waitForResponse(
      (response) =>
        response.request().isNavigationRequest() &&
        new URL(response.url()).pathname === `/${slug}` &&
        REVOKED_PUBLIC_STATUSES.includes(response.status()),
      { timeout: REVOCATION_BOUND_MS },
    );
    const started = Date.now();
    revokingPublic = true;
    await unpublishDialog
      .getByRole("button", { name: "Unpublish", exact: true })
      .press("Enter");
    statuses.unpublish = (await unpublishResponse).status();
    expect(statuses.unpublish).toBe(200);
    await expect(
      unpublishDialog.getByLabel("Slug", { exact: true }),
    ).toHaveValue(slug);
    await expect(publicMark).toHaveCount(0);
    await expect(previewStamp).toHaveCount(0);
    stage("revoked-navigation");
    const revokedResponse = await revokedNavigation;
    const elapsed = Date.now() - started;
    revocationElapsed = elapsed;
    statuses.revoked = revokedResponse?.status() ?? 0;
    expect(REVOKED_PUBLIC_STATUSES).toContain(statuses.revoked);
    expect(elapsed).toBeLessThanOrEqual(REVOCATION_BOUND_MS);
    steps.unpublish = true;
    steps.revocation = true;
    steps.headers = Object.values(headerPresence).every(Boolean);
    stage("revoked");
    await publicContext.close();
    publicContext = undefined;
    await unpublishDialog
      .getByRole("button", { name: "Cancel", exact: true })
      .press("Enter");
    await page.goto(`${ORIGIN}/app/resumes`);
    await expect(
      page.getByRole("heading", { name: "Resumes", exact: true }),
    ).toBeVisible();
    // `press` only focuses, and a disabled button takes no focus, so an Enter before the /me read lands is lost (Playwright locator.press).
    let releaseMe = (): void => {};
    const meHeld = new Promise<void>((resolve) => {
      releaseMe = resolve;
    });
    const isMe = (url: URL): boolean => url.pathname === "/api/v1/me";
    const holdMe = async (route: Route): Promise<void> => {
      await meHeld;
      try {
        await route.continue();
      } catch (error) {
        // The page can abandon a held read for a newer one; Playwright then refuses to continue it (Playwright route.continue).
        if (!String(error).includes("Route is already handled")) throw error;
      }
    };
    await page.route(isMe, holdMe);
    await page.goto(`${ORIGIN}/app/settings/sessions`);
    const logOut = page
      .getByTestId("settings-page")
      .getByRole("button", { name: "Log out", exact: true });
    await expect(logOut).toBeDisabled();
    setTimeout(releaseMe, 1_000);
    await expect(logOut).toBeEnabled();
    await logOut.press("Enter");
    await expect(page).toHaveURL(`${ORIGIN}/login`);
    // Keep the route until the sign-out ends: removing it earlier can let
    // the held reads through before the press and hide a missing wait.
    await page.unroute(isMe, holdMe);
    steps.signOut = true;
    stage("signed-out");
  } catch (error) {
    failedStage = currentStage;
    const sourceLine =
      error instanceof Error
        ? /publish\.spec\.ts:([0-9]{1,4}):/u.exec(error.stack ?? "")?.[1]
        : undefined;
    if (sourceLine !== undefined) stage(`failure-at-line-${sourceLine}`);
    throw error;
  } finally {
    if (secondEditor !== undefined) await secondEditor.close();
    if (publicContext !== undefined) await publicContext.close();
    if (resumeID !== undefined) {
      await page.close();
    }
  }

  stage("final-diagnostics");
  if (counters.certificateErrors !== 0) stage("certificate-errors");
  if (counters.consoleErrors !== 0) {
    stage(countedConsoleFailure ?? "console-errors");
  }
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
  stage("final-mutation-count");
  expect(publishMutationCount).toBe(3);
  stage("final-certificate-errors");
  expect(counters.certificateErrors).toBe(0);
  stage("final-console-errors");
  expect(counters.consoleErrors).toBe(0);
  stage("final-external-requests");
  expect(counters.externalRequests).toBe(0);
  stage("final-page-errors");
  expect(counters.pageErrors).toBe(0);
  writePublishEvidence = async () => {
    stage("final-steps");
    expect(steps).toEqual(
      Object.fromEntries(Object.keys(steps).map((key) => [key, true])),
    );
    stage("final-project-steps");
    expect(projectSteps).toEqual(
      Object.fromEntries(Object.keys(projectSteps).map((key) => [key, true])),
    );
    stage("final-entry-point-steps");
    expect(entryPointSteps).toEqual(
      Object.fromEntries(
        Object.keys(entryPointSteps).map((key) => [key, true]),
      ),
    );
    stage("final-evidence");
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
        projects: {
          errors: projectErrors,
          steps: projectSteps,
          counts: projectCounts,
        },
        entryPoints: {
          errors: entryPointErrors,
          steps: entryPointSteps,
          counts: entryPointCounts,
        },
      })}\n`,
      { flag: "wx", mode: 0o600 },
    );
  };
});

const SHOWCASE_LISTING_PATH = "/api/v1/public/showcase";
const SHOWCASE_REVOKED_STATUSES: readonly number[] = [401, 403, 404, 410];
let listingNavigation = false;
const CARD_RENDER_BOUND_MS = 20_000;

function classifyConsoleFailure(
  message: ConsoleMessage,
  resumeID: string | undefined,
  slug: string,
): string {
  let path = "";
  try {
    const location = new URL(message.location().url);
    if (location.origin === ORIGIN) path = location.pathname;
  } catch {
  }
  const route =
    resumeID !== undefined && path === `/api/v1/resumes/${resumeID}`
      ? "owner-read"
      : resumeID !== undefined && path === `/api/v1/resumes/${resumeID}/publish`
        ? "owner-publish"
        : path === "/api/v1/events"
          ? "owner-stream"
          : path === SHOWCASE_LISTING_PATH
            ? "showcase-listing"
            : path === "/showcase"
              ? "showcase-page"
              : path === "/app/resumes"
                ? "resumes-page"
                : path === `/${slug}`
                  ? "public-page"
                  : path === `/api/v1/public/resumes/${slug}`
                    ? "public-read"
                    : path.startsWith(`/api/v1/public/resumes/${slug}/og/`)
                      ? "public-card"
                      : path === `/api/v1/live/${slug}`
                        ? "public-stream"
                        : path.startsWith(`/${slug}/projects`)
                          ? "project-page"
                          : path.startsWith("/api/v1/projects")
                            ? "owner-project"
                            : "other";
  return `console-${httpFailureStatus(message.text()) ?? "non-http"}-${route}`;
}

function recordCountedConsole(
  message: ConsoleMessage,
  resumeID: string | undefined,
  slug: string,
): void {
  countedConsoleFailure ??= `${classifyConsoleFailure(message, resumeID, slug)}-at-${currentStage}`;
}

function isAbortedListingConsole(message: ConsoleMessage): boolean {
  let location: URL;
  try {
    location = new URL(message.location().url);
  } catch {
    return false;
  }
  return (
    message.text() === "Failed to load resource: net::ERR_ABORTED" &&
    location.origin === ORIGIN &&
    location.pathname === SHOWCASE_LISTING_PATH &&
    location.search === ""
  );
}

function isPublishResponse(resumeID: string): (response: Response) => boolean {
  return (response) =>
    response.request().method() === "POST" &&
    new URL(response.url()).pathname === `/api/v1/resumes/${resumeID}/publish`;
}

async function submitPublishDialog(
  page: Page,
  dialog: Locator,
  resumeID: string,
): Promise<void> {
  const publishStage = currentStage;
  stage(`${publishStage}-submit`);
  const accepted = page.waitForResponse(isPublishResponse(resumeID));
  await dialog.locator('[data-action="publish-submit"]').press("Enter");
  stage(`${publishStage}-status`);
  expect((await accepted).status()).toBe(200);
}

async function waitForListedCard(page: Page, slug: string): Promise<void> {
  const image = page.locator(`[data-showcase-slug="${slug}"] img`);
  await image.scrollIntoViewIfNeeded();
  await expect
    .poll(
      () => image.evaluate((element: HTMLImageElement) => element.complete),
      { timeout: CARD_RENDER_BOUND_MS },
    )
    .toBe(true);
  expect(
    await image.evaluate((element: HTMLImageElement) => element.naturalWidth),
  ).toBeGreaterThan(0);
}

async function completeResume(page: Page, fullName: string): Promise<void> {
  await page.locator('[data-action="open-document"]').press("Enter");
  await page.getByLabel("Full name").fill(fullName);
  await page.getByLabel("Full name").press("Tab");
  await expect(page.getByTestId("save-status")).toContainText("Saved");
  await page.locator('[data-action="open-structure"]').press("Enter");
  await page.locator('[data-action="section-type"]').selectOption("work");
  await page
    .getByTestId("section-create-form")
    .locator('[data-action="create"]')
    .press("Enter");
  await expect(page.getByTestId("save-status")).toContainText("Saved");
  await page.locator('[data-outline-key="work"]').press("Enter");
  await page.locator('[data-action="add-entry"]').press("Enter");
  const entry = page.locator("[data-entry-id]");
  await expect(entry).toHaveCount(1);
  await entry.getByLabel("Job title").fill("Engineer");
  await entry.getByLabel("Job title").press("Tab");
  await entry.getByLabel("Employer", { exact: true }).fill("Example Corp");
  await entry.getByLabel("Employer", { exact: true }).press("Tab");
  await expect(page.getByTestId("save-status")).toContainText("Saved");
  await page.locator('[data-action="open-document"]').press("Enter");
}

async function openPublishDialog(page: Page) {
  stage(`${currentStage}-open-dialog`);
  await page.locator('[data-action="publish"]').press("Enter");
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  return dialog;
}

async function readListing(
  page: Page,
): Promise<{ document: Response; listing: Response; slugs: string[] }> {
  const listingStage = currentStage;
  stage(`${listingStage}-navigation`);
  const read = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === SHOWCASE_LISTING_PATH &&
      response.request().method() === "GET",
  );
  listingNavigation = true;
  let document: Response | null;
  try {
    document = await page.goto("/showcase");
  } finally {
    listingNavigation = false;
  }
  if (document === null) throw new Error("showcase document is unavailable");
  stage(`${listingStage}-response`);
  const listing = await read;
  stage(`${listingStage}-document-status`);
  expect(document.status()).toBe(200);
  stage(`${listingStage}-status`);
  expect(listing.status()).toBe(200);
  stage(`${listingStage}-content`);
  const body = (await listing.json()) as { items?: { slug?: string }[] };
  expect(Array.isArray(body.items)).toBe(true);
  return {
    document,
    listing,
    slugs: (body.items ?? []).map((item) => item.slug ?? ""),
  };
}

test("proves the showcase opt-in, its revocations, and delivery rules", async ({
  browser,
  context,
  page,
}) => {
  stage("showcase-setup");
  await pinEnglish(context);
  const counters = newDiagnosticCounters();
  const slug = `showcase-${crypto.randomUUID().slice(0, 8)}`;
  let resumeID: string | undefined;
  let publicContext: BrowserContext | undefined;
  let revokingPublic = false;
  const hooks = {
    countConsoleError: (message: ConsoleMessage): boolean =>
      !isExpectedAnonymousMeConsole(message.text(), message.location().url) &&
      !(
        revokingPublic &&
        isExpectedRevokedPublicConsole(message, slug, SHOWCASE_REVOKED_STATUSES)
      ) &&
      !(listingNavigation && isAbortedListingConsole(message)),
    onCountedConsoleError: (message: ConsoleMessage): void => {
      recordCountedConsole(message, resumeID, slug);
    },
  };
  await installPublicGuards(context, counters);
  pageDiagnosticsAttacher(counters, hooks)(page);

  try {
    stage("showcase-login");
    await signInWithPassword(page);

    stage("showcase-create-resume");
    const created = await createBlankResume(
      page,
      `Showcase proof ${crypto.randomUUID()}`,
      undefined,
      (accepted) => {
        createdID = resumeID = accepted.metadata.id;
      },
    );
    resumeID = created.metadata.id;
    const id = resumeID;
    stage("showcase-complete-resume");
    await completeResume(page, "Showcase proof resume");

    let dialog = await openPublishDialog(page);
    stage("showcase-opt-in-controls");
    await dialog.getByLabel("Slug", { exact: true }).fill(slug);
    const showcaseSwitch = dialog.getByLabel("Show in the community showcase", {
      exact: true,
    });
    const publicSwitch = dialog.getByLabel("Public resume", { exact: true });
    const signInSwitch = dialog.getByLabel("Require sign-in to view", {
      exact: true,
    });
    const description = dialog.getByTestId("publish-showcase-description");
    await expect(showcaseSwitch).not.toBeChecked();
    await expect(showcaseSwitch).toBeDisabled();
    await expect(description).toContainText(
      "Turn on Public resume to show it in the community showcase.",
    );
    await publicSwitch.press("Space");
    await expect(showcaseSwitch).toBeEnabled();
    await expect(showcaseSwitch).not.toBeChecked();
    await showcaseSwitch.press("Space");
    await expect(showcaseSwitch).toBeChecked();
    await dialog
      .locator('[data-action="publish-showcase-role"]')
      .selectOption("backend");
    await submitPublishDialog(page, dialog, id);

    stage("showcase-opt-in-message");
    const status = dialog.getByTestId("publish-showcase-status");
    await expect(status).toContainText("Shown in the community showcase.");

    publicContext = await browser.newContext();
    await installPublicGuards(publicContext, counters);
    const anonymous = await publicContext.newPage();
    pageDiagnosticsAttacher(counters, hooks)(anonymous);
    stage("showcase-initial-listing");
    const listed = await readListing(anonymous);
    stage("showcase-listed-content");
    expect(listed.slugs).toContain(slug);
    await expect(
      anonymous.locator(`[data-showcase-slug="${slug}"]`),
    ).toBeVisible();
    await expect(anonymous.locator('[data-state="empty"]')).toHaveCount(0);
    stage("showcase-listed-card");
    await waitForListedCard(anonymous, slug);

    stage("showcase-delivery-headers");
    expect(listed.document.headers()["x-robots-tag"]).toBe("noindex, nofollow");
    expect(Object.keys(await listed.document.allHeaders())).not.toContain(
      "set-cookie",
    );
    await expect(anonymous.locator('meta[name="robots"]')).toHaveAttribute(
      "content",
      "noindex, nofollow",
    );
    expect(listed.listing.headers()["cache-control"]).toContain("no-store");
    expect(Object.keys(await listed.listing.allHeaders())).not.toContain(
      "set-cookie",
    );
    expect(await publicContext.cookies(ORIGIN)).toEqual([]);

    stage("showcase-sign-in-controls");
    await signInSwitch.press("Space");
    await expect(showcaseSwitch).not.toBeChecked();
    await expect(showcaseSwitch).toBeDisabled();
    await expect(description).toContainText(
      "Not available while Require sign-in to view is on.",
    );
    revokingPublic = true;
    await submitPublishDialog(page, dialog, id);
    stage("showcase-sign-in-reload");
    await page.reload();
    await waitForHydration(page);
    dialog = await openPublishDialog(page);
    await expect(signInSwitch).toBeChecked();
    await expect(showcaseSwitch).not.toBeChecked();
    await expect(showcaseSwitch).toBeDisabled();
    await expect(description).toContainText(
      "Not available while Require sign-in to view is on.",
    );
    await expect(dialog.getByTestId("publish-showcase-status")).toHaveCount(0);
    stage("showcase-sign-in-listing");
    expect((await readListing(anonymous)).slugs).not.toContain(slug);

    stage("showcase-sign-in-off-controls");
    await signInSwitch.press("Space");
    await expect(signInSwitch).not.toBeChecked();
    await expect(showcaseSwitch).toBeEnabled();
    await expect(showcaseSwitch).not.toBeChecked();
    await submitPublishDialog(page, dialog, id);
    stage("showcase-sign-in-off-reload");
    await page.reload();
    await waitForHydration(page);
    dialog = await openPublishDialog(page);
    await expect(showcaseSwitch).toBeEnabled();
    await expect(showcaseSwitch).not.toBeChecked();
    stage("showcase-sign-in-off-listing");
    expect((await readListing(anonymous)).slugs).not.toContain(slug);

    stage("showcase-reopt-in-controls");
    revokingPublic = false;
    await showcaseSwitch.press("Space");
    await expect(showcaseSwitch).toBeChecked();
    await submitPublishDialog(page, dialog, id);
    stage("showcase-reopt-in-message");
    await expect(dialog.getByTestId("publish-showcase-status")).toContainText(
      "Shown in the community showcase.",
    );
    stage("showcase-reopt-in-listing");
    expect((await readListing(anonymous)).slugs).toContain(slug);
    stage("showcase-reopt-in-card");
    await waitForListedCard(anonymous, slug);
    stage("showcase-unpublish-controls");
    await publicSwitch.press("Space");
    await expect(showcaseSwitch).toBeDisabled();
    revokingPublic = true;
    await submitPublishDialog(page, dialog, id);
    stage("showcase-unpublished-listing");
    expect((await readListing(anonymous)).slugs).not.toContain(slug);
    stage("showcase-republish-controls");
    revokingPublic = false;
    await publicSwitch.press("Space");
    await expect(showcaseSwitch).toBeEnabled();
    await expect(showcaseSwitch).not.toBeChecked();
    await submitPublishDialog(page, dialog, id);
    stage("showcase-republish-reload");
    await expect(dialog.getByTestId("publish-showcase-status")).toHaveCount(0);
    await page.reload();
    await waitForHydration(page);
    dialog = await openPublishDialog(page);
    await expect(publicSwitch).toBeChecked();
    await expect(showcaseSwitch).not.toBeChecked();
    stage("showcase-republished-listing");
    expect((await readListing(anonymous)).slugs).not.toContain(slug);
  } catch (error) {
    failedStage = currentStage;
    throw error;
  } finally {
    stage("showcase-cleanup-public-context");
    if (publicContext !== undefined) await publicContext.close();
    if (resumeID !== undefined) {
      stage("showcase-cleanup-resume");
      await page.close();
    }
  }

  stage("showcase-final-certificate-errors");
  expect(counters.certificateErrors).toBe(0);
  stage("showcase-final-console-errors");
  expect(counters.consoleErrors).toBe(0);
  stage("showcase-final-external-requests");
  expect(counters.externalRequests).toBe(0);
  stage("showcase-final-page-errors");
  expect(counters.pageErrors).toBe(0);
});

const DELETION_TEST_TITLE =
  "proves deleting a published resume ends its owner and public reads";

function isExpectedDeletedConsole(
  message: ConsoleMessage,
  resumeID: string,
  slug: string,
): boolean {
  if (isExpectedRevokedPublicConsole(message, slug)) return true;
  let location: URL;
  try {
    location = new URL(message.location().url);
  } catch {
    return false;
  }
  const status = httpFailureStatus(message.text());
  return (
    status !== null &&
    REVOKED_PUBLIC_STATUSES.includes(status) &&
    location.origin === ORIGIN &&
    location.pathname === `/api/v1/resumes/${resumeID}` &&
    location.search === ""
  );
}

async function ownerDelete(
  page: Page,
  id: string,
): Promise<{ read: number; deleted: number }> {
  const token = await freshCSRF(page);
  return page.evaluate(
    async ({ id, schemaVersion, token }) => {
      const path = `/api/v1/resumes/${encodeURIComponent(id)}`;
      const read = await fetch(path, {
        cache: "no-store",
        credentials: "include",
        headers: { "X-Resume-Schema-Version": schemaVersion },
      });
      const removed = await fetch(path, {
        cache: "no-store",
        credentials: "include",
        headers: {
          "Idempotency-Key": crypto.randomUUID(),
          "If-Match": read.headers.get("etag") ?? "",
          "X-CSRF-Token": token,
          "X-Resume-Schema-Version": schemaVersion,
        },
        method: "DELETE",
      });
      return { read: read.status, deleted: removed.status };
    },
    { id, schemaVersion: SCHEMA_VERSION, token },
  );
}

async function ownerReadStatus(page: Page, id: string): Promise<number> {
  return page.evaluate(
    async ({ id, schemaVersion }) =>
      (
        await fetch(`/api/v1/resumes/${encodeURIComponent(id)}`, {
          cache: "no-store",
          credentials: "include",
          headers: { "X-Resume-Schema-Version": schemaVersion },
        })
      ).status,
    { id, schemaVersion: SCHEMA_VERSION },
  );
}

async function ownerCreate(
  page: Page,
  title: string,
  fullName: string,
): Promise<{ status: number; id: string | undefined }> {
  const token = await freshCSRF(page);
  return page.evaluate(
    async ({ fullName, schemaVersion, title, token }) => {
      const created = await fetch("/api/v1/resumes", {
        body: JSON.stringify({
          title,
          document: {
            schemaVersion: Number(schemaVersion),
            personalDetails: {
              fullName,
              details: [],
            },
            content: {
              work: {
                sectionType: "work",
                iconKey: "briefcase",
                entries: [
                  {
                    id: crypto.randomUUID(),
                    jobTitle: "Engineer",
                    employer: "Example Corp",
                  },
                ],
              },
            },
            customization: {
              font: { family: "inter", baseSizePx: 14 },
              colors: {
                primary: "#1a1a1a",
                text: "#1a1a1a",
                background: "#ffffff",
              },
              spacing: { sectionGap: 16, entryGap: 8, lineHeight: 1.4 },
              heading: { style: "normal", showRule: false },
              layout: {
                columns: 1,
                sections: { main: ["work"], sidebar: [] },
              },
              sectionDisplay: {
                skill: { style: "text" },
                language: { style: "text" },
              },
              pageFormat: "a4",
              dateFormat: "MM/YYYY",
            },
          },
        }),
        cache: "no-store",
        credentials: "include",
        headers: {
          "Content-Type": "application/json",
          "Idempotency-Key": crypto.randomUUID(),
          "X-CSRF-Token": token,
          "X-Resume-Schema-Version": schemaVersion,
        },
        method: "POST",
      });
      const body = (await created.json()) as { data?: { id?: unknown } };
      const id = body.data?.id;
      return {
        status: created.status,
        id: typeof id === "string" ? id : undefined,
      };
    },
    { fullName, schemaVersion: SCHEMA_VERSION, title, token },
  );
}

async function ownerPublish(
  page: Page,
  id: string,
  slug: string,
): Promise<number> {
  const token = await freshCSRF(page);
  return page.evaluate(
    async ({ id, schemaVersion, slug, token }) => {
      const path = `/api/v1/resumes/${encodeURIComponent(id)}`;
      const read = await fetch(path, {
        cache: "no-store",
        credentials: "include",
        headers: { "X-Resume-Schema-Version": schemaVersion },
      });
      const published = await fetch(`${path}/publish`, {
        body: JSON.stringify({
          slug,
          live: true,
          downloadEnabled: false,
          seoGeoEnabled: false,
        }),
        cache: "no-store",
        credentials: "include",
        headers: {
          "Content-Type": "application/json",
          "Idempotency-Key": crypto.randomUUID(),
          "If-Match": read.headers.get("etag") ?? "",
          "X-CSRF-Token": token,
          "X-Resume-Schema-Version": schemaVersion,
        },
        method: "POST",
      });
      return published.status;
    },
    { id, schemaVersion: SCHEMA_VERSION, slug, token },
  );
}

test(DELETION_TEST_TITLE, async ({ browser, context, page }) => {
  stage("deletion-setup");
  await pinEnglish(context);
  const counters = newDiagnosticCounters();
  const slug = `deletion-${crypto.randomUUID().slice(0, 8)}`;
  let resumeID: string | undefined;
  let publicContext: BrowserContext | undefined;
  let deleting = false;
  const hooks = {
    countConsoleError: (message: ConsoleMessage): boolean =>
      !isExpectedAnonymousMeConsole(message.text(), message.location().url) &&
      !(
        deleting &&
        resumeID !== undefined &&
        isExpectedDeletedConsole(message, resumeID, slug)
      ),
    onCountedConsoleError: (message: ConsoleMessage): void => {
      recordCountedConsole(message, resumeID, slug);
    },
  };
  await installPublicGuards(context, counters);
  pageDiagnosticsAttacher(counters, hooks)(page);

  try {
    stage("deletion-login");
    await signInWithPassword(page);
    stage("deletion-create-status");
    const created = await ownerCreate(
      page,
      `Deletion proof ${crypto.randomUUID()}`,
      "Deletion proof resume",
    );
    createdID = resumeID = created.id;
    expect(created.status).toBe(201);
    const id = created.id ?? "";
    stage("deletion-publish-status");
    expect(await ownerPublish(page, id, slug)).toBe(200);

    stage("deletion-public-before-status");
    publicContext = await browser.newContext();
    await installPublicGuards(publicContext, counters);
    let publicPage = await publicContext.newPage();
    pageDiagnosticsAttacher(counters, hooks)(publicPage);
    expect((await publicPage.goto(`${ORIGIN}/${slug}`))?.status()).toBe(200);
    // An open public page reloads when its resume goes away. Closing it
    // first leaves one read of the deleted page, the one asserted below.
    await publicContext.close();
    publicContext = undefined;

    stage("deletion-request-status");
    deleting = true;
    expect(await ownerDelete(page, id)).toEqual({ read: 200, deleted: 204 });
    stage("deletion-owner-read-status");
    expect(await ownerReadStatus(page, id)).toBe(404);
    stage("deletion-public-status");
    publicContext = await browser.newContext();
    await installPublicGuards(publicContext, counters);
    publicPage = await publicContext.newPage();
    pageDiagnosticsAttacher(counters, hooks)(publicPage);
    const gone = await publicPage.goto(`${ORIGIN}/${slug}`);
    expect(REVOKED_PUBLIC_STATUSES).toContain(gone?.status() ?? 0);
  } catch (error) {
    failedStage = currentStage;
    throw error;
  } finally {
    stage("deletion-cleanup");
    if (publicContext !== undefined) await publicContext.close();
    if (resumeID !== undefined) {
      await page.close();
    }
  }

  stage("deletion-final-certificate-errors");
  expect(counters.certificateErrors).toBe(0);
  stage("deletion-final-console-errors");
  expect(counters.consoleErrors).toBe(0);
  stage("deletion-final-external-requests");
  expect(counters.externalRequests).toBe(0);
  stage("deletion-final-page-errors");
  expect(counters.pageErrors).toBe(0);
});
