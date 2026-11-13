import { expect, type Page } from '@playwright/test';

import type { ScreenContract } from './display-assertions';
import type { StyleRule } from './screen-expectations';
import { denyExternalRequests } from './support';

export type ProjectPageKind = 'project-list' | 'project';
export type Lng = 'vi' | 'en';
export type Scheme = 'light' | 'dark';

const FAMILY = '"Be Vietnam Pro", Inter, system-ui, sans-serif';

export const PROJECT_COLORS = {
  light: {
    ground: 'rgb(249, 248, 245)', card: 'rgb(255, 255, 255)',
    ink: 'rgb(16, 27, 63)', muted: 'rgb(92, 97, 120)',
    link: 'rgb(35, 57, 154)', primary: 'rgb(38, 64, 156)',
    onPrimary: 'rgb(255, 255, 255)', chip: 'rgb(239, 237, 230)',
    mark: 'rgb(204, 38, 73)',
  },
  dark: {
    ground: 'rgb(12, 16, 32)', card: 'rgb(20, 26, 46)',
    ink: 'rgb(243, 241, 236)', muted: 'rgb(165, 171, 191)',
    link: 'rgb(163, 182, 245)', primary: 'rgb(143, 166, 240)',
    onPrimary: 'rgb(12, 16, 32)', chip: 'rgb(26, 33, 56)',
    mark: 'rgb(255, 107, 138)',
  },
} as const;

const OWNER: Record<Lng, string> = {
  vi: 'Nguyễn Văn An', en: 'Ada Lovelace',
};

const PROJECT_NAME: Record<Lng, string> = {
  vi: 'Hệ thống thanh toán thời gian thực',
  en: 'Real-time payments platform',
};

export const projectPageURL = (
  kind: ProjectPageKind, lng: Lng, scheme: Scheme | 'system',
): string => `/_harness/render?mode=${kind}&lng=${lng}&scheme=${scheme}`;

const INLINE_STYLES = 'style-src \'self\' \'unsafe-inline\'';

async function serveWithoutInlineStyles(page: Page): Promise<void> {
  await page.route((url) => url.pathname === '/_harness/render',
    async (route) => {
      const response = await route.fetch();
      const headers = response.headers();
      const policy = headers['content-security-policy'] ?? '';
      expect(policy, 'Project page style policy.').toContain(INLINE_STYLES);
      headers['content-security-policy']
        = policy.replace(INLINE_STYLES, 'style-src \'self\'');
      await route.fulfill({ response, headers });
    });
}

export async function openProjectPage(
  page: Page, kind: ProjectPageKind, lng: Lng, scheme: Scheme | 'system',
): Promise<string[]> {
  const external = await denyExternalRequests(page);
  await serveWithoutInlineStyles(page);
  const response = await page.goto(projectPageURL(kind, lng, scheme));
  expect(response?.ok()).toBe(true);
  await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
  return external;
}

const backLabel = (lng: Lng): string => lng === 'vi'
  ? `Về CV của ${OWNER[lng]}`
  : `Back to ${OWNER[lng]}'s resume`;

export function projectPageType(kind: ProjectPageKind): StyleRule[] {
  const shared: StyleRule[] = [
    {
      selector: '.public-credit', message: 'Project credit typography',
      css: { 'font-family': FAMILY, 'font-size': '13px',
        'font-weight': '500', 'line-height': '20px' },
    },
    {
      selector: '.public-back', message: 'Project back link typography',
      css: { 'font-family': FAMILY, 'font-size': '14px',
        'font-weight': '500', 'line-height': '20px', 'min-height': '32px' },
    },
    {
      selector: '.project-chips li', message: 'Project chip typography',
      css: { 'font-family': FAMILY, 'font-size': '12px',
        'font-weight': '500', 'line-height': '18px' },
    },
  ];
  return kind === 'project-list'
    ? [...shared, {
        selector: '.project-row-name', message: 'Project row typography',
        css: { 'font-family': FAMILY, 'font-size': '16px',
          'font-weight': '600', 'line-height': '24px' },
      }]
    : [...shared, {
        selector: '.project-external', message: 'Project button typography',
        css: { 'font-family': FAMILY, 'font-size': '14px',
          'font-weight': '500', 'line-height': '20px', 'height': '40px' },
      }, {
        selector: '.project-text h2', message: 'Project section typography',
        css: { 'font-family': FAMILY, 'font-size': '20px',
          'font-weight': '600', 'line-height': '26px' },
      }];
}

export function projectContract(
  kind: ProjectPageKind, lng: Lng, scheme: Scheme,
): ScreenContract {
  const title = kind === 'project-list'
    ? (lng === 'vi' ? 'Dự án' : 'Projects')
    : PROJECT_NAME[lng];
  return {
    root: '.public-project-page',
    text: '.public-toolbar :is(.public-credit, .public-back), '
      + 'main :is(h1, h2, p, dt, dd, li, a)',
    flow: 'main > *',
    type: ['main h1', FAMILY, 'Be Vietnam Pro'],
    styles: projectPageType(kind),
    paint: ['.public-project-page', 'background-color',
      PROJECT_COLORS[scheme].ground],
    copy: ['main h1', title],
    aria: ['main h1', `- heading "${title}" [level=1]`],
    controls: [['.public-back', backLabel(lng)]],
    counts: kind === 'project-list'
      ? [['main h1', 1], ['.project-rows', 1], ['.project-rows > li', 4],
          ['.project-chips-more', 1], ['.public-download', 0]]
      : [['main h1', 1], ['.project-external', 3],
          ['.project-external-primary', 1], ['.project-facts li', 9],
          ['.project-description', 1], ['.public-download', 0]],
    structure: (page) => expectProjectStructure(page, kind, lng),
  };
}

export async function expectProjectStructure(
  page: Page, kind: ProjectPageKind, lng: Lng,
): Promise<void> {
  const message = 'Project page structure.';
  await expect(page.getByRole('main'), message).toHaveCount(1);
  if (kind === 'project-list') {
    await expect(page.getByRole('list', {
      name: lng === 'vi' ? 'Công nghệ' : 'Technologies',
    }), message).toHaveCount(3);
    await expect(page.getByRole('heading', { level: 2 }), message)
      .toHaveCount(4);
    return;
  }
  await expect(page.getByRole('navigation').getByRole('link'), message)
    .toHaveText([OWNER[lng], lng === 'vi' ? 'Dự án' : 'Projects']);
  for (const name of lng === 'vi'
    ? ['Kết quả', 'Mô tả']
    : ['Outcome', 'About the project']) {
    await expect(page.getByRole('region', { name }), message).toBeVisible();
  }
  await expect(page.locator('a.project-external'), message).toHaveText(
    lng === 'vi'
      ? ['Repository', 'Demo', 'Liên kết']
      : ['Repository', 'Demo', 'Link'],
  );
  await expect(page.locator('.project-facts dt'), message).toHaveText(
    lng === 'vi' ? ['Vai trò', 'Thời gian'] : ['Role', 'Dates'],
  );
}

export async function expectProjectMeasure(
  page: Page, kind: ProjectPageKind,
): Promise<void> {
  const measure = kind === 'project-list' ? 44 : 59;
  await expect.poll(() => page.evaluate((measure) => {
    const rem = parseFloat(getComputedStyle(document.documentElement)
      .fontSize);
    const width = document.documentElement.clientWidth;
    const gutter = width >= 56 * rem ? 32 : width >= 40 * rem ? 24 : 16;
    const content = (selector: string) => {
      const element = document.querySelector(selector)!;
      const box = element.getBoundingClientRect();
      const css = getComputedStyle(element);
      return {
        left: box.left + parseFloat(css.paddingLeft),
        right: box.right - parseFloat(css.paddingRight),
      };
    };
    const main = content('main#public-project');
    const bar = content('.public-toolbar-inner');
    const expected = Math.min(measure * rem, width - 2 * gutter);
    return Math.abs(main.right - main.left - expected) <= 1
      && Math.abs(main.left - (width - expected) / 2) <= 1
      && Math.abs(bar.left - main.left) <= 1
      && Math.abs(bar.right - main.right) <= 1;
  }, measure), 'Project measure.').toBe(true);
}

export async function expectProjectColumns(page: Page): Promise<void> {
  await expect.poll(() => page.evaluate(() => {
    const box = (selector: string) =>
      document.querySelector(selector)!.getBoundingClientRect();
    const columns = box('.project-columns');
    const facts = box('.project-columns > .project-facts');
    const text = box('.project-columns > .project-text');
    const rem = parseFloat(getComputedStyle(document.documentElement)
      .fontSize);
    if (innerWidth >= 56 * rem) {
      return Math.abs(facts.width - 17.5 * rem) <= 1
        && facts.left >= text.right
        && Math.abs(facts.right - columns.right) <= 1
        && Math.abs(text.left - columns.left) <= 1
        && Math.abs(facts.top - text.top) <= 1;
    }
    return Math.abs(facts.width - columns.width) <= 1
      && Math.abs(text.width - columns.width) <= 1
      && facts.bottom <= text.top;
  }), 'Project columns.').toBe(true);
}

export async function expectRowMeta(page: Page): Promise<void> {
  await expect.poll(() => page.evaluate(() => {
    const row = document.querySelectorAll('.project-rows > li')[1]!;
    const name = row.querySelector('h2')!.getBoundingClientRect();
    const meta = row.querySelector('.project-row-meta')!
      .getBoundingClientRect();
    const rem = parseFloat(getComputedStyle(document.documentElement)
      .fontSize);
    return innerWidth >= 40 * rem
      ? meta.left >= name.right && meta.top < name.bottom
      : meta.top >= name.bottom - 1 && Math.abs(meta.left - name.left) <= 1;
  }), 'Project row meta.').toBe(true);
}

export async function expectProjectStylesheet(page: Page): Promise<void> {
  await expect.poll(() => page.evaluate(() => {
    const sheet = [...document.styleSheets].find((candidate) =>
      candidate.href !== null
      && new URL(candidate.href).pathname
      === '/_nuxt/assets/project-pages.css');
    const styled = document.querySelectorAll(
      '.public-project-page[style], .public-project-page [style]');
    return sheet !== undefined && !sheet.disabled
      && sheet.cssRules.length > 0 && styled.length === 0;
  }), 'Project stylesheet.').toBe(true);
}

export async function expectProjectColors(
  page: Page, kind: ProjectPageKind, scheme: Scheme,
): Promise<void> {
  const color = PROJECT_COLORS[scheme];
  const checks: [string, string, string][] = [
    ['.public-project-page', 'background-color', color.ground],
    ['main h1', 'color', color.ink],
    ['.public-credit', 'color', color.muted],
    ['.public-mark', 'color', color.mark],
    ['.public-back', 'color', color.link],
    ['.public-back', 'background-color', color.card],
    ['.project-chips li:not(.project-chips-more)', 'background-color',
      color.chip],
    ['.project-chips li:not(.project-chips-more)', 'color', color.ink],
    ...(kind === 'project-list'
      ? [
          ['.project-rows', 'background-color', color.card],
          ['.project-row-name a', 'color', color.link],
          ['.project-row-meta', 'color', color.muted],
          ['.project-chips-more', 'color', color.muted],
        ] as [string, string, string][]
      : [
          ['.project-external-primary', 'background-color', color.primary],
          ['.project-external-primary', 'color', color.onPrimary],
          ['.project-external:not(.project-external-primary)', 'color',
            color.link],
          ['.project-facts', 'background-color', color.card],
          ['.project-outcome', 'background-color', color.card],
          ['.project-facts dt', 'color', color.muted],
          ['.project-description', 'color', color.ink],
          ['.project-description a', 'color', color.link],
        ] as [string, string, string][]),
  ];
  for (const [selector, property, value] of checks) {
    const nodes = page.locator(selector);
    expect(await nodes.count(), 'Project scheme colors.').toBeGreaterThan(0);
    for (const node of await nodes.all()) {
      await expect(node, 'Project scheme colors.').toHaveCSS(property, value);
    }
  }
}
