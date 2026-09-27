import { describe, expect, it } from 'vitest';

import { importCopy } from '../app/i18n/import';

describe('import catalog (docs/design/linkedin-import-ui.md Copy)', () => {
  it('has the same keys in Vietnamese and English', () => {
    expect(Object.keys(importCopy.vi).sort()).toEqual(
      Object.keys(importCopy.en).sort(),
    );
  });

  it('reads the singular for one item and one section', () => {
    expect(importCopy.en.selectedCount(1, 1)).toBe('1 item from 1 section');
  });

  it('reads the plural for more items and sections', () => {
    expect(importCopy.en.selectedCount(3, 2)).toBe('3 items from 2 sections');
  });
});
