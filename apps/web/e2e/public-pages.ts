import { expect, type Page } from '@playwright/test';
import { TEMPLATES } from '@aboutme/schema/templates';

import { expectBarPaint } from './public-bar';
import { measureScreenText } from './screen-containment';
import { denyExternalRequests, waitForImages } from './support';

export type Lng = 'vi' | 'en';
export type Owner = 'light' | 'dark' | 'system';
export type Viewer = 'light' | 'dark';

export const WIDE_SHEETS = 'Public wide sheets.';
export const SHEET_SCALE = 'Public sheet scale.';
export const PHONE_SHEETS = 'Public phone sheets.';
export const SNAP = 'Public snap.';
export const PAGER = 'Public pager.';
export const PAGE_INDEX = 'Public page index.';
export const PAGES_ARIA = 'Public pages accessibility.';
export const STAGING = 'Public staging.';
export const FALLBACK = 'Public fallback.';
export const SWAP = 'Public swap stability.';
export const CLIPPING = 'Public sheet clipping.';
export const PRINT = 'Public print.';
export const PAPER = 'Public page paper.';
export const BAND = 'Public header band.';
export const FOREGROUND = 'Public foreground.';
export const DESK = 'Public desk paint.';
export const BAR = 'Public bar paint.';
export const SHADOW = 'Public sheet shadow.';
export const FOCUS = 'Public scroller focus.';

export const PAGE = '.public-page';
export const SHEET = '.public-page-sheet';
export const SCROLLER = '.public-pages .paged-resume';
const PAGE_ARTICLE = `${PAGE} .resume-page`;
const CONTINUOUS = '.public-resume-page > .resume-document';
const PAGE_WIDTH = 794;

const DESK_COLORS = {
  light: 'rgb(230, 223, 208)', dark: 'rgb(20, 18, 16)',
} as const;
const SHADOW_CHANNELS = { light: '74, 54, 22', dark: '0, 0, 0' } as const;

export const PUBLIC_PAINT = {
  'classic-serif': { foreground: '#23262c' },
  'modern-sidebar': { foreground: '#1c2b33' },
  'editorial-wide': { foreground: '#24211c' },
  'creative-accent': { foreground: '#1f1a17', band: '#fff0eb' },
  'executive-band': { foreground: '#1f2937', band: '#16273d' },
} as const;
export type PaintTemplate = keyof typeof PUBLIC_PAINT;

export const PAGES_COPY = {
  vi: {
    pages: 'Các trang CV', previous: 'Trang trước', next: 'Trang sau',
    page: (n: number, count: number) => `Trang ${n}/${count}`,
  },
  en: {
    pages: 'Resume pages', previous: 'Previous page', next: 'Next page',
    page: (n: number, count: number) => `Page ${n} of ${count}`,
  },
} as const;

export function rgb(hex: string): string {
  const channels = [1, 3, 5].map((index) =>
    Number.parseInt(hex.slice(index, index + 2), 16));
  return `rgb(${channels.join(', ')})`;
}

export function paper(template: string): string {
  const preset = TEMPLATES.find(({ id }) => id === template);
  if (preset === undefined) throw new Error(`Unknown template ${template}`);
  return rgb(preset.customization.colors.background.toLowerCase());
}

export const fixtureFor = (lng: Lng): string =>
  lng === 'vi' ? 'vn-full' : 'full';

export const darkDesk = (owner: Owner, viewer: Viewer): boolean =>
  owner === 'dark' || (owner === 'system' && viewer === 'dark');

export interface OpenOptions {
  readonly template: string;
  readonly lng: Lng;
  readonly width: number;
  readonly height?: number;
  readonly owner?: Owner;
  readonly viewer?: Viewer;
  readonly query?: string;
  readonly settle?: boolean;
}

export async function openPublicPages(
  page: Page, options: OpenOptions,
): Promise<string[]> {
  await page.setViewportSize({
    width: options.width, height: options.height ?? 900,
  });
  await page.emulateMedia({ colorScheme: options.viewer ?? 'light' });
  const external = await denyExternalRequests(page);
  const owner = options.owner ?? 'light';
  const response = await page.goto(
    `/_harness/render?fixture=${fixtureFor(options.lng)}`
    + `&template=${options.template}&mode=public`
    + (owner === 'light' ? '' : `&scheme=${owner}`)
    + (options.query ?? ''),
  );
  expect(response?.ok()).toBe(true);
  if (options.settle !== false) {
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    await expectPagesState(page, 'shown');
    await waitForImages(page);
  }
  return external;
}

export async function expectPagesState(
  page: Page, state: string,
): Promise<void> {
  await expect(page.locator('.public-resume-page'), FALLBACK)
    .toHaveAttribute('data-pages', state, { timeout: 15_000 });
}

export async function holdPaginationFonts(page: Page): Promise<void> {
  await page.addInitScript(() => {
    const original = FontFaceSet.prototype.load;
    const waiting: (() => void)[] = [];
    let held = true;
    FontFaceSet.prototype.load = function load(
      this: FontFaceSet, ...args: Parameters<FontFaceSet['load']>
    ) {
      if (held && document.querySelector('.pagination-measurement') !== null) {
        return new Promise<FontFace[]>((resolve, reject) => {
          waiting.push(() => {
            original.apply(this, args).then(resolve, reject);
          });
        });
      }
      return original.apply(this, args);
    };
    Object.assign(window, {
      releasePaginationFonts: () => {
        held = false;
        for (const resume of waiting.splice(0)) resume();
      },
    });
  });
}

export const expectedScale = (clientWidth: number, width = PAGE_WIDTH) =>
  Math.min(1, (clientWidth - 32) / width);

interface Box {
  readonly left: number;
  readonly right: number;
  readonly top: number;
  readonly bottom: number;
  readonly width: number;
  readonly height: number;
}

async function measureSheets(page: Page) {
  return page.evaluate(() => {
    const box = (element: Element) => {
      const rect = element.getBoundingClientRect();
      return {
        left: rect.left, right: rect.right, top: rect.top,
        bottom: rect.bottom, width: rect.width, height: rect.height,
      };
    };
    const content = document.querySelector('.public-resume-page')!;
    return {
      clientWidth: content.clientWidth,
      contentLeft: content.getBoundingClientRect().left,
      rootWidth: document.documentElement.clientWidth,
      scrollWidth: document.documentElement.scrollWidth,
      bar: box(document.querySelector('.public-toolbar')!),
      row: box(document.querySelector('.public-toolbar-inner')!),
      sheets: [...document.querySelectorAll('.public-page')].map((section) => ({
        sheet: box(section.querySelector('.public-page-sheet')!),
        article: box(section.querySelector('.resume-page')!),
        position: box(section.querySelector('.public-page-position')!),
        positionText: section.querySelector('.public-page-position')!
          .textContent?.trim() ?? '',
        positionShown: getComputedStyle(
          section.querySelector('.public-page-position')!,
        ).display !== 'none',
        matrix: getComputedStyle(section.querySelector('.public-page-scale')!)
          .transform,
      })),
    };
  });
}

const near = (actual: number, expected: number, tolerance = 1) =>
  Math.abs(actual - expected) <= tolerance;

export async function expectWideSheets(page: Page): Promise<void> {
  const measured = await measureSheets(page);
  const { sheets } = measured;
  const width = Math.min(PAGE_WIDTH,
    measured.clientWidth - 32);
  const center = measured.contentLeft + measured.clientWidth / 2;
  expect(sheets.length, WIDE_SHEETS).toBeGreaterThan(1);
  const failures: string[] = [];
  if (!near(sheets[0]!.sheet.top, measured.bar.bottom + 24)) {
    failures.push('first sheet 24 px below the bar');
  }
  if (!near(measured.row.left, sheets[0]!.sheet.left)
    || !near(measured.row.right, sheets[0]!.sheet.right)) {
    failures.push('bar row as wide as the page');
  }
  for (const [index, entry] of sheets.entries()) {
    const sheet: Box = entry.sheet;
    if (!near(sheet.width, width)) failures.push(`width ${index}`);
    if (!near(sheet.left + sheet.width / 2, center, 2)) {
      failures.push(`center ${index}: ${sheet.left} ${sheet.width} `
        + `${measured.clientWidth}`);
    }
    if (!entry.positionShown
      || entry.positionText !== `${index + 1} / ${sheets.length}`) {
      failures.push(`position text ${index}`);
    }
    if (entry.position.top < sheet.bottom - 1
      || entry.position.left < sheet.left - 1
      || entry.position.right > sheet.right + 1) {
      failures.push(`position outside ${index}`);
    }
    const next = sheets[index + 1];
    if (next !== undefined) {
      if (!near(next.sheet.top - sheet.bottom, 24)) {
        failures.push(`gap ${index}`);
      }
      if (entry.position.bottom > next.sheet.top + 1) {
        failures.push(`position in gap ${index}`);
      }
    }
  }
  if (measured.scrollWidth > measured.rootWidth + 1) {
    failures.push('page overflow');
  }
  expect(failures, WIDE_SHEETS).toEqual([]);
}

export async function expectSheetScale(page: Page): Promise<void> {
  const measured = await measureSheets(page);
  const scale = expectedScale(measured.clientWidth);
  const failures = measured.sheets.flatMap((entry, index) => {
    const values = /^matrix\(([^)]+)\)$/u.exec(entry.matrix)?.[1]
      ?.split(',').map(Number) ?? [];
    const [a, , , d] = values;
    const problems: string[] = [];
    const scaled = entry.matrix === 'none'
      ? scale === 1
      : near(a ?? 0, scale, 0.001) && near(d ?? 0, scale, 0.001);
    if (!scaled) {
      problems.push(`matrix ${index} ${entry.matrix}`);
    }
    if (!near(entry.article.width, PAGE_WIDTH * scale)) {
      problems.push(`article ${index}`);
    }
    if (!near(entry.sheet.width, entry.article.width)
      || !near(entry.sheet.height, entry.article.height)) {
      problems.push(`box ${index}`);
    }
    return problems;
  });
  expect(failures, SHEET_SCALE).toEqual([]);
}

export async function expectPhoneSheets(page: Page): Promise<void> {
  const measured = await page.evaluate(() => {
    const scroller = document.querySelector<HTMLElement>(
      '.public-pages .paged-resume',
    )!;
    const css = getComputedStyle(scroller);
    const sections = [...scroller.querySelectorAll(':scope > .public-page')];
    const sheet = getComputedStyle(
      scroller.querySelector('.public-page-sheet')!,
    );
    const root = document.documentElement;
    return {
      clientWidth: document.querySelector('.public-resume-page')!.clientWidth,
      rootOverflow: root.scrollWidth - root.clientWidth,
      bodyOverflow: document.body.scrollWidth - document.body.clientWidth,
      scrollerWidth: scroller.clientWidth,
      scrollable: scroller.scrollWidth - scroller.clientWidth,
      overflowX: css.overflowX,
      snapType: css.scrollSnapType,
      snapAlign: sheet.scrollSnapAlign,
      snapStop: sheet.scrollSnapStop,
      sections: sections.map((section) => section.getBoundingClientRect()
        .width),
      sheets: sections.map((section) => section
        .querySelector('.public-page-sheet')!.getBoundingClientRect().width),
    };
  });
  const width = measured.clientWidth - 32;
  expect({
    rootOverflow: measured.rootOverflow <= 1,
    bodyOverflow: measured.bodyOverflow <= 1,
    scroller: near(measured.scrollerWidth, measured.clientWidth)
      || `${measured.scrollerWidth} ${measured.clientWidth}`,
    scrollable: near(measured.scrollable,
      measured.scrollerWidth * (measured.sections.length - 1)),
    overflowX: measured.overflowX,
    snap: [measured.snapType, measured.snapAlign, measured.snapStop],
    onePerScreen: measured.sections.every((section) =>
      near(section, measured.scrollerWidth)),
    sheets: measured.sheets.every((sheet) => near(sheet, width)),
  }, PHONE_SHEETS).toEqual({
    rootOverflow: true,
    bodyOverflow: true,
    scroller: true,
    scrollable: true,
    overflowX: 'auto',
    snap: ['x mandatory', 'center', 'always'],
    onePerScreen: true,
    sheets: true,
  });
}

export async function expectSnapStops(page: Page): Promise<void> {
  const scroller = page.locator(SCROLLER);
  const width = await scroller.evaluate((node) => node.clientWidth);
  const count = await page.locator(PAGE).count();
  for (let target = 1; target < count; target += 1) {
    await scroller.evaluate((node, left) => {
      node.scrollTo({ left, behavior: 'instant' });
    }, (target - 0.4) * width);
    await expect.poll(() => scroller.evaluate((node) => node.scrollLeft),
      SNAP).toBeCloseTo(target * width, 0);
  }
  await scroller.evaluate((node) => {
    node.scrollTo({ left: 0, behavior: 'instant' });
  });
  await expect.poll(() => scroller.evaluate((node) => node.scrollLeft), SNAP)
    .toBe(0);
}

export async function expectPager(
  page: Page, lng: Lng, index: number, count: number,
): Promise<void> {
  const copy = PAGES_COPY[lng];
  const pager = page.locator('.public-pager');
  await expect(pager, PAGER).toBeVisible();
  await expect(pager.locator('.public-pager-position'), PAGER)
    .toHaveText(`${index + 1} / ${count}`);
  await expect(pager.locator('.public-pager-status'), PAGER)
    .toHaveAttribute('aria-hidden', 'true');
  await expect(pager.locator('.public-pager-dot'), PAGER).toHaveCount(count);
  await expect(pager.locator('.public-pager-dot[data-current]'), PAGER)
    .toHaveCount(1);
  const current = await pager.locator('.public-pager-dot').evaluateAll(
    (dots) => dots.findIndex((dot) => dot.hasAttribute('data-current')),
  );
  expect(current, PAGER).toBe(index);
  const previous = pager.getByRole('button', { name: copy.previous });
  const next = pager.getByRole('button', { name: copy.next });
  if (index === 0) await expect(previous, PAGER).toBeDisabled();
  else await expect(previous, PAGER).toBeEnabled();
  if (index === count - 1) await expect(next, PAGER).toBeDisabled();
  else await expect(next, PAGER).toBeEnabled();
  for (const button of [previous, next]) {
    const box = await button.boundingBox();
    expect(box!.width, PAGER).toBeGreaterThanOrEqual(44);
    expect(box!.height, PAGER).toBeGreaterThanOrEqual(44);
  }
}

export async function expectPageIndex(
  page: Page, lng: Lng, index: number, count: number,
): Promise<void> {
  const scroller = page.locator(SCROLLER);
  await expect.poll(async () => {
    const [left, width] = await scroller.evaluate((node) =>
      [node.scrollLeft, node.clientWidth]);
    return Math.abs(left - index * width) <= 2;
  }, PAGE_INDEX).toBe(true);
  await expect(page.locator('.public-pager-position'), PAGE_INDEX)
    .toHaveText(`${index + 1} / ${count}`);
  await expect(page.locator('.public-pages-live'), PAGE_INDEX)
    .toHaveText(PAGES_COPY[lng].page(index + 1, count));
}

export async function expectPagesAria(
  page: Page, lng: Lng, phone: boolean,
): Promise<void> {
  const copy = PAGES_COPY[lng];
  const count = await page.locator(PAGE).count();
  const pages = page.locator('.public-pages');
  await expect(pages, PAGES_ARIA).not.toHaveAttribute('inert');
  await expect(pages, PAGES_ARIA).not.toHaveAttribute('aria-hidden');
  const scroller = page.getByRole('region', { name: copy.pages, exact: true });
  await expect(scroller, PAGES_ARIA).toHaveCount(1);
  if (phone) {
    await expect(scroller, PAGES_ARIA).toHaveAttribute('tabindex', '0');
  } else {
    await expect(scroller, PAGES_ARIA).not.toHaveAttribute('tabindex');
  }
  for (let number = 1; number <= count; number += 1) {
    await expect(scroller.getByRole('region', {
      name: copy.page(number, count), exact: true,
    }), PAGES_ARIA).toHaveCount(1);
  }
  const hidden = await page.locator('.public-page-position').evaluateAll(
    (nodes) => nodes.every((node) =>
      node.getAttribute('aria-hidden') === 'true'),
  );
  expect(hidden, PAGES_ARIA).toBe(true);
  await expect(page.locator('.public-pages-live'), PAGES_ARIA)
    .toHaveAttribute('aria-live', 'polite');
}

export async function expectStaging(page: Page): Promise<void> {
  const pages = page.locator('.public-pages');
  await expect(pages, STAGING).toHaveAttribute('inert', '');
  await expect(pages, STAGING).toHaveAttribute('aria-hidden', 'true');
  await expect(pages, STAGING).toHaveCSS('visibility', 'hidden');
  await expect(pages, STAGING).not.toHaveCSS('display', 'none');
  await expect(page.locator(CONTINUOUS), STAGING).toBeVisible();
}

export async function expectContinuous(
  page: Page, name: string,
): Promise<void> {
  const article = page.locator(CONTINUOUS);
  await expect(article, FALLBACK).toBeVisible();
  await expect(article.locator('.resume-name'), FALLBACK).toHaveText(name);
  await expect(page.locator(PAGE).filter({ visible: true }), FALLBACK)
    .toHaveCount(0);
  const box = await article.boundingBox();
  expect(box?.height ?? 0, FALLBACK).toBeGreaterThan(100);
}

export async function expectNoClipping(page: Page): Promise<void> {
  const scroller = page.locator(SCROLLER);
  const phone = await scroller.evaluate((node) =>
    getComputedStyle(node).overflowX !== 'visible');
  for (const text of await page.locator(
    `${PAGE_ARTICLE} :is(.entry-body p, .entry-body li, .section-heading h2)`,
  ).all()) {
    await expect.poll(async () => {
      const measured = await measureScreenText(text, undefined,
        phone ? [SCROLLER] : []);
      return {
        visible: measured.visible,
        contained: measured.contained && measured.separated,
      };
    }, CLIPPING).toEqual({ visible: true, contained: true });
  }
  if (phone) {
    expect(await scroller.evaluate((node) =>
      node.scrollHeight - node.clientHeight), CLIPPING)
      .toBeLessThanOrEqual(1);
  }
  const overflow = await page.locator(
    `${PAGE}, ${SHEET}, .public-page-scale, ${PAGE_ARTICLE}`,
  ).evaluateAll((nodes) => nodes.map((node) =>
    getComputedStyle(node).overflowY).filter((value) => value !== 'visible'));
  expect(overflow, CLIPPING).toEqual([]);
}

export async function expectPagesPaint(
  page: Page, template: PaintTemplate, dark: boolean,
): Promise<void> {
  const expected = PUBLIC_PAINT[template] as {
    foreground: string; band?: string;
  };
  const articles = page.locator(PAGE_ARTICLE);
  expect(await articles.count(), PAPER).toBeGreaterThan(0);
  for (const article of await articles.all()) {
    await expect(article, PAPER)
      .toHaveCSS('background-color', paper(template));
    await expect(article, FOREGROUND)
      .toHaveCSS('color', rgb(expected.foreground));
  }
  if (expected.band !== undefined) {
    await expect(page.locator(`${PAGE} .resume-header`).first(), BAND)
      .toHaveCSS('background-color', rgb(expected.band));
  }
  await expect.poll(() => page.evaluate(() =>
    getComputedStyle(document.documentElement).backgroundColor), DESK)
    .toBe(DESK_COLORS[dark ? 'dark' : 'light']);
  await expect(page.locator('.public-resume-page'), DESK)
    .toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
  await expectBarPaint(page, dark, BAR);
  const channels = SHADOW_CHANNELS[dark ? 'dark' : 'light'];
  await expect(page.locator(SHEET).first(), SHADOW).toHaveCSS('box-shadow', [
    `rgba(${channels}, 0.14) 0px 0px 0px 0.5px`,
    `rgba(${channels}, 0.08) 0px 1px 2px 0px`,
    `rgba(${channels}, 0.2) 0px 10px 26px -8px`,
    `rgba(${channels}, 0.22) 0px 32px 60px -28px`,
  ].join(', '));
  await expect(page.locator(SHEET).first(), SHADOW)
    .toHaveCSS('border-top-left-radius', '4px');
}

export async function expectPrint(page: Page): Promise<void> {
  await page.emulateMedia({ media: 'print' });
  try {
    const article = page.locator(CONTINUOUS);
    await expect(article, PRINT).toBeVisible();
    for (const side of ['top', 'right', 'bottom', 'left']) {
      await expect(article, PRINT).toHaveCSS(`padding-${side}`, '0px');
    }
    for (const section of await page.locator(PAGE).all()) {
      await expect(section, PRINT).toBeHidden();
    }
    await expect(page.locator('.public-toolbar'), PRINT).toBeHidden();
    await expect(page.locator('.public-pager'), PRINT).toBeHidden();
    const ground = await page.evaluate(() =>
      getComputedStyle(document.documentElement).backgroundColor);
    expect(Object.values(DESK_COLORS), PRINT).not.toContain(ground);
    await expect(page.locator('style[data-public-page-rule]'), PRINT)
      .toHaveCount(1);
  } finally {
    await page.emulateMedia({ media: 'screen' });
  }
  await page.evaluate(() => new Promise((resolve) => {
    requestAnimationFrame(() => requestAnimationFrame(resolve));
  }));
  await expect(page.locator('.public-resume-page'), PRINT)
    .toHaveAttribute('data-pages', 'shown');
}

export async function pageOneBlocks(
  page: Page, scope: string,
): Promise<Record<string, readonly number[]>> {
  return page.locator(scope).first().evaluate((root) => {
    const result: Record<string, number[]> = {};
    const keep = (key: string, element: Element) => {
      const rect = element.getBoundingClientRect();
      if (rect.height > 0) {
        result[key] = [rect.left, rect.top + window.scrollY, rect.width];
      }
    };
    keep('name', root.querySelector('.resume-name')!);
    keep('photo', root.querySelector('.resume-photo') ?? root);
    for (const heading of root.querySelectorAll('.section-heading h2')) {
      keep(`heading ${heading.textContent?.trim()}`, heading);
    }
    for (const title of root.querySelectorAll('.entry-title')) {
      keep(`title ${title.textContent?.trim()}`, title);
    }
    return result;
  });
}
