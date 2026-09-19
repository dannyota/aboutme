// @vitest-environment node

// A rendered resume never contains a <main> landmark. The public page wraps the
// resume in its own <main id="public-resume">, and the server accepts exactly
// one <main> per page, so a column that rendered as <main> would make every
// two-column public resume unavailable.

import { JSDOM } from 'jsdom';
import { afterAll, describe, expect, it } from 'vitest';

import { buildGoldenMatrix, renderGoldenCell } from './golden.generate.mts';

const matrix = buildGoldenMatrix();

// Counted by the per-cell tests so the suite proves it saw two-column presets
// without rendering the matrix a second time.
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
