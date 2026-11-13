import { CURRENT_VERSION } from '@aboutme/schema/released';
import { expect, test, type Page } from '@playwright/test';

import { expectScreen, type ScreenContract } from './display-assertions';
import { screenMutationTests } from './style-mutation';
import {
  chromeScreenContract,
  denyExternalRequests,
  mockSignedInSession,
} from './support';

// The workspace Projects screens (docs/design/ui/projects.md) against a
// stateful fake of the owner project API (docs/api/openapi.yaml, /projects)
// and of the resume routes a link, refresh or personal detail writes through.

interface ProjectText {
  name?: string;
  role?: string;
  summary?: string;
  outcome?: string;
  description?: string;
}

interface Project {
  id: string;
  slug: string;
  revision: string;
  createdAt: string;
  updatedAt: string;
  defaultLanguage: string;
  text: Record<string, ProjectText>;
  technologies?: string[];
  repositoryUrl?: string;
  demoUrl?: string;
  linkedResumeIds: string[];
}

interface Write {
  readonly method: string;
  readonly path: string;
  readonly ifMatch: string | null;
  readonly key: string | null;
  readonly body: unknown;
}

const FIRST = '0190f000-0000-7000-8000-000000000001';
const SECOND = '0190f000-0000-7000-8000-000000000002';
const CREATED = '0190f000-0000-7000-8000-0000000000aa';
const HEADERS = { 'Cache-Control': 'no-store, no-transform' };

function seed(id: string, name: string, updatedAt: string): Project {
  return {
    id,
    slug: name.toLowerCase().replaceAll(' ', '-'),
    revision: '1',
    createdAt: '2026-10-01T00:00:00Z',
    updatedAt,
    defaultLanguage: 'en',
    text: { en: { name, role: 'Lead engineer' } },
    technologies: ['Go', 'Vue', 'Postgres', 'Caddy'],
    linkedResumeIds: [],
  };
}

type ResumeEntry = Record<string, unknown> & { id: string; projectId?: string };

interface ResumeRow {
  id: string;
  title: string;
  lng: string;
  slug: string | null;
  revision: number;
  /** The project section's entries; null when the resume has none. */
  entries: ResumeEntry[] | null;
  /** The last personal details saved; the fixed details until then. */
  personalDetails?: Record<string, unknown>;
}

interface LibraryOptions {
  /** The account has a second factor, so a password only opens it. */
  readonly enrolled?: boolean;
  readonly resumes?: readonly ResumeRow[];
  /** Holds each refresh response this long, as a slow server would. */
  readonly refreshDelayMs?: number;
  /** Resumes another tab saves just before their first link arrives. */
  readonly savedElsewhere?: readonly string[];
  /** Headlines another tab saves just before a resume's first write. */
  readonly headlinesElsewhere?: Readonly<Record<string, string>>;
  /** A second tab of the same session, served by the same fake. */
  readonly otherTab?: Page;
  /** Resume writes whose connection drops before they reach the server. */
  readonly lostWrites?: number;
  /** Resume writes refused for a stale CSRF token. */
  readonly csrfRejectedWrites?: number;
}

const BACKEND = '0190f000-0000-7000-8000-0000000000b1';
const FRONTEND = '0190f000-0000-7000-8000-0000000000b2';
const DATA = '0190f000-0000-7000-8000-0000000000b3';

/** Three resumes; `linked` maps a resume to the project it already shows. */
function resumeRows(linked: Record<string, string> = {}): ResumeRow[] {
  const entries = (id: string): ResumeEntry[] => {
    const projectId = linked[id];
    if (projectId === undefined) return [];
    return [{ id: `${id.slice(0, -2)}e${id.slice(-1)}`, title: 'Linked',
      projectId }];
  };
  return [
    { id: BACKEND, title: 'Backend', lng: 'en', slug: 'ada-backend',
      revision: 1, entries: entries(BACKEND) },
    { id: FRONTEND, title: 'Frontend', lng: 'vi', slug: null,
      revision: 1, entries: entries(FRONTEND) },
    // Data has no project section until a link creates one.
    { id: DATA, title: 'Data', lng: 'und', slug: null,
      revision: 1,
      entries: linked[DATA] === undefined ? null : entries(DATA) },
  ];
}

function resumeDocument(row: ResumeRow) {
  return {
    schemaVersion: CURRENT_VERSION,
    personalDetails: row.personalDetails
      ?? { fullName: 'Ada Lovelace', details: [] },
    content: row.entries === null
      ? {}
      : {
          projects: {
            sectionType: 'project',
            displayName: 'Projects',
            entries: row.entries,
          },
        },
    customization: {
      font: { family: 'inter', baseSizePx: 14 },
      colors: { primary: '#1a1a1a', text: '#1a1a1a', background: '#ffffff' },
      spacing: { sectionGap: 16, entryGap: 8, lineHeight: 1.4 },
      heading: { style: 'normal', showRule: false },
      layout: {
        columns: 1,
        sections: {
          main: row.entries === null ? [] : ['projects'], sidebar: [],
        },
      },
      sectionDisplay: {
        skill: { style: 'text' }, language: { style: 'text' },
      },
      pageFormat: 'a4',
      dateFormat: 'MM/YYYY',
    },
  };
}

function resumeData(row: ResumeRow, withDocument: boolean) {
  return {
    id: row.id, title: row.title, lng: row.lng, live: row.slug !== null,
    downloadEnabled: false, seoGeoEnabled: false, signInToView: false,
    slug: row.slug, publicTitle: null, faviconEmoji: null,
    schemaVersion: CURRENT_VERSION,
    createdAt: '2026-10-01T00:00:00Z', updatedAt: '2026-10-01T00:00:00Z',
    revision: String(row.revision),
    ...(withDocument ? { document: resumeDocument(row) } : {}),
  };
}

/** The server's prefill: the resume language's text, else the default. */
function prefill(entry: ResumeEntry, project: Project, lng: string) {
  const primary = lng.toLowerCase().split('-')[0] ?? '';
  const text = (primary === 'und' ? undefined : project.text[primary])
    ?? project.text[project.defaultLanguage] ?? {};
  const link = project.repositoryUrl ?? project.demoUrl;
  return {
    id: entry.id,
    projectId: entry.projectId,
    ...(text.name === undefined ? {} : { title: text.name }),
    ...(text.role === undefined ? {} : { subtitle: text.role }),
    ...(link === undefined ? {} : { link }),
    ...(text.summary === undefined
      ? {}
      : { description: `<p>${text.summary}</p>` }),
  };
}

async function fakeLibrary(
  tabs: readonly Page[],
  initial: readonly Project[],
  options: LibraryOptions = {},
) {
  const routeAll = async (...args: Parameters<Page['route']>) => {
    for (const tab of tabs) await tab.route(...args);
  };
  const projects = new Map(
    initial.map((item) => [item.id, structuredClone(item)]),
  );
  const resumes = new Map((options.resumes ?? []).map((row) =>
    [row.id, structuredClone(row)]));
  const linkedTo = (id: string) => [...resumes.values()]
    .filter((row) => row.entries?.some((entry) => entry.projectId === id))
    .map((row) => row.id);
  const shown = (project: Project): Project =>
    ({ ...project, linkedResumeIds: linkedTo(project.id) });
  const writes: Write[] = [];
  const resumeWrites: Write[] = [];
  const savedElsewhere = new Set(options.savedElsewhere ?? []);
  const headlinesElsewhere = new Map(
    Object.entries(options.headlinesElsewhere ?? {}),
  );
  let lostWrites = options.lostWrites ?? 0;
  let csrfRejectedWrites = options.csrfRejectedWrites ?? 0;
  let nextEntry = 0;
  const reauths: string[] = [];
  let reauthenticated = false;
  const problem = (status: number, code: string, details?: unknown) => ({
    status,
    headers: HEADERS,
    json: {
      error: { code, message: 'fixed', ...(details ? { details } : {}) },
    },
  });
  await routeAll('**/api/v1/auth/password/reauth', async (route) => {
    reauths.push(route.request().method());
    if (options.enrolled) {
      await route.fulfill({
        status: 202, headers: HEADERS,
        json: { data: { secondFactorRequired: true } },
      });
      return;
    }
    reauthenticated = true;
    await route.fulfill({ status: 204 });
  });
  await routeAll(/\/api\/v1\/projects(?:\/[^/?]+)?$/u, async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    const id = path.split('/')[4];
    const method = request.method();
    if (method !== 'GET') {
      writes.push({
        method, path,
        ifMatch: request.headers()['if-match'] ?? null,
        key: request.headers()['idempotency-key'] ?? null,
        body: request.postData() ? request.postDataJSON() : null,
      });
    }
    if (id === undefined && method === 'GET') {
      await route.fulfill({
        headers: HEADERS,
        json: { data: { projects: [...projects.values()].map(shown) } },
      });
      return;
    }
    if (id === undefined && method === 'POST') {
      const body = request.postDataJSON() as Project;
      const created: Project = {
        ...body, id: CREATED, slug: 'shop-site', revision: '1',
        createdAt: '2026-11-11T00:00:00Z', updatedAt: '2026-11-11T00:00:00Z',
        linkedResumeIds: [],
      };
      projects.set(CREATED, created);
      await route.fulfill({
        status: 201, headers: HEADERS, json: { data: created },
      });
      return;
    }
    const current = id === undefined ? undefined : projects.get(id);
    if (current === undefined) {
      await route.fulfill(problem(404, 'project_not_found'));
      return;
    }
    if (method === 'GET') {
      await route.fulfill({ headers: HEADERS, json: { data: shown(current) } });
      return;
    }
    if (request.headers()['if-match'] !== `"r${current.revision}"`) {
      await route.fulfill(problem(412, 'revision_mismatch', {
        revision: current.revision, project: current,
      }));
      return;
    }
    const body = method === 'PUT' ? request.postDataJSON() as Project : null;
    const releases = method === 'DELETE' || body?.slug !== current.slug;
    if (releases && !reauthenticated) {
      await route.fulfill(problem(403, 'reauth_required'));
      return;
    }
    if (method === 'DELETE') {
      const changedResumes = linkedTo(current.id).map((resumeId) => {
        const row = resumes.get(resumeId)!;
        row.entries = row.entries!.map(({ projectId: _, ...entry }) => entry);
        row.revision += 1;
        return { id: row.id, revision: String(row.revision) };
      });
      projects.delete(current.id);
      await route.fulfill({
        headers: HEADERS, json: { data: { changedResumes } },
      });
      return;
    }
    const next: Project = {
      ...body!, id: current.id, createdAt: current.createdAt,
      revision: String(Number(current.revision) + 1),
      updatedAt: '2026-11-11T01:00:00Z', linkedResumeIds: [],
    };
    projects.set(current.id, next);
    // Every replacement in these tests changes the project, so each
    // published resume linking it advances its revision and keeps its
    // document.
    const changedResumes = linkedTo(current.id)
      .map((resumeId) => resumes.get(resumeId)!)
      .filter((row) => row.slug !== null)
      .map((row) => {
        row.revision += 1;
        return { id: row.id, revision: String(row.revision) };
      });
    await route.fulfill({
      headers: HEADERS,
      json: { data: { project: shown(next), changedResumes } },
    });
  });
  // A successful, immediately closed stream keeps the editor's live
  // connection quiet.
  await routeAll('**/api/v1/events', async (route) => {
    await route.fulfill({
      status: 200, contentType: 'text/event-stream', body: '',
    });
  });
  const written = (row: ResumeRow, withDocument = true) => ({
    headers: {
      ...HEADERS,
      'ETag': `"r${row.revision}"`,
      'X-Resume-Schema-Version': String(CURRENT_VERSION),
    },
    json: { data: resumeData(row, withDocument) },
  });
  await routeAll((url) => url.pathname.startsWith('/api/v1/resumes'),
    async (route) => {
      const request = route.request();
      const parts = new URL(request.url()).pathname.split('/');
      const method = request.method();
      if (parts.length === 4 && method === 'GET') {
        await route.fulfill({
          headers: {
            ...HEADERS, 'X-Resume-Schema-Version': String(CURRENT_VERSION),
          },
          json: {
            data: [...resumes.values()].map((row) => resumeData(row, false)),
          },
        });
        return;
      }
      const row = resumes.get(parts[4] ?? '');
      if (row === undefined) {
        await route.fulfill(problem(404, 'resume_not_found'));
        return;
      }
      if (parts.length === 5 && method === 'GET') {
        await route.fulfill(written(row));
        return;
      }
      resumeWrites.push({
        method, path: parts.join('/'),
        ifMatch: request.headers()['if-match'] ?? null,
        key: request.headers()['idempotency-key'] ?? null,
        body: request.postData() ? request.postDataJSON() : null,
      });
      if (lostWrites > 0) {
        lostWrites -= 1;
        await route.abort('failed');
        return;
      }
      if (csrfRejectedWrites > 0) {
        csrfRejectedWrites -= 1;
        await route.fulfill(problem(403, 'csrf_rejected'));
        return;
      }
      if (savedElsewhere.delete(row.id)) row.revision += 1;
      const headline = headlinesElsewhere.get(row.id);
      if (headlinesElsewhere.delete(row.id)) {
        row.personalDetails = {
          ...resumeDocument(row).personalDetails, headline,
        };
        row.revision += 1;
      }
      if (request.headers()['if-match'] !== `"r${row.revision}"`) {
        await route.fulfill(problem(412, 'revision_mismatch', {
          revision: String(row.revision), document: resumeDocument(row),
        }));
        return;
      }
      const entries = row.entries ?? [];
      if (parts[5] === 'personal-details') {
        row.personalDetails
          = request.postDataJSON() as Record<string, unknown>;
      } else if (parts[5] === 'project-links') {
        const { projectId, entryId } = request.postDataJSON() as {
          projectId: string;
          entryId?: string;
        };
        const project = projects.get(projectId);
        if (project === undefined) {
          await route.fulfill(problem(404, 'project_not_found'));
          return;
        }
        if (entryId !== undefined) {
          // An existing entry gains the reference and keeps its text.
          const index = entries.findIndex((entry) => entry.id === entryId);
          if (index === -1) {
            await route.fulfill(problem(404, 'entry_not_found'));
            return;
          }
          if (entries.some((entry) => entry.projectId !== undefined
            && (entry.projectId === projectId || entry.id === entryId))) {
            await route.fulfill(problem(409, 'project_already_linked'));
            return;
          }
          entries[index] = { ...entries[index]!, projectId };
          row.entries = entries;
          row.revision += 1;
          await route.fulfill(written(row));
          return;
        }
        nextEntry += 1;
        const id = `0190f000-0000-7000-8000-0000000001${
          String(nextEntry).padStart(2, '0')}`;
        row.entries = [
          ...entries, prefill({ id, projectId }, project, row.lng),
        ];
      } else {
        if (options.refreshDelayMs !== undefined) {
          await new Promise((resolve) =>
            setTimeout(resolve, options.refreshDelayMs));
        }
        const index = entries.findIndex((entry) => entry.id === parts[6]);
        const project = projects.get(entries[index]?.projectId ?? '');
        if (project === undefined) {
          await route.fulfill(problem(404, 'project_not_found'));
          return;
        }
        entries[index] = prefill(entries[index]!, project, row.lng);
      }
      row.revision += 1;
      await route.fulfill(written(row));
    });
  return {
    projects,
    resumes,
    writes,
    resumeWrites,
    reauths,
    /** The owner finishes the factor in the other tab. */
    completeFactor: () => {
      reauthenticated = true;
    },
  };
}

async function open(
  page: Page,
  path: string,
  initial: readonly Project[],
  options: LibraryOptions = {},
) {
  const tabs = options.otherTab === undefined
    ? [page]
    : [page, options.otherTab];
  for (const tab of tabs) await denyExternalRequests(tab);
  await page.context().addCookies([{
    name: 'aboutme-locale', value: 'en', url: 'http://127.0.0.1:20092',
  }]);
  for (const tab of tabs) await mockSignedInSession(tab);
  const library = await fakeLibrary(tabs, initial, options);
  await page.goto(path);
  await page.evaluate(() => document.fonts.ready);
  return library;
}

const library = () => [
  seed(FIRST, 'Shop site', '2026-11-01T00:00:00Z'),
  seed(SECOND, 'Ops tool', '2026-11-05T00:00:00Z'),
];

test('an empty list creates a project and opens its editor', async ({
  page,
}) => {
  const { writes } = await open(page, '/app/projects', []);
  const empty = page.getByTestId('projects-empty');
  await expect(empty).toContainText('No projects yet.');
  await expect(empty).toContainText(
    'Write each project once, then choose which resumes show it.',
  );

  await empty.getByRole('button', { name: 'New project' }).click();
  const dialog = page.getByRole('dialog', { name: 'New project' });
  await dialog.getByLabel('Name').fill('Shop site');
  await dialog.getByRole('button', { name: 'Create' }).click();

  await expect(page).toHaveURL(`/app/projects/${CREATED}`);
  await expect(page.getByTestId('project-title')).toHaveText('Shop site');
  expect(writes).toEqual([expect.objectContaining({
    method: 'POST', ifMatch: null, key: expect.any(String),
    body: { defaultLanguage: 'en', text: { en: { name: 'Shop site' } } },
  })]);
});

test('a filled list shows rows newest first with chips and actions', async ({
  page,
}) => {
  await open(page, '/app/projects', library());
  await expect(page.getByTestId('project-count')).toHaveText('2 / 30');
  const rows = page.getByTestId('project-list').locator(':scope > li');
  await expect(rows.locator('[data-project-link]'))
    .toHaveText(['Ops tool', 'Shop site']);
  const first = rows.first();
  await expect(first.locator('[data-project-technologies] li'))
    .toHaveText(['Go', 'Vue', 'Postgres', '+1']);
  await expect(first.locator('[data-project-languages] li'))
    .toHaveText(['EN · default']);
  await expect(first.locator('[data-project-role]'))
    .toHaveText('Lead engineer');
  await first.getByRole('button', { name: 'More actions for Ops tool' })
    .click();
  await expect(page.getByRole('menuitem'))
    .toHaveText(['Change address', 'Delete']);
  await page.keyboard.press('Escape');
  await first.locator('[data-project-link]').click();
  await expect(page).toHaveURL(`/app/projects/${SECOND}`);
});

test('the editor saves the whole document with the loaded revision', async ({
  page,
}) => {
  const { writes } = await open(page, `/app/projects/${FIRST}`, library());
  const save = page.locator('[data-action="project-save"]');
  await expect(save).toBeDisabled();
  await page.getByLabel('Role').fill('Staff engineer');
  await page.getByLabel('Demo link').fill('https://shop.example');
  await expect(page.getByTestId('project-save-bar')).toContainText('Unsaved');
  await save.click();

  await expect(page.locator('[data-state-mark="saved"]')).toHaveText('Saved');
  await expect(save).toBeDisabled();
  expect(writes).toEqual([expect.objectContaining({
    method: 'PUT', ifMatch: '"r1"', body: {
      slug: 'shop-site',
      demoUrl: 'https://shop.example',
      technologies: ['Go', 'Vue', 'Postgres', 'Caddy'],
      defaultLanguage: 'en',
      text: { en: { name: 'Shop site', role: 'Staff engineer' } },
    },
  })]);
  await page.getByLabel('Role').fill('Principal engineer');
  await save.click();
  await expect(page.locator('[data-state-mark="saved"]')).toBeVisible();
  expect(writes[1]?.ifMatch).toBe('"r2"');
  expect(writes[1]?.key).not.toBe(writes[0]?.key);
});

test('a project with dates loads, edits and saves', async ({ page }) => {
  const dated = {
    ...seed(FIRST, 'Shop site', '2026-11-01T00:00:00Z'),
    dates: { start: { y: 2024, m: 3 }, end: { y: 2025, m: 6 }, present: false },
  };
  const { writes } = await open(page, `/app/projects/${FIRST}`, [dated]);
  await expect(page.locator('[data-part="start-year"]')).toHaveValue('2024');
  await page.getByLabel('Role').fill('Staff engineer');
  await page.locator('[data-action="project-save"]').click();
  await expect(page.locator('[data-state-mark="saved"]')).toBeVisible();

  await page.locator('[data-part="end-year"]').fill('2026');
  await page.locator('[data-part="end-year"]').blur();
  await page.locator('[data-action="project-save"]').click();
  await expect(page.locator('[data-state-mark="saved"]')).toBeVisible();
  expect(writes.map((write) => (write.body as Project & {
    dates: unknown;
  }).dates)).toEqual([
    { start: { y: 2024, m: 3 }, end: { y: 2025, m: 6 }, present: false },
    { start: { y: 2024, m: 3 }, end: { y: 2026, m: 6 }, present: false },
  ]);
});

test('leaving with unsaved changes asks first', async ({ page }) => {
  await open(page, `/app/projects/${FIRST}`, library());
  await page.getByLabel('Role').fill('Typed');
  const back = page.getByTestId('project-breadcrumb');

  await back.click();
  const confirm = page.getByRole('alertdialog', {
    name: 'You have unsaved changes.',
  });
  await expect(confirm.getByRole('button', { name: 'Stay' })).toBeFocused();
  await confirm.getByRole('button', { name: 'Stay' }).click();
  await expect(confirm).toBeHidden();
  await expect(page).toHaveURL(`/app/projects/${FIRST}`);
  await expect(page.getByLabel('Role')).toHaveValue('Typed');

  await back.click();
  await confirm.getByRole('button', { name: 'Leave without saving' }).click();
  await expect(page).toHaveURL('/app/projects');
  await expect(page.getByTestId('project-list')).toBeVisible();
});

test('languages are added, made default and removed', async ({ page }) => {
  const { writes } = await open(page, `/app/projects/${FIRST}`, library());
  await page.getByRole('button', { name: 'Add language' }).click();
  const dialog = page.getByRole('dialog', { name: 'Add language' });
  await dialog.getByLabel('Language').selectOption('vi');
  await dialog.getByRole('button', { name: 'Add' }).click();

  const tabs = page.getByRole('tablist', { name: 'Project languages' });
  await expect(tabs.getByRole('tab'))
    .toHaveText(['English · default', 'Tiếng Việt']);
  await expect(tabs.getByRole('tab', { name: 'Tiếng Việt' }))
    .toHaveAttribute('aria-selected', 'true');
  await page.getByLabel('Name', { exact: true }).fill('Trang bán hàng');
  await page.getByRole('button', { name: 'Actions for Tiếng Việt' }).click();
  await page.getByRole('menuitem', { name: 'Make default' }).click();
  await expect(tabs.getByRole('tab'))
    .toHaveText(['English', 'Tiếng Việt · default']);

  await tabs.getByRole('tab', { name: 'Tiếng Việt · default' }).focus();
  await page.keyboard.press('ArrowLeft');
  await expect(tabs.getByRole('tab', { name: 'English' })).toBeFocused();
  await expect(tabs.getByRole('tab', { name: 'English' }))
    .toHaveAttribute('aria-selected', 'true');
  await page.getByRole('button', { name: 'Actions for English' }).click();
  await page.getByRole('menuitem', { name: 'Remove language' }).click();
  await expect(tabs.getByRole('tab')).toHaveText(['Tiếng Việt · default']);

  await page.locator('[data-action="project-save"]').click();
  await expect(page.locator('[data-state-mark="saved"]')).toBeVisible();
  expect(writes[0]?.body).toMatchObject({
    defaultLanguage: 'vi',
    text: { vi: { name: 'Trang bán hàng' } },
  });
  expect(Object.keys((writes[0]?.body as Project).text)).toEqual(['vi']);
});

test('technology chips add, remove and stop at twenty', async ({ page }) => {
  test.setTimeout(40_000);
  await open(page, `/app/projects/${FIRST}`, library());
  const input = page.getByTestId('project-technology-input');
  const chips = page.getByRole('list', { name: 'Technologies' })
    .getByRole('listitem');
  await input.fill('Podman');
  await input.press('Enter');
  await input.pressSequentially('Rust,');
  await expect(chips).toHaveText(['Go', 'Vue', 'Postgres', 'Caddy', 'Podman',
    'Rust']);
  await input.press('Backspace');
  await expect(chips).toHaveCount(5);
  await page.getByRole('button', { name: 'Remove Vue' }).click();
  await expect(chips).toHaveText(['Go', 'Postgres', 'Caddy', 'Podman']);
  await expect(input).toBeFocused();
  for (let index = 4; index < 20; index += 1) {
    await input.fill(`tool-${index}`);
    await input.press('Enter');
  }
  await expect(chips).toHaveCount(20);
  await input.fill('one-too-many');
  await input.press('Enter');
  await expect(chips).toHaveCount(20);
  await expect(page.getByText('All 20 technologies are used.')).toBeVisible();
});

test('changing the address reauthenticates and repeats the request once',
  async ({ page }) => {
    const { writes } = await open(page, `/app/projects/${FIRST}`, library());
    const trigger = page.getByRole('button', { name: 'Change address' });
    await trigger.click();
    const dialog = page.getByRole('dialog', { name: 'Change address' });
    await expect(dialog)
      .toContainText('Links to the old address stop working.');
    await dialog.getByLabel('New address').fill('shop-v2');
    await dialog.locator('[data-action="project-address-submit"]').click();

    await expect(dialog)
      .toContainText('This action needs you to reauthenticate.');
    await dialog.getByLabel('Current password').fill('correct horse');
    await dialog.getByRole('button', { name: 'Reauthenticate and continue' })
      .click();

    await expect(dialog).toBeHidden();
    await expect(page.getByTestId('project-slug')).toHaveText('shop-v2');
    await expect(trigger).toBeFocused();
    expect(writes.map((write) => write.method)).toEqual(['PUT', 'PUT']);
    expect(writes[1]?.key).toBe(writes[0]?.key);
    expect(writes[1]?.body).toEqual(writes[0]?.body);
  });

test('deleting reauthenticates and returns to the list', async ({ page }) => {
  const { writes } = await open(page, `/app/projects/${FIRST}`, library());
  await page.getByRole('button', { name: 'Delete project' }).click();
  const dialog = page.getByRole('dialog', { name: 'Delete project' });
  await expect(dialog).toContainText('Delete “Shop site”?');
  await dialog.locator('[data-action="project-delete-submit"]').click();
  await dialog.getByLabel('Current password').fill('correct horse');
  await dialog.getByRole('button', { name: 'Reauthenticate and continue' })
    .click();

  await expect(page).toHaveURL('/app/projects');
  await expect(page.getByTestId('project-list').locator('[data-project-link]'))
    .toHaveText(['Ops tool']);
  expect(writes.map((write) => [write.method, write.ifMatch]))
    .toEqual([['DELETE', '"r1"'], ['DELETE', '"r1"']]);
});

const SECOND_FACTOR_INTRO = 'Your account has a second verification step. '
  + 'Complete it in the new tab, then come back here.';

test('an enrolled account finishes its second factor, then the address '
  + 'change continues once', async ({ page }) => {
  const fake = await open(page, `/app/projects/${FIRST}`, library(), {
    enrolled: true,
  });
  await page.getByRole('button', { name: 'Change address' }).click();
  const dialog = page.getByRole('dialog', { name: 'Change address' });
  await dialog.getByLabel('New address').fill('shop-v2');
  await dialog.locator('[data-action="project-address-submit"]').click();
  const password = dialog.getByLabel('Current password');
  const reauthenticate = dialog.getByRole('button', {
    name: 'Reauthenticate and continue',
  });
  const next = dialog.getByRole('button', { name: 'Continue', exact: true });
  await password.fill('correct horse');
  await reauthenticate.click();

  await expect(dialog).toContainText(SECOND_FACTOR_INTRO);
  const link = dialog.getByRole('link', { name: 'Open verification step' });
  await expect(link).toHaveAttribute('href', '/login/second-factor');
  await expect(link).toHaveAttribute('target', '_blank');
  await expect(link).toHaveAttribute('rel', 'noopener');
  await expect(password).toHaveCount(0);
  expect(fake.writes).toHaveLength(1);

  await next.click();
  await expect(dialog.getByText(
    'The second verification step is not complete yet.',
  )).toBeVisible();
  await expect(next).toHaveCount(0);
  expect(fake.writes).toHaveLength(2);
  await password.fill('correct horse');
  await reauthenticate.click();
  await expect(link).toBeVisible();
  expect(fake.reauths).toHaveLength(2);
  expect(fake.writes).toHaveLength(2);

  fake.completeFactor();
  await next.click();

  await expect(dialog).toBeHidden();
  await expect(page.getByTestId('project-slug')).toHaveText('shop-v2');
  expect(fake.writes.map((write) => write.method))
    .toEqual(['PUT', 'PUT', 'PUT']);
  expect(new Set(fake.writes.map((write) => write.key)).size).toBe(1);
  expect(fake.writes[2]?.body).toEqual(fake.writes[0]?.body);
});

test('an enrolled account cancels the second factor step, then deletes '
  + 'once after finishing it', async ({ page }) => {
  const fake = await open(page, `/app/projects/${FIRST}`, library(), {
    enrolled: true,
  });
  const trigger = page.getByRole('button', { name: 'Delete project' });
  const dialog = page.getByRole('dialog', { name: 'Delete project' });
  const link = dialog.getByRole('link', { name: 'Open verification step' });
  const reauthenticate = async () => {
    await trigger.click();
    await dialog.locator('[data-action="project-delete-submit"]').click();
    await dialog.getByLabel('Current password').fill('correct horse');
    await dialog.getByRole('button', { name: 'Reauthenticate and continue' })
      .click();
    await expect(link).toBeVisible();
  };

  await reauthenticate();
  await dialog.locator('[data-action="project-release-cancel"]').click();
  await expect(dialog).toBeHidden();
  expect(fake.writes).toHaveLength(1);

  await reauthenticate();
  fake.completeFactor();
  await dialog.getByRole('button', { name: 'Continue', exact: true }).click();

  await expect(page).toHaveURL('/app/projects');
  await expect(page.getByTestId('project-list').locator('[data-project-link]'))
    .toHaveText(['Ops tool']);
  expect(fake.writes.map((write) => [write.method, write.ifMatch]))
    .toEqual([['DELETE', '"r1"'], ['DELETE', '"r1"'], ['DELETE', '"r1"']]);
  expect(fake.writes[1]?.key).not.toBe(fake.writes[0]?.key);
  expect(fake.writes[2]?.key).toBe(fake.writes[1]?.key);
});

test('a new project is created on the two resumes checked, then shown on '
  + 'them', async ({ page }) => {
  const fake = await open(page, '/app/projects', [], {
    resumes: resumeRows(),
  });
  await page.getByTestId('projects-empty')
    .getByRole('button', { name: 'New project' }).click();
  const dialog = page.getByRole('dialog', { name: 'New project' });
  const choices = dialog.getByRole('group', { name: 'Show on resumes' })
    .getByRole('checkbox');
  await expect(choices).toHaveCount(3);
  for (const choice of await choices.all()) {
    await expect(choice).not.toBeChecked();
  }
  await expect(dialog.locator('[data-resume-choice] p'))
    .toHaveText(['Public', 'Draft', 'Draft']);
  await dialog.getByLabel('Name').fill('Shop site');
  await dialog.getByRole('checkbox', { name: 'Backend' }).check();
  await dialog.getByRole('checkbox', { name: 'Data' }).check();
  await dialog.getByRole('button', { name: 'Create' }).click();

  await expect(page).toHaveURL(`/app/projects/${CREATED}`);
  const rows = page.getByTestId('project-resumes').locator('[data-resume-row]');
  await expect(rows.filter({ hasText: 'Shown' })).toHaveCount(2);
  await expect(page.locator(`[data-resume-row="${FRONTEND}"]`)
    .getByRole('button', { name: 'Add to this resume' })).toBeVisible();
  expect(fake.resumeWrites.map((write) =>
    [write.method, write.path, write.ifMatch, write.body])).toEqual([
    ['POST', `/api/v1/resumes/${BACKEND}/project-links`, '"r1"',
      { projectId: CREATED }],
    ['POST', `/api/v1/resumes/${DATA}/project-links`, '"r1"',
      { projectId: CREATED }],
  ]);
  expect(new Set(fake.resumeWrites.map((write) => write.key)).size).toBe(2);
});

test('the project editor adds the project to a resume without saving',
  async ({ page }) => {
    const fake = await open(page, `/app/projects/${FIRST}`, library(), {
      resumes: resumeRows(),
    });
    const row = page.locator(`[data-resume-row="${FRONTEND}"]`);
    await expect(row.locator('[data-project-fallback]'))
      .toHaveText('Project page uses English');

    await row.getByRole('button', { name: 'Add to this resume' }).click();

    await expect(row).toContainText('Shown');
    await expect(page.getByTestId('project-resumes-status'))
      .toHaveText('Added to “Frontend”.');
    await expect(row.getByRole('link', { name: 'Open in editor' }))
      .toHaveAttribute('href', `/app/resumes/${FRONTEND}?project=${FIRST}`);
    expect(fake.writes).toEqual([]);
    expect(fake.resumeWrites).toEqual([expect.objectContaining({
      method: 'POST', path: `/api/v1/resumes/${FRONTEND}/project-links`,
      ifMatch: '"r1"', body: { projectId: FIRST },
    })]);

    await page.getByLabel('Role').fill('Typed');
    await expect(page.locator(`[data-resume-row="${DATA}"]`)
      .getByRole('button', { name: 'Add to this resume' })).toBeDisabled();
    await expect(page.getByTestId('project-resumes'))
      .toContainText('Save the project first.');
  });

test('adding to a second published resume asks first, once', async ({
  page,
}) => {
  const rows = resumeRows({ [BACKEND]: FIRST });
  rows[1]!.slug = 'ada-frontend';
  const fake = await open(page, `/app/projects/${FIRST}`, library(), {
    resumes: rows, savedElsewhere: [FRONTEND],
  });
  const row = page.locator(`[data-resume-row="${FRONTEND}"]`);
  const ask = page.getByRole('alertdialog', {
    name: 'This project is already shown on another public resume.',
  });

  await row.getByRole('button', { name: 'Add to this resume' }).click();
  await expect(ask).toContainText(
    'A reader of both can tell they belong to the same person.',
  );
  await expect(ask.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await ask.getByRole('button', { name: 'Cancel' }).click();
  await expect(ask).toBeHidden();
  expect(fake.resumeWrites).toEqual([]);

  await row.getByRole('button', { name: 'Add to this resume' }).click();
  await ask.getByRole('button', { name: 'Add anyway' }).click();
  await expect(row).toContainText('Shown');
  // The first link meets a revision another tab saved; the same request is
  // sent once more on the new revision without asking again.
  await expect(ask).toBeHidden();
  expect(fake.resumeWrites.map((write) => [write.path, write.ifMatch]))
    .toEqual([
      [`/api/v1/resumes/${FRONTEND}/project-links`, '"r1"'],
      [`/api/v1/resumes/${FRONTEND}/project-links`, '"r2"'],
    ]);
});

test('the resume editor asks before showing a project on a second '
  + 'published resume', async ({ page }) => {
  const rows = resumeRows({ [FRONTEND]: SECOND });
  rows[1]!.slug = 'ada-frontend';
  const fake = await open(page, `/app/resumes/${BACKEND}`, library(), {
    resumes: rows,
  });
  const ask = page.getByRole('alertdialog', {
    name: 'This project is already shown on another public resume.',
  });
  const choose = async () => {
    await page.locator('[data-region="inspector"] [data-action="add-entry"]')
      .click();
    await page.getByRole('menuitem', { name: 'From your projects' }).click();
    await page.getByRole('menuitem', { name: 'Ops tool' }).click();
  };
  await page.locator('[data-region="outline"] [data-outline-key="projects"]')
    .click();

  await choose();
  await expect(ask.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await ask.getByRole('button', { name: 'Cancel' }).click();
  expect(fake.resumeWrites).toEqual([]);

  await choose();
  await ask.getByRole('button', { name: 'Add anyway' }).click();
  await expect(page.locator('[data-entry-id][data-selected]'))
    .toContainText('Ops tool');
  expect(fake.resumeWrites.map((write) => [write.path, write.body])).toEqual([
    [`/api/v1/resumes/${BACKEND}/project-links`, { projectId: SECOND }],
  ]);
});

test('New project decides the note at submit from the resumes read then',
  async ({ page }) => {
    const fake = await open(page, '/app/projects', [], {
      resumes: resumeRows(),
    });
    await page.getByTestId('projects-empty')
      .getByRole('button', { name: 'New project' }).click();
    const dialog = page.getByRole('dialog', { name: 'New project' });
    const note = dialog.getByTestId('project-create-privacy');
    await dialog.getByLabel('Name').fill('Shop site');
    await dialog.getByRole('checkbox', { name: 'Backend' }).check();
    await dialog.getByRole('checkbox', { name: 'Frontend' }).check();
    await expect(note).toHaveCount(0);
    // Another tab publishes Frontend after the dialog read the resumes.
    fake.resumes.get(FRONTEND)!.slug = 'ada-frontend';

    await dialog.getByRole('button', { name: 'Create' }).click();
    await expect(note).toHaveText(
      'This project is already shown on another public resume. '
      + 'A reader of both can tell they belong to the same person.',
    );
    expect(fake.writes).toEqual([]);

    await dialog.getByRole('button', { name: 'Create' }).click();
    await expect(page).toHaveURL(`/app/projects/${CREATED}`);
    expect(fake.writes.map((write) => write.method)).toEqual(['POST']);
    expect(fake.resumeWrites).toHaveLength(2);
  });

test('the resume editor adds a library project, selects it and refreshes it',
  async ({ page }) => {
    const fake = await open(page, `/app/resumes/${BACKEND}`, library(), {
      resumes: resumeRows(), refreshDelayMs: 1_000,
    });
    await page.locator('[data-region="outline"] [data-outline-key="projects"]')
      .click();
    await page.locator('[data-region="inspector"] [data-action="add-entry"]')
      .click();
    await page.getByRole('menuitem', { name: 'From your projects' }).click();
    await expect(page.locator('[data-project-option]'))
      .toHaveText(['Shop site', 'Ops tool']);
    await page.getByRole('menuitem', { name: 'Ops tool' }).click();

    const card = page.locator('[data-entry-id][data-selected]');
    await expect(card).toBeFocused();
    await expect(card.locator('[data-entry-project]'))
      .toContainText('Ops tool');
    await expect(card.locator('[data-project-note]')).toContainText(
      'This entry comes from the project Ops tool. '
      + 'The text here belongs to this resume.',
    );
    await expect(card.locator('[data-entry-field="title"] input'))
      .toHaveValue('Ops tool');
    const entryId = fake.resumes.get(BACKEND)!.entries![0]!.id;
    expect(fake.resumeWrites[0]).toMatchObject({
      method: 'POST', path: `/api/v1/resumes/${BACKEND}/project-links`,
      ifMatch: '"r1"', body: { projectId: SECOND },
    });

    fake.projects.get(SECOND)!.text.en = {
      name: 'Ops console', role: 'Lead engineer',
    };
    await card.getByRole('button', { name: 'Refresh from library' }).click();
    const confirm = page.getByRole('alertdialog', {
      name: 'Refresh from library',
    });
    await expect(confirm).toContainText(
      'Replace this entry\'s text with the project\'s current text?',
    );
    await confirm.getByRole('button', { name: 'Replace text' }).click();

    // Refresh is disabled while the refresh is out, so focus waits on Open
    // project and returns once the response has landed.
    await expect(card.getByRole('link', { name: 'Open project' }))
      .toBeFocused();
    await expect(card.locator('[data-entry-field="title"] input'))
      .toHaveValue('Ops console');
    await expect(card.getByRole('button', { name: 'Refresh from library' }))
      .toBeFocused();
    await expect(page.getByTestId('save-status'))
      .toHaveAttribute('data-state', 'saved');
    expect(fake.resumeWrites[1]).toMatchObject({
      method: 'POST',
      path: `/api/v1/resumes/${BACKEND}/entries/${entryId}/project-refresh`,
      ifMatch: '"r2"', body: null,
    });
    expect(fake.resumeWrites).toHaveLength(2);
  });

const OLD_ENTRY = '0190f000-0000-7000-8000-0000000000d1';

test('the resume editor saves an entry to my projects, links it and offers '
  + 'Open project', async ({ page }) => {
  const rows = resumeRows();
  const entry = {
    id: OLD_ENTRY,
    title: 'Old shop',
    subtitle: 'Lead',
    link: 'https://github.com/ada/old-shop',
    description: '<p>Built &amp; ran it.</p>',
  };
  rows[0]!.entries = [entry];
  const fake = await open(page, `/app/resumes/${BACKEND}`, library(), {
    resumes: rows,
  });
  await page.locator('[data-region="outline"] [data-outline-key="projects"]')
    .click();
  const card = page.locator(
    `[data-region="inspector"] [data-entry-id="${OLD_ENTRY}"]`,
  );

  await card.getByRole('button', { name: 'Save to my projects' }).click();

  await expect(card.locator('[data-entry-project-status]'))
    .toHaveText('Saved to your projects.');
  await expect(card.locator('[data-project-note]')).toContainText(
    'This entry comes from the project Old shop. '
    + 'The text here belongs to this resume.',
  );
  await expect(card.getByRole('link', { name: 'Open project' }))
    .toHaveAttribute('href', `/app/projects/${CREATED}`);
  await expect(card.getByRole('button', { name: 'Save to my projects' }))
    .toHaveCount(0);
  await expect(card.locator('[data-entry-field="title"] input'))
    .toHaveValue('Old shop');
  expect(fake.writes).toEqual([expect.objectContaining({
    method: 'POST', path: '/api/v1/projects', ifMatch: null,
    key: expect.any(String),
    body: {
      defaultLanguage: 'en',
      repositoryUrl: 'https://github.com/ada/old-shop',
      text: {
        en: { name: 'Old shop', role: 'Lead', summary: 'Built & ran it.' },
      },
    },
  })]);
  expect(fake.resumeWrites.map((write) =>
    [write.path, write.ifMatch, write.body])).toEqual([[
    `/api/v1/resumes/${BACKEND}/project-links`, '"r1"',
    { projectId: CREATED, entryId: OLD_ENTRY },
  ]]);
  expect(fake.resumes.get(BACKEND)!.entries)
    .toEqual([{ ...entry, projectId: CREATED }]);
});

test('the resume editor asks before adding a project it already has as an '
  + 'entry, and links that entry', async ({ page }) => {
  const rows = resumeRows();
  const entry = { id: OLD_ENTRY, title: 'ops tool ', subtitle: 'My words' };
  rows[0]!.entries = [entry];
  const fake = await open(page, `/app/resumes/${BACKEND}`, library(), {
    resumes: rows,
  });
  await page.locator('[data-region="outline"] [data-outline-key="projects"]')
    .click();
  await page.locator('[data-region="inspector"] [data-action="add-entry"]')
    .click();
  await page.getByRole('menuitem', { name: 'From your projects' }).click();
  await page.getByRole('menuitem', { name: 'Ops tool' }).click();

  const ask = page.getByRole('alertdialog', { name: 'From your projects' });
  await expect(ask).toContainText('This resume already has an entry '
    + '“ops tool”. Link that entry to the project, or add a new one?');
  const linkThat = ask.getByRole('button', { name: 'Link that entry' });
  await expect(linkThat).toBeFocused();
  await expect(ask.getByRole('button', { name: 'Add a new entry' }))
    .toBeVisible();
  await linkThat.click();

  const card = page.locator(
    `[data-region="inspector"] [data-entry-id="${OLD_ENTRY}"]`,
  );
  await expect(card).toHaveAttribute('data-selected', '');
  await expect(card.locator('[data-project-note]'))
    .toContainText('This entry comes from the project Ops tool.');
  await expect(card.locator('[data-entry-field="subtitle"] input'))
    .toHaveValue('My words');
  await expect(page.locator('[data-region="inspector"] [data-entry-id]'))
    .toHaveCount(1);
  await expect.poll(() => fake.resumeWrites.length).toBe(1);
  expect(fake.resumeWrites.map((write) =>
    [write.path, write.ifMatch, write.body])).toEqual([[
    `/api/v1/resumes/${BACKEND}/project-links`, '"r1"',
    { projectId: SECOND, entryId: OLD_ENTRY },
  ]]);
  expect(fake.resumes.get(BACKEND)!.entries)
    .toEqual([{ ...entry, projectId: SECOND }]);
});

test('the resume editor sends an edit typed after another tab saved the '
  + 'same value', async ({ page }) => {
  const fake = await open(page, `/app/resumes/${BACKEND}`, library(), {
    resumes: resumeRows(),
    headlinesElsewhere: { [BACKEND]: 'Staff engineer' },
  });
  const field = (name: string) => page.locator(
    `[data-region="inspector"] [data-field="${name}"] [data-field-input]`,
  );
  const status = page.getByTestId('save-status');
  const personal = `/api/v1/resumes/${BACKEND}/personal-details`;

  await field('headline').fill('Staff engineer');
  await field('headline').press('Tab');
  await expect.poll(() => fake.resumeWrites.length).toBe(1);
  await expect(status).toHaveAttribute('data-state', 'saved');
  await field('fullName').fill('Ada Byron');
  await field('fullName').press('Tab');

  await expect.poll(() => fake.resumeWrites.length).toBe(2);
  await expect(status).toHaveAttribute('data-state', 'saved');
  expect(fake.resumeWrites.map((write) => [write.path, write.ifMatch]))
    .toEqual([[personal, '"r1"'], [personal, '"r2"']]);
  expect(fake.resumes.get(BACKEND)!.personalDetails).toMatchObject({
    fullName: 'Ada Byron', headline: 'Staff engineer',
  });
});

test('the resume editor sends a write again with the same key after its '
  + 'connection drops, and saves it', async ({ page }) => {
  const fake = await open(page, `/app/resumes/${BACKEND}`, library(), {
    resumes: resumeRows(), lostWrites: 2,
  });
  const headline = page.locator(
    '[data-region="inspector"] [data-field="headline"] [data-field-input]',
  );
  const status = page.getByTestId('save-status');

  await headline.fill('Staff engineer');
  await headline.press('Tab');
  await expect.poll(() => fake.resumeWrites.length).toBe(2);
  await expect(status).toHaveAttribute('data-state', 'retrying');

  await expect.poll(() => fake.resumeWrites.length).toBe(3);
  await expect(status).toHaveAttribute('data-state', 'saved');
  const sent = fake.resumeWrites.map((write) => [write.key, write.ifMatch]);
  expect(sent).toEqual([sent[0], sent[0], sent[0]]);
  expect(sent[0]).toEqual([expect.any(String), '"r1"']);
  expect(fake.resumes.get(BACKEND)!.personalDetails).toMatchObject({
    headline: 'Staff engineer',
  });
});

test('the resume editor keeps an edit while the session check fails, and '
  + 'saves it after Try again', async ({ page }) => {
  const fake = await open(page, `/app/resumes/${BACKEND}`, library(), {
    resumes: resumeRows(), csrfRejectedWrites: 1,
  });
  const headline = page.locator(
    '[data-region="inspector"] [data-field="headline"] [data-field-input]',
  );
  const status = page.getByTestId('save-status');
  const banner = page.locator('[data-session-check]');
  // The session goes down only once the editor is open: a check that fails
  // before the first load shows Editor unavailable instead.
  await expect(headline).toBeVisible();
  await expect(status).toHaveAttribute('data-state', 'saved');
  // The token refresh after the refused write cannot reach the session.
  let sessionDown = true;
  await page.route('**/api/v1/me', async (route) => {
    if (!sessionDown) {
      await route.fallback();
      return;
    }
    await route.fulfill({
      status: 503, headers: HEADERS,
      json: { error: { code: 'unavailable', message: 'fixed' } },
    });
  });

  await headline.fill('Staff engineer');
  await headline.press('Tab');
  await expect(banner).toContainText('Can\'t check your sign-in');
  await expect(status).toHaveAttribute('data-state', 'retrying');
  await expect(headline).toHaveValue('Staff engineer');

  sessionDown = false;
  await banner.getByRole('button', { name: 'Try again' }).click();
  await expect(status).toHaveAttribute('data-state', 'saved');
  await expect(banner).toBeHidden();
  const keys = fake.resumeWrites.map((write) => write.key);
  expect(keys).toEqual([keys[0], keys[0]]);
  expect(fake.resumes.get(BACKEND)!.personalDetails).toMatchObject({
    headline: 'Staff engineer',
  });
});

test('deleting a linked project says how many resumes it leaves', async ({
  page,
}) => {
  const fake = await open(page, `/app/projects/${FIRST}`, library(), {
    resumes: resumeRows({ [BACKEND]: FIRST, [DATA]: FIRST }),
  });
  await page.getByRole('button', { name: 'Delete project' }).click();
  const dialog = page.getByRole('dialog', { name: 'Delete project' });

  await expect(dialog.getByTestId('project-delete-linked')).toHaveText(
    'It will be removed from 2 resumes. The entries keep their text.',
  );
  await dialog.locator('[data-action="project-delete-submit"]').click();
  await dialog.getByLabel('Current password').fill('correct horse');
  await dialog.getByRole('button', { name: 'Reauthenticate and continue' })
    .click();

  await expect(page).toHaveURL('/app/projects');
  expect([...fake.resumes.values()].map((row) =>
    [row.revision, row.entries?.map((entry) => entry.projectId ?? null)]))
    .toEqual([[2, [null]], [1, []], [2, [null]]]);
});

test('a held resume edit saves after a project replacement advances only '
  + 'the resume revision', async ({ page }) => {
  test.setTimeout(40_000);
  const other = await page.context().newPage();
  const fake = await open(page, `/app/resumes/${BACKEND}`, library(), {
    resumes: resumeRows({ [BACKEND]: FIRST }), otherTab: other,
  });
  const linked = structuredClone(fake.resumes.get(BACKEND)!.entries);
  const personalPath = `/api/v1/resumes/${BACKEND}/personal-details`;
  let release = () => {};
  const released = new Promise<void>((resolve) => {
    release = () => resolve();
  });
  // The first save waits here, so it reaches the fake after the replacement.
  await page.route(`**${personalPath}`, async (route) => {
    await released;
    await route.fallback();
  }, { times: 1 });
  const held = page.waitForRequest((request) => request.method() === 'PATCH'
    && new URL(request.url()).pathname === personalPath);
  await page.locator('[data-region="outline"] [data-outline-key="personal"]')
    .click();
  const headline = page.locator('[data-region="inspector"]')
    .getByLabel('Headline');
  await headline.fill('Analytical engineer');
  await headline.press('Tab');
  expect((await held).headers()['if-match']).toBe('"r1"');

  // The other tab replaces the project's summary.
  await other.goto(`/app/projects/${FIRST}`);
  const replaced = other.waitForResponse((response) =>
    response.request().method() === 'PUT'
    && new URL(response.url()).pathname === `/api/v1/projects/${FIRST}`);
  await other.getByLabel('Summary', { exact: true })
    .fill('Ships every order in a day.');
  await other.locator('[data-action="project-save"]').click();
  const replacement = await (await replaced).json() as {
    data: { changedResumes: unknown };
  };
  expect(replacement.data.changedResumes)
    .toEqual([{ id: BACKEND, revision: '2' }]);

  // The held save meets revision 2 with the document it was based on.
  release();
  await expect(page.getByTestId('save-status'))
    .toHaveAttribute('data-state', 'saved');
  await expect(page.locator('[data-conflict]')).toHaveCount(0);
  expect(fake.resumeWrites.map((write) => [write.path, write.ifMatch]))
    .toEqual([[personalPath, '"r1"'], [personalPath, '"r2"']]);
  expect(fake.resumeWrites[1]?.body).toEqual(fake.resumeWrites[0]?.body);
  const reread = await page.evaluate(async (path) => {
    const response = await fetch(path, { credentials: 'include' });
    return await response.json() as unknown;
  }, `/api/v1/resumes/${BACKEND}`);
  expect(reread).toEqual({
    data: expect.objectContaining({
      revision: '3',
      document: expect.objectContaining({
        personalDetails: expect.objectContaining({
          headline: 'Analytical engineer',
        }),
        content: { projects: expect.objectContaining({ entries: linked }) },
      }),
    }),
  });
});

function listContract(): ScreenContract {
  return chromeScreenContract('[data-testid="projects-page"]',
    '[data-testid="projects-page"] :is(h1, p, [data-project-link], '
    + '[data-project-role], time):visible',
    '[data-testid="projects-page"] > div:first-child, '
    + '[data-testid="project-list"] > li',
    '[data-testid="projects-page"] h1', 'Projects', 'light');
}

function editorContract(): ScreenContract {
  return chromeScreenContract('[data-testid="project-editor"]',
    '[data-testid="project-form"] :is(h1, h2, label, dt, dd):visible',
    '[data-testid="project-form"] > section',
    '[data-testid="project-title"]', 'Shop site', 'light');
}

async function openListAt320(page: Page): Promise<void> {
  await page.setViewportSize({ width: 320, height: 900 });
  await open(page, '/app/projects', library(), {
    resumes: resumeRows({ [BACKEND]: FIRST }),
  });
  await expect(page.getByTestId('project-list')).toBeVisible();
}

test('both screens fit 320 px without horizontal overflow', async ({
  page,
}) => {
  test.setTimeout(60_000);
  await openListAt320(page);
  await expectScreen(page, listContract());
  const row = page.getByTestId('project-list').locator(':scope > li').first();
  const menu = row.getByRole('button', { name: /More actions/u });
  const [rowBox, menuBox] = await Promise.all([
    row.boundingBox(), menu.boundingBox(),
  ]);
  expect(menuBox!.y - rowBox!.y).toBeLessThan(16);
  expect(rowBox!.x + rowBox!.width - (menuBox!.x + menuBox!.width))
    .toBeLessThan(16);

  await page.getByRole('button', { name: 'New project' }).click();
  const dialog = page.getByRole('dialog', { name: 'New project' });
  await expect(dialog.getByRole('checkbox')).toHaveCount(3);
  await expect.poll(() => page.evaluate(() =>
    document.documentElement.scrollWidth
    <= document.documentElement.clientWidth), 'Dialog fit at 320px.')
    .toBe(true);
  const box = await dialog.boundingBox();
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(320);
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(dialog).toBeHidden();

  await page.locator('[data-project-link]').last().click();
  await expect(page.getByTestId('project-title')).toHaveText('Shop site');
  await expect(page.locator('[data-resume-row]')).toHaveCount(3);
  await expectScreen(page, editorContract());
  // The signed-in English bar overflows 640 px without the Projects link
  // too, so only the page column is checked there.
  await page.setViewportSize({ width: 640, height: 900 });
  await expect.poll(() => page.getByTestId('project-editor').evaluate(
    (main) => main.scrollWidth <= main.clientWidth
      && main.getBoundingClientRect().right <= innerWidth + 1),
  'Fit at 640px.').toBe(true);
  const link = page.getByTestId('app-shell')
    .getByRole('link', { name: 'Projects', exact: true });
  for (const width of [700, 768, 1024]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(link).toBeVisible({ visible: width >= 768 });
    await expect.poll(() => page.evaluate(() =>
      document.documentElement.scrollWidth
      <= document.documentElement.clientWidth), `Fit at ${width}px.`)
      .toBe(true);
  }
});

screenMutationTests('projects list', openListAt320, listContract());
