import { expect, test, type Page } from '@playwright/test';

import { expectScreen, type ScreenContract } from './display-assertions';
import { screenMutationTests } from './style-mutation';
import {
  chromeScreenContract,
  denyExternalRequests,
  mockSignedInSession,
} from './support';

// The workspace Projects screens (docs/design/ui/projects.md) against a
// stateful fake of the owner project API (docs/api/openapi.yaml, /projects).

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

interface LibraryOptions {
  /** The account has a second factor, so a password only opens it. */
  readonly enrolled?: boolean;
}

async function fakeLibrary(
  page: Page,
  initial: readonly Project[],
  options: LibraryOptions = {},
) {
  const projects = new Map(
    initial.map((item) => [item.id, structuredClone(item)]),
  );
  const writes: Write[] = [];
  const reauths: string[] = [];
  let reauthenticated = false;
  const problem = (status: number, code: string, details?: unknown) => ({
    status,
    headers: HEADERS,
    json: {
      error: { code, message: 'fixed', ...(details ? { details } : {}) },
    },
  });
  await page.route('**/api/v1/auth/password/reauth', async (route) => {
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
  await page.route(/\/api\/v1\/projects(?:\/[^/?]+)?$/u, async (route) => {
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
        headers: HEADERS, json: { data: { projects: [...projects.values()] } },
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
      await route.fulfill({ headers: HEADERS, json: { data: current } });
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
      projects.delete(current.id);
      await route.fulfill({
        headers: HEADERS, json: { data: { changedResumes: [] } },
      });
      return;
    }
    const next: Project = {
      ...body!, id: current.id, createdAt: current.createdAt,
      revision: String(Number(current.revision) + 1),
      updatedAt: '2026-11-11T01:00:00Z', linkedResumeIds: [],
    };
    projects.set(current.id, next);
    await route.fulfill({
      headers: HEADERS, json: { data: { project: next, changedResumes: [] } },
    });
  });
  return {
    projects,
    writes,
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
  await denyExternalRequests(page);
  await page.context().addCookies([{
    name: 'aboutme-locale', value: 'en', url: 'http://127.0.0.1:20092',
  }]);
  await mockSignedInSession(page);
  const library = await fakeLibrary(page, initial, options);
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
  await open(page, '/app/projects', library());
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

  await page.locator('[data-project-link]').last().click();
  await expect(page.getByTestId('project-title')).toHaveText('Shop site');
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
