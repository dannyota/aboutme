import { expect, test, type Page } from '@playwright/test';

import {
  applyStyleMutation, mutateAttribute, mutateText, mutationTest,
  proveMutation,
} from './style-mutation';
import {
  authoredGround, CONTACT_ROWS, DESK_PAINT, expectContactRows, expectFolios,
  expectForcedBorders, expectNoOverflow, expectOneColumn, expectPaperAndDesk,
  expectReadingMeasure, expectSheetGeometry, expectSheetsPrint,
  expectSidePhoto, FOLIO, FORCED, GEOMETRY, MEASURE, ONE_COLUMN, OVERFLOW,
  PAPER, placeSidePhoto, PRINT, SHEETS, SIDE_PHOTO, TYPE_FLOOR,
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
      await expectReadingMeasure(page);
    });
  }
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
  paperMutations();
}
