import type { Resume } from '@aboutme/schema';
import { readFileSync } from 'node:fs';
import { describe, expect, it, vi } from 'vitest';

import {
  bodyBlockElements,
  measurePagination,
  type MeasuredLayout,
} from '../../app/components/resume/measure';
import type { PaginationRequest } from '../../app/components/resume/paginate';

const fixture = JSON.parse(
  readFileSync('../../packages/schema/fixtures/minimal.json', 'utf8'),
) as Resume;

const request = (): PaginationRequest => ({
  document: fixture,
  context: { lng: 'en', mode: 'paged' },
  columns: 1,
  blocks: [
    { sectionKey: 'profile', kind: 'heading', column: 'main' },
    {
      sectionKey: 'profile',
      kind: 'entry',
      entryIndex: 0,
      column: 'main',
    },
  ],
  page: {
    format: 'a4',
    widthPx: 794,
    heightPx: 1123,
    marginXmm: 15,
    marginYmm: 15,
  },
});

const rect = (height: number): DOMRect => ({
  x: 0,
  y: 0,
  width: 100,
  height,
  top: 0,
  right: 100,
  bottom: height,
  left: 0,
  toJSON: () => ({}),
});

describe('measurePagination', () => {
  it('waits for the selected font stack before reading layout', async () => {
    let releaseFonts!: () => void;
    const fontGate = new Promise<void>((resolve) => {
      releaseFonts = resolve;
    });
    const load = vi.fn(async () => {
      await fontGate;
      return [{} as FontFace];
    });
    const root = document.createElement('div');
    root.style.setProperty('--gap-section', '20px');
    root.style.setProperty('--gap-header', '30px');
    root.style.setProperty('--gap-heading', '8px');
    root.style.setProperty('--gap-entry', '6px');
    Object.defineProperty(root.ownerDocument, 'fonts', {
      configurable: true,
      value: { load, ready: Promise.resolve() },
    });
    document.body.replaceChildren(root);

    const header = document.createElement('div');
    header.dataset.paginationHeader = '';
    header.getBoundingClientRect = vi.fn(() => rect(40));
    root.append(header);
    for (const [index, height] of [18, 52].entries()) {
      const block = document.createElement('div');
      block.dataset.paginationBlockIndex = String(index);
      block.getBoundingClientRect = vi.fn(() => rect(height));
      root.append(block);
    }

    const pending = measurePagination(root, request());
    await Promise.resolve();
    expect(header.getBoundingClientRect).not.toHaveBeenCalled();

    releaseFonts();
    const measured = await pending;
    expect(measured).toEqual<MeasuredLayout>({
      columns: 1,
      headerHeightPx: 40,
      headerBodyGapPx: 30,
      blocks: [
        {
          sectionKey: 'profile',
          kind: 'heading',
          column: 'main',
          heightPx: 18,
          gapBeforePx: 20,
        },
        {
          sectionKey: 'profile',
          kind: 'entry',
          entryIndex: 0,
          column: 'main',
          heightPx: 52,
          gapBeforePx: 8,
        },
      ],
    });
    expect(load).toHaveBeenCalledTimes(2);
  });

  it('normalizes measurements taken inside a zoomed preview', async () => {
    const root = document.createElement('div');
    root.style.setProperty('--gap-section', '20px');
    root.style.setProperty('--gap-header', '30px');
    root.style.setProperty('--gap-heading', '8px');
    root.style.setProperty('--gap-entry', '6px');
    root.getBoundingClientRect = () => ({ ...rect(100), width: 50 });
    Object.defineProperty(root, 'offsetWidth', { value: 100 });
    Object.defineProperty(root.ownerDocument, 'fonts', {
      configurable: true,
      value: {
        load: async () => [{} as FontFace],
        ready: Promise.resolve(),
      },
    });
    document.body.replaceChildren(root);

    const header = document.createElement('div');
    header.dataset.paginationHeader = '';
    header.getBoundingClientRect = () => rect(20);
    root.append(header);
    for (const [index, height] of [9, 26].entries()) {
      const block = document.createElement('div');
      block.dataset.paginationBlockIndex = String(index);
      block.getBoundingClientRect = () => rect(height);
      root.append(block);
    }

    await expect(measurePagination(root, request())).resolves.toMatchObject({
      headerHeightPx: 40,
      blocks: [
        { heightPx: 18 },
        { heightPx: 52 },
      ],
    });
  });

  it('gives a later entry part the gap the whole entry lays out', async () => {
    const root = document.createElement('div');
    root.style.setProperty('--gap-section', '20px');
    root.style.setProperty('--gap-header', '30px');
    root.style.setProperty('--gap-heading', '8px');
    root.style.setProperty('--gap-entry', '6px');
    root.getBoundingClientRect = () => ({ ...rect(100), width: 50 });
    Object.defineProperty(root, 'offsetWidth', { value: 100 });
    Object.defineProperty(root.ownerDocument, 'fonts', {
      configurable: true,
      value: {
        load: async () => [{} as FontFace],
        ready: Promise.resolve(),
      },
    });
    document.body.replaceChildren(root);
    const header = document.createElement('div');
    header.dataset.paginationHeader = '';
    header.getBoundingClientRect = () => rect(20);
    root.append(header);
    for (const index of [0, 1, 2, 3]) {
      const block = document.createElement('div');
      block.dataset.paginationBlockIndex = String(index);
      block.getBoundingClientRect = () => rect(10);
      root.append(block);
    }
    // The whole entry: a paragraph, then a two-item list. Rendered at half
    // size, so every gap reads half as large as it lays out.
    const whole = document.createElement('div');
    whole.dataset.paginationWholeEntry = '1';
    whole.innerHTML = '<article class="entry"><div class="entry-body">'
      + '<p>Intro</p><ul><li>One</li><li>Two</li></ul></div></article>';
    const [paragraph, first, second] = [
      ...whole.querySelectorAll('p, li'),
    ] as HTMLElement[];
    const at = (top: number, height: number): DOMRect => ({
      ...rect(height),
      top,
      y: top,
      bottom: top + height,
    });
    paragraph!.getBoundingClientRect = () => at(0, 10);
    first!.getBoundingClientRect = () => at(14, 10);
    second!.getBoundingClientRect = () => at(26, 10);
    root.append(whole);

    const split: PaginationRequest = {
      ...request(),
      blocks: [
        { sectionKey: 'work', kind: 'heading', column: 'main' },
        ...[0, 1, 2].map((part) => ({
          sectionKey: 'work',
          kind: 'entry' as const,
          entryIndex: 0,
          part,
          column: 'main' as const,
        })),
      ],
    };
    const measured = await measurePagination(root, split);
    expect(measured.blocks.map((block) => block.gapBeforePx))
      .toEqual([20, 8, 8, 4]);
  });

  it('counts a trailing ordered list as one body block and takes its '
    + 'part\'s gap from the whole entry', async () => {
    const root = document.createElement('div');
    root.style.setProperty('--gap-section', '20px');
    root.style.setProperty('--gap-header', '30px');
    root.style.setProperty('--gap-heading', '8px');
    root.style.setProperty('--gap-entry', '6px');
    root.getBoundingClientRect = () => ({ ...rect(100), width: 50 });
    Object.defineProperty(root, 'offsetWidth', { value: 100 });
    Object.defineProperty(root.ownerDocument, 'fonts', {
      configurable: true,
      value: {
        load: async () => [{} as FontFace],
        ready: Promise.resolve(),
      },
    });
    document.body.replaceChildren(root);
    const header = document.createElement('div');
    header.dataset.paginationHeader = '';
    header.getBoundingClientRect = () => rect(20);
    root.append(header);
    for (const index of [0, 1, 2]) {
      const block = document.createElement('div');
      block.dataset.paginationBlockIndex = String(index);
      block.getBoundingClientRect = () => rect(10);
      root.append(block);
    }
    // The whole entry: a paragraph, then a two-item ordered list. The list
    // stays one body block even though it has two items, unlike a <ul>.
    const whole = document.createElement('div');
    whole.dataset.paginationWholeEntry = '1';
    whole.innerHTML = '<article class="entry"><div class="entry-body">'
      + '<p>Intro</p><ol><li>One</li><li>Two</li></ol></div></article>';
    const body = whole.querySelector('.entry-body')!;
    expect(bodyBlockElements(body)).toHaveLength(2);
    const [paragraph, ol] = [...body.children] as HTMLElement[];
    const at = (top: number, height: number): DOMRect => ({
      ...rect(height),
      top,
      y: top,
      bottom: top + height,
    });
    paragraph!.getBoundingClientRect = () => at(0, 10);
    ol!.getBoundingClientRect = () => at(16, 20);
    root.append(whole);

    const split: PaginationRequest = {
      ...request(),
      blocks: [
        { sectionKey: 'work', kind: 'heading', column: 'main' },
        ...[0, 1].map((part) => ({
          sectionKey: 'work',
          kind: 'entry' as const,
          entryIndex: 0,
          part,
          column: 'main' as const,
        })),
      ],
    };
    const measured = await measurePagination(root, split);
    // The part after the list takes the whole entry's rendered gap (12),
    // not the 0 a caller falls back to when the whole entry can't be read.
    expect(measured.blocks.map((block) => block.gapBeforePx))
      .toEqual([20, 8, 12]);
  });

  it('falls back to no part gap when the whole entry has other blocks',
    async () => {
      const root = document.createElement('div');
      root.style.setProperty('--gap-section', '20px');
      root.style.setProperty('--gap-header', '30px');
      root.style.setProperty('--gap-heading', '8px');
      root.style.setProperty('--gap-entry', '6px');
      Object.defineProperty(root.ownerDocument, 'fonts', {
        configurable: true,
        value: {
          load: async () => [{} as FontFace],
          ready: Promise.resolve(),
        },
      });
      document.body.replaceChildren(root);
      const header = document.createElement('div');
      header.dataset.paginationHeader = '';
      header.getBoundingClientRect = () => rect(20);
      root.append(header);
      for (const index of [0, 1]) {
        const block = document.createElement('div');
        block.dataset.paginationBlockIndex = String(index);
        block.getBoundingClientRect = () => rect(10);
        root.append(block);
      }
      const whole = document.createElement('div');
      whole.dataset.paginationWholeEntry = '0';
      whole.innerHTML = '<div class="entry-body"><p>Only one</p></div>';
      root.append(whole);

      const measured = await measurePagination(root, {
        ...request(),
        blocks: [0, 1].map((part) => ({
          sectionKey: 'work',
          kind: 'entry' as const,
          entryIndex: 0,
          part,
          column: 'main' as const,
        })),
      });
      expect(measured.blocks.map((block) => block.gapBeforePx))
        .toEqual([6, 0]);
    });

  it('fails closed when a split entry is not laid out whole', async () => {
    const root = document.createElement('div');
    root.style.setProperty('--gap-section', '20px');
    root.style.setProperty('--gap-header', '30px');
    root.style.setProperty('--gap-heading', '8px');
    root.style.setProperty('--gap-entry', '6px');
    Object.defineProperty(root.ownerDocument, 'fonts', {
      configurable: true,
      value: {
        load: async () => [{} as FontFace],
        ready: Promise.resolve(),
      },
    });
    document.body.replaceChildren(root);
    const header = document.createElement('div');
    header.dataset.paginationHeader = '';
    header.getBoundingClientRect = () => rect(20);
    root.append(header);
    for (const index of [0, 1]) {
      const block = document.createElement('div');
      block.dataset.paginationBlockIndex = String(index);
      block.getBoundingClientRect = () => rect(10);
      root.append(block);
    }

    await expect(measurePagination(root, {
      ...request(),
      blocks: [0, 1].map((part) => ({
        sectionKey: 'work',
        kind: 'entry' as const,
        entryIndex: 0,
        part,
        column: 'main' as const,
      })),
    })).rejects.toMatchObject({ code: 'invalid_measurement' });
  });

  it('fails closed when the measurement tree is incomplete', async () => {
    const root = document.createElement('div');
    root.style.setProperty('--gap-section', '20px');
    root.style.setProperty('--gap-header', '30px');
    root.style.setProperty('--gap-heading', '8px');
    root.style.setProperty('--gap-entry', '6px');
    Object.defineProperty(root.ownerDocument, 'fonts', {
      configurable: true,
      value: {
        load: async () => [{} as FontFace],
        ready: Promise.resolve(),
      },
    });
    document.body.replaceChildren(root);
    const header = document.createElement('div');
    header.dataset.paginationHeader = '';
    header.getBoundingClientRect = () => rect(40);
    root.append(header);

    await expect(measurePagination(root, request())).rejects.toMatchObject({
      code: 'invalid_measurement',
    });
  });
});

describe('bodyBlockElements', () => {
  it('expands a <ul>\'s items into separate blocks but keeps an <ol> whole',
    () => {
      const body = document.createElement('div');
      body.innerHTML = '<p>Intro</p><ul><li>A</li><li>B</li></ul>'
        + '<ol><li>1</li><li>2</li></ol>';
      const elements = bodyBlockElements(body);
      expect(elements.map((element) => element.tagName))
        .toEqual(['P', 'LI', 'LI', 'OL']);
    });
});
