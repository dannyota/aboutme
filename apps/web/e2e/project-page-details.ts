import { expect, type Page } from '@playwright/test';

import type { ScreenContract } from './display-assertions';
import { BAR_COLORS } from './public-bar';
import type { StyleRule } from './screen-expectations';
import { denyExternalRequests } from './support';

export type ProjectPageKind = 'project-list' | 'project';
export type Lng = 'vi' | 'en';
export type Scheme = 'light' | 'dark';

const FAMILY = '"Be Vietnam Pro", Inter, system-ui, sans-serif';

export const PROJECT_COLORS = {
  light: {
    ground: 'rgb(255, 255, 255)', card: 'rgb(255, 255, 255)',
    ink: 'rgb(26, 22, 80)', muted: 'rgb(85, 81, 125)',
    link: 'rgb(67, 54, 201)', primary: 'rgb(89, 75, 173)',
    onPrimary: 'rgb(255, 255, 255)', chip: 'rgb(241, 239, 250)',
    border: 'rgb(227, 224, 242)', hairline: 'rgba(26, 22, 80, 0.14)',
  },
  dark: {
    ground: 'rgb(14, 10, 13)', card: 'rgb(27, 20, 25)',
    ink: 'rgb(211, 202, 208)', muted: 'rgb(163, 149, 159)',
    link: 'rgb(181, 155, 219)', primary: 'rgb(114, 103, 192)',
    onPrimary: 'rgb(255, 255, 255)', chip: 'rgb(21, 16, 21)',
    border: 'rgb(46, 36, 44)', hairline: 'rgba(211, 202, 208, 0.14)',
  },
} as const;

const PHONE_WIDTH = 480;
export const PROJECT_BAR_ROW = 'Project bar one row.';
export const PROJECT_SHAPES = 'Project shapes.';

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

export function projectPageType(
  kind: ProjectPageKind, width = 1280,
): StyleRule[] {
  const shared: StyleRule[] = [
    {
      selector: '.public-credit', message: 'Project credit typography',
      css: { 'font-family': FAMILY,
        'font-size': width < PHONE_WIDTH ? '12px' : '13px',
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
  kind: ProjectPageKind, lng: Lng, scheme: Scheme, width = 1280,
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
    styles: projectPageType(kind, width),
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
  const bar = BAR_COLORS[scheme];
  const checks: [string, string, string][] = [
    ['.public-project-page', 'background-color', color.ground],
    ['main h1', 'color', color.ink],
    ['.public-toolbar', 'background-color', bar.ground],
    ['.public-toolbar', 'border-bottom-color', bar.border],
    ['.public-credit', 'color', bar.credit],
    ['.public-mark', 'color', bar.mark],
    ['.public-back', 'color', bar.label],
    ['.public-back', 'border-top-color', bar.label],
    ['.public-back', 'background-color', 'rgba(0, 0, 0, 0)'],
    ['.project-chips li:not(.project-chips-more)', 'background-color',
      color.chip],
    ['.project-chips li:not(.project-chips-more)', 'color', color.ink],
    ...(kind === 'project-list'
      ? [
          ['.project-rows', 'background-color', color.card],
          ['.project-rows', 'border-top-color', color.border],
          ['.project-row + .project-row', 'border-top-color', color.hairline],
          ['.project-row-name a', 'color', color.link],
          ['.project-row-meta', 'color', color.muted],
          ['.project-chips-more', 'color', color.muted],
        ] as [string, string, string][]
      : [
          ['.project-external-primary', 'background-color', color.primary],
          ['.project-external-primary', 'color', color.onPrimary],
          ['.project-external:not(.project-external-primary)', 'color',
            color.ink],
          ['.project-external:not(.project-external-primary)',
            'border-top-color', color.ink],
          ['.project-external:not(.project-external-primary)',
            'background-color', 'rgba(0, 0, 0, 0)'],
          ['.project-external', 'box-shadow', 'none'],
          ['.project-facts', 'background-color', color.card],
          ['.project-facts', 'border-top-color', color.border],
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

export async function expectProjectBarRow(page: Page): Promise<void> {
  const row = await page.evaluate(() => {
    const box = (selector: string) =>
      document.querySelector(selector)!.getBoundingClientRect();
    const inner = document.querySelector('.public-toolbar-inner')!;
    const innerBox = inner.getBoundingClientRect();
    const innerStyle = getComputedStyle(inner);
    const brand = box('.public-brand');
    const back = box('.public-back');
    const range = document.createRange();
    range.selectNodeContents(document.querySelector('.public-back')!);
    const label = range.getBoundingClientRect();
    const lines = new Set([...range.getClientRects()]
      .map((rect) => Math.round(rect.top))).size;
    const root = document.documentElement;
    return {
      coarse: matchMedia('(pointer: coarse)').matches,
      overflow: root.scrollWidth > root.clientWidth,
      viewport: innerWidth,
      barHeight: box('.public-toolbar').height,
      contentLeft: innerBox.left + parseFloat(innerStyle.paddingLeft),
      contentRight: innerBox.right - parseFloat(innerStyle.paddingRight),
      brand: { left: brand.left, right: brand.right,
        middle: (brand.top + brand.bottom) / 2 },
      back: { left: back.left, right: back.right,
        middle: (back.top + back.bottom) / 2 },
      labelInside: label.width > 0 && label.left >= back.left
        && label.right <= back.right && label.top >= back.top
        && label.bottom <= back.bottom,
      lines,
    };
  });
  expect({
    coarse: row.coarse,
    overflow: row.overflow,
    barHeight: Math.abs(row.barHeight - 56) <= 1,
    oneRow: Math.abs(row.brand.middle - row.back.middle) <= 1,
    apart: row.brand.right + 8 <= row.back.left + 1,
    gutter: Math.abs(row.contentLeft - 16) <= 1
      && Math.abs(row.viewport - 16 - row.contentRight) <= 1,
    edges: Math.abs(row.brand.left - row.contentLeft) <= 1
      && Math.abs(row.back.right - row.contentRight) <= 1,
    labelInside: row.labelInside,
    lines: row.lines,
  }, PROJECT_BAR_ROW).toEqual({
    coarse: true,
    overflow: false,
    barHeight: true,
    oneRow: true,
    apart: true,
    gutter: true,
    edges: true,
    labelInside: true,
    lines: 1,
  });
}

export async function expectProjectShapes(
  page: Page, kind: ProjectPageKind,
): Promise<void> {
  const width = page.viewportSize()!.width;
  const card = width >= 1024 ? '28px' : width >= 768 ? '26px' : '22px';
  const checks: [string, string][] = [
    ['.public-back', '999px'],
    ...(kind === 'project-list'
      ? [['.project-rows', card]] as [string, string][]
      : [['.project-external', '999px'], ['.project-facts', card],
          ['.project-outcome', card]] as [string, string][]),
  ];
  for (const [selector, radius] of checks) {
    const nodes = page.locator(selector);
    expect(await nodes.count(), PROJECT_SHAPES).toBeGreaterThan(0);
    for (const node of await nodes.all()) {
      await expect(node, PROJECT_SHAPES)
        .toHaveCSS('border-top-left-radius', radius);
    }
  }
}
