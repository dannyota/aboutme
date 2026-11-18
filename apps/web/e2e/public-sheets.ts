import { expect, type Page } from '@playwright/test';
import { TEMPLATES } from '@aboutme/schema/templates';

import { resumeContract, type ScreenContract } from './display-assertions';

export const SHEETS = '.resume-document.resume-sheets';
export const DESK = { light: '#e6dfd0', dark: '#141210' } as const;
export const DARK_GROUND = '#1f2430';
const LIGHT_PAPER = '#faf6ec';
const DARK_PAPER = '#2a2723';
const SHADOW = { light: '74, 54, 22', dark: '0, 0, 0' } as const;

export const PAPER = 'Sheets paper.';
export const DESK_PAINT = 'Sheets desk.';
export const GEOMETRY = 'Sheets geometry.';
export const FOLIO = 'Sheets folio.';
export const ONE_COLUMN = 'Sheets one column.';
export const OVERFLOW = 'Sheets overflow.';
export const CONTACT_ROWS = 'Sheets contact rows.';
export const TYPE_FLOOR = 'Sheets type floor.';
export const MEASURE = 'Sheets measure.';
export const SIDE_PHOTO = 'Sheets side photo.';
export const PRINT = 'Sheets print.';
export const FORCED = 'Sheets forced colors.';
export const FOCUS_RING = 'Sheets contact focus ring.';
export const LEADER = 'Sheets contact leader.';
export const FOLIO_LINE = 'Sheets folio page line.';
export const PRINT_OVERRIDES = 'Sheets print overrides.';
export const BAND = 'Sheets header band.';
export const NAME_SIZE = 'Sheets name size.';
export const RHYTHM = 'Sheets contact rhythm.';
export const BAND_TEMPLATES = ['creative-accent', 'executive-band',
  'graduate-friendly', 'international-lang'] as const;
export const LONG_FOLIO_NAME = 'Nguyễn Thị Minh Châu Tôn Nữ Hoàng Phương Thảo '
  + 'Trần Lê Bảo Ngọc';

export function rgb(hex: string): string {
  const channels = [1, 3, 5].map((index) =>
    Number.parseInt(hex.slice(index, index + 2), 16));
  return `rgb(${channels.join(', ')})`;
}

const lightness = (hex: string): number => {
  const [r, g, b] = [1, 3, 5].map((start) => {
    const value = Number.parseInt(hex.slice(start, start + 2), 16) / 255;
    return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
  }) as [number, number, number];
  const l = Math.cbrt(0.4122214708 * r + 0.5363770262 * g + 0.0514459929 * b);
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
  return 0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s;
};

export function authoredGround(template: string, darkGround = false): string {
  if (darkGround) return DARK_GROUND;
  const preset = TEMPLATES.find(({ id }) => id === template);
  if (preset === undefined) throw new Error(`Unknown template ${template}`);
  return preset.customization.colors.background.toLowerCase();
}

export function expectedPaper(ground: string, dark: boolean): string {
  if (dark) return lightness(ground) < 0.5 ? ground : DARK_PAPER;
  return ground === '#ffffff' ? LIGHT_PAPER : ground;
}

export function sheetsContract(
  template: string, language: 'vi' | 'en', dark: boolean,
): ScreenContract {
  return {
    ...resumeContract(template, language),
    paint: [`:nth-match(${SHEETS} > .resume-sheet, 1)`, 'background-color',
      rgb(expectedPaper(authoredGround(template), dark))],
  };
}

export async function expectPaperAndDesk(
  page: Page, ground: string, dark: boolean,
): Promise<void> {
  const desk = rgb(dark ? DESK.dark : DESK.light);
  await expect.poll(() => page.evaluate(() =>
    getComputedStyle(document.documentElement).backgroundColor), DESK_PAINT)
    .toBe(desk);
  await expect(page.locator(SHEETS), DESK_PAINT)
    .toHaveCSS('background-color', desk);
  await expect(page.locator('.public-resume-page'), DESK_PAINT)
    .toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
  const sheets = page.locator(`${SHEETS} > .resume-sheet`);
  expect(await sheets.count(), PAPER).toBeGreaterThan(1);
  for (const sheet of await sheets.all()) {
    await expect(sheet, PAPER)
      .toHaveCSS('background-color', rgb(expectedPaper(ground, dark)));
  }
  const shadow = SHADOW[dark ? 'dark' : 'light'];
  await expect(sheets.first(), PAPER).toHaveCSS('box-shadow', [
    `rgba(${shadow}, 0.14) 0px 0px 0px 0.5px`,
    `rgba(${shadow}, 0.08) 0px 1px 2px 0px`,
    `rgba(${shadow}, 0.2) 0px 10px 26px -8px`,
    `rgba(${shadow}, 0.22) 0px 32px 60px -28px`,
  ].join(', '));
}

const clamp = (low: number, value: number, high: number): number =>
  Math.min(Math.max(value, low), high);

export function expectedSheetGeometry(width: number) {
  return {
    width: Math.min(width - 20, 736),
    paddingTop: clamp(28, width * 0.06, 60),
    paddingInline: clamp(22, width * 0.09, 84),
    paddingBottom: clamp(20, width * 0.03, 30),
    radius: clamp(14, width * 0.015, 18),
    gap: clamp(14, width * 0.025, 26),
    folioSpace: clamp(30, width * 0.04, 44),
  };
}

export async function expectSheetGeometry(page: Page): Promise<void> {
  const measured = await page.evaluate((selector) => {
    const article = document.querySelector(selector)!;
    const box = article.getBoundingClientRect();
    const sheets = [...article.querySelectorAll(':scope > .resume-sheet')];
    const bar = document.querySelector('.public-toolbar');
    return {
      article: { left: box.left, width: box.width },
      viewport: document.documentElement.clientWidth,
      barBottom: bar?.getBoundingClientRect().bottom ?? null,
      sheets: sheets.map((sheet) => {
        const rect = sheet.getBoundingClientRect();
        const css = getComputedStyle(sheet);
        const folio = sheet.querySelector(':scope > .resume-folio');
        return {
          left: rect.left, right: rect.right, top: rect.top,
          bottom: rect.bottom, width: rect.width,
          paddingTop: parseFloat(css.paddingTop),
          paddingLeft: parseFloat(css.paddingLeft),
          paddingRight: parseFloat(css.paddingRight),
          paddingBottom: parseFloat(css.paddingBottom),
          radius: [css.borderTopLeftRadius, css.borderTopRightRadius,
            css.borderBottomLeftRadius, css.borderBottomRightRadius]
            .map((value) => parseFloat(value)),
          overflow: [css.overflowX, css.overflowY],
          folioSpace: folio === null
            ? null
            : parseFloat(getComputedStyle(folio).marginTop),
        };
      }),
    };
  }, SHEETS);
  const width = measured.article.width;
  const expected = expectedSheetGeometry(width);
  const near = (value: number, target: number) =>
    Math.abs(value - target) <= 1;
  const centre = measured.article.left + width / 2;
  expect({
    fullWidth: near(width, measured.viewport) && near(measured.article.left, 0),
    sheets: measured.sheets.length > 1,
    width: measured.sheets.every((sheet) => near(sheet.width, expected.width)),
    centred: measured.sheets.every((sheet) =>
      Math.abs((sheet.left + sheet.right) / 2 - centre) <= 2),
    padding: measured.sheets.every((sheet) =>
      near(sheet.paddingTop, expected.paddingTop)
      && near(sheet.paddingLeft, expected.paddingInline)
      && near(sheet.paddingRight, expected.paddingInline)
      && near(sheet.paddingBottom, expected.paddingBottom)),
    radius: measured.sheets.every((sheet) =>
      sheet.radius.every((value) => near(value, expected.radius))),
    gap: measured.sheets.every((sheet, index) => index === 0
      || near(sheet.top - measured.sheets[index - 1]!.bottom, expected.gap)),
    firstGap: measured.barBottom === null
      || near(measured.sheets[0]!.top - measured.barBottom, expected.gap),
    folioSpace: measured.sheets.every((sheet) => sheet.folioSpace !== null
      && near(sheet.folioSpace, expected.folioSpace)),
    unclipped: measured.sheets.every((sheet) =>
      sheet.overflow.every((value) => value === 'visible')),
  }, GEOMETRY).toEqual({
    fullWidth: true, sheets: true, width: true, centred: true, padding: true,
    radius: true, gap: true, firstGap: true, folioSpace: true,
    unclipped: true,
  });
}

export async function expectFolios(
  page: Page, fullName: string,
): Promise<void> {
  const article = page.locator(SHEETS);
  const measured = await article.evaluate((root) => {
    const sheets = [...root.querySelectorAll(':scope > .resume-sheet')];
    const pseudo = (element: Element, which: '::before' | '::after') => {
      const css = getComputedStyle(element, which);
      return {
        content: css.content,
        width: parseFloat(css.width) || 0,
        height: parseFloat(css.height) || 0,
      };
    };
    return {
      count: sheets.length,
      text: (document.body as HTMLElement).innerText,
      folios: sheets.map((sheet) => {
        const folio = sheet.lastElementChild!;
        return {
          folio: folio.classList.contains('resume-folio'),
          hidden: folio.getAttribute('aria-hidden'),
          name: folio.getAttribute('data-folio-name'),
          page: folio.getAttribute('data-folio-page'),
          text: folio.textContent,
          children: folio.childNodes.length,
          before: pseudo(folio, '::before'),
          after: pseudo(folio, '::after'),
        };
      }),
    };
  });
  const pages = Array.from({ length: measured.count }, (_, index) =>
    `${index + 1} / ${measured.count}`);
  expect(measured.folios.map((folio) => [
    folio.folio, folio.hidden, folio.name, folio.page, folio.text,
    folio.children,
  ]), FOLIO).toEqual(pages.map((value) =>
    [true, 'true', fullName, value, '', 0]));
  for (const folio of measured.folios) {
    expect([folio.before.content, folio.after.content].every((content) =>
      content.endsWith(' / ""') && content.length > 6), FOLIO).toBe(true);
    expect(folio.before.width > 0 && folio.before.height > 0
      && folio.after.width > 0 && folio.after.height > 0, FOLIO).toBe(true);
  }
  expect(pages.filter((value) => measured.text.includes(value)), FOLIO)
    .toEqual([]);
  const snapshot = await article.ariaSnapshot();
  expect(pages.filter((value) => snapshot.includes(value)), FOLIO)
    .toEqual([]);
}

export async function expectOneColumn(page: Page): Promise<void> {
  const measured = await page.locator(SHEETS).evaluate((root) => ({
    columns: root.querySelectorAll('.layout-two-columns, aside').length,
    sections: [...root.querySelectorAll('.resume-sheet-body')].flatMap(
      (body) => {
        const css = getComputedStyle(body);
        const box = body.getBoundingClientRect();
        const left = box.left + parseFloat(css.paddingLeft);
        const width = box.width - parseFloat(css.paddingLeft)
          - parseFloat(css.paddingRight);
        return [...body.querySelectorAll(':scope > .resume-section')]
          .map((section) => {
            const rect = section.getBoundingClientRect();
            return Math.abs(rect.left - left) <= 1
              && Math.abs(rect.width - width) <= 1 && rect.height > 0;
          });
      }),
  }));
  expect(measured.columns, ONE_COLUMN).toBe(0);
  expect(measured.sections.length, ONE_COLUMN).toBeGreaterThan(3);
  expect(measured.sections.every(Boolean), ONE_COLUMN).toBe(true);
}

export async function expectNoOverflow(page: Page): Promise<void> {
  expect(await page.evaluate((selector) => {
    const root = document.documentElement;
    return root.scrollWidth <= root.clientWidth + 1
      && [...document.querySelectorAll(`${selector} > .resume-sheet`)]
        .every((sheet) => {
          const box = sheet.getBoundingClientRect();
          return box.left >= -1 && box.right <= root.clientWidth + 1;
        });
  }, SHEETS), OVERFLOW).toBe(true);
}

export async function expectContactRows(page: Page): Promise<void> {
  const rows = page.locator(
    `${SHEETS} header.resume-header .contact-chip--sheets`,
  );
  expect(await rows.count(), CONTACT_ROWS).toBe(8);
  let links = 0;
  for (const row of await rows.all()) {
    await row.scrollIntoViewIfNeeded();
    const measured = await row.evaluate((element) => {
      const box = element.getBoundingClientRect();
      const anchor = element.querySelector('a');
      const x = (box.left + box.right) / 2;
      const y = (box.top + box.bottom) / 2;
      const points = [[box.left + 2, y], [box.right - 2, y],
        [x, box.top + 2], [x, box.bottom - 2]] as const;
      return {
        height: box.height,
        link: anchor !== null,
        covered: anchor === null || points.every(([left, top]) =>
          document.elementFromPoint(left, top)?.closest('a') === anchor),
      };
    });
    if (measured.link) links += 1;
    expect(measured.height, CONTACT_ROWS).toBeGreaterThanOrEqual(43.5);
    expect(measured.covered, CONTACT_ROWS).toBe(true);
  }
  expect(links, CONTACT_ROWS).toBeGreaterThan(0);
}

export async function expectReadingMeasure(page: Page): Promise<void> {
  const measured = await page.locator(SHEETS).evaluate((root) => {
    const limit = (element: Element) => {
      const probe = document.createElement('span');
      probe.style.cssText = 'position:absolute;visibility:hidden;'
        + 'display:block;width:62ch;padding:0;border:0';
      element.append(probe);
      const width = probe.getBoundingClientRect().width;
      probe.remove();
      return width;
    };
    const blocks = [...root.querySelectorAll('.entry-body, .resume-headline')];
    return {
      base: parseFloat(getComputedStyle(root).fontSize),
      body: [...root.querySelectorAll('.entry-body p, .entry-body li')]
        .map((node) => parseFloat(getComputedStyle(node).fontSize)),
      profile: root.querySelectorAll('.resume-section .entry-body').length,
      blocks: blocks.map((block) => {
        const max = limit(block);
        const css = getComputedStyle(block).maxWidth;
        return css !== 'none' && parseFloat(css) <= max + 0.5
          && block.getBoundingClientRect().width <= max + 1;
      }),
    };
  });
  expect(measured.base, TYPE_FLOOR).toBeGreaterThanOrEqual(16);
  expect(measured.body.length, TYPE_FLOOR).toBeGreaterThan(0);
  expect(measured.body.every((size) => size >= 15.95), TYPE_FLOOR).toBe(true);
  expect(measured.profile, MEASURE).toBeGreaterThan(0);
  expect(measured.blocks.length, MEASURE).toBeGreaterThan(1);
  expect(measured.blocks.every(Boolean), MEASURE).toBe(true);
}

export async function expectSidePhoto(
  page: Page, contentWidth: 479 | 480,
): Promise<void> {
  const measured = await page.locator(`${SHEETS} > .resume-sheet`).first()
    .evaluate((sheet) => {
      const header = sheet.querySelector('.resume-header')!;
      const photo = header.querySelector('.resume-photo')!
        .getBoundingClientRect();
      const text = header.querySelector('.resume-header-text')!
        .getBoundingClientRect();
      const body = sheet.querySelector('.resume-sheet-body')!;
      return {
        body: body.getBoundingClientRect().width,
        photo: photo.width,
        gap: text.left - photo.right,
        side: photo.right <= text.left + 0.5,
        start: Math.abs(photo.top - text.top),
        tall: text.height - photo.height,
        fontSize: parseFloat(getComputedStyle(header).fontSize),
      };
    });
  const narrow = contentWidth === 479;
  expect(Math.abs(measured.body - contentWidth), SIDE_PHOTO)
    .toBeLessThanOrEqual(0.5);
  expect(measured.side, SIDE_PHOTO).toBe(true);
  expect(Math.abs(measured.photo - (narrow ? 66 : 96)), SIDE_PHOTO)
    .toBeLessThanOrEqual(0.5);
  expect(Math.abs(measured.gap - (narrow ? 14 : 1.25 * measured.fontSize)),
    SIDE_PHOTO).toBeLessThanOrEqual(1);
  expect(measured.tall, SIDE_PHOTO).toBeGreaterThan(20);
  expect(measured.start, SIDE_PHOTO).toBeLessThanOrEqual(1);
}

export async function placeSidePhoto(
  page: Page, contentWidth: 479 | 480,
): Promise<void> {
  await page.locator(`${SHEETS} > .resume-sheet`).first()
    .evaluate((sheet, width) => {
      const body = sheet.querySelector<HTMLElement>('.resume-sheet-body')!;
      body.style.width = `${width}px`;
      sheet.querySelector('.resume-header')!
        .setAttribute('data-photo-position', 'left');
    }, contentWidth);
}

export async function expectSheetsPrint(
  page: Page, ground: string,
): Promise<void> {
  const measured = await page.evaluate((selector) => {
    const article = document.querySelector(selector)!;
    const sheets = [...article.querySelectorAll(':scope > .resume-sheet')];
    return {
      root: getComputedStyle(document.documentElement).backgroundColor,
      article: getComputedStyle(article).backgroundColor,
      folios: [...article.querySelectorAll('.resume-folio')]
        .map((folio) => getComputedStyle(folio).display),
      sheets: sheets.map((sheet) => {
        const css = getComputedStyle(sheet);
        return [css.boxShadow, parseFloat(css.borderTopLeftRadius),
          parseFloat(css.marginBottom)];
      }),
      columns: article.querySelectorAll('.layout-two-columns, aside').length,
    };
  }, SHEETS);
  expect(measured.article, PRINT).toBe(rgb(ground));
  expect([rgb(DESK.light), rgb(DESK.dark)], PRINT)
    .not.toContain(measured.root);
  expect(measured.folios.length, PRINT).toBeGreaterThan(1);
  expect(measured.folios.every((display) => display === 'none'), PRINT)
    .toBe(true);
  expect(measured.sheets.every(([shadow, radius, gap]) =>
    shadow === 'none' && radius === 0 && gap === 0), PRINT).toBe(true);
  expect(measured.columns, PRINT).toBe(0);
}

export async function expectForcedBorders(page: Page): Promise<void> {
  const sheets = page.locator(`${SHEETS} > .resume-sheet`);
  expect(await sheets.count(), FORCED).toBeGreaterThan(1);
  for (const sheet of await sheets.all()) {
    await expect(sheet, FORCED).toHaveCSS('border-top-style', 'solid');
    await expect(sheet, FORCED).toHaveCSS('border-top-width', '1px');
    await expect(sheet, FORCED).toHaveCSS('border-left-width', '1px');
  }
}

export async function setFolioName(page: Page, name: string): Promise<void> {
  await page.locator(`${SHEETS} .resume-folio`).evaluateAll(
    (folios, value) => {
      for (const folio of folios) folio.setAttribute('data-folio-name', value);
    }, name);
  await expect(page.locator(`${SHEETS} .resume-folio`).first())
    .toHaveAttribute('data-folio-name', name);
}

export async function expectFolioPageOnOneLine(page: Page): Promise<void> {
  const measured = await page.locator(`${SHEETS} > .resume-sheet`)
    .evaluateAll((sheets) => sheets.map((sheet) => {
      const folio = sheet.querySelector<HTMLElement>(':scope > .resume-folio')!;
      const after = getComputedStyle(folio, '::after');
      const before = getComputedStyle(folio, '::before');
      const probe = document.createElement('span');
      probe.textContent = folio.getAttribute('data-folio-page');
      probe.style.cssText = 'position:absolute;visibility:hidden;'
        + 'white-space:nowrap;padding:0;border:0';
      for (const property of ['font-family', 'font-size', 'font-weight',
        'font-style', 'letter-spacing', 'text-transform']) {
        probe.style.setProperty(property, after.getPropertyValue(property));
      }
      sheet.append(probe);
      const natural = probe.getBoundingClientRect().width;
      probe.remove();
      const size = parseFloat(after.fontSize);
      const line = after.lineHeight.endsWith('px')
        ? parseFloat(after.lineHeight)
        : parseFloat(after.lineHeight) * size;
      return {
        line,
        natural,
        afterWidth: parseFloat(after.width),
        beforeHeight: parseFloat(before.height),
        beforeWidth: parseFloat(before.width),
        gap: parseFloat(getComputedStyle(folio).columnGap) || 0,
        width: folio.getBoundingClientRect().width,
        overflow: folio.scrollWidth - folio.clientWidth,
      };
    }));
  expect(measured.length, FOLIO_LINE).toBeGreaterThan(1);
  for (const folio of measured) {
    expect({
      oneLine: folio.afterWidth >= folio.natural - 0.5,
      longName: folio.beforeHeight >= 2 * folio.line - 0.5,
      fits: folio.beforeWidth + folio.gap + folio.afterWidth
        <= folio.width + 1 && folio.overflow <= 1,
    }, FOLIO_LINE).toEqual({ oneLine: true, longName: true, fits: true });
  }
}

export async function tabToContactLink(
  page: Page, after: number,
): Promise<number> {
  for (let presses = 0; presses < 40; presses += 1) {
    await page.keyboard.press('Tab');
    const index = await page.evaluate((selector) => {
      const active = document.activeElement;
      if (!(active instanceof HTMLAnchorElement)) return -1;
      return [...document.querySelectorAll(
        `${selector} header.resume-header .contact-chip--sheets`,
      )].findIndex((row) => row.contains(active));
    }, SHEETS);
    if (index > after) return index;
  }
  return -1;
}

export async function expectContactFocusRing(page: Page): Promise<void> {
  const measured = await page.evaluate((selector) => {
    const active = document.activeElement;
    const row = active?.closest(
      `${selector} header.resume-header .contact-chip--sheets`,
    );
    if (!(active instanceof HTMLAnchorElement) || row === null
      || row === undefined) {
      return null;
    }
    const probe = document.createElement('span');
    probe.style.cssText = 'position:absolute;color:var(--color-link)';
    row.append(probe);
    const link = getComputedStyle(probe).color;
    probe.remove();
    const css = getComputedStyle(row);
    const rowBox = row.getBoundingClientRect();
    const anchorBox = active.getBoundingClientRect();
    return {
      focusVisible: active.matches(':focus-visible'),
      style: css.outlineStyle,
      width: css.outlineWidth,
      offset: css.outlineOffset,
      color: css.outlineColor === link,
      anchor: getComputedStyle(active).outlineStyle,
      contains: anchorBox.left >= rowBox.left - 0.5
        && anchorBox.right <= rowBox.right + 0.5
        && anchorBox.top >= rowBox.top - 0.5
        && anchorBox.bottom <= rowBox.bottom + 0.5,
      painted: link !== 'rgba(0, 0, 0, 0)',
    };
  }, SHEETS);
  expect(measured, FOCUS_RING).not.toBeNull();
  expect(measured, FOCUS_RING).toEqual({
    focusVisible: true, style: 'solid', width: '2px', offset: '2px',
    color: true, anchor: 'none', contains: true, painted: true,
  });
}

export async function expectContactLeaders(
  page: Page, wrapped: boolean,
): Promise<void> {
  const rows = await page.locator(
    `${SHEETS} header.resume-header .contact-chip--sheets`,
  ).evaluateAll((elements) => elements.map((row) => {
    const label = row.querySelector(':scope > .contact-label')!;
    const value = row.querySelector(':scope > .contact-value');
    const rowBox = row.getBoundingClientRect();
    const labelBox = label.getBoundingClientRect();
    const clip = ['clip', 'hidden'].includes(getComputedStyle(row).overflowX);
    if (value === null) {
      return {
        kind: 'label', clip, ok: label.querySelectorAll(':scope > a').length
          === 1 && Math.abs(labelBox.left - rowBox.left) <= 1,
      };
    }
    const css = getComputedStyle(value);
    const leader = getComputedStyle(value, '::before');
    const box = value.getBoundingClientRect();
    const right = box.left + parseFloat(css.borderLeftWidth)
      + parseFloat(css.paddingLeft) + parseFloat(leader.marginLeft)
      + parseFloat(leader.width);
    const range = document.createRange();
    range.selectNodeContents(value);
    const start = Math.min(...[...range.getClientRects()]
      .filter((rect) => rect.width > 0).map((rect) => rect.left));
    if (box.top >= labelBox.bottom - 1) {
      return { kind: 'wrapped', clip, ok: right <= rowBox.left + 1 };
    }
    return {
      kind: 'line', clip,
      ok: Math.abs(right - start) <= 1 && right - labelBox.right >= 1
        && leader.content === '""' && leader.borderBottomStyle === 'dotted'
        && leader.borderBottomWidth === '1px',
    };
  }));
  const count = (kind: string) =>
    rows.filter((row) => row.kind === kind).length;
  expect(rows.map((row) => [row.kind, row.clip, row.ok]), LEADER)
    .toEqual(rows.map((row) => [row.kind, true, true]));
  expect(count('line'), LEADER).toBeGreaterThan(0);
  if (wrapped) expect(count('wrapped'), LEADER).toBeGreaterThan(0);
}

export async function expectHeaderBand(page: Page): Promise<void> {
  const measured = await page.locator(`${SHEETS} > .resume-sheet`).first()
    .evaluate((sheet) => {
      const band = sheet.querySelector(':scope > .resume-sheet-body'
        + ' > div > header.resume-header');
      const section = sheet.closest('.resume-sheets')!
        .querySelector('.resume-section');
      if (band === null || section === null) return null;
      const near = (left: number, right: number) =>
        Math.abs(left - right) <= 1;
      const sheetBox = sheet.getBoundingClientRect();
      const box = band.getBoundingClientRect();
      const sectionBox = section.getBoundingClientRect();
      const sheetCss = getComputedStyle(sheet);
      const css = getComputedStyle(band);
      const radius = parseFloat(sheetCss.borderTopLeftRadius);
      return {
        marked: band.parentElement!.className === 'resume-sheet-band',
        top: near(box.top, sheetBox.top),
        left: near(box.left, sheetBox.left),
        right: near(box.right, sheetBox.right),
        corners: radius >= 14 && [css.borderTopLeftRadius,
          css.borderTopRightRadius].every((value) =>
          near(parseFloat(value), radius)),
        foot: [css.borderBottomLeftRadius, css.borderBottomRightRadius]
          .every((value) => parseFloat(value) === 0),
        start: near(box.left + parseFloat(css.borderLeftWidth)
          + parseFloat(css.paddingLeft), sectionBox.left),
        end: near(box.right - parseFloat(css.borderRightWidth)
          - parseFloat(css.paddingRight), sectionBox.right),
        tinted: css.backgroundColor !== sheetCss.backgroundColor,
        unclipped: sheetCss.overflowX === 'visible'
          && sheetCss.overflowY === 'visible',
      };
    });
  expect(measured, BAND).toEqual({
    marked: true, top: true, left: true, right: true, corners: true, foot: true,
    start: true, end: true, tinted: true, unclipped: true,
  });
}

export function expectedNameSize(template: string, narrow: boolean): number {
  const preset = TEMPLATES.find(({ id }) => id === template);
  if (preset === undefined) throw new Error(`Unknown template ${template}`);
  const base = preset.customization.font.baseSizePx;
  return Math.min(2 * base, (narrow ? 1.7 : 2) * Math.max(base, 16));
}

export async function expectNameSize(
  page: Page, template: string, narrow: boolean,
): Promise<void> {
  const measured = await page.locator(`${SHEETS} .resume-name`)
    .evaluate((name) => ({
      size: parseFloat(getComputedStyle(name).fontSize),
      content: name.closest('.resume-sheet-body')!
        .getBoundingClientRect().width,
    }));
  expect(measured.content < 480, NAME_SIZE).toBe(narrow);
  expect(Math.abs(measured.size - expectedNameSize(template, narrow)),
    NAME_SIZE).toBeLessThanOrEqual(0.1);
}

export async function expectContactRhythm(page: Page): Promise<void> {
  const rows = await page.locator(
    `${SHEETS} header.resume-header .contact-chip--sheets`,
  ).evaluateAll((elements) => elements.map((row) => {
    const rects: DOMRect[] = [];
    const walker = document.createTreeWalker(row, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node !== null;
      node = walker.nextNode()) {
      if ((node.textContent ?? '').trim() === '') continue;
      const range = document.createRange();
      range.selectNodeContents(node);
      rects.push(...[...range.getClientRects()]
        .filter((rect) => rect.height > 0));
    }
    const label = row.querySelector(':scope > .contact-label')!
      .getBoundingClientRect();
    const value = row.querySelector(':scope > .contact-value')
      ?.getBoundingClientRect();
    return {
      top: Math.min(...rects.map((rect) => rect.top)),
      bottom: Math.max(...rects.map((rect) => rect.bottom)),
      wrapped: value !== undefined && value.top >= label.bottom - 1,
      height: row.getBoundingClientRect().height,
    };
  }));
  const gaps = rows.slice(1).map((row, index) =>
    row.top - rows[index]!.bottom);
  expect(rows.filter((row) => row.wrapped).length, RHYTHM)
    .toBeGreaterThan(0);
  expect(rows.filter((row) => !row.wrapped).length, RHYTHM)
    .toBeGreaterThan(0);
  expect(gaps.length, RHYTHM).toBeGreaterThan(1);
  expect(Math.max(...gaps) - Math.min(...gaps), RHYTHM)
    .toBeLessThanOrEqual(1);
  expect(rows.every((row) => row.height >= 43.5), RHYTHM).toBe(true);
}
