// @vitest-environment node
// The committed Save to PDF layout fixtures, read end to end with pdf.js:
// read, lines, layout, and section mapping (docs/design/linkedin-import.md,
// "Tests"; fixtures/README.md says how they are made).
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

import * as pdfjs from 'pdfjs-dist/legacy/build/pdf.mjs';
import { describe, expect, it } from 'vitest';

import {
  analyzeLayout,
  type LayoutResult,
} from '../../../app/import/linkedin/layout';
import {
  buildPageLines,
  columnBoundary,
} from '../../../app/import/linkedin/lines';
import { readLinkedInPdf } from '../../../app/import/linkedin/read';
import {
  parseProfile,
  type ParsedProfile,
} from '../../../app/import/linkedin/sections';

const FIXTURES = join(import.meta.dirname, 'fixtures');

async function layoutOf(name: string): Promise<LayoutResult> {
  const bytes = readFileSync(join(FIXTURES, `${name}.pdf`));
  const read = await readLinkedInPdf(new Blob([bytes]), { pdfjs });
  if (!read.ok) throw new Error(`${name}: ${read.reason}`);
  const boundary = columnBoundary(read.pages[0] ?? []);
  return analyzeLayout(read.pages.map((items, index) =>
    buildPageLines(index + 1, items, boundary)));
}

async function profileOf(name: string): Promise<ParsedProfile> {
  const layout = await layoutOf(name);
  if (layout.kind !== 'ok') throw new Error(`${name}: ${layout.kind}`);
  return parseProfile(layout.layout);
}

describe('basic-en.pdf', () => {
  it('maps the intro, contact, and sidebar lists', async () => {
    const profile = await profileOf('basic-en');
    expect(profile.profile).toEqual({
      fullName: 'Sample Person',
      headline: 'Product Manager # Example-first & Co',
      location: 'Ho Chi Minh City, Vietnam',
    });
    expect(profile.contacts).toEqual([
      { type: 'phone', value: '0900000000', label: 'Home' },
      { type: 'email', value: 'sample.person@example.com' },
      { type: 'linkedin', value: 'https://linkedin.com/in/sample-person' },
      {
        type: 'website',
        value: 'https://example.com/sample-person',
        label: 'Portfolio',
      },
    ]);
    expect(profile.skills).toEqual([
      'Product Management',
      'Cross-functional Team Leadership',
      'Public Speaking',
    ]);
    expect(profile.languages).toEqual([
      { name: 'Vietnamese', level: 5 },
      { name: 'English', level: 3 },
    ]);
    expect(profile.certificates).toEqual([
      'Example Certified Professional',
      'Example Advanced Certificate',
    ]);
    expect(profile.dropped).toEqual([]);
  });

  it('keeps the summary\'s bullets as a list', async () => {
    const { summary } = await profileOf('basic-en');
    expect(summary?.html).toMatch(/^<p>A product manager .*research\.<\/p>/);
    expect(summary?.html).toContain(
      '<ul><li>Grew example activation by a sample margin.</li>'
      + '<li>Ran a weekly example discovery practice.</li></ul>',
    );
  });

  it('reads four roles, a group, and a role crossing a page', async () => {
    const { work } = await profileOf('basic-en');
    expect(work.map((role) => [role.employer, role.jobTitle, role.city]))
      .toEqual([
        ['Example Co.', 'Senior Product Manager, Head of PM Function',
          'Ho Chi Minh City'],
        ['Mẫu Group', 'Product Manager, Growth', 'Hanoi'],
        ['Mẫu Group', 'Product Manager, Platform', 'Hanoi'],
        ['Example Co. Two', 'Founding Product Manager', 'Da Nang'],
      ]);
    expect(work[0]!.dates).toEqual({
      start: { y: 2019, m: 7 },
      end: { y: 2023, m: 8 },
      present: false,
    });
    // The group's duration line is dropped, not read as a description.
    expect(work[0]!.description).not.toContain('2 years 1 month');
    expect(work[0]!.description).not.toContain('Mẫu Group');
    expect(work[1]!.description).toBe('');
    // The last role's description starts at the top of page 2.
    expect(work[3]!.description).toMatch(
      /^<p>Built the first version .* workstream\.<\/p>$/,
    );
  });

  it('reads two schools, one with its date wrapped', async () => {
    const { education } = await profileOf('basic-en');
    expect(education).toEqual([
      {
        school: 'University of Sample Studies',
        degree: 'Bachelor of Science - BS, Computer Science',
        dates: { start: { y: 2011, m: 12 }, end: { y: 2015, m: 11 },
          present: false },
        dateNotice: undefined,
      },
      {
        school: 'Sample Vocational College',
        degree: 'Diploma of Business - DB, Business Administration',
        dates: { start: { y: 2008 }, end: { y: 2010 }, present: false },
        dateNotice: undefined,
      },
    ]);
  });
});

describe('wraps-en.pdf', () => {
  it('joins wrapped lines and reads a sidebar that continues', async () => {
    const profile = await profileOf('wraps-en');
    expect(profile.profile.headline).toBe(
      'Product Manager, Example Co # a headline long enough to wrap '
      + 'a second visual line, followed by the location',
    );
    expect(profile.profile.location).toBe('Ho Chi Minh City, Vietnam');
    expect(profile.contacts).toContainEqual({
      type: 'linkedin',
      value: 'https://linkedin.com/in/sample-person-with-a-long-slug',
    });
    expect(profile.skills).toEqual([
      'An Example Skill Name That Wraps Onto A Second Line',
      'Public Speaking',
    ]);
    expect(profile.languages).toEqual([{ name: 'Vietnamese', level: 5 }]);
    expect(profile.certificates).toEqual([
      'An Example Certification Whose Title Wraps',
      'Second Example Certificate',
    ]);
    expect(profile.work[0]!.jobTitle).toBe(
      'Senior Product Manager Whose Title Is Long Enough To Wrap '
      + 'Onto A Second Visual Line',
    );
    expect(profile.work[0]!.dates?.present).toBe(true);
  });
});

describe('dates-en.pdf', () => {
  it('reads every date form and marks the unreadable ones', async () => {
    const { work } = await profileOf('dates-en');
    expect(work.map((role) => [role.employer, role.jobTitle, role.dates,
      role.dateNotice])).toEqual([
      ['Example Co. A', 'Analyst',
        { start: { y: 2020, m: 1 }, end: null, present: true }, undefined],
      ['Example Co. B', 'Analyst',
        { start: { y: 2018 }, end: { y: 2019 }, present: false }, undefined],
      // A start after the end.
      ['Example Co. C', 'Analyst', undefined, 'unreadable'],
      // Unknown month words.
      ['Example Co. D', 'Analyst', undefined, 'unreadable'],
      // "less than a year" as a group duration.
      ['Example Co. E', 'Junior Analyst',
        { start: { y: 2022, m: 6 }, end: { y: 2022, m: 8 }, present: false },
        undefined],
      // Short month names and an en dash.
      ['Example Co. F', 'Analyst',
        { start: { y: 2016, m: 1 }, end: { y: 2017, m: 9 }, present: false },
        undefined],
    ]);
  });
});

describe('dropped-en.pdf', () => {
  it('lists each section it does not import with its count', async () => {
    const profile = await profileOf('dropped-en');
    expect(profile.dropped).toEqual([
      { heading: 'Honors-Awards', count: 1 },
      { heading: 'Publications', count: 1 },
      { heading: 'Patents', count: 1 },
      { heading: 'Volunteer Experience', count: 1 },
      { heading: 'Projects', count: 1 },
    ]);
    expect(profile.work).toHaveLength(1);
  });
});

describe('vietnamese-letters.pdf', () => {
  it('keeps Vietnamese letters in an English profile', async () => {
    const profile = await profileOf('vietnamese-letters');
    expect(profile.profile.fullName).toBe('Nguyễn Văn Mẫu');
    expect(profile.work[0]!.employer).toBe('Mẫu Group');
    expect(profile.profile.fullName).toBe(
      profile.profile.fullName.normalize('NFC'),
    );
  });
});

describe('the English-only and LinkedIn checks', () => {
  it('localized-vi.pdf is a LinkedIn PDF that is not in English', async () => {
    expect((await layoutOf('localized-vi')).kind).toBe('notEnglish');
  });

  it('other.pdf is not a LinkedIn PDF', async () => {
    expect((await layoutOf('other')).kind).toBe('notLinkedIn');
  });
});

describe('limits-en.pdf', () => {
  it('reads the long headline and 80 roles, and cuts rich text', async () => {
    const profile = await profileOf('limits-en');
    expect([...profile.profile.headline]).toHaveLength(220);
    expect(profile.work).toHaveLength(80);
    const long = profile.work[40]!;
    expect(long.descriptionCut).toBe(true);
    expect(new TextEncoder().encode(long.description).length)
      .toBeLessThanOrEqual(16384);
  });
});

describe('injection-en.pdf', () => {
  it('keeps hostile text plain and escapes it in rich text', async () => {
    const profile = await profileOf('injection-en');
    expect(profile.profile.fullName).toBe('Sample <script>');
    expect(profile.work[0]!.employer).toBe('<img src=x onerror=alert(1)>');
    expect(profile.work[0]!.jobTitle).toBe('<script>alert(1)</script>');
    expect(profile.summary?.html).not.toMatch(/<(script|img)/);
    expect(profile.work[0]!.description).not.toMatch(/<(script|img)/);
    // javascript: never becomes a link: its "website" is not imported.
    expect(profile.contacts.map((contact) => contact.value)).toEqual([
      '0900000000',
      '<script>@example.com',
      'https://example.com/%3Cscript%3E',
    ]);
  });
});
