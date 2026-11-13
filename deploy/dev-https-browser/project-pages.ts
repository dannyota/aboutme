import type { ConsoleMessage, Page, Response } from '@playwright/test';

import { freshCSRF } from './editor-fixtures';
import { ALLOWED_ORIGIN, httpFailureStatus } from './network-policy';

const ORIGIN = ALLOWED_ORIGIN;
const SCHEMA_VERSION = '6';
const VIEW_PASS_COOKIE = '__Host-view-pass';
const GATE_PROVIDERS: readonly string[] = ['google', 'linkedin'];
const STYLESHEET_HREF =
  /^\/_nuxt\/assets\/(print-fonts|project-pages)\.css\?v=([A-Za-z0-9_-]+)$/u;

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

export async function ownerSetEntryHidden(
  page: Page,
  resumeID: string,
  linked: LinkedEntry,
  hidden: boolean,
): Promise<number> {
  if (linked.sectionKey === undefined || linked.entry === undefined) {
    throw new Error('linked entry is unavailable');
  }
  return ownerWriteEntry(page, resumeID, linked.sectionKey, { ...linked.entry, isHidden: hidden });
}

export async function ownerWriteEntry(
  page: Page,
  resumeID: string,
  sectionKey: string,
  entry: Readonly<Record<string, unknown>>,
): Promise<number> {
  const token = await freshCSRF(page);
  return page.evaluate(
    async ({ entry, resumeID, schemaVersion, sectionKey, token }) => {
      const path = `/api/v1/resumes/${encodeURIComponent(resumeID)}`;
      const read = await fetch(path, {
        cache: 'no-store',
        credentials: 'include',
        headers: { 'X-Resume-Schema-Version': schemaVersion },
      });
      const written = await fetch(`${path}/entries/${encodeURIComponent(sectionKey)}`, {
        body: JSON.stringify({ entry }),
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
    { entry, resumeID, schemaVersion: SCHEMA_VERSION, sectionKey, token },
  );
}

export interface PublishState {
  readonly slug: string;
  readonly live: boolean;
  readonly signInToView: boolean;
  readonly download?: boolean;
}

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
          downloadEnabled: state.download ?? false,
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

export async function openPublic(page: Page, path: string): Promise<Response> {
  const response = await page.goto(`${ORIGIN}${path}`);
  if (response === null) throw new Error('public document is unavailable');
  return response;
}

export interface NotFoundShape {
  readonly status: number;
  readonly contentType: string | null;
  readonly cacheControl: string | null;
  readonly contentSecurityPolicy: string | null;
  readonly robotsHeader: string | null;
  readonly robotsMeta: string | null;
  readonly bytes: string;
}

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

export interface GateFacts {
  readonly noStore: boolean;
  readonly robotsHeader: string | null;
  readonly gates: number;
  readonly projectMains: number;
  readonly providers: number;
  readonly providersWithPage: number;
  readonly revealsName: boolean;
}

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

export function gateProviderLink(page: Page, slug: string, gatePage: string) {
  return page.locator(
    `a.gate-provider[href="/api/v1/auth/google/start?purpose=view&slug=${slug}&page=${gatePage}"]`,
  );
}

export async function hasViewPass(page: Page): Promise<boolean> {
  return (await page.context().cookies(ORIGIN)).some((cookie) => cookie.name === VIEW_PASS_COOKIE);
}
