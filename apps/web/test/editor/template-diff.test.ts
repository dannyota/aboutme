import { TEMPLATES } from '@aboutme/schema/templates';
import { describe, expect, it, vi } from 'vitest';

import { applyTemplate } from '../../app/components/resume/applyTemplate';
import {
  diffCustomization,
  diffPlacement,
} from '../../app/editor/templateDiff';
import { acceptedFixture } from './fixture';

describe('template diffs', () => {
  it('uses remove-then-insert indices in target visual order', () => {
    expect(
      diffPlacement(
        { main: ['a', 'b'], sidebar: ['c'] },
        { main: ['b', 'c'], sidebar: ['a'] },
      ),
    ).toEqual([
      { op: 'moveSection', key: 'b', column: 'main', index: 0 },
      { op: 'moveSection', key: 'c', column: 'main', index: 1 },
      { op: 'moveSection', key: 'a', column: 'sidebar', index: 0 },
    ]);
  });

  it('sorts customization leaves and excludes layout sections', () => {
    const current = acceptedFixture().document.customization;
    const preset = TEMPLATES.find(
      ({ customization }) => customization.layout.placement === 'byType',
    )!;
    const intended = applyTemplate(
      current,
      preset,
      acceptedFixture().document.content,
    );

    expect(diffCustomization(current, intended)).not.toContainEqual(
      expect.objectContaining({ path: 'layout.sections' }),
    );
    expect(
      diffCustomization(current, intended).map(({ path }) => path),
    ).toEqual(
      [...diffCustomization(current, intended).map(({ path }) => path)].sort(),
    );
  });

  it('sorts paths by raw code units instead of locale collation', () => {
    const current = acceptedFixture().document.customization;
    const intended = {
      ...current,
      font: { ...current.font, baseSizePx: 13 },
      colors: { ...current.colors, primary: '#000000' },
    };
    const localeCompare = vi
      .spyOn(String.prototype, 'localeCompare')
      .mockReturnValue(0);

    try {
      const paths = diffCustomization(current, intended).map(
        ({ path }) => path,
      );
      expect(paths).toEqual([
        'colors.primary',
        'font.baseSizePx',
      ]);
    } finally {
      localeCompare.mockRestore();
    }
  });
});
