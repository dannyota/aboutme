// @vitest-environment node
// Tests for the LinkedIn import parser's section mapping, per
// docs/design/linkedin-import.md "Mapping", "Dates", and "What is dropped".
import { describe, expect, it } from 'vitest';
import {
  parseProfile,
  parseIntro,
  parseContact,
  sidebarEntries,
  parseLanguages,
  parseExperience,
  parseEducation,
  splitLocation,
} from '../../../app/import/linkedin/sections';
import type {
  Layout,
  Section,
  ColumnLine,
} from '../../../app/import/linkedin/layout';
import { columnLine } from './helpers/lineFixtures';

const NAME = columnLine(1, 223.6, 726.5, 26, 'Sample Person', null);

function section(
  heading: string,
  known: boolean,
  lines: ColumnLine[],
): Section {
  return { heading, known, lines };
}

function layout(overrides: Partial<Layout> = {}): Layout {
  return {
    name: NAME,
    intro: [],
    sidebar: [],
    main: [],
    ...overrides,
  };
}

describe('parseIntro', () => {
  it('has an empty headline with no intro lines', () => {
    const draft = parseIntro(NAME, []);
    expect(draft).toEqual({
      fullName: 'Sample Person',
      headline: '',
      location: undefined,
    });
  });

  it('treats one intro line as headline only', () => {
    const draft = parseIntro(NAME, [
      columnLine(1, 223.6, 705.3, 12, 'Sample Engineer', 21),
    ]);
    expect(draft.headline).toBe('Sample Engineer');
    expect(draft.location).toBeUndefined();
  });

  it('takes the last of two or more intro lines as the location', () => {
    const draft = parseIntro(NAME, [
      columnLine(1, 223.6, 705.3, 12, 'Sample Engineer # Example Co & Co', 21),
      columnLine(1, 223.6, 689.9, 12, 'Ho Chi Minh City, Vietnam', 15.5),
    ]);
    expect(draft.headline).toBe('Sample Engineer # Example Co & Co');
    expect(draft.location).toBe('Ho Chi Minh City, Vietnam');
  });

  it('joins a wrapped headline with spaces before the location', () => {
    const draft = parseIntro(NAME, [
      columnLine(1, 223.6, 705.3, 12, 'Senior Sample Engineer for the', 21),
      columnLine(1, 223.6, 689.9, 12, 'Example Platform Team', 15.5),
      columnLine(1, 223.6, 674.4, 12, 'Ho Chi Minh City, Vietnam', 15.5),
    ]);
    expect(draft.headline).toBe(
      'Senior Sample Engineer for the Example Platform Team',
    );
    expect(draft.location).toBe('Ho Chi Minh City, Vietnam');
  });

  it('keeps a <script> name as printed', () => {
    const draft = parseIntro(
      columnLine(1, 223.6, 726.5, 26, '<script>alert(1)</script>', null),
      [],
    );
    expect(draft.fullName).toBe('<script>alert(1)</script>');
  });
});

describe('parseContact', () => {
  it('reads contact shape: phone, email, URL then (LinkedIn), website', () => {
    const lines = [
      columnLine(1, 21.6, 718.2, 10.5, '0900000000 (Home)', 19.5),
      columnLine(1, 21.6, 705.6, 10.5, 'sample.person@example.com', 12.5),
      columnLine(1, 21.6, 681.2, 11, 'linkedin.com/in/sample-person', 24.5),
      columnLine(1, 21.6, 666.8, 11, '(LinkedIn)', 14.5),
      columnLine(1, 21.6, 653.3, 11, 'example.com/portfolio', 13.5),
      columnLine(1, 21.6, 640.2, 11, '(Portfolio)', 13),
    ];
    const contacts = parseContact(lines);
    expect(contacts).toEqual([
      { type: 'phone', value: '0900000000', label: 'Home' },
      { type: 'email', value: 'sample.person@example.com' },
      {
        type: 'linkedin',
        value: 'https://linkedin.com/in/sample-person',
      },
      {
        type: 'website',
        value: 'https://example.com/portfolio',
        label: 'Portfolio',
      },
    ]);
  });

  it('keeps a labeled email an email, never a link', () => {
    const lines = [
      columnLine(1, 21.6, 705.6, 10.5, 'sample@example.com (Work)', 12.5),
    ];
    expect(parseContact(lines)).toEqual([
      { type: 'email', value: 'sample@example.com' },
    ]);
  });

  it('joins a URL wrapped over two lines before its label', () => {
    const lines = [
      columnLine(1, 21.6, 681.2, 11, 'example.com/very-long-portfolio-', 24.5),
      columnLine(1, 21.6, 666.8, 11, 'path-name', 13.5),
      columnLine(1, 21.6, 653.3, 11, '(Portfolio)', 13),
    ];
    const contacts = parseContact(lines);
    expect(contacts).toEqual([
      {
        type: 'website',
        value: 'https://example.com/very-long-portfolio-path-name',
        label: 'Portfolio',
      },
    ]);
  });

  it('drops a javascript: value with a website label', () => {
    const lines = [
      columnLine(1, 21.6, 681.2, 11, 'javascript:alert(1)', 24.5),
      columnLine(1, 21.6, 666.8, 11, '(Portfolio)', 13.5),
    ];
    expect(parseContact(lines)).toEqual([]);
  });

  it('does not let a gap separate contact entries', () => {
    const lines = [
      columnLine(1, 21.6, 681.2, 11, 'example.com/x', 24.5),
      columnLine(2, 21.6, 700.0, 11, '(Portfolio)', null),
    ];
    expect(parseContact(lines)).toEqual([
      { type: 'website', value: 'https://example.com/x', label: 'Portfolio' },
    ]);
  });

  it('treats a digits-only value as a phone whatever its label', () => {
    const lines = [
      columnLine(1, 21.6, 718.2, 10.5, '+1 (555) 000-0000 (Work)', 19.5),
    ];
    expect(parseContact(lines)).toEqual([
      { type: 'phone', value: '+1 (555) 000-0000', label: 'Work' },
    ]);
  });

  it('lowercases the host and keeps an already-https URL as is', () => {
    const lines = [
      columnLine(1, 21.6, 681.2, 11, 'https://Example.com/x', 24.5),
      columnLine(1, 21.6, 666.8, 11, '(Portfolio)', 13.5),
    ];
    expect(parseContact(lines)).toEqual([
      { type: 'website', value: 'https://example.com/x', label: 'Portfolio' },
    ]);
  });

  it('drops leftover text with no label at the end of the section', () => {
    const lines = [
      columnLine(1, 21.6, 705.6, 10.5, 'sample.person@example.com', 12.5),
      columnLine(1, 21.6, 690.0, 11, 'unlabeled trailing text', 15.6),
    ];
    expect(parseContact(lines)).toEqual([
      { type: 'email', value: 'sample.person@example.com' },
    ]);
  });

  it('keeps a <script> value as raw text in a website label', () => {
    const lines = [
      columnLine(1, 21.6, 681.2, 11, 'example.com/<script>', 24.5),
      columnLine(1, 21.6, 666.8, 11, '(Portfolio)', 13.5),
    ];
    expect(parseContact(lines)).toEqual([
      {
        type: 'website',
        value: 'https://example.com/%3Cscript%3E',
        label: 'Portfolio',
      },
    ]);
  });
});

describe('sidebarEntries', () => {
  it('starts a new entry over a 17.5 gap and joins a 12.5 gap wrap', () => {
    const lines = [
      columnLine(1, 21.6, 585.9, 10.5, 'Sample Skill One', 19.5),
      columnLine(1, 21.6, 568.3, 10.5, 'Sample Skill Continued', 12.5),
      columnLine(1, 21.6, 550.7, 10.5, 'Sample Skill Two', 17.5),
    ];
    expect(sidebarEntries(lines)).toEqual([
      'Sample Skill One Sample Skill Continued',
      'Sample Skill Two',
    ]);
  });

  it('starts a new entry on a null gap (a new page)', () => {
    const lines = [
      columnLine(1, 21.6, 585.9, 10.5, 'Sample Skill One', 19.5),
      columnLine(2, 21.6, 700.0, 10.5, 'Sample Skill Two', null),
    ];
    expect(sidebarEntries(lines)).toEqual([
      'Sample Skill One',
      'Sample Skill Two',
    ]);
  });
});

describe('parseLanguages', () => {
  it('reads all five proficiency names, with or without "proficiency"', () => {
    const cases: [string, number][] = [
      ['English (Elementary proficiency)', 1],
      ['French (LIMITED WORKING proficiency)', 2],
      ['Spanish (Professional Working)', 3],
      ['German (Full Professional proficiency)', 4],
      ['Vietnamese (Native or Bilingual)', 5],
    ];
    for (const [text, level] of cases) {
      const lines = [columnLine(1, 21.6, 585.9, 10.5, text, 19.5)];
      expect(parseLanguages(lines)).toEqual([
        { name: text.split(' (')[0], level },
      ]);
    }
  });

  it('joins a language wrapped before its closing parenthesis', () => {
    const lines = [
      columnLine(1, 21.6, 585.9, 10.5, 'Portuguese (Brazil) (Native or', 19.5),
      columnLine(1, 21.6, 568.3, 10.5, 'Bilingual)', 12.5),
    ];
    expect(parseLanguages(lines)).toEqual([
      { name: 'Portuguese (Brazil)', level: 5 },
    ]);
  });

  it('gives no level for an unknown or missing proficiency', () => {
    const lines = [
      columnLine(1, 21.6, 585.9, 10.5, 'Klingon (Made Up)', 19.5),
      columnLine(1, 21.6, 568.3, 10.5, 'Elvish', 17.5),
    ];
    expect(parseLanguages(lines)).toEqual([
      { name: 'Klingon', level: undefined },
      { name: 'Elvish', level: undefined },
    ]);
  });
});

describe('parseExperience', () => {
  it('reads a two-line title, its location, and its description', () => {
    const lines = [
      columnLine(1, 223.6, 308.5, 12, 'Example Co.', 38.5),
      columnLine(1, 223.6, 292.4, 11.5, 'Senior Sample Engineer for the', 16),
      columnLine(1, 223.6, 277.9, 11.5, 'Platform Team', 14.5),
      columnLine(
        1,
        223.6,
        263.2,
        10.5,
        'July 2020 - August 2022 (2 years 2 months)',
        14.5,
      ),
      columnLine(1, 223.6, 248.7, 10.5, 'Ho Chi Minh City, Vietnam', 14.5),
      columnLine(1, 223.6, 230.7, 10.5, 'Built the sample platform.', 18),
    ];
    const work = parseExperience(lines);
    expect(work).toEqual([
      {
        employer: 'Example Co.',
        jobTitle: 'Senior Sample Engineer for the Platform Team',
        dates: {
          start: { y: 2020, m: 7 },
          end: { y: 2022, m: 8 },
          present: false,
        },
        dateNotice: undefined,
        city: 'Ho Chi Minh City',
        country: 'Vietnam',
        description: '<p>Built the sample platform.</p>',
        descriptionCut: false,
      },
    ]);
  });

  it('gives a role without a location no city or country', () => {
    const lines = [
      columnLine(1, 223.6, 308.5, 12, 'Example Co.', 38.5),
      columnLine(1, 223.6, 292.4, 11.5, 'Sample Engineer', 16),
      columnLine(1, 223.6, 277.9, 10.5, 'July 2020 - Present', 14.5),
      // 21.5 exceeds 1.6 * 10.5 = 16.8, so this line cannot be mistaken for
      // a location line even though it shares the date line's size.
      columnLine(1, 223.6, 256.4, 10.5, 'Built the sample platform.', 21.5),
    ];
    const work = parseExperience(lines);
    expect(work[0].city).toBeUndefined();
    expect(work[0].country).toBeUndefined();
    expect(work[0].description).toBe('<p>Built the sample platform.</p>');
    expect(work[0].dates).toEqual({
      start: { y: 2020, m: 7 },
      end: null,
      present: true,
    });
  });

  it('requires a gap at or under 1.6x the size for a location line', () => {
    const lines = [
      columnLine(1, 223.6, 308.5, 12, 'Example Co.', 38.5),
      columnLine(1, 223.6, 292.4, 11.5, 'Sample Engineer', 16),
      columnLine(1, 223.6, 277.9, 10.5, 'July 2020 - August 2022', 14.5),
      // 1.6 * 10.5 = 16.8; 17 exceeds it, so this is description not location.
      columnLine(1, 223.6, 260.9, 10.5, 'Ho Chi Minh City, Vietnam', 17),
    ];
    const work = parseExperience(lines);
    expect(work[0].city).toBeUndefined();
    expect(work[0].description).toContain('Ho Chi Minh City, Vietnam');
  });

  it('groups two roles under an employer and a duration line', () => {
    const lines = [
      columnLine(1, 223.6, 308.5, 12, 'Example Co.', 38.5),
      columnLine(1, 223.6, 292.4, 10.5, '2 years 1 month', 16.5),
      columnLine(1, 223.6, 277.9, 11.5, 'Senior Sample Engineer', 21.5),
      columnLine(1, 223.6, 263.2, 10.5, 'April 2021 - August 2022', 14.5),
      columnLine(1, 223.6, 248.7, 10.5, 'Ho Chi Minh City, Vietnam', 14.5),
      columnLine(1, 223.6, 230.7, 10.5, 'Led the sample team.', 18),
      columnLine(2, 223.6, 700.0, 10.5, 'through the next page.', null),
      columnLine(2, 223.6, 682.0, 11.5, 'Sample Engineer', 35),
      columnLine(2, 223.6, 667.5, 10.5, 'April 2020 - April 2021', 14.5),
      columnLine(2, 223.6, 653.0, 10.5, 'Ho Chi Minh City, Vietnam', 14.5),
      columnLine(2, 223.6, 635.0, 10.5, 'Built the sample product.', 18),
    ];
    const work = parseExperience(lines);
    expect(work).toHaveLength(2);
    expect(work[0].employer).toBe('Example Co.');
    expect(work[0].jobTitle).toBe('Senior Sample Engineer');
    expect(work[0].description).toBe(
      '<p>Led the sample team. through the next page.</p>',
    );
    expect(work[1].employer).toBe('Example Co.');
    expect(work[1].jobTitle).toBe('Sample Engineer');
    expect(work[1].description).toBe('<p>Built the sample product.</p>');
  });

  it('treats an employer named "Experience" as an ordinary line', () => {
    const lines = [
      columnLine(1, 223.6, 308.5, 12, 'Experience', 38.5),
      columnLine(1, 223.6, 292.4, 11.5, 'Sample Engineer', 16),
      columnLine(1, 223.6, 277.9, 10.5, 'July 2020 - August 2022', 14.5),
    ];
    expect(parseExperience(lines)[0].employer).toBe('Experience');
  });

  it('gives an unreadable date notice and no dates', () => {
    const lines = [
      columnLine(1, 223.6, 308.5, 12, 'Example Co.', 38.5),
      columnLine(1, 223.6, 292.4, 11.5, 'Sample Engineer', 16),
      columnLine(1, 223.6, 277.9, 10.5, 'August 2022 - July 2020', 14.5),
    ];
    const work = parseExperience(lines);
    expect(work[0].dates).toBeUndefined();
    expect(work[0].dateNotice).toBe('unreadable');
  });

  it('gives a startOnly date notice for a start-only date line', () => {
    const lines = [
      columnLine(1, 223.6, 308.5, 12, 'Example Co.', 38.5),
      columnLine(1, 223.6, 292.4, 11.5, 'Sample Engineer', 16),
      // matchDateLine only recognizes a single date as a date line when a
      // duration follows in parentheses (docs/design/linkedin-import.md
      // "Experience"); parseDateRange then reports it as start-only.
      columnLine(1, 223.6, 277.9, 10.5, 'July 2020 (5 years)', 14.5),
    ];
    const work = parseExperience(lines);
    expect(work[0].dates).toBeUndefined();
    expect(work[0].dateNotice).toBe('startOnly');
  });

  it('drops lines before the first entry', () => {
    const lines = [
      columnLine(1, 223.6, 320.0, 10.5, 'stray line before any entry', 30.5),
      columnLine(1, 223.6, 308.5, 12, 'Example Co.', 38.5),
      columnLine(1, 223.6, 292.4, 11.5, 'Sample Engineer', 16),
      columnLine(1, 223.6, 277.9, 10.5, 'July 2020 - August 2022', 14.5),
    ];
    expect(parseExperience(lines)).toHaveLength(1);
  });

  it('keeps a <script> description as raw text in the draft', () => {
    const lines = [
      columnLine(1, 223.6, 308.5, 12, 'Example Co.', 38.5),
      columnLine(1, 223.6, 292.4, 11.5, 'Sample Engineer', 16),
      columnLine(1, 223.6, 277.9, 10.5, 'July 2020 - August 2022', 14.5),
      // 21.5 keeps this line out of the location check (gap over 1.6 * size)
      // so it lands in the description instead.
      columnLine(1, 223.6, 256.4, 10.5, '<script>alert(1)</script>', 21.5),
    ];
    // Rich text HTML-escapes, but the draft's job title / employer fields
    // are raw; check the underlying description carries escaped text since
    // linesToRichText always escapes (design: "HTML special characters are
    // escaped" for rich text output only, not other draft fields).
    expect(parseExperience(lines)[0].description).toContain(
      '&lt;script&gt;',
    );
  });
});

describe('parseEducation', () => {
  it('reads a school over two lines, a degree, and a wrapped date', () => {
    const lines = [
      columnLine(1, 223.6, 709.8, 12, 'Example University of', 25.5),
      columnLine(1, 223.6, 692.2, 12, 'Technology', 33.5),
      columnLine(
        1,
        223.6,
        674.2,
        10.5,
        'Bachelor of Science - BS, Computer Science · (December 2012 - '
        + 'November',
        17.5,
      ),
      columnLine(1, 223.6, 656.2, 10.5, '2016)', 18),
    ];
    const education = parseEducation(lines);
    expect(education).toEqual([
      {
        school: 'Example University of Technology',
        degree: 'Bachelor of Science - BS, Computer Science',
        dates: {
          start: { y: 2012, m: 12 },
          end: { y: 2016, m: 11 },
          present: false,
        },
      },
    ]);
  });

  it('reads a year-only range', () => {
    const lines = [
      columnLine(1, 223.6, 709.8, 12, 'Example University', 25.5),
      columnLine(
        1,
        223.6,
        692.2,
        10.5,
        'Bachelor of Arts - BA, History · (2012 - 2016)',
        17.5,
      ),
    ];
    expect(parseEducation(lines)[0].dates).toEqual({
      start: { y: 2012 },
      end: { y: 2016 },
      present: false,
    });
  });

  it('gives the whole text as degree, no dates, when there is no dot', () => {
    const lines = [
      columnLine(1, 223.6, 709.8, 12, 'Example University', 25.5),
      columnLine(1, 223.6, 692.2, 10.5, 'Bachelor of Arts, History', 17.5),
    ];
    const education = parseEducation(lines);
    expect(education[0].degree).toBe('Bachelor of Arts, History');
    expect(education[0].dates).toBeUndefined();
  });

  it('marks an unreadable education date', () => {
    const lines = [
      columnLine(1, 223.6, 709.8, 12, 'Example University', 25.5),
      columnLine(
        1,
        223.6,
        692.2,
        10.5,
        'Bachelor of Arts, History · (not a date)',
        17.5,
      ),
    ];
    const education = parseEducation(lines);
    expect(education[0].dates).toBeUndefined();
    expect(education[0].dateNotice).toBe('unreadable');
  });

  it('joins consecutive school-size lines as one school', () => {
    const lines = [
      columnLine(1, 223.6, 709.8, 12, 'Example School One', 25.5),
      columnLine(1, 223.6, 692.2, 12, 'Example School Two', 33.5),
      columnLine(1, 223.6, 674.2, 10.5, 'Degree A · (2012 - 2016)', 17.5),
    ];
    expect(parseEducation(lines)[0].school).toBe(
      'Example School One Example School Two',
    );
  });
});

describe('splitLocation', () => {
  it('splits at the last comma into city and country', () => {
    expect(splitLocation('Ho Chi Minh City, Vietnam')).toEqual({
      city: 'Ho Chi Minh City',
      country: 'Vietnam',
    });
  });

  it('gives the city only with no comma', () => {
    expect(splitLocation('Vietnam')).toEqual({ city: 'Vietnam' });
  });

  it('omits empty parts', () => {
    expect(splitLocation('Vietnam,')).toEqual({ city: 'Vietnam' });
    expect(splitLocation(', Vietnam')).toEqual({ country: 'Vietnam' });
    expect(splitLocation('')).toEqual({});
  });

  it('splits at the last comma with more than one comma', () => {
    expect(splitLocation('District 1, Ho Chi Minh City, Vietnam')).toEqual({
      city: 'District 1, Ho Chi Minh City',
      country: 'Vietnam',
    });
  });
});

describe('parseProfile', () => {
  it('lists dropped sections with entry counts, a zero-line one too', () => {
    const result = parseProfile(
      layout({
        sidebar: [
          section('Honors-Awards', true, [
            columnLine(1, 21.6, 500, 10.5, 'Award One', 19.5),
            columnLine(1, 21.6, 480, 10.5, 'Award Two', 17.5),
          ]),
        ],
        main: [
          section('Volunteer Experience', true, []),
          section('Projects', true, [
            columnLine(1, 223.6, 500, 12, 'Sample Project', 30.5),
            columnLine(
              1,
              223.6,
              480,
              10.5,
              'July 2020 - August 2021',
              14.5,
            ),
          ]),
          section('Unknown Heading', false, [
            columnLine(1, 223.6, 400, 12, 'Something', 30.5),
          ]),
        ],
      }),
    );
    expect(result.dropped).toEqual([
      { heading: 'Honors-Awards', count: 2 },
      { heading: 'Volunteer Experience', count: 0 },
      { heading: 'Projects', count: 1 },
      { heading: 'Unknown Heading', count: 1 },
    ]);
  });

  it('appends repeated sections with the same heading in order', () => {
    const result = parseProfile(
      layout({
        sidebar: [
          section('Top Skills', true, [
            columnLine(1, 21.6, 500, 10.5, 'Skill One', 19.5),
          ]),
          section('Top Skills', true, [
            columnLine(1, 21.6, 480, 10.5, 'Skill Two', 19.5),
          ]),
        ],
      }),
    );
    expect(result.skills).toEqual(['Skill One', 'Skill Two']);
  });

  it('builds contacts, work, education, skills, languages, and summary', () => {
    const result = parseProfile(
      layout({
        intro: [
          columnLine(1, 223.6, 705.3, 12, 'Sample Engineer', 21),
          columnLine(1, 223.6, 689.9, 12, 'Ho Chi Minh City, Vietnam', 15.5),
        ],
        sidebar: [
          section('Contact', true, [
            columnLine(1, 21.6, 705.6, 10.5, 'sample.person@example.com', 19.5),
          ]),
          section('Top Skills', true, [
            columnLine(1, 21.6, 585.9, 10.5, 'Sample Skill', 19.5),
          ]),
          section('Languages', true, [
            columnLine(1, 21.6, 550, 10.5, 'English (Native or Bilingual)',
              19.5),
          ]),
          section('Certifications', true, [
            columnLine(1, 21.6, 500, 10.5, 'Sample Certificate', 19.5),
          ]),
        ],
        main: [
          section('Summary', true, [
            columnLine(1, 223.6, 600, 12, 'A summary line.', 25.5),
          ]),
          section('Experience', true, [
            columnLine(1, 223.6, 500, 12, 'Example Co.', 38.5),
            columnLine(1, 223.6, 484, 11.5, 'Sample Engineer', 16),
            columnLine(1, 223.6, 469.5, 10.5, 'July 2020 - August 2022', 14.5),
          ]),
          section('Education', true, [
            columnLine(1, 223.6, 400, 12, 'Example University', 25.5),
            columnLine(1, 223.6, 382.5, 10.5, 'Degree · (2012 - 2016)', 17.5),
          ]),
        ],
      }),
    );
    expect(result.profile).toEqual({
      fullName: 'Sample Person',
      headline: 'Sample Engineer',
      location: 'Ho Chi Minh City, Vietnam',
    });
    expect(result.contacts).toEqual([
      { type: 'email', value: 'sample.person@example.com' },
    ]);
    expect(result.summary).toEqual({
      html: '<p>A summary line.</p>',
      cut: false,
    });
    expect(result.work).toHaveLength(1);
    expect(result.education).toHaveLength(1);
    expect(result.skills).toEqual(['Sample Skill']);
    expect(result.languages).toEqual([{ name: 'English', level: 5 }]);
    expect(result.certificates).toEqual(['Sample Certificate']);
    expect(result.dropped).toEqual([]);
  });
});
