import catalog from '../../app/assets/fonts/catalog.json';
import {
  CUSTOMIZATION_FIELDS,
} from '../../app/components/editor/customization/fields';
import {
  enumLabel,
  fieldLabel,
  FIELD_GROUPS,
} from '../../app/components/editor/customization/labels';
import { editorFieldsCopy } from '../../app/i18n/editor-fields';
import { describe, expect, it } from 'vitest';

describe('customization field labels', () => {
  it.each([
    ['vi', 'Họ và tên', 'Ngày bắt đầu'],
    ['en', 'Full name', 'Start date'],
  ] as const)(
    'has %s copy for personal and date fields',
    (locale, name, date) => {
      expect(editorFieldsCopy[locale].personal.fullName).toBe(name);
      expect(editorFieldsCopy[locale].dates.startDate).toBe(date);
    },
  );

  it.each([
    ['vi', 'Chức danh', 'Xóa mục'],
    ['en', 'Job title', 'Delete entry'],
  ] as const)(
    'has %s copy for entry fields and actions',
    (locale, title, remove) => {
      expect(editorFieldsCopy[locale].entry.work.jobTitle).toBe(title);
      expect(editorFieldsCopy[locale].entryCard.delete).toBe(remove);
    },
  );
  it('labels every customization field in exactly one group', () => {
    const groupedPaths = FIELD_GROUPS.flatMap((group) => group.paths);
    for (const field of CUSTOMIZATION_FIELDS) {
      expect(fieldLabel('en', field.path)).not.toBe(field.path);
      expect(groupedPaths.filter((path) => path === field.path)).toHaveLength(
        1,
      );
    }
    expect(groupedPaths).toHaveLength(CUSTOMIZATION_FIELDS.length);
  });

  it('uses catalog display names for font family values', () => {
    for (const entry of catalog.entries) {
      expect(enumLabel('en', 'font.family', entry.id)).toBe(entry.displayName);
    }
  });

  it('humanizes known enum values and preserves unknown values', () => {
    expect(enumLabel('en', 'sectionDisplay.skill.style', 'bar')).toBe('Bar');
    expect(enumLabel('en', 'sectionDisplay.skill.style', 'dots')).toBe('Dots');
    expect(enumLabel('en', 'heading.style', 'uppercase')).toBe('Uppercase');
    expect(enumLabel('en', 'pageFormat', 'letter')).toBe('Letter');
    expect(enumLabel('en', 'pageFormat', 'a4')).toBe('A4');
    expect(enumLabel('en', 'heading.style', 'custom-value')).toBe(
      'custom-value',
    );
    expect(enumLabel('en', 'layout.columns', 2)).toBe('2');
    expect(enumLabel('en', 'heading.showRule', true)).toBe('true');
  });
});
