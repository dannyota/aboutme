import type { Resume } from '@aboutme/schema';
import { readFileSync } from 'node:fs';
import { describe, expect, it, vi } from 'vitest';

import {
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
      headerBodyGapPx: 20,
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

  it('fails closed when the measurement tree is incomplete', async () => {
    const root = document.createElement('div');
    root.style.setProperty('--gap-section', '20px');
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
