import { expect, test, type Page } from '@playwright/test';

import {
  applyStyleMutation, mutateAttribute, mutateText, mutationTest,
  proveMutation,
} from './style-mutation';
import {
  authoredGround, BAND, BAND_TEMPLATES, CONTACT_ROWS, DESK_PAINT,
  expectContactFocusRing, expectContactLeaders, expectContactRhythm,
  expectContactRows, expectFolioPageOnOneLine, expectFolios,
  expectForcedBorders, expectHeaderBand, expectNameSize, expectNoOverflow,
  expectOneColumn, expectPaperAndDesk, expectReadingMeasure,
  expectSheetGeometry, expectSheetsPrint, expectSidePhoto, FOCUS_RING, FOLIO,
  FOLIO_LINE, FORCED, GEOMETRY, LEADER, LONG_FOLIO_NAME, MEASURE, NAME_SIZE,
  ONE_COLUMN, OVERFLOW, PAPER, placeSidePhoto, PRINT, PRINT_OVERRIDES, RHYTHM,
  SHEETS, setFolioName, SIDE_PHOTO, tabToContactLink, TYPE_FLOOR,
} from './public-sheets';
import { denyExternalRequests, waitForImages } from './support';

type Language = 'vi' | 'en';
type Owner = 'light' | 'dark' | 'system';

const NAMES = { vi: 'Nguyễn Thị Minh Châu', en: 'Ada Lovelace' } as const;

export async function openSheets(
  page: Page,
  options: {
    template: string; language: Language; width: number;
    owner?: Owner; darkGround?: boolean;
  },
): Promise<void> {
  await page.setViewportSize({ width: options.width, height: 900 });
  const external = await denyExternalRequests(page);
  const fixture = options.language === 'vi' ? 'vn-full' : 'full';
  const response = await page.goto(
    `/_harness/render?fixture=${fixture}&template=${options.template}`
    + '&mode=public'
    + (options.owner === undefined || options.owner === 'light'
      ? ''
      : `&scheme=${options.owner}`)
    + (options.darkGround === true ? '&ground=dark' : ''),
  );
  expect(response?.ok()).toBe(true);
  await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
  await waitForImages(page);
  expect(external).toEqual([]);
}

const owners = ['light', 'dark', 'system'] as const;
const viewers = ['light', 'dark'] as const;

function paperTests(): void {
  for (const [index, cell] of [
    { template: 'designer-tag', darkGround: false },
    { template: 'classic-serif', darkGround: true },
  ].entries()) {
    for (const [ownerIndex, owner] of owners.entries()) {
      for (const [viewerIndex, viewer] of viewers.entries()) {
        const language: Language
          = (index + ownerIndex + viewerIndex) % 2 === 0 ? 'vi' : 'en';
        const name = cell.darkGround ? 'already-dark ground' : cell.template;
        test(`sheets paper and desk ${name} ${owner} ${viewer}`, async ({
          page,
        }) => {
          await page.emulateMedia({ colorScheme: viewer });
          await openSheets(page, { ...cell, language, width: 390, owner });
          const dark = owner === 'dark'
            || (owner === 'system' && viewer === 'dark');
          await expectPaperAndDesk(page,
            authoredGround(cell.template, cell.darkGround), dark);
          await expectSheetsPrintAfter(page,
            authoredGround(cell.template, cell.darkGround));
        });
      }
    }
  }
}

async function expectSheetsPrintAfter(
  page: Page, ground: string,
): Promise<void> {
  await page.emulateMedia({ media: 'print' });
  await expectSheetsPrint(page, ground);
  await page.emulateMedia({ media: 'screen' });
}

function geometryTests(): void {
  for (const [index, width] of [320, 390, 768, 1440].entries()) {
    const language: Language = index % 2 === 0 ? 'vi' : 'en';
    test(`sheets geometry, folio and rows at ${width} ${language}`, async ({
      page,
    }) => {
      await openSheets(page, { template: 'modern-sidebar', language, width });
      await expectSheetGeometry(page);
      await expectOneColumn(page);
      await expectNoOverflow(page);
      await expectFolios(page, NAMES[language]);
      await expectContactRows(page);
      await expectContactLeaders(page, width === 320);
      await expectReadingMeasure(page);
    });
  }
  for (const language of ['vi', 'en'] as const) {
    test(`sheets folio keeps the page on one line at 320 ${language}`, async ({
      page,
    }) => {
      await openSheets(page, { template: 'modern-sidebar', language,
        width: 320 });
      await setFolioName(page, LONG_FOLIO_NAME);
      await expectFolioPageOnOneLine(page);
    });
  }
  for (const [index, template] of BAND_TEMPLATES.entries()) {
    const language: Language = index % 2 === 0 ? 'vi' : 'en';
    const owner: Owner = index === 1 ? 'dark' : 'light';
    test(`sheets header band of ${template} bleeds to the sheet edges`,
      async ({ page }) => {
        await openSheets(page, { template, language, width: 390, owner });
        await expectHeaderBand(page);
        await page.setViewportSize({ width: 1440, height: 900 });
        await expectHeaderBand(page);
      });
  }
  for (const template of ['high-contrast', 'modern-sidebar'] as const) {
    test(`sheets cap the name of ${template} at 390 and 1440`, async ({
      page,
    }) => {
      await openSheets(page, { template, language: 'en', width: 390 });
      await expectNameSize(page, template, true);
      await page.setViewportSize({ width: 1440, height: 900 });
      await expectNameSize(page, template, false);
    });
  }
  for (const language of ['vi', 'en'] as const) {
    test(`sheets rows keep an even rhythm at 390 ${language}`, async ({
      page,
    }) => {
      await openSheets(page,
        { template: 'modern-sidebar', language, width: 390 });
      await expectContactRhythm(page);
    });
  }
  test('sheets rows ring the whole row on keyboard focus', async ({
    page,
  }) => {
    await openSheets(page,
      { template: 'modern-sidebar', language: 'en', width: 390 });
    const rows = page.locator(
      `${SHEETS} header.resume-header .contact-chip--sheets:has(a)`,
    );
    const total = await rows.count();
    expect(total, FOCUS_RING).toBe(7);
    let index = -1;
    for (let visited = 0; visited < total; visited += 1) {
      index = await tabToContactLink(page, index);
      expect(index, FOCUS_RING).toBeGreaterThanOrEqual(0);
      await expectContactFocusRing(page);
    }
  });
  test('sheets print drops the type floor, measure and row height', async ({
    page,
  }) => {
    const authored = await continuousRoles(page, 'classic-serif');
    await openSheets(page, { template: 'classic-serif', language: 'en',
      width: 1440, owner: 'dark' });
    await expectPrintedOverrides(page, authored);
  });
  for (const contentWidth of [479, 480] as const) {
    test(`sheets side photo at a ${contentWidth} px content box`, async ({
      page,
    }) => {
      await openSheets(page,
        { template: 'classic-serif', language: 'en', width: 1440 });
      await placeSidePhoto(page, contentWidth);
      await expectSidePhoto(page, contentWidth);
    });
  }
  test('sheets keep their borders under forced colors', async ({ page }) => {
    await page.emulateMedia({ forcedColors: 'active' });
    await openSheets(page,
      { template: 'executive-band', language: 'vi', width: 768 });
    await expectForcedBorders(page);
  });
}

function geometryMutations(): void {
  const open = (page: Page, width = 390) => openSheets(page,
    { template: 'modern-sidebar', language: 'en', width });
  const sheet = (page: Page) =>
    page.locator(`${SHEETS} > .resume-sheet`).nth(1);
  mutationTest('sheets geometry rejects a sheet shifted 4 px', async ({
    page,
  }) => {
    await open(page);
    await proveMutation(() => expectSheetGeometry(page),
      () => applyStyleMutation(sheet(page),
        { transform: ['translateX(4px)', /matrix/u] }), GEOMETRY);
  });
  mutationTest('sheets geometry rejects a square sheet', async ({ page }) => {
    await open(page);
    await proveMutation(() => expectSheetGeometry(page),
      () => applyStyleMutation(sheet(page), { 'border-radius': '0px' }),
      GEOMETRY);
  });
  mutationTest('sheets geometry rejects a missing sheet gap', async ({
    page,
  }) => {
    await open(page, 1440);
    await proveMutation(() => expectSheetGeometry(page),
      () => applyStyleMutation(sheet(page), { 'margin-bottom': '0px' }),
      GEOMETRY);
  });
  mutationTest('sheets reject a section in a second column', async ({
    page,
  }) => {
    await open(page, 1440);
    await proveMutation(() => expectOneColumn(page),
      () => applyStyleMutation(
        page.locator(`${SHEETS} .resume-section`).nth(2),
        { width: ['50%', /px$/u] }), ONE_COLUMN);
  });
  mutationTest('sheets reject horizontal overflow at 320 px', async ({
    page,
  }) => {
    await open(page, 320);
    await proveMutation(() => expectNoOverflow(page),
      () => applyStyleMutation(sheet(page), { width: '400px' }), OVERFLOW);
  });
  mutationTest('sheets reject a body under 16 px', async ({ page }) => {
    await open(page);
    await proveMutation(() => expectReadingMeasure(page),
      () => applyStyleMutation(page.locator(SHEETS),
        { 'font-size': '14px' }), TYPE_FLOOR);
  });
  mutationTest('sheets reject a text block wider than 62ch', async ({
    page,
  }) => {
    await open(page, 1440);
    await proveMutation(() => expectReadingMeasure(page),
      () => applyStyleMutation(page.locator(`${SHEETS} .entry-body`),
        { 'max-width': 'none' }), MEASURE);
  });
  mutationTest('sheets reject a full-size photo at a 479 px box', async ({
    page,
  }) => {
    await openSheets(page,
      { template: 'classic-serif', language: 'vi', width: 1440 });
    await placeSidePhoto(page, 479);
    await proveMutation(() => expectSidePhoto(page, 479),
      () => applyStyleMutation(
        page.locator(`${SHEETS} .resume-sheet-body`).first(),
        { 'container-type': 'normal' }), SIDE_PHOTO);
  });
  mutationTest('sheets reject a missing forced-colors border', async ({
    page,
  }) => {
    await page.emulateMedia({ forcedColors: 'active' });
    await openSheets(page,
      { template: 'executive-band', language: 'en', width: 768 });
    await proveMutation(() => expectForcedBorders(page),
      () => applyStyleMutation(sheet(page),
        { 'border-top-style': 'none' }), FORCED);
  });
}

interface AuthoredRoles {
  readonly colors: Readonly<Record<string, string>>;
  readonly sizes: Readonly<Record<string, number>>;
}

async function readAuthoredRoles(
  page: Page, root: string,
): Promise<AuthoredRoles> {
  return page.locator(root).first().evaluate((article) => {
    const css = (selector: string) =>
      getComputedStyle(article.querySelector(selector)!);
    const selectors = {
      name: '.resume-name', heading: '.section-heading h2',
      title: '.entry-title', body: '.entry-body p', meta: '.entry-meta',
      link: 'header.resume-header a',
    };
    return {
      colors: Object.fromEntries(Object.entries(selectors)
        .map(([role, selector]) => [role, css(selector).color])),
      sizes: Object.fromEntries(['name', 'title', 'body', 'meta']
        .map((role) => [role, parseFloat(css(
          selectors[role as keyof typeof selectors]).fontSize)])),
    };
  });
}

async function expectSheetsPrintOverrides(
  page: Page, authored: AuthoredRoles,
): Promise<void> {
  const printed = await readAuthoredRoles(page, SHEETS);
  const measured = await page.locator(SHEETS).evaluate((root) => ({
    measure: [...root.querySelectorAll('.entry-body, .resume-headline')]
      .map((block) => getComputedStyle(block).maxWidth),
    rows: [...root.querySelectorAll('.contact-chip--sheets')]
      .map((row) => [getComputedStyle(row).minHeight,
        row.getBoundingClientRect().height] as const),
  }));
  expect(authored.sizes.body, PRINT_OVERRIDES).toBeLessThan(16);
  expect(printed.colors, PRINT_OVERRIDES).toEqual(authored.colors);
  for (const [role, size] of Object.entries(authored.sizes)) {
    expect(Math.abs(printed.sizes[role]! - size), PRINT_OVERRIDES)
      .toBeLessThanOrEqual(0.1);
  }
  expect(measured.measure.length, PRINT_OVERRIDES).toBeGreaterThan(1);
  expect(measured.measure.every((value) => value === 'none'),
    PRINT_OVERRIDES).toBe(true);
  expect(measured.rows.length, PRINT_OVERRIDES).toBe(8);
  expect(measured.rows.every(([min, height]) =>
    (min === 'auto' || min === '0px') && height < 43.5), PRINT_OVERRIDES)
    .toBe(true);
}

async function continuousRoles(
  page: Page, template: string,
): Promise<AuthoredRoles> {
  const reference = await page.context().newPage();
  try {
    await reference.setViewportSize({ width: 1440, height: 900 });
    const external = await denyExternalRequests(reference);
    const response = await reference.goto(
      `/_harness/render?fixture=full&template=${template}&mode=continuous`,
    );
    expect(response?.ok()).toBe(true);
    await expect(reference.locator('[data-fonts-ready="true"]'))
      .toHaveCount(1);
    expect(external).toEqual([]);
    return await readAuthoredRoles(reference,
      '.resume-document:not(.pagination-measurement)');
  } finally {
    await reference.close();
  }
}

async function expectPrintedOverrides(
  page: Page, authored: AuthoredRoles,
): Promise<void> {
  const screen = await readAuthoredRoles(page, SHEETS);
  expect(screen.colors.body, PRINT_OVERRIDES)
    .not.toBe(authored.colors.body);
  expect(screen.sizes.body, PRINT_OVERRIDES).toBeGreaterThanOrEqual(16);
  await page.emulateMedia({ media: 'print' });
  await expectSheetsPrintOverrides(page, authored);
}

function rowMutations(): void {
  const open = (page: Page, width = 320) => openSheets(page,
    { template: 'modern-sidebar', language: 'en', width });
  const rows = (page: Page) =>
    page.locator(`${SHEETS} header.resume-header .contact-chip--sheets`);
  mutationTest('sheets leader rejects a gap before the value', async ({
    page,
  }) => {
    await open(page);
    await proveMutation(() => expectContactLeaders(page, true),
      () => applyStyleMutation(
        page.locator(`${SHEETS} .contact-chip--sheets .contact-value`),
        { 'margin-inline-end': '8px' }, '::before'), LEADER);
  });
  mutationTest('sheets leader rejects a leader before a wrapped value',
    async ({ page }) => {
      await open(page);
      await proveMutation(() => expectContactLeaders(page, true),
        () => applyStyleMutation(rows(page),
          { 'justify-content': 'flex-end' }), LEADER);
    });
  const focused = async (page: Page) => {
    await open(page, 390);
    expect(await tabToContactLink(page, -1), FOCUS_RING).toBe(0);
    return rows(page).first();
  };
  mutationTest('sheets focus ring rejects a ring in another color', async ({
    page,
  }) => {
    const row = await focused(page);
    await proveMutation(() => expectContactFocusRing(page),
      () => applyStyleMutation(row, { 'outline-color': 'rgb(255, 0, 0)' }),
      FOCUS_RING);
  });
  mutationTest('sheets focus ring rejects a row without its ring', async ({
    page,
  }) => {
    const row = await focused(page);
    await proveMutation(() => expectContactFocusRing(page),
      () => applyStyleMutation(row, { 'outline-style': 'none' }),
      FOCUS_RING);
  });
  mutationTest('sheets folio rejects a page number that wraps', async ({
    page,
  }) => {
    await open(page);
    await setFolioName(page, LONG_FOLIO_NAME);
    await proveMutation(() => expectFolioPageOnOneLine(page),
      () => applyStyleMutation(page.locator(`${SHEETS} .resume-folio`),
        { 'flex-shrink': '1', 'white-space': 'normal' }, '::after'),
      FOLIO_LINE);
  });
  mutationTest('sheets side photo rejects a centred photo', async ({
    page,
  }) => {
    await openSheets(page,
      { template: 'classic-serif', language: 'en', width: 1440 });
    await placeSidePhoto(page, 480);
    await proveMutation(() => expectSidePhoto(page, 480),
      () => applyStyleMutation(page.locator(`${SHEETS} .resume-photo`),
        { 'align-self': 'center' }), SIDE_PHOTO);
  });
}

function bandMutations(): void {
  const band = (page: Page) => page.locator(
    `${SHEETS} .resume-sheet-band > header.resume-header`);
  const open = (page: Page) => openSheets(page,
    { template: 'executive-band', language: 'en', width: 390 });
  for (const [name, styles] of [
    ['an inset band', { 'margin-left': '0px' }],
    ['square band corners', { 'border-top-left-radius': '0px' }],
    ['band content off the section start', { 'padding-left': '12px' }],
  ] as const) {
    mutationTest(`sheets band rejects ${name}`, async ({ page }) => {
      await open(page);
      await proveMutation(() => expectHeaderBand(page),
        () => applyStyleMutation(band(page), styles), BAND);
    });
  }
  mutationTest('sheets name rejects a name above its cap', async ({
    page,
  }) => {
    await openSheets(page,
      { template: 'high-contrast', language: 'vi', width: 390 });
    await proveMutation(() => expectNameSize(page, 'high-contrast', true),
      () => applyStyleMutation(page.locator(`${SHEETS} .resume-name`),
        { 'font-size': '36px' }), NAME_SIZE);
  });
  mutationTest('sheets rhythm rejects rows without their padding', async ({
    page,
  }) => {
    await openSheets(page,
      { template: 'modern-sidebar', language: 'en', width: 390 });
    await proveMutation(() => expectContactRhythm(page),
      () => applyStyleMutation(
        page.locator(`${SHEETS} header.resume-header .contact-chip--sheets`),
        { 'padding-top': '0px', 'padding-bottom': '0px' }), RHYTHM);
  });
}

function printMutations(): void {
  const cases = [
    ['the type floor', `${SHEETS}`, { '--fs-body': '16px' }],
    ['the 62ch measure', `${SHEETS} .entry-body`,
      { 'max-width': ['62ch', /px$/u] }],
    ['the 44 px row height', `${SHEETS} .contact-chip--sheets`,
      { 'min-height': '44px' }],
    ['a paper role', `${SHEETS} .entry-body p`,
      { color: 'rgb(255, 0, 0)' }],
  ] as const;
  for (const [name, selector, styles] of cases) {
    mutationTest(`sheets print rejects ${name}`, async ({ page }) => {
      const authored = await continuousRoles(page, 'classic-serif');
      await openSheets(page, { template: 'classic-serif', language: 'en',
        width: 1440, owner: 'dark' });
      await expectPrintedOverrides(page, authored);
      await proveMutation(() => expectSheetsPrintOverrides(page, authored),
        () => applyStyleMutation(page.locator(selector), styles),
        PRINT_OVERRIDES);
    });
  }
}

function folioAndRowMutations(): void {
  const open = (page: Page) => openSheets(page,
    { template: 'modern-sidebar', language: 'vi', width: 390 });
  const folio = (page: Page) => page.locator(`${SHEETS} .resume-folio`).nth(1);
  const assertFolios = (page: Page) => () =>
    expectFolios(page, NAMES.vi);
  mutationTest('sheets folio rejects a wrong page', async ({ page }) => {
    await open(page);
    await proveMutation(assertFolios(page),
      () => mutateAttribute(folio(page), 'data-folio-page', '9 / 9'), FOLIO);
  });
  mutationTest('sheets folio rejects an empty page box', async ({ page }) => {
    await open(page);
    await proveMutation(assertFolios(page),
      () => applyStyleMutation(folio(page), { content: 'none' }, '::after'),
      FOLIO);
  });
  mutationTest('sheets folio rejects folio text in the text layer', async ({
    page,
  }) => {
    await open(page);
    await proveMutation(assertFolios(page),
      () => mutateText(folio(page), '2 / 6'), FOLIO);
  });
  mutationTest('sheets folio rejects a folio in the accessibility tree',
    async ({ page }) => {
      await open(page);
      await proveMutation(assertFolios(page),
        () => mutateAttribute(folio(page), 'aria-hidden', 'false'), FOLIO);
    });
  mutationTest('sheets rows reject a contact row under 44 px', async ({
    page,
  }) => {
    await open(page);
    await proveMutation(() => expectContactRows(page),
      () => applyStyleMutation(
        page.locator(`${SHEETS} .contact-chip--sheets`).first(),
        { 'min-height': '0px', 'height': '30px' }), CONTACT_ROWS);
  });
  mutationTest('sheets rows reject a link hit area smaller than the row',
    async ({ page }) => {
      await open(page);
      await proveMutation(() => expectContactRows(page),
        () => applyStyleMutation(
          page.locator(`${SHEETS} .contact-chip--sheets a`).first(),
          { display: 'none' }, '::after'), CONTACT_ROWS);
    });
}

function paperMutations(): void {
  const open = (page: Page) => openSheets(page,
    { template: 'designer-tag', language: 'en', width: 390, owner: 'dark' });
  const ground = authoredGround('designer-tag');
  mutationTest('sheets paper rejects a changed paper', async ({ page }) => {
    await open(page);
    await proveMutation(() => expectPaperAndDesk(page, ground, true),
      () => applyStyleMutation(
        page.locator(`${SHEETS} > .resume-sheet`).nth(1),
        { 'background-color': 'rgb(255, 0, 0)' }), PAPER);
  });
  mutationTest('sheets desk rejects a changed desk', async ({ page }) => {
    await open(page);
    await proveMutation(() => expectPaperAndDesk(page, ground, true),
      () => applyStyleMutation(page.locator('html'),
        { 'background-color': 'rgb(255, 0, 0)' }), DESK_PAINT);
  });
  mutationTest('sheets print rejects a printed folio', async ({ page }) => {
    await open(page);
    await page.emulateMedia({ media: 'print' });
    await proveMutation(() => expectSheetsPrint(page, ground),
      () => applyStyleMutation(page.locator(`${SHEETS} .resume-folio`),
        { display: 'flex' }), PRINT);
  });
}

export function publicSheetsTests(): void {
  paperTests();
  geometryTests();
  geometryMutations();
  folioAndRowMutations();
  rowMutations();
  bandMutations();
  printMutations();
  paperMutations();
}
