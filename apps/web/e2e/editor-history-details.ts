import { CURRENT_VERSION } from '@aboutme/schema/released';
import { expect, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { resolve as resolvePath } from 'node:path';

import { mockSignedInSession } from './support';

export type HistoryLocale = 'vi' | 'en';

export const LONG_ASSISTANT = 'Northwind Quarterly Planning Assistant for '
  + 'Distributed Teams with an Unusually Long Display Name';
export const LONG_TITLE = 'Senior Staff Backend and Platform Reliability '
  + 'Engineer, Payments Infrastructure and Risk, Southeast Asia Region';

export const HISTORY_COPY = {
  vi: {
    history: 'Lịch sử',
    listLabel: 'Các phiên bản của CV',
    current: 'Phiên bản hiện tại',
    causeEdit: 'Trước khi bạn chỉnh sửa',
    causeAssistant: (name: string) => `Trước khi trợ lý ${name} chỉnh sửa`,
    causeRestore: 'Trước khi bạn khôi phục một phiên bản',
    countName: (n: number) => `${n} trên tối đa 50 phiên bản`,
    backToEditing: 'Quay lại chỉnh sửa',
    restore: 'Khôi phục',
    deleteVersion: 'Xóa phiên bản này',
    versionTitle: 'Phiên bản lúc',
    showing: 'Đang xem phiên bản lúc',
    restored: 'Đã khôi phục phiên bản lúc',
    restoreTitle: /^Khôi phục phiên bản lúc/u,
  },
  en: {
    history: 'History',
    listLabel: 'Resume versions',
    current: 'Current version',
    causeEdit: 'Before your edits',
    causeAssistant: (name: string) => `Before edits by the assistant ${name}`,
    causeRestore: 'Before you restored a version',
    countName: (n: number) => `${n} of at most 50 versions`,
    backToEditing: 'Back to editing',
    restore: 'Restore',
    deleteVersion: 'Delete this version',
    versionTitle: 'Version from',
    showing: 'Showing the version from',
    restored: 'Restored the version from',
    restoreTitle: /^Restore the version from/u,
  },
} as const;

const TABS = {
  vi: { edit: 'Chỉnh sửa', preview: 'Xem trước' },
  en: { edit: 'Edit', preview: 'Preview' },
} as const;

const RAIL_ACTIONS = [
  'open-document', 'open-structure', 'open-design', 'open-templates',
  'open-photo', 'open-history',
] as const;

const DOCUMENT = JSON.parse(readFileSync(resolvePath(
  import.meta.dirname, '../../../packages/schema/fixtures/full.json',
), 'utf8')) as { personalDetails?: { photo?: unknown } };
delete DOCUMENT.personalDetails?.photo;

interface Checkpoint {
  readonly id: string;
  readonly startedAt: string;
  readonly cause: 'edit' | 'assistant' | 'restore';
  readonly actor: { kind: 'owner' } | { kind: 'assistant'; name: string };
  readonly title: string;
  readonly byteSize: number;
}

export interface HistoryHarness {
  readonly restores: { id: string; ifMatch: string | null;
    key: string | null; }[];
}

const SEED_AGE = [5 * 60_000, 3 * 3_600_000, 2 * 86_400_000, 10 * 86_400_000];

function seed(): Checkpoint[] {
  const at = (index: number) =>
    new Date(Date.now() - SEED_AGE[index]!).toISOString();
  const owner = { kind: 'owner' } as const;
  return [
    { id: 'cp-1', startedAt: at(0), cause: 'edit', actor: owner,
      title: LONG_TITLE, byteSize: 900 },
    { id: 'cp-2', startedAt: at(1), cause: 'assistant',
      actor: { kind: 'assistant', name: LONG_ASSISTANT },
      title: 'Fixture', byteSize: 900 },
    { id: 'cp-3', startedAt: at(2), cause: 'restore', actor: owner,
      title: 'Fixture', byteSize: 900 },
    { id: 'cp-4', startedAt: at(3), cause: 'edit', actor: owner,
      title: 'Fixture', byteSize: 900 },
  ];
}

const NO_STORE = { 'Cache-Control': 'no-store, no-transform' };

export async function openHistoryEditor(
  page: Page, locale: HistoryLocale, width: number,
): Promise<HistoryHarness> {
  const harness: HistoryHarness = { restores: [] };
  const items = seed();
  const metadata = {
    id: 'resume-1', title: 'Fixture', lng: 'en', live: false,
    downloadEnabled: false, seoGeoEnabled: false, slug: null,
    publicTitle: null, faviconEmoji: null, schemaVersion: CURRENT_VERSION,
    createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z',
  };
  let current: Record<string, unknown> = { ...metadata };
  let revision = 1;
  const resumeHeaders = () => ({
    ...NO_STORE,
    'Content-Type': 'application/json',
    'ETag': `"r${revision}"`,
    'X-Resume-Schema-Version': String(CURRENT_VERSION),
  });
  await page.setViewportSize({ width, height: 900 });
  await page.context().addCookies([{
    name: 'aboutme-locale', value: locale, url: 'http://127.0.0.1:20092',
  }]);
  await mockSignedInSession(page);
  await page.route('**/api/v1/events', async (route) => {
    await route.fulfill({
      status: 200, contentType: 'text/event-stream', body: '',
    });
  });
  await page.route('**/api/v1/resumes/resume-1', async (route) => {
    if (route.request().method() !== 'GET') {
      await route.continue();
      return;
    }
    await route.fulfill({
      status: 200,
      headers: resumeHeaders(),
      json: { data: {
        ...current, revision: String(revision), document: DOCUMENT,
      } },
    });
  });
  await page.route(/\/api\/v1\/resumes\/resume-1\/checkpoints(\/.*)?$/u,
    async (route) => {
      const request = route.request();
      const path = new URL(request.url()).pathname.split('/checkpoints')[1]!;
      if (request.method() === 'GET' && path === '') {
        await route.fulfill({ status: 200, headers: NO_STORE,
          json: { data: items } });
        return;
      }
      const id = path.split('/')[1] ?? '';
      if (request.method() === 'GET') {
        const item = items.find((entry) => entry.id === id)!;
        await route.fulfill({ status: 200, headers: NO_STORE, json: { data: {
          ...item, lng: item.id === 'cp-1' ? 'vi' : 'en',
          schemaVersion: CURRENT_VERSION, document: DOCUMENT,
        } } });
        return;
      }
      if (request.method() === 'POST' && path.endsWith('/restore')) {
        const headers = request.headers();
        harness.restores.push({
          id, ifMatch: headers['if-match'] ?? null,
          key: headers['idempotency-key'] ?? null,
        });
        const restored = items.find((entry) => entry.id === id)!;
        revision += 1;
        current = { ...metadata, title: restored.title, lng: 'vi' };
        items.unshift({
          id: 'cp-restore', startedAt: new Date().toISOString(),
          cause: 'restore', actor: { kind: 'owner' }, title: 'Fixture',
          byteSize: 900,
        });
        await route.fulfill({
          status: 200, headers: resumeHeaders(),
          json: { data: {
            ...current, revision: String(revision), document: DOCUMENT,
          } },
        });
        return;
      }
      await route.fulfill({ status: 204, headers: NO_STORE });
    });
  const response = await page.goto('/app/resumes/resume-1');
  expect(response?.status()).toBe(200);
  await page.addStyleTag({
    content: '*, *::before, *::after { transition: none !important; '
      + 'animation: none !important; }',
  });
  await expect(page.getByTestId('save-status')).toBeVisible();
  await page.evaluate(() => document.fonts.ready);
  return harness;
}

export async function openHistoryPanel(
  page: Page, locale: HistoryLocale,
): Promise<void> {
  await page.locator('[data-action="open-history"]').click();
  await expect(page.getByRole('listbox', {
    name: HISTORY_COPY[locale].listLabel,
  }).getByRole('option')).toHaveCount(5);
  await page.evaluate(() => document.fonts.ready);
}

export async function chooseVersion(
  page: Page, locale: HistoryLocale, id = 'cp-1',
): Promise<void> {
  await page.locator(`[data-version-id="${id}"]`).click();
  const restore = page.locator('[data-action="history-restore"]');
  await expect(restore).toBeVisible();
  await expect(restore).toBeEnabled();
  await expect(page.locator('[data-history-bar] h2')).toContainText(
    HISTORY_COPY[locale].versionTitle,
  );
  await page.evaluate(() => document.fonts.ready);
}

export const isNarrowHistory = (width: number): boolean => width <= 1152;
export const isCompactBar = (width: number): boolean => width <= 672;

interface RailTool {
  action: string;
  left: number; right: number; top: number; bottom: number;
  width: number; height: number; pressed: string | null;
}

async function measureRail(page: Page) {
  return page.locator('[data-region="app-rail"]').evaluate((rail, actions) => {
    const box = rail.getBoundingClientRect();
    const tools: RailTool[] = actions.map((action) => {
      const button = rail.querySelector<HTMLElement>(
        `[data-action="${action}"]`)!;
      const b = button.getBoundingClientRect();
      return {
        action, left: b.left, right: b.right, top: b.top, bottom: b.bottom,
        width: b.width, height: b.height,
        pressed: button.getAttribute('aria-pressed'),
      };
    });
    const doc = document.documentElement;
    return {
      horizontal: getComputedStyle(rail).flexDirection === 'row',
      left: box.left, right: box.right, top: box.top, bottom: box.bottom,
      scrollWidth: rail.scrollWidth, clientWidth: rail.clientWidth,
      scrollHeight: rail.scrollHeight, clientHeight: rail.clientHeight,
      docOverflow: doc.scrollWidth - doc.clientWidth,
      tools,
    };
  }, RAIL_ACTIONS);
}

export async function expectRailGap(page: Page, width: number): Promise<void> {
  const rail = await measureRail(page);
  const expected = width < 360 ? 4 : 8;
  const gaps = rail.tools.slice(0, 5).slice(1).map((tool, index) => {
    const previous = rail.tools[index]!;
    const gap = rail.horizontal
      ? tool.left - previous.right
      : tool.top - previous.bottom;
    return Math.abs(gap - expected) <= 1 ? expected : gap;
  });
  expect(gaps, 'History rail gap.').toEqual([expected, expected, expected,
    expected]);
}

export async function expectRailFit(page: Page): Promise<void> {
  const rail = await measureRail(page);
  const inside = rail.tools.every((tool) => tool.left >= rail.left - 1
    && tool.right <= rail.right + 1 && tool.top >= rail.top - 1
    && tool.bottom <= rail.bottom + 1 && tool.width > 0);
  expect({
    tools: rail.tools.length,
    inside,
    railWidth: rail.scrollWidth <= rail.clientWidth + 1,
    railHeight: rail.scrollHeight <= rail.clientHeight + 1,
    page: rail.docOverflow <= 0,
  }, 'History rail fit.').toEqual({
    tools: 6, inside: true, railWidth: true, railHeight: true, page: true,
  });
}

export async function expectHistoryTool(
  page: Page, locale: HistoryLocale, width: number, open: boolean,
): Promise<void> {
  const rail = await measureRail(page);
  const tool = rail.tools[5]!;
  const before = rail.tools[4]!;
  const expected = width < 360 ? 4 : 8;
  const after = rail.horizontal
    ? tool.left - before.right
    : tool.top - before.bottom;
  const end = rail.horizontal
    ? rail.right - tool.right
    : rail.bottom - tool.bottom;
  const named = page.locator('[data-region="app-rail"]').getByRole('button', {
    name: HISTORY_COPY[locale].history, exact: true,
  });
  await expect(named, 'History tool name.').toHaveAttribute('data-action',
    'open-history');
  expect({
    size: [Math.round(tool.width), Math.round(tool.height)],
    afterSpacer: after >= expected * 2 - 1,
    atEnd: Math.abs(end - (rail.horizontal ? 12 : 8)) <= 1,
  }, 'History tool size.').toEqual({
    size: [44, 44], afterSpacer: true, atEnd: true,
  });
  expect(
    rail.tools.map((entry) => entry.pressed),
    'History tool pressed.',
  ).toEqual(RAIL_ACTIONS.map((action) =>
    open && action === 'open-history'
      ? 'true'
      : !open && action === 'open-document' ? 'true' : 'false'));
}

interface OptionMeasure {
  id: string; selected: string | null;
  top: number; bottom: number; left: number; right: number; height: number;
  scrollWidth: number; clientWidth: number;
  line1: string; line2: string;
  line1Height: number; line2Height: number;
  line2ScrollWidth: number; line2ClientWidth: number;
  line2Right: number; line2Style: string[]; relative: boolean;
  iconSize: number;
}

async function measureList(page: Page) {
  return page.locator('[role="listbox"]').first().evaluate((list) => {
    const box = list.getBoundingClientRect();
    const options: OptionMeasure[] = Array.from(
      list.querySelectorAll<HTMLElement>('[role="option"]'), (option) => {
        const b = option.getBoundingClientRect();
        const text = option.querySelector<HTMLElement>(':scope > span')!;
        const [one, two] = Array.from(text.children) as HTMLElement[];
        const style = getComputedStyle(two!);
        return {
          id: option.dataset.versionId!,
          selected: option.getAttribute('aria-selected'),
          top: b.top, bottom: b.bottom, left: b.left, right: b.right,
          height: b.height,
          scrollWidth: option.scrollWidth, clientWidth: option.clientWidth,
          line1: one!.textContent!.replace(/\s+/gu, ' ').trim(),
          line2: two!.textContent!.trim(),
          line1Height: one!.getBoundingClientRect().height,
          line2Height: two!.getBoundingClientRect().height,
          line2ScrollWidth: two!.scrollWidth,
          line2ClientWidth: two!.clientWidth,
          line2Right: Math.max(
            one!.getBoundingClientRect().right,
            two!.getBoundingClientRect().right,
          ),
          line2Style: [style.whiteSpace, style.textOverflow, style.overflowX],
          relative: one!.querySelector('span') !== null,
          iconSize: option.querySelector('svg')!
            .getBoundingClientRect().width,
        };
      });
    const rule = list.querySelector('[role="presentation"]')!
      .getBoundingClientRect();
    const doc = document.documentElement;
    return {
      left: box.left, right: box.right,
      rule: { top: rule.top, bottom: rule.bottom },
      docOverflow: doc.scrollWidth - doc.clientWidth,
      options,
    };
  });
}

export async function expectCurrentFirst(
  page: Page, locale: HistoryLocale,
): Promise<void> {
  const list = await measureList(page);
  const [first, ...rest] = list.options;
  expect({
    ids: list.options.map((option) => option.id),
    firstBeforeAll: rest.every((option) => first!.top < option.top),
    selected: list.options.map((option) => option.selected),
    label: first!.line1,
    ruleAfterFirst: list.rule.top >= first!.bottom - 1
      && rest[0]!.top >= list.rule.bottom - 1,
  }, 'History current first.').toEqual({
    ids: ['current', 'cp-1', 'cp-2', 'cp-3', 'cp-4'],
    firstBeforeAll: true,
    selected: ['true', 'false', 'false', 'false', 'false'],
    label: HISTORY_COPY[locale].current,
    ruleAfterFirst: true,
  });
}

export async function expectCount(
  page: Page, locale: HistoryLocale,
): Promise<void> {
  const count = page.locator('[data-history-count]');
  await expect(count, 'History count.').toHaveText('4 / 50');
  await expect(count, 'History count.').toHaveAttribute('aria-label',
    HISTORY_COPY[locale].countName(4));
  const place = await count.evaluate((node) => {
    const row = node.parentElement!.getBoundingClientRect();
    const heading = node.parentElement!.querySelector('h2')!
      .getBoundingClientRect();
    const own = node.getBoundingClientRect();
    return {
      atEnd: Math.abs(row.right - own.right) <= 1,
      sameRow: own.top < heading.bottom && own.bottom > heading.top,
      size: getComputedStyle(node).fontSize,
    };
  });
  expect(place, 'History count.').toEqual({
    atEnd: true, sameRow: true, size: '14px',
  });
}

export async function expectOptionLayout(page: Page): Promise<void> {
  const { options } = await measureList(page);
  const versions = options.slice(1);
  const gaps = versions.slice(1).map((option, index) => {
    const gap = option.top - versions[index]!.bottom;
    return Math.abs(gap - 4) <= 1 ? 4 : gap;
  });
  expect({
    tall: options.every((option) => option.height >= 63.5),
    gaps,
    icons: options.map((option) => Math.round(option.iconSize)),
    relative: versions.map((option) => option.relative),
  }, 'History option layout.').toEqual({
    tall: true,
    gaps: [4, 4, 4],
    icons: [16, 16, 16, 16, 16],
    relative: [true, true, true, false],
  });
}

export async function expectOptionContainment(page: Page): Promise<void> {
  const list = await measureList(page);
  expect(list.options.map((option) => ({
    id: option.id,
    inside: option.left >= list.left - 1 && option.right <= list.right + 1,
    own: option.scrollWidth <= option.clientWidth + 1,
    text: option.line2Right <= option.right + 1,
  })), 'History option containment.').toEqual(list.options.map(
    (option) => ({ id: option.id, inside: true, own: true, text: true }),
  ));
  expect(list.docOverflow <= 0, 'History option containment.').toBe(true);
}

export async function expectLineTruncation(
  page: Page, locale: HistoryLocale,
): Promise<void> {
  const { options } = await measureList(page);
  const copy = HISTORY_COPY[locale];
  expect(options.slice(1).map((option) => option.line2),
    'History line truncation.').toEqual([
    copy.causeEdit, copy.causeAssistant(LONG_ASSISTANT), copy.causeRestore,
    copy.causeEdit,
  ]);
  expect(options.map((option) => ({
    oneLine: option.line1Height <= 22 && option.line2Height <= 22,
    style: option.line2Style,
  })), 'History line truncation.').toEqual(options.map(() => ({
    oneLine: true, style: ['nowrap', 'ellipsis', 'hidden'],
  })));
  const long = options[2]!;
  expect(long.line2ScrollWidth > long.line2ClientWidth + 1,
    'History line truncation.').toBe(true);
}

interface BarMeasure {
  bar: { left: number; right: number; top: number; bottom: number;
    scrollWidth: number; clientWidth: number; };
  title: { top: number; bottom: number; right: number };
  note: string;
  row: { left: number; right: number };
  buttons: { left: number; right: number; top: number; width: number;
    height: number; scrollWidth: number; clientWidth: number; }[];
  viewport: number;
  docOverflow: number;
}

async function measureBar(page: Page): Promise<BarMeasure> {
  return page.locator('[data-history-bar]').evaluate((bar) => {
    const box = bar.getBoundingClientRect();
    const block = bar.firstElementChild!.getBoundingClientRect();
    const note = bar.querySelector('p')!;
    const buttons = [
      '[data-action="history-delete"]', '[data-action="history-back"]',
      '[data-action="history-restore"]',
    ].map((selector) => {
      const b = bar.querySelector(selector)!.getBoundingClientRect();
      const node = bar.querySelector(selector)!;
      return { left: b.left, right: b.right, top: b.top, width: b.width,
        height: b.height, scrollWidth: node.scrollWidth,
        clientWidth: node.clientWidth };
    });
    const rowBox = bar.children[1]!.getBoundingClientRect();
    const doc = document.documentElement;
    return {
      row: { left: rowBox.left, right: rowBox.right },
      bar: { left: box.left, right: box.right, top: box.top,
        bottom: box.bottom, scrollWidth: bar.scrollWidth,
        clientWidth: bar.clientWidth },
      title: { top: block.top, bottom: block.bottom, right: block.right },
      note: getComputedStyle(note).display,
      buttons,
      viewport: doc.clientWidth,
      docOverflow: doc.scrollWidth - doc.clientWidth,
    };
  });
}

export async function expectBarWrap(page: Page, width: number): Promise<void> {
  const m = await measureBar(page);
  const [del, back, restore] = m.buttons as [
    BarMeasure['buttons'][0], BarMeasure['buttons'][0],
    BarMeasure['buttons'][0]];
  const pad = isCompactBar(width) ? 16 : 24;
  const middle = (b: typeof del): number => b.top + b.height / 2;
  const sameRow = Math.abs(middle(back) - middle(restore)) <= 1
    && Math.abs(middle(back) - middle(del)) <= 1;
  const ordered = del.left < back.left && back.right <= restore.left + 1;
  const toEdge = Math.abs(m.bar.right - pad - restore.right) <= 1;
  if (isCompactBar(width)) {
    expect({
      sameRow, ordered, toEdge,
      below: Math.min(del.top, back.top) >= m.title.bottom - 1,
      start: Math.abs(del.left - (m.bar.left + pad)) <= 1,
      note: m.note,
    }, 'History version bar wrap.').toEqual({
      sameRow: true, ordered: true, toEdge: true, below: true, start: true,
      note: 'none',
    });
  } else {
    expect({
      sameRow, ordered,
      beside: del.left >= m.title.right - 1 && del.top < m.title.bottom,
      note: m.note,
    }, 'History version bar wrap.').toEqual({
      sameRow: true, ordered: true, beside: true, note: 'block',
    });
  }
}

export async function expectBarFit(page: Page): Promise<void> {
  const m = await measureBar(page);
  expect({
    bar: m.bar.scrollWidth <= m.bar.clientWidth + 1,
    buttons: m.buttons.every((button) => button.left >= m.bar.left - 1
      && button.right <= m.bar.right + 1 && button.width > 0),
    row: m.row.left >= m.bar.left - 1 && m.row.right <= m.bar.right + 1,
    labels: m.buttons.every((button) =>
      button.scrollWidth <= button.clientWidth + 1),
    inView: m.bar.left >= -1 && m.bar.right <= m.viewport + 1,
    page: m.docOverflow <= 0,
  }, 'History version bar overflow.').toEqual({
    bar: true, buttons: true, row: true, labels: true, inView: true,
    page: true,
  });
  const heading = page.locator('[data-history-bar] h2');
  await expect(heading, 'History version bar overflow.').toHaveCSS(
    'text-overflow', 'ellipsis');
  expect(await heading.evaluate((node) => ({
    wrap: getComputedStyle(node).whiteSpace,
    height: node.getBoundingClientRect().height <= 21 + 1,
  })), 'History version bar overflow.').toEqual({
    wrap: 'nowrap', height: true,
  });
}

export async function expectBarButtons(
  page: Page, width: number,
): Promise<void> {
  const m = await measureBar(page);
  const [del, back, restore] = m.buttons as [
    BarMeasure['buttons'][0], BarMeasure['buttons'][0],
    BarMeasure['buttons'][0]];
  expect({
    delete: [Math.round(del.width), Math.round(del.height)],
    tall: isCompactBar(width)
      ? back.height >= 39.5 && restore.height >= 39.5
      : Math.round(back.height) === 40 && Math.round(restore.height) === 40,
  }, 'History version bar buttons.').toEqual({
    delete: isCompactBar(width) ? [40, 40] : [36, 36],
    tall: true,
  });
}

export async function expectPreviewTab(
  page: Page, locale: HistoryLocale,
): Promise<void> {
  const tabs = page.getByRole('tablist');
  const copy = TABS[locale];
  await expect(tabs.getByRole('tab', { name: copy.preview }),
    'History preview tab.').toHaveAttribute('aria-selected', 'true');
  await expect(tabs.getByRole('tab', { name: copy.edit }),
    'History preview tab.').toHaveAttribute('aria-selected', 'false');
  await expect(page.locator('[data-region="preview"]'),
    'History preview tab.').toHaveAttribute('data-narrow-active', 'true');
  await expect(page.locator('[data-region="inspector"]'),
    'History preview tab.').toHaveAttribute('data-narrow-active', 'false');
  await expect(page.locator('[data-history-bar] h2'),
    'History preview tab.').toBeFocused();
  const box = await page.locator('[data-history-bar]').boundingBox();
  const height = page.viewportSize()!.height;
  expect(box !== null && box.y >= 0 && box.y + box.height <= height,
    'History preview tab.').toBe(true);
}

export async function expectEditTab(
  page: Page, locale: HistoryLocale,
): Promise<void> {
  const tabs = page.getByRole('tablist');
  const copy = TABS[locale];
  await expect(tabs.getByRole('tab', { name: copy.edit }),
    'History edit tab.').toHaveAttribute('aria-selected', 'true');
  await expect(tabs.getByRole('tab', { name: copy.preview }),
    'History edit tab.').toHaveAttribute('aria-selected', 'false');
  await expect(page.locator('[data-region="inspector"]'),
    'History edit tab.').toHaveAttribute('data-narrow-active', 'true');
  await expect(page.locator('[data-history-bar]'),
    'History edit tab.').toHaveCount(0);
  await expect(page.locator('[data-action="open-history"]'),
    'History edit tab.').toHaveAttribute('aria-pressed', 'false');
  await expect(page.locator('[data-action="open-document"]'),
    'History edit tab.').toHaveAttribute('aria-pressed', 'true');
  await expect(page.locator('[data-action="open-document"]'),
    'History edit tab.').toBeFocused();
}

export async function expectWideSelection(
  page: Page, locale: HistoryLocale,
): Promise<void> {
  await expect(page.getByRole('tablist'), 'History wide selection.')
    .toBeHidden();
  const option = page.locator('[data-version-id="cp-1"]');
  await expect(option, 'History wide selection.')
    .toHaveAttribute('aria-selected', 'true');
  await expect(page.locator('[data-version-id="current"]'),
    'History wide selection.').toHaveAttribute('aria-selected', 'false');
  await expect(option, 'History wide selection.').toBeFocused();
  await expect(page.locator('[data-history-status]'),
    'History wide selection.').toContainText(HISTORY_COPY[locale].showing);
  await expect(page.locator('[data-region="preview"] [data-history-bar]'),
    'History wide selection.').toBeVisible();
}
