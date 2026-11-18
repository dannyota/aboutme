import type { Section } from '@aboutme/schema';
import { describe, expect, it } from 'vitest';

import { groupSheets } from '../../app/components/resume/sheetGroups';

type SectionType = Section['sectionType'];

let next = 0;
const entry = (isHidden = false) => {
  next += 1;
  return {
    id: `018f0000-0000-7000-8000-${String(next).padStart(12, '0')}`,
    isHidden,
  };
};

const item = (
  key: string,
  sectionType: SectionType,
  visible = 1,
  hidden = 0,
) => ({
  key,
  section: {
    sectionType,
    displayName: key,
    entries: [
      ...Array.from({ length: visible }, () => entry()),
      ...Array.from({ length: hidden }, () => entry(true)),
    ],
  } as unknown as Section,
});

const layout = (sheets: ReturnType<typeof groupSheets>) =>
  sheets.map((sheet) => ({
    header: sheet.header,
    keys: sheet.sections.map((section) => section.key),
  }));

describe('sheet grouping', () => {
  it('puts a leading summary on the header sheet, big sections alone', () => {
    const sheets = groupSheets([
      item('profile', 'profile'),
      item('work', 'work', 2),
      item('education', 'education'),
      item('skill', 'skill', 2),
      item('language', 'language', 2),
      item('certificate', 'certificate'),
      item('project', 'project'),
      item('custom', 'custom', 2),
    ]);
    expect(layout(sheets)).toEqual([
      { header: true, keys: ['profile'] },
      { header: false, keys: ['work'] },
      { header: false, keys: ['education'] },
      { header: false, keys: ['skill', 'language', 'certificate'] },
      { header: false, keys: ['project'] },
      { header: false, keys: ['custom'] },
    ]);
  });

  it('keeps the header alone when there is no summary', () => {
    expect(layout(groupSheets([
      item('work', 'work'),
      item('skill', 'skill'),
    ]))).toEqual([
      { header: true, keys: [] },
      { header: false, keys: ['work'] },
      { header: false, keys: ['skill'] },
    ]);
  });

  it('starts a small run on its own sheet after the header', () => {
    expect(layout(groupSheets([
      item('skill', 'skill'),
      item('language', 'language'),
    ]))).toEqual([
      { header: true, keys: [] },
      { header: false, keys: ['skill', 'language'] },
    ]);
  });

  it('gives a profile section later in the order a sheet of its own', () => {
    expect(layout(groupSheets([
      item('work', 'work'),
      item('profile', 'profile'),
      item('skill', 'skill'),
    ]))).toEqual([
      { header: true, keys: [] },
      { header: false, keys: ['work'] },
      { header: false, keys: ['profile'] },
      { header: false, keys: ['skill'] },
    ]);
  });

  it('gives a second profile section its own sheet after the summary', () => {
    expect(layout(groupSheets([
      item('summary', 'profile'),
      item('more', 'profile'),
    ]))).toEqual([
      { header: true, keys: ['summary'] },
      { header: false, keys: ['more'] },
    ]);
  });

  it('skips a section whose entries are all hidden', () => {
    expect(layout(groupSheets([
      item('profile', 'profile', 0, 2),
      item('work', 'work'),
      item('skill', 'skill'),
      item('language', 'language', 0, 1),
      item('certificate', 'certificate'),
      item('empty', 'custom', 0, 0),
    ]))).toEqual([
      { header: true, keys: [] },
      { header: false, keys: ['work'] },
      { header: false, keys: ['skill', 'certificate'] },
    ]);
  });

  it('takes the first visible profile as the summary', () => {
    expect(layout(groupSheets([
      item('hidden-work', 'work', 0, 1),
      item('profile', 'profile'),
      item('work', 'work'),
    ]))).toEqual([
      { header: true, keys: ['profile'] },
      { header: false, keys: ['work'] },
    ]);
  });

  it('starts a new sheet at the fourth small section', () => {
    const smalls = ['a', 'b', 'c', 'd', 'e', 'f', 'g'].map((key, index) =>
      item(key, (['skill', 'language', 'certificate'] as const)[index % 3]!));
    expect(layout(groupSheets(smalls))).toEqual([
      { header: true, keys: [] },
      { header: false, keys: ['a', 'b', 'c'] },
      { header: false, keys: ['d', 'e', 'f'] },
      { header: false, keys: ['g'] },
    ]);
  });

  it('continues a run across the column boundary', () => {
    const main = [
      item('work', 'work'),
      item('skill', 'skill'),
      item('language', 'language'),
    ];
    const sidebar = [
      item('certificate', 'certificate'),
      item('notes', 'custom', 2),
    ];
    expect(layout(groupSheets([...main, ...sidebar]))).toEqual([
      { header: true, keys: [] },
      { header: false, keys: ['work'] },
      { header: false, keys: ['skill', 'language', 'certificate'] },
      { header: false, keys: ['notes'] },
    ]);
  });

  it('ends a run at a big section', () => {
    expect(layout(groupSheets([
      item('skill', 'skill'),
      item('project', 'project'),
      item('language', 'language'),
    ]))).toEqual([
      { header: true, keys: [] },
      { header: false, keys: ['skill'] },
      { header: false, keys: ['project'] },
      { header: false, keys: ['language'] },
    ]);
  });

  it('gives a custom section with three or more entries a sheet', () => {
    expect(layout(groupSheets([
      item('skill', 'skill'),
      item('awards', 'custom', 3),
      item('language', 'language'),
      item('notes', 'custom', 2, 1),
      item('more', 'custom', 4),
    ]))).toEqual([
      { header: true, keys: [] },
      { header: false, keys: ['skill'] },
      { header: false, keys: ['awards'] },
      { header: false, keys: ['language', 'notes'] },
      { header: false, keys: ['more'] },
    ]);
  });

  it('always renders the header sheet, even with no section at all', () => {
    expect(layout(groupSheets([]))).toEqual([{ header: true, keys: [] }]);
    expect(layout(groupSheets([item('gone', 'work', 0, 3)]))).toEqual([
      { header: true, keys: [] },
    ]);
  });
});
