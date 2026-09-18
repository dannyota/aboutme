// @vitest-environment node

// A rendered resume never contains a <main> landmark. The public page wraps the
// resume in its own <main id="public-resume">, and the server accepts exactly
// one <main> per page, so a column that rendered as <main> would make every
// two-column public resume unavailable.

import { JSDOM } from 'jsdom';
import { describe, expect, it } from 'vitest';

import { buildGoldenMatrix, renderGoldenCell } from './golden.generate.mts';

const matrix = buildGoldenMatrix();

describe('resume landmarks', () => {
  it('covers two-column presets in both render modes', async () => {
    const columns = await Promise.all(matrix.map(async (cell) => {
      const { document } = new JSDOM(await renderGoldenCell(cell)).window;
      return document.querySelector('.layout-two-columns') !== null;
    }));
    expect(columns.filter(Boolean).length).toBeGreaterThan(0);
  });

  it.each(matrix)('$filename renders no <main> element', async (cell) => {
    const { document } = new JSDOM(await renderGoldenCell(cell)).window;
    expect(document.querySelectorAll('main')).toHaveLength(0);
    const twoColumn = document.querySelector('.layout-two-columns');
    if (twoColumn !== null) {
      expect(twoColumn.querySelector(':scope > .resume-main')).not.toBeNull();
      expect(
        twoColumn.querySelector(':scope > aside.resume-sidebar'),
      ).not.toBeNull();
    }
  });
});
