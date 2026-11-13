import { expect, test, type Page } from '@playwright/test';
import { TEMPLATES } from '@aboutme/schema/templates';
import { getDocument } from 'pdfjs-dist/legacy/build/pdf.mjs';

import {
  applyStyleMutation, mutateAttribute, mutationTest, proveMutation,
} from './style-mutation';
import { denyExternalRequests } from './support';

type Lng = 'vi' | 'en';
type Paper = 'a4' | 'letter';

const PROJECTS_URL = 'https://aboutme.vn/harness-public/projects';
const PROJECT_PAGES = [
  `${PROJECTS_URL}/short-title`, `${PROJECTS_URL}/long-title`,
];
const OWN_LINK = 'https://github.com/ada/engine';
const PAPER_PX: Readonly<Record<Paper, number>> = { a4: 794, letter: 816 };
const MM_TO_PX = 96 / 25.4;
const MIN_TARGET = 44;

const PARITY = 'Title link parity.';
const ANNOTATIONS = 'Title link annotations.';
const TARGETS = 'Bar target size.';
const LEGIBLE = 'Bar legibility.';

const fixture = (lng: Lng) => (lng === 'vi' ? 'vn-full' : 'full');

async function open(page: Page, query: string): Promise<void> {
  const external = await denyExternalRequests(page);
  const response = await page.goto(`/_harness/render?${query}&links=1`);
  expect(response?.ok()).toBe(true);
  await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
  expect(external).toEqual([]);
}

async function openScreen(
  page: Page, template: string, lng: Lng, width: number,
): Promise<void> {
  await page.setViewportSize({ width, height: 900 });
  await open(page,
    `fixture=${fixture(lng)}&template=${template}&mode=public`);
}

async function openPrint(
  page: Page, template: string, lng: Lng, paper: Paper,
): Promise<void> {
  await open(page, [
    `fixture=${fixture(lng)}`, `template=${template}`, 'mode=continuous',
    'print=1', `paper=${paper}`,
  ].join('&'));
  const margin = await page.locator('.resume-document').evaluate((node) =>
    getComputedStyle(node).getPropertyValue('--page-margin-x').trim());
  expect(margin).toMatch(/^\d+(?:\.\d+)?mm$/u);
  const width = Math.round(
    PAPER_PX[paper] - 2 * Number.parseFloat(margin) * MM_TO_PX,
  );
  await page.setViewportSize({ width, height: 1123 });
  await page.emulateMedia({ media: 'print' });
}

interface Title {
  readonly text: string;
  readonly entry: string | null;
  readonly href: string | null;
  readonly box: readonly number[];
  readonly lines: number;
  readonly glyphs: readonly (readonly number[])[];
  readonly font: readonly string[];
  readonly color: string | null;
  readonly decoration: string | null;
}

async function measureTitles(page: Page): Promise<Title[]> {
  return page.locator('.resume-document .entry-title').evaluateAll(
    (titles) => titles.map((title) => {
      const round = (value: number) => Math.round(value * 100) / 100;
      const anchor = title.querySelector('a');
      const leaf = anchor ?? title.querySelector('strong') ?? title;
      const origin = title.getBoundingClientRect();
      const text = leaf.firstChild!;
      const range = document.createRange();
      const data = text.textContent ?? '';
      const indices = Array.from({ length: data.length }, (_, at) => at);
      const boxes = indices.flatMap((index) => {
        if (/\s/u.test(data[index]!)) return [];
        range.setStart(text, index);
        range.setEnd(text, index + 1);
        const rect = range.getBoundingClientRect();
        return [[
          rect.left - origin.left, rect.top - origin.top,
          rect.height, rect.width,
        ].map(round)];
      });
      const glyphs = boxes.map((box, index) => {
        const next = boxes[index + 1];
        return next !== undefined && next[1] !== box[1]
          ? box.slice(0, 3)
          : box;
      });
      const style = getComputedStyle(leaf);
      const anchorStyle = anchor === null ? null : getComputedStyle(anchor);
      return {
        text: title.textContent ?? '',
        entry: anchor?.getAttribute('data-project-entry') ?? null,
        href: anchor?.getAttribute('href') ?? null,
        box: [origin.width, origin.height].map(round),
        lines: new Set(glyphs.map((glyph) => glyph[1])).size,
        glyphs,
        font: [
          style.fontWeight, style.fontSize, style.fontFamily, style.fontStyle,
          style.letterSpacing, style.wordSpacing, style.lineHeight,
        ],
        color: anchorStyle?.color ?? null,
        decoration: anchorStyle === null
          ? null
          : [
              anchorStyle.textDecorationLine, anchorStyle.textDecorationStyle,
            ].join(' '),
      };
    }),
  );
}

async function expectTitleParity(page: Page, where: string): Promise<void> {
  const message = `${PARITY} ${where}`;
  const titles = await measureTitles(page);
  const linked = titles.filter((title) => title.entry !== null);
  expect(linked.map((title) => title.href), message).toEqual(PROJECT_PAGES);
  const own = titles.filter((title) => title.href === OWN_LINK);
  expect(own, message).toHaveLength(1);
  for (const title of linked) {
    const plain = titles.filter((other) =>
      other.href === null && other.text === title.text);
    expect(plain, message).toHaveLength(1);
    expect({
      box: title.box, glyphs: title.glyphs, font: title.font,
    }, message).toEqual({
      box: plain[0]!.box, glyphs: plain[0]!.glyphs, font: plain[0]!.font,
    });
    expect([title.color, title.decoration], message)
      .toEqual([own[0]!.color, own[0]!.decoration]);
  }
  expect(linked[1]!.lines, message).toBeGreaterThan(1);
}

for (const [index, template] of TEMPLATES.entries()) {
  const lng: Lng = index % 2 === 0 ? 'vi' : 'en';
  test(`linked and plain titles match in ${template.id} ${lng}`, async ({
    page,
  }) => {
    test.setTimeout(60_000);
    for (const width of [390, 1440]) {
      await openScreen(page, template.id, lng, width);
      await expectTitleParity(page, `screen ${width}`);
    }
    for (const paper of ['a4', 'letter'] as const) {
      await page.emulateMedia({ media: 'screen' });
      await openPrint(page, template.id, lng, paper);
      await expectTitleParity(page, `print ${paper}`);
    }
  });
}

mutationTest('title parity rejects a wider linked title', async ({ page }) => {
  await openScreen(page, 'modern-sidebar', 'vi', 1440);
  await proveMutation(() => expectTitleParity(page, 'mutation'),
    () => applyStyleMutation(page.locator('a[data-project-entry]'),
      { 'letter-spacing': '1px' }), PARITY);
});

mutationTest('title parity rejects a lighter linked title in print', async ({
  page,
}) => {
  await openPrint(page, 'classic-serif', 'en', 'letter');
  await proveMutation(() => expectTitleParity(page, 'mutation'),
    () => applyStyleMutation(page.locator('a[data-project-entry]'),
      { 'font-weight': '400' }), PARITY);
});

mutationTest('title parity rejects a project link styled unlike a link',
  async ({ page }) => {
    await openScreen(page, 'engineer-compact', 'en', 1440);
    await proveMutation(() => expectTitleParity(page, 'mutation'),
      () => applyStyleMutation(page.locator('a[data-project-entry]'),
        { 'text-decoration-line': 'none' }), PARITY);
  });

async function expectAnnotations(page: Page): Promise<void> {
  const pdf = await page.pdf({
    displayHeaderFooter: false,
    margin: { bottom: 0, left: 0, right: 0, top: 0 },
    preferCSSPageSize: true,
    printBackground: true,
    scale: 1,
  });
  const loadingTask = getDocument({
    data: new Uint8Array(pdf),
    isImageDecoderSupported: false,
    isOffscreenCanvasSupported: false,
    useSystemFonts: false,
  });
  const urls: string[] = [];
  try {
    const document = await loadingTask.promise;
    for (let number = 1; number <= document.numPages; number += 1) {
      const annotations = await (await document.getPage(number))
        .getAnnotations() as { subtype?: string; url?: string }[];
      for (const annotation of annotations) {
        if (annotation.subtype === 'Link' && annotation.url !== undefined) {
          urls.push(annotation.url);
        }
      }
    }
  } finally {
    await loadingTask.destroy();
  }
  expect([...new Set(urls.filter((url) => url.startsWith(PROJECTS_URL)))]
    .sort(), ANNOTATIONS).toEqual([...PROJECT_PAGES].sort());
  expect(urls, ANNOTATIONS).toContain(OWN_LINK);
}

test('print titles become link annotations', async ({ page }) => {
  test.setTimeout(60_000);
  await openPrint(page, 'engineer-compact', 'en', 'a4');
  await expectAnnotations(page);
});

mutationTest('title annotations reject a lost project page link', async ({
  page,
}) => {
  test.setTimeout(60_000);
  await openPrint(page, 'engineer-compact', 'en', 'a4');
  await proveMutation(() => expectAnnotations(page),
    () => mutateAttribute(page.locator('a[data-project-entry]').first(),
      'href', OWN_LINK), ANNOTATIONS);
});

interface Target {
  readonly box: { left: number; right: number; top: number; bottom: number };
  readonly width: number;
  readonly height: number;
  readonly clipped: boolean;
  readonly fontSize: number;
}

async function measureBar(page: Page) {
  return page.evaluate(() => {
    const target = (selector: string): Target => {
      const element = document.querySelector(selector)!;
      const rect = element.getBoundingClientRect();
      const hits = (x: number, y: number) =>
        document.elementFromPoint(x, y)?.closest('a') === element;
      const edge = (
        inside: number, outside: number, hit: (value: number) => boolean,
      ) => {
        let near = inside;
        let far = outside;
        for (let step = 0; step < 20; step += 1) {
          const middle = (near + far) / 2;
          if (hit(middle)) near = middle;
          else far = middle;
        }
        return (near + far) / 2;
      };
      const range = document.createRange();
      range.selectNodeContents(element);
      const label = range.getBoundingClientRect();
      const x = rect.left + rect.width / 2;
      const y = rect.top + rect.height / 2;
      const reachX = rect.width / 2 + 10;
      const reachY = rect.height / 2 + 10;
      const style = getComputedStyle(element);
      return {
        box: {
          left: rect.left, right: rect.right, top: rect.top,
          bottom: rect.bottom,
        },
        width: edge(x, x + reachX, (value) => hits(value, y))
          - edge(x, x - reachX, (value) => hits(value, y)),
        height: edge(y, y + reachY, (value) => hits(x, value))
          - edge(y, y - reachY, (value) => hits(x, value)),
        clipped: label.left < rect.left || label.right > rect.right
          || label.top < rect.top || label.bottom > rect.bottom,
        fontSize: Number.parseFloat(style.fontSize),
      };
    };
    const root = document.documentElement;
    const credit = document.createRange();
    credit.selectNodeContents(document.querySelector('.public-credit')!);
    const text = credit.getBoundingClientRect();
    return {
      coarse: matchMedia('(pointer: coarse)').matches,
      credit: {
        left: text.left, right: text.right, top: text.top,
        bottom: text.bottom,
      },
      projects: target('.public-projects'),
      download: target('.public-download'),
      overflow: root.scrollWidth > root.clientWidth,
      viewport: innerWidth,
      bar: document.querySelector('.public-toolbar')?.textContent ?? '',
    };
  });
}

async function expectBar(page: Page, lng: Lng): Promise<void> {
  const label = lng === 'vi' ? 'Dự án' : 'Projects';
  const projects = page.locator('.public-toolbar a.public-projects');
  await expect(projects, LEGIBLE).toHaveCount(1);
  await expect(projects, LEGIBLE).toHaveText(label);
  await expect(projects, LEGIBLE).toHaveAccessibleName(label);
  await expect(projects, LEGIBLE).toHaveAttribute('href', PROJECTS_URL);
  const bar = await measureBar(page);
  expect(bar.coarse, TARGETS).toBe(true);
  for (const name of ['projects', 'download'] as const) {
    expect(bar[name].width, TARGETS).toBeGreaterThanOrEqual(MIN_TARGET - 0.01);
    expect(bar[name].height, TARGETS).toBeGreaterThanOrEqual(MIN_TARGET - 0.01);
  }
  const overlaps = (
    a: Target['box'], b: Target['box'],
  ) => a.left < b.right && b.left < a.right && a.top < b.bottom
    && b.top < a.bottom;
  expect({
    overflow: bar.overflow,
    projectsInside: bar.projects.box.left >= 0
      && bar.projects.box.right <= bar.viewport,
    downloadInside: bar.download.box.left >= 0
      && bar.download.box.right <= bar.viewport,
    clipped: [bar.projects.clipped, bar.download.clipped],
    fontSizes: [bar.projects.fontSize, bar.download.fontSize],
    sameRow: Math.abs(bar.projects.box.top - bar.download.box.top) < 1,
    projectsFirst: bar.projects.box.right <= bar.download.box.left,
    apart: overlaps(bar.projects.box, bar.download.box)
      || overlaps(bar.credit, bar.projects.box)
      || overlaps(bar.credit, bar.download.box),
    count: /\d/u.test(bar.bar),
  }, LEGIBLE).toEqual({
    overflow: false,
    projectsInside: true,
    downloadInside: true,
    clipped: [false, false],
    fontSizes: [14, 14],
    sameRow: true,
    projectsFirst: true,
    apart: false,
    count: false,
  });
}

test.describe('public bar at 320 px', () => {
  test.use({ hasTouch: true });

  for (const lng of ['vi', 'en'] as const) {
    test(`Projects and Download PDF stay legible and tappable ${lng}`,
      async ({ page }) => {
        await openScreen(page, 'modern-sidebar', lng, 320);
        await expectBar(page, lng);
      });
  }

  mutationTest('bar rejects a Projects target under 44 px', async ({
    page,
  }) => {
    await openScreen(page, 'modern-sidebar', 'en', 320);
    await proveMutation(() => expectBar(page, 'en'),
      () => applyStyleMutation(page.locator('.public-projects'),
        { top: '0px', bottom: '0px' }, '::before'), TARGETS);
  });

  mutationTest('bar rejects a squeezed Projects label', async ({ page }) => {
    await openScreen(page, 'modern-sidebar', 'vi', 320);
    await proveMutation(() => expectBar(page, 'vi'),
      () => applyStyleMutation(page.locator('.public-projects'),
        { 'font-size': '9px' }), LEGIBLE);
  });

  mutationTest('bar rejects Projects right of Download PDF', async ({
    page,
  }) => {
    await openScreen(page, 'modern-sidebar', 'en', 320);
    await proveMutation(() => expectBar(page, 'en'),
      () => applyStyleMutation(page.locator('.public-projects'),
        { order: '1' }), LEGIBLE);
  });
});
