// Owner API calls and page readings for the public project pages proof in
// publish.spec.ts (docs/design/projects-contract.md "Routes and dispatch",
// "Representations", "Render envelope"; docs/design/ui/project-pages.md).
// Every reading returns closed facts the spec compares; none returns a URL,
// slug, name, header value or page text for evidence.
import type { ConsoleMessage, Page, Response } from '@playwright/test';

import { freshCSRF } from './editor-fixtures';
import { ALLOWED_ORIGIN, httpFailureStatus } from './network-policy';

const ORIGIN = ALLOWED_ORIGIN;
const SCHEMA_VERSION = '6';
const VIEW_PASS_COOKIE = '__Host-view-pass';
// The gate offers these providers in the harness, as public.spec.ts proves.
const GATE_PROVIDERS: readonly string[] = ['google', 'linkedin'];
// The two versioned stylesheets a project page may link
// (docs/design/projects-contract.md "Render envelope").
const STYLESHEET_HREF =
  /^\/_nuxt\/assets\/(print-fonts|project-pages)\.css\?v=([A-Za-z0-9_-]+)$/u;

// The library project this proof creates: one English text with a name, so
// the project is available wherever an entry links it.
export interface ProjectDocument {
  readonly defaultLanguage: string;
  readonly technologies: readonly string[];
  readonly text: Readonly<Record<string, { readonly name: string; readonly summary: string }>>;
}

export function projectDocument(name: string): ProjectDocument {
  return {
    defaultLanguage: 'en',
    technologies: ['Go', 'TypeScript'],
    text: { en: { name, summary: 'A fictional project for the public page proof.' } },
  };
}

// ownerCreateProject creates a library project at slug through the owner
// API. It returns the status and the new project's ID.
export async function ownerCreateProject(
  page: Page,
  slug: string,
  document: ProjectDocument,
): Promise<{ status: number; id: string | undefined }> {
  const token = await freshCSRF(page);
  return page.evaluate(
    async ({ document, slug, token }) => {
      const created = await fetch('/api/v1/projects', {
        body: JSON.stringify({ ...document, slug }),
        cache: 'no-store',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
          'Idempotency-Key': crypto.randomUUID(),
          'X-CSRF-Token': token,
        },
        method: 'POST',
      });
      const body = (await created.json()) as { data?: { id?: unknown } };
      const id = body.data?.id;
      return { status: created.status, id: typeof id === 'string' ? id : undefined };
    },
    { document, slug, token },
  );
}

// ownerRenameProject replaces the project whole with its document at a new
// slug, the rename the contract defines. It returns the replace status.
export async function ownerRenameProject(
  page: Page,
  id: string,
  slug: string,
  document: ProjectDocument,
): Promise<number> {
  const token = await freshCSRF(page);
  return page.evaluate(
    async ({ document, id, slug, token }) => {
      const path = `/api/v1/projects/${encodeURIComponent(id)}`;
      const read = await fetch(path, { cache: 'no-store', credentials: 'include' });
      const replaced = await fetch(path, {
        body: JSON.stringify({ ...document, slug }),
        cache: 'no-store',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
          'Idempotency-Key': crypto.randomUUID(),
          'If-Match': read.headers.get('etag') ?? '',
          'X-CSRF-Token': token,
        },
        method: 'PUT',
      });
      return replaced.status;
    },
    { document, id, slug, token },
  );
}

// deleteRecordedProject deletes a project this proof created, if it still
// exists. It runs in the cleanup session, outside every counted page.
export async function deleteRecordedProject(page: Page, id: string): Promise<void> {
  const token = await freshCSRF(page);
  const statuses = await page.evaluate(
    async ({ id, token }) => {
      const path = `/api/v1/projects/${encodeURIComponent(id)}`;
      const read = await fetch(path, { cache: 'no-store', credentials: 'include' });
      if (read.status !== 200) return { read: read.status, deleted: 0 };
      const removed = await fetch(path, {
        cache: 'no-store',
        credentials: 'include',
        headers: {
          'Idempotency-Key': crypto.randomUUID(),
          'If-Match': read.headers.get('etag') ?? '',
          'X-CSRF-Token': token,
        },
        method: 'DELETE',
      });
      return { read: read.status, deleted: removed.status };
    },
    { id, token },
  );
  if (statuses.read === 404) return;
  if (statuses.read !== 200 || statuses.deleted !== 200) {
    throw new Error('recorded project deletion failed');
  }
}

export interface LinkedEntry {
  readonly status: number;
  readonly sectionKey: string | undefined;
  readonly entry: Readonly<Record<string, unknown>> | undefined;
}

// ownerLinkProject links the project to the resume as a new prefilled entry
// and returns the status with the linked entry and its section key.
export async function ownerLinkProject(
  page: Page,
  resumeID: string,
  projectID: string,
): Promise<LinkedEntry> {
  const token = await freshCSRF(page);
  return page.evaluate(
    async ({ projectID, resumeID, schemaVersion, token }) => {
      const path = `/api/v1/resumes/${encodeURIComponent(resumeID)}`;
      const read = await fetch(path, {
        cache: 'no-store',
        credentials: 'include',
        headers: { 'X-Resume-Schema-Version': schemaVersion },
      });
      const linked = await fetch(`${path}/project-links`, {
        body: JSON.stringify({ projectId: projectID }),
        cache: 'no-store',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
          'Idempotency-Key': crypto.randomUUID(),
          'If-Match': read.headers.get('etag') ?? '',
          'X-CSRF-Token': token,
          'X-Resume-Schema-Version': schemaVersion,
        },
        method: 'POST',
      });
      const body = (await linked.json()) as {
        data?: { document?: { content?: Record<string, { entries?: Record<string, unknown>[] }> } };
      };
      for (const [sectionKey, section] of Object.entries(body.data?.document?.content ?? {})) {
        const entry = section.entries?.find((candidate) => candidate.projectId === projectID);
        if (entry !== undefined) return { status: linked.status, sectionKey, entry };
      }
      return { status: linked.status, sectionKey: undefined, entry: undefined };
    },
    { projectID, resumeID, schemaVersion: SCHEMA_VERSION, token },
  );
}

// ownerSetEntryHidden replaces the linked entry with its hidden flag set as
// given and returns the write status.
export async function ownerSetEntryHidden(
  page: Page,
  resumeID: string,
  linked: LinkedEntry,
  hidden: boolean,
): Promise<number> {
  if (linked.sectionKey === undefined || linked.entry === undefined) {
    throw new Error('linked entry is unavailable');
  }
  const token = await freshCSRF(page);
  return page.evaluate(
    async ({ entry, hidden, resumeID, schemaVersion, sectionKey, token }) => {
      const path = `/api/v1/resumes/${encodeURIComponent(resumeID)}`;
      const read = await fetch(path, {
        cache: 'no-store',
        credentials: 'include',
        headers: { 'X-Resume-Schema-Version': schemaVersion },
      });
      const written = await fetch(`${path}/entries/${encodeURIComponent(sectionKey)}`, {
        body: JSON.stringify({ entry: { ...entry, isHidden: hidden } }),
        cache: 'no-store',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
          'Idempotency-Key': crypto.randomUUID(),
          'If-Match': read.headers.get('etag') ?? '',
          'X-CSRF-Token': token,
          'X-Resume-Schema-Version': schemaVersion,
        },
        method: 'PATCH',
      });
      return written.status;
    },
    {
      entry: linked.entry,
      hidden,
      resumeID,
      schemaVersion: SCHEMA_VERSION,
      sectionKey: linked.sectionKey,
      token,
    },
  );
}

export interface PublishState {
  readonly slug: string;
  readonly live: boolean;
  readonly signInToView: boolean;
}

// ownerSetPublishState sends one complete publish command with downloads and
// discovery off and returns its status.
export async function ownerSetPublishState(
  page: Page,
  resumeID: string,
  state: PublishState,
): Promise<number> {
  const token = await freshCSRF(page);
  return page.evaluate(
    async ({ resumeID, schemaVersion, state, token }) => {
      const path = `/api/v1/resumes/${encodeURIComponent(resumeID)}`;
      const read = await fetch(path, {
        cache: 'no-store',
        credentials: 'include',
        headers: { 'X-Resume-Schema-Version': schemaVersion },
      });
      const published = await fetch(`${path}/publish`, {
        body: JSON.stringify({
          slug: state.slug,
          live: state.live,
          downloadEnabled: false,
          seoGeoEnabled: false,
          signInToView: state.signInToView,
        }),
        cache: 'no-store',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
          'Idempotency-Key': crypto.randomUUID(),
          'If-Match': read.headers.get('etag') ?? '',
          'X-CSRF-Token': token,
          'X-Resume-Schema-Version': schemaVersion,
        },
        method: 'POST',
      });
      return published.status;
    },
    { resumeID, schemaVersion: SCHEMA_VERSION, state, token },
  );
}

// openPublic navigates page to a same-origin path and returns the document
// response.
export async function openPublic(page: Page, path: string): Promise<Response> {
  const response = await page.goto(`${ORIGIN}${path}`);
  if (response === null) throw new Error('public document is unavailable');
  return response;
}

// The parts of a not-found answer a reader or crawler can tell apart.
export interface NotFoundShape {
  readonly status: number;
  readonly contentType: string | null;
  readonly cacheControl: string | null;
  readonly contentSecurityPolicy: string | null;
  readonly robotsHeader: string | null;
  readonly robotsMeta: string | null;
  readonly bytes: string;
}

// readNotFoundShape opens path and returns its status, the headers that
// matter, the robots meta and the exact body bytes, for comparison with an
// unknown resume's answer.
export async function readNotFoundShape(page: Page, path: string): Promise<NotFoundShape> {
  const response = await openPublic(page, path);
  const headers = await response.allHeaders();
  return {
    status: response.status(),
    contentType: headers['content-type'] ?? null,
    cacheControl: headers['cache-control'] ?? null,
    contentSecurityPolicy: headers['content-security-policy'] ?? null,
    robotsHeader: headers['x-robots-tag'] ?? null,
    robotsMeta: await page.evaluate(
      () => document.querySelector('meta[name="robots"]')?.getAttribute('content') ?? null,
    ),
    bytes: (await response.body()).toString('base64'),
  };
}

// isExpectedNotFoundConsole matches the line the browser logs for a
// document this test reads as the uniform 404, and only for an address the
// test has already asked for as one.
export function isExpectedNotFoundConsole(
  message: ConsoleMessage,
  paths: ReadonlySet<string>,
): boolean {
  let location: URL;
  try {
    location = new URL(message.location().url);
  } catch {
    return false;
  }
  return (
    httpFailureStatus(message.text()) === 404
    && location.origin === ORIGIN
    && paths.has(location.pathname)
    && location.search === ''
  );
}

// What a served project page or list shows about its delivery rules.
export interface ProjectPageFacts {
  readonly styleSources: readonly string[];
  readonly unsafeInline: boolean;
  readonly robotsHeader: string | null;
  readonly stylesheets: readonly string[];
  readonly styleVersions: number;
  readonly loadedStylesheets: number;
  readonly styleAttributes: number;
  readonly styleElements: number;
  readonly scripts: number;
  readonly projectMains: number;
  readonly gates: number;
}

// projectPageFacts reads a served project page: its CSP style directive,
// its robots header, which versioned stylesheets it links and how many of
// them loaded, and whether any inline style or script is in the DOM.
export async function projectPageFacts(page: Page, response: Response): Promise<ProjectPageFacts> {
  const headers = await response.allHeaders();
  const csp = headers['content-security-policy'] ?? '';
  const dom = await page.evaluate(() => ({
    links: Array.from(
      document.querySelectorAll<HTMLLinkElement>('link[rel="stylesheet"]'),
      (link) => ({
        href: link.getAttribute('href') ?? '',
        loaded: (link.sheet?.cssRules.length ?? 0) > 0,
      }),
    ),
    styleAttributes: document.querySelectorAll('[style]').length,
    styleElements: document.querySelectorAll('style').length,
    scripts: document.querySelectorAll('script').length,
    projectMains: document.querySelectorAll('main#public-project').length,
    gates: document.querySelectorAll('#public-gate').length,
  }));
  const sheets = dom.links.map(({ href, loaded }) => {
    const match = STYLESHEET_HREF.exec(href);
    return { name: match?.[1] ?? 'other', version: match?.[2] ?? '', loaded };
  });
  return {
    styleSources: csp
      .split(';')
      .map((directive) => directive.trim())
      .filter((directive) => /^style-src(?:\s|$)/u.test(directive)),
    unsafeInline: csp.includes("'unsafe-inline'"),
    robotsHeader: headers['x-robots-tag'] ?? null,
    stylesheets: sheets.map(({ name }) => name).sort(),
    styleVersions: new Set(sheets.map(({ version }) => version)).size,
    loadedStylesheets: sheets.filter(({ loaded }) => loaded).length,
    styleAttributes: dom.styleAttributes,
    styleElements: dom.styleElements,
    scripts: dom.scripts,
    projectMains: dom.projectMains,
    gates: dom.gates,
  };
}

// The gate as it stands in for a project address.
export interface GateFacts {
  readonly noStore: boolean;
  readonly robotsHeader: string | null;
  readonly gates: number;
  readonly projectMains: number;
  readonly providers: number;
  readonly providersWithPage: number;
  readonly revealsName: boolean;
}

// gateFacts reads a gate page served for /{slug}/{gatePage}: its caching and
// robots headers, whether it is the gate and not a project page, whether
// each offered provider link carries exactly that page, and whether the
// project's name appears anywhere in it.
export async function gateFacts(
  page: Page,
  response: Response,
  slug: string,
  gatePage: string,
  projectName: string,
): Promise<GateFacts> {
  const headers = await response.allHeaders();
  const hrefs = await page
    .locator('a.gate-provider')
    .evaluateAll((links) => links.map((link) => link.getAttribute('href') ?? ''));
  const expected = new Set(
    GATE_PROVIDERS.map(
      (provider) => `/api/v1/auth/${provider}/start?purpose=view&slug=${slug}&page=${gatePage}`,
    ),
  );
  return {
    noStore: (headers['cache-control'] ?? '')
      .split(',')
      .map((part) => part.trim())
      .includes('no-store'),
    robotsHeader: headers['x-robots-tag'] ?? null,
    gates: await page.locator('#public-gate').count(),
    projectMains: await page.locator('main#public-project').count(),
    providers: hrefs.length,
    providersWithPage: hrefs.filter((href) => expected.has(href)).length,
    revealsName: (await response.text()).includes(projectName),
  };
}

// gateProviderLink is the gate's Google link for /{slug}/{gatePage}.
export function gateProviderLink(page: Page, slug: string, gatePage: string) {
  return page.locator(
    `a.gate-provider[href="/api/v1/auth/google/start?purpose=view&slug=${slug}&page=${gatePage}"]`,
  );
}

// hasViewPass reports whether the context holds a view pass cookie, without
// reading its value.
export async function hasViewPass(page: Page): Promise<boolean> {
  return (await page.context().cookies(ORIGIN)).some((cookie) => cookie.name === VIEW_PASS_COOKIE);
}
