// @vitest-environment node

import { JSDOM } from 'jsdom';
import { afterAll, describe, expect, it } from 'vitest';

import { buildGoldenMatrix, renderGoldenCell } from './golden.generate.mts';

const matrix = buildGoldenMatrix();

let twoColumnCells = 0;

afterAll(() => {
  expect(twoColumnCells).toBeGreaterThan(0);
});

describe('resume landmarks', () => {
  it.each(matrix)('$filename renders no <main> element', async (cell) => {
    const { document } = new JSDOM(await renderGoldenCell(cell)).window;
    expect(document.querySelectorAll('main')).toHaveLength(0);
    const twoColumn = document.querySelector('.layout-two-columns');
    if (twoColumn !== null) {
      twoColumnCells += 1;
      expect(twoColumn.querySelector(':scope > .resume-main')).not.toBeNull();
      expect(
        twoColumn.querySelector(':scope > aside.resume-sidebar'),
      ).not.toBeNull();
    }
  });
});
