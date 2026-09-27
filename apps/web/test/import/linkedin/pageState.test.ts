// @vitest-environment node
// Tests for the review screen's pure state and derived values
// (docs/design/linkedin-import-ui.md, "Review state" and "Action panel").
import { describe, expect, it } from 'vitest';

import type {
  ImportReview,
  ReviewEntry,
  ReviewSection,
} from '../../../app/import/linkedin/build';
import {
  buildNoticeLines,
  cutFieldName,
  entryDescription,
  entryIndicator,
  entryLabel,
  groupCheckState,
  importSectionName,
  initialChoiceSets,
  languageLevelName,
  MAX_REQUEST_KB,
  requestKb,
  richTextToPlainText,
  sectionCount,
  selectedSummary,
  toggleGroup,
} from '../../../app/import/linkedin/pageState';

function entry(overrides: Partial<ReviewEntry> = {}): ReviewEntry {
  return {
    id: 'e1',
    section: 'work',
    entry: { id: 'e1' },
    marks: [],
    selectedByDefault: true,
    ...overrides,
  };
}

function section(overrides: Partial<ReviewSection> = {}): ReviewSection {
  return { key: 'work', entries: [], overLimit: false, ...overrides };
}

function review(overrides: Partial<ImportReview> = {}): ImportReview {
  return {
    fullName: 'Sample Person',
    headline: 'Engineer',
    nameMarks: [],
    details: [],
    sections: [],
    dropped: [],
    template: { id: 'classic-serif', name: 'Classic serif' },
    ...overrides,
  };
}

describe('initialChoiceSets', () => {
  it('selects only details and entries marked selectedByDefault', () => {
    const work = section({
      key: 'work',
      entries: [
        entry({ id: 'w1', selectedByDefault: true }),
        entry({ id: 'w2', selectedByDefault: false }),
      ],
    });
    const choices = initialChoiceSets(review({
      sections: [work],
      details: [
        {
          id: 'd1', type: 'email', value: 'a@example.com',
          selectedByDefault: false,
        },
        {
          id: 'd2', type: 'location', value: 'Vietnam',
          selectedByDefault: true,
        },
      ],
    }));
    expect(choices.entryIds).toEqual(new Set(['w1']));
    expect(choices.detailIds).toEqual(new Set(['d2']));
  });
});

describe('groupCheckState', () => {
  const work = section({
    key: 'work',
    entries: [entry({ id: 'w1' }), entry({ id: 'w2' })],
  });

  it('is unchecked with no entries selected', () => {
    expect(groupCheckState(work, new Set())).toBe('unchecked');
  });

  it('is checked with every entry selected', () => {
    expect(groupCheckState(work, new Set(['w1', 'w2']))).toBe('checked');
  });

  it('is indeterminate with some entries selected', () => {
    expect(groupCheckState(work, new Set(['w1']))).toBe('indeterminate');
  });

  it('is unchecked for an empty section', () => {
    expect(groupCheckState(section({ entries: [] }), new Set()))
      .toBe('unchecked');
  });
});

describe('toggleGroup', () => {
  const work = section({
    key: 'work',
    entries: [entry({ id: 'w1' }), entry({ id: 'w2' })],
  });

  it('adds every entry when checked', () => {
    expect(toggleGroup(work, new Set(), true)).toEqual(new Set(['w1', 'w2']));
  });

  it('removes every entry when unchecked, keeping other sections', () => {
    const next = toggleGroup(work, new Set(['w1', 'w2', 'other']), false);
    expect(next).toEqual(new Set(['other']));
  });
});

describe('sectionCount and selectedSummary', () => {
  it('counts one section\'s selection', () => {
    const work = section({
      key: 'work',
      entries: [entry({ id: 'w1' }), entry({ id: 'w2' })],
    });
    expect(sectionCount(work, new Set(['w1']))).toEqual({ n: 1, m: 2 });
  });

  it('counts items and sections with at least one selection', () => {
    const work = section({
      key: 'work',
      entries: [entry({ id: 'w1' }), entry({ id: 'w2' })],
    });
    const education = section({
      key: 'education',
      entries: [entry({ id: 'ed1', section: 'education' })],
    });
    const skill = section({ key: 'skill', entries: [entry({ id: 's1' })] });
    const summary = selectedSummary(
      review({ sections: [work, education, skill] }),
      new Set(['w1', 'ed1']),
    );
    expect(summary).toEqual({ n: 2, s: 2 });
  });
});

describe('requestKb', () => {
  it('rounds up to the next whole KB', () => {
    expect(requestKb(0)).toBe(0);
    expect(requestKb(1)).toBe(1);
    expect(requestKb(1024)).toBe(1);
    expect(requestKb(1025)).toBe(2);
  });

  it('reads the 256 KB maximum from the request byte limit', () => {
    expect(MAX_REQUEST_KB).toBe(256);
    expect(requestKb(262144)).toBe(256);
  });
});

describe('catalog lookups', () => {
  it('names every cut field from the editor catalogs', () => {
    expect(cutFieldName('en', 'fullName')).toBe('Full name');
    expect(cutFieldName('en', 'jobTitle')).toBe('Job title');
    expect(cutFieldName('en', 'school')).toBe('School');
    expect(cutFieldName('vi', 'employer')).toBe('Công ty');
  });

  it('names a section with the editor\'s section name', () => {
    expect(importSectionName('en', 'profile')).toBe('Profile');
    expect(importSectionName('vi', 'profile')).toBe('Hồ sơ');
    expect(importSectionName('en', 'work')).toBe('Work experience');
  });

  it('names a language level in the interface language, or nothing '
    + 'unread', () => {
    expect(languageLevelName('en', 3)).toBe('Professional working');
    expect(languageLevelName('vi', 5)).toBe('Bản ngữ hoặc song ngữ');
    expect(languageLevelName('en', undefined)).toBeUndefined();
  });
});

describe('entryLabel and entryDescription', () => {
  const dateFormat = 'Mon YYYY' as const;

  it('labels a work entry as job title and employer', () => {
    const draft = { jobTitle: 'Engineer', employer: 'Example Co.' };
    expect(entryLabel('work', draft)).toBe('Engineer · Example Co.');
  });

  it('describes a work entry with dates and location', () => {
    const draft = {
      dates: { start: { y: 2020, m: 1 }, end: null, present: true },
      city: 'Ho Chi Minh City',
      country: 'Vietnam',
    };
    expect(entryDescription('work', draft, 'en', dateFormat))
      .toBe('Jan 2020 – Present · Ho Chi Minh City, Vietnam');
  });

  it('labels an education entry as the school', () => {
    expect(entryLabel('education', { school: 'Example University' }))
      .toBe('Example University');
  });

  it('describes an education entry with degree and dates', () => {
    const draft = {
      degree: 'BSc, Computer Science',
      dates: { start: { y: 2018 }, end: { y: 2022 }, present: false },
    };
    expect(entryDescription('education', draft, 'en', dateFormat))
      .toBe('BSc, Computer Science · 2018 – 2022');
  });

  it('has no description for skills and certificates', () => {
    expect(entryDescription('skill', { name: 'Go' }, 'en', dateFormat))
      .toBeUndefined();
    expect(entryDescription('certificate', { title: 'X' }, 'en', dateFormat))
      .toBeUndefined();
  });

  it('describes a language entry with its level name', () => {
    expect(entryDescription(
      'language', { name: 'English', level: 3 }, 'en', dateFormat,
    )).toBe('Professional working');
    expect(entryDescription('language', { name: 'French' }, 'en', dateFormat))
      .toBeUndefined();
  });
});

describe('entryIndicator', () => {
  it('shows invalid over any other mark', () => {
    const marked = entry({ id: 'w1', marks: [{ kind: 'noDates' }] });
    expect(entryIndicator(marked, new Set(['w1']))).toBe('invalid');
  });

  it('shows noDates for a noDates or startOnly mark', () => {
    expect(entryIndicator(entry({ marks: [{ kind: 'noDates' }] }), new Set()))
      .toBe('noDates');
    expect(entryIndicator(entry({ marks: [{ kind: 'startOnly' }] }), new Set()))
      .toBe('noDates');
  });

  it('shows cut when only a cut mark is present', () => {
    const marked = entry({
      marks: [{ kind: 'cut', field: 'employer', max: 160 }],
    });
    expect(entryIndicator(marked, new Set())).toBe('cut');
  });

  it('has no indicator with no marks', () => {
    expect(entryIndicator(entry(), new Set())).toBeUndefined();
  });
});

describe('buildNoticeLines', () => {
  it('lists a cut name field with no entry context', () => {
    const lines = buildNoticeLines('en', review({
      nameMarks: [{ kind: 'cut', field: 'headline', max: 160 }],
    }));
    expect(lines).toEqual([{ kind: 'cut', field: 'Headline', max: 160 }]);
  });

  it('lists date and cut marks per entry, then an over-limit section', () => {
    const work = section({
      key: 'work',
      overLimit: true,
      entries: [
        entry({
          id: 'w1',
          entry: { jobTitle: 'Engineer', employer: 'Example Co.' },
          marks: [
            { kind: 'noDates' },
            { kind: 'cut', field: 'employer', max: 160 },
          ],
        }),
      ],
    });
    const lines = buildNoticeLines('en', review({ sections: [work] }));
    expect(lines).toEqual([
      { kind: 'dates', entry: 'Engineer · Example Co.' },
      { kind: 'cut', field: 'Employer', max: 160 },
      { kind: 'overLimit', section: 'Work experience' },
    ]);
  });

  it('lists a startOnly mark', () => {
    const education = section({
      key: 'education',
      entries: [
        entry({
          id: 'ed1',
          section: 'education',
          entry: { school: 'Example University' },
          marks: [{ kind: 'startOnly' }],
        }),
      ],
    });
    const lines = buildNoticeLines('en', review({ sections: [education] }));
    expect(lines).toEqual([
      { kind: 'startOnly', entry: 'Example University' },
    ]);
  });
});

describe('richTextToPlainText', () => {
  it('joins paragraphs onto their own lines', () => {
    expect(richTextToPlainText('<p>First.</p><p>Second.</p>'))
      .toBe('First.\nSecond.');
  });

  it('prefixes list items with a bullet, one per line', () => {
    expect(richTextToPlainText('<ul><li>One</li><li>Two</li></ul>'))
      .toBe('• One\n• Two');
  });

  it('decodes entities without double-decoding an already-decoded amp', () => {
    const html = '<p>&lt;script&gt;alert(1)&lt;/script&gt;</p>'
      + '<p>&lt;img src=x onerror=alert(1)&gt;</p>'
      + '<p>Tom &amp; Jerry &quot;&#39;&quot;</p>';
    expect(richTextToPlainText(html)).toBe(
      '<script>alert(1)</script>\n'
      + '<img src=x onerror=alert(1)>\n'
      + 'Tom & Jerry "\'"',
    );
  });
});
