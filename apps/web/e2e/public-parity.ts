import { type Browser, expect, type Page } from '@playwright/test';

import { denyExternalRequests } from './support';

export const PUBLIC_PARITY = 'Public page parity.';
export const PARITY_FIXTURE_PAGES = 'Public parity fixture pages.';
export const PARITY_MUTATION = 'Public parity mutation took effect.';
export const PARITY_WIDTHS = [390, 768, 1440] as const;
export const PREVIEW_PAGES = '.harness-render .resume-page';
export const PUBLIC_PAGES = '.public-page .resume-page';

const MEASURED = '.public-pages .pagination-measurement';

export interface ParityMutation {
  readonly rule: string;
  readonly samePages: boolean;
}

export const FORCED_BREAK: ParityMutation = {
  rule: `${MEASURED} :is(.layout-one-column, .resume-main)`
    + ' > .pagination-atomic:nth-last-child(1 of .pagination-atomic)'
    + ' { padding-block-start: 600px !important; }',
  samePages: false,
};
export const MOVED_BREAK: ParityMutation = {
  rule: `${MEASURED} .resume-main > .pagination-atomic:has(`
    + '+ [data-section-key="education"][data-block-kind="heading"])'
    + ' { padding-block-start: 300px !important; }',
  samePages: true,
};

export interface PageStart {
  readonly header: boolean;
  readonly blocks: readonly string[];
}

export interface ParityResult {
  readonly sample: string;
  readonly lng: string;
  readonly engine: string;
  readonly width: number;
  readonly previewPages: number;
  readonly publicPages: number;
  readonly matches: boolean;
  readonly previewStarts: readonly PageStart[];
  readonly publicStarts: readonly PageStart[];
}

export function readPageStarts(scope: string): PageStart[] {
  const pages = Array.from(document.querySelectorAll(scope))
    .filter((element) => !element.classList.contains('pagination-measurement'))
    .sort((a, b) => Number(a.getAttribute('data-page-index'))
      - Number(b.getAttribute('data-page-index')));
  return pages.map((article) => ({
    header: article.querySelector(':scope > div > .pagination-header') !== null,
    blocks: Array.from(article.querySelectorAll(
      ':scope > .layout-one-column, '
      + ':scope > .layout-two-columns > :is(.resume-main, .resume-sidebar)',
    ), (flow) => {
      const name = flow.classList.contains('resume-sidebar')
        ? 'sidebar'
        : 'main';
      const first = flow.querySelector(':scope > .pagination-atomic');
      if (first === null) return `${name}: none`;
      const text = (first.textContent ?? '').normalize('NFC')
        .replace(/\s+/gu, ' ').trim().slice(0, 48);
      return `${name}: ${first.getAttribute('data-section-key') ?? ''} `
        + `${first.getAttribute('data-block-kind') ?? ''} "${text}"`;
    }),
  }));
}

export function expectPageParity(
  preview: readonly PageStart[], shown: readonly PageStart[],
): void {
  expect({ pages: shown.length, starts: shown }, PUBLIC_PARITY)
    .toEqual({ pages: preview.length, starts: preview });
}

export interface ParityCase {
  readonly sample: string;
  readonly lng: string;
  readonly engine: string;
}

export async function checkParity(
  browser: Browser,
  url: string,
  parityCase: ParityCase,
  previewStarts: readonly PageStart[],
): Promise<{ results: ParityResult[]; failures: string[] }> {
  const results: ParityResult[] = [];
  const failures: string[] = [];
  const key = `${parityCase.sample}|${parityCase.lng}|${parityCase.engine}`;
  for (const width of PARITY_WIDTHS) {
    try {
      const shown = await readPublicStarts(browser, url, width);
      const matches = JSON.stringify(shown) === JSON.stringify(previewStarts);
      results.push({
        ...parityCase,
        width,
        previewPages: previewStarts.length,
        publicPages: shown.length,
        matches,
        previewStarts,
        publicStarts: shown,
      });
      console.log(`public-parity ${key} ${width}: pages `
        + `${String(shown.length)}/${String(previewStarts.length)}`
        + (matches ? '' : ' mismatch'));
      expectPageParity(previewStarts, shown);
    } catch (error) {
      failures.push(`${key} ${width}: `
        + (error instanceof Error ? error.message : String(error)));
    }
  }
  return { results, failures };
}

export async function readPublicStarts(
  browser: Browser,
  url: string,
  width: number,
  prepare?: (page: Page) => Promise<void>,
): Promise<PageStart[]> {
  const context = await browser.newContext({
    colorScheme: 'light',
    locale: 'en-US',
    reducedMotion: 'reduce',
    timezoneId: 'UTC',
    viewport: { height: 900, width },
  });
  try {
    const page = await context.newPage();
    const external = await denyExternalRequests(page);
    await prepare?.(page);
    const response = await page.goto(url);
    expect(response?.ok()).toBe(true);
    await expect(page.locator('.public-resume-page'), PUBLIC_PARITY)
      .toHaveAttribute('data-pages', 'shown', { timeout: 15_000 });
    const starts = await page.evaluate(readPageStarts, PUBLIC_PAGES);
    expect(external).toEqual([]);
    return starts;
  } finally {
    await context.close();
  }
}

const REPEATED_SAMPLES: ReadonlySet<string> = new Set([
  'engineer-compact', 'one-page-tight',
]);

export interface MultiPageCase {
  readonly name: string;
  readonly preview: string;
  readonly shown: string;
}

export function multiPageCase(templateId: string): MultiPageCase {
  const base = `/_harness/render?template=${templateId}`;
  if (REPEATED_SAMPLES.has(templateId)) {
    const fixture = `&fixture=sample-${templateId}-en&repeat=2`;
    return {
      name: `sample-x2-${templateId}`,
      preview: `${base}${fixture}&mode=paged`,
      shown: `${base}${fixture}&mode=public`,
    };
  }
  return {
    name: `full-${templateId}`,
    preview: `${base}&fixture=full&mode=paged&wrap=1`,
    shown: `${base}&fixture=full&mode=public`,
  };
}

export async function readPreviewStarts(
  browser: Browser, url: string,
): Promise<PageStart[]> {
  const context = await browser.newContext({
    colorScheme: 'light',
    locale: 'en-US',
    reducedMotion: 'reduce',
    timezoneId: 'UTC',
    viewport: { height: 1123, width: 794 },
  });
  try {
    const page = await context.newPage();
    const external = await denyExternalRequests(page);
    const response = await page.goto(url);
    expect(response?.ok()).toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    await expect(page.locator('[data-pagination-settled="true"]'))
      .toHaveCount(1);
    const starts = await page.evaluate(readPageStarts, PREVIEW_PAGES);
    expect(external).toEqual([]);
    return starts;
  } finally {
    await context.close();
  }
}

async function injectMeasuringRule(page: Page, rule: string): Promise<void> {
  await page.addInitScript((css) => {
    const insert = (): boolean => {
      if (document.head === null) return false;
      const style = document.createElement('style');
      style.textContent = css;
      document.head.append(style);
      return true;
    };
    if (insert()) return;
    const observer = new MutationObserver(() => {
      if (insert()) observer.disconnect();
    });
    observer.observe(document, { childList: true, subtree: true });
  }, rule);
}

export async function proveParityMutation(
  browser: Browser,
  url: string,
  preview: readonly PageStart[],
  mutation: ParityMutation,
): Promise<void> {
  const clean = await readPublicStarts(browser, url, 1440);
  expectPageParity(preview, clean);
  const mutated = await readPublicStarts(browser, url, 1440,
    (page) => injectMeasuringRule(page, mutation.rule));
  if (mutation.samePages) {
    expect({
      pages: mutated.length,
      moved: JSON.stringify(mutated.at(-1)) !== JSON.stringify(clean.at(-1)),
    }, PARITY_MUTATION).toEqual({ pages: clean.length, moved: true });
  } else {
    expect(mutated.length, PARITY_MUTATION).toBeGreaterThan(clean.length);
  }
  expect(() => expectPageParity(preview, mutated)).toThrow(PUBLIC_PARITY);
}
