import catalog from '../../app/assets/fonts/catalog.json';
import { CUSTOMIZATION_FIELDS } from
  '../../app/components/editor/customization/fields';
import {
  enumLabel,
  FIELD_GROUPS,
  FIELD_LABELS,
} from '../../app/components/editor/customization/labels';
import { describe, expect, it } from 'vitest';

describe('customization field labels', () => {
  it('labels every customization field in exactly one group', () => {
    const groupedPaths = FIELD_GROUPS.flatMap((group) => group.paths);
    for (const field of CUSTOMIZATION_FIELDS) {
      expect(FIELD_LABELS[field.path]).toBeDefined();
      expect(
        groupedPaths.filter((path) => path === field.path),
      ).toHaveLength(1);
    }
    expect(groupedPaths).toHaveLength(CUSTOMIZATION_FIELDS.length);
  });

  it('uses catalog display names for font family values', () => {
    for (const entry of catalog.entries) {
      expect(enumLabel('font.family', entry.id)).toBe(entry.displayName);
    }
  });

  it('humanizes known enum values and preserves unknown values', () => {
    expect(enumLabel('sectionDisplay.skill.style', 'bar')).toBe('Bar');
    expect(enumLabel('sectionDisplay.skill.style', 'dots')).toBe('Dots');
    expect(enumLabel('heading.style', 'uppercase')).toBe('Uppercase');
    expect(enumLabel('pageFormat', 'letter')).toBe('Letter');
    expect(enumLabel('pageFormat', 'a4')).toBe('A4');
    expect(enumLabel('heading.style', 'custom-value')).toBe('custom-value');
    expect(enumLabel('layout.columns', 2)).toBe('2');
    expect(enumLabel('heading.showRule', true)).toBe('true');
  });
});
