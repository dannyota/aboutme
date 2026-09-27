// @vitest-environment node
import { describe, expect, it } from 'vitest';
import {
  analyzeLayout,
  SIDEBAR_HEADINGS,
  MAIN_HEADINGS,
} from '../../../app/import/linkedin/layout';
import type { Line } from '../../../app/import/linkedin/lines';
import { line } from './helpers/lineFixtures';

// A LinkedIn Save to PDF export has the sidebar at x 21.6 and the main
// column at x 223.6 (docs/design/linkedin-import.md, "Save to PDF
// structure"), so the boundary B = 223.6 - 12 = 211.6.
const SIDEBAR_X = 21.6;
const MAIN_X = 223.6;

function footer(page: number, numPages: number): Line {
  return line(page, 264, 40, 9, `Page ${page} of ${numPages}`);
}

/** A three-page profile shaped like the shape report, with synthetic words. */
function buildValidPages(): Line[][] {
  const page1: Line[] = [
    line(1, SIDEBAR_X, 737.6, 13, 'Contact'),
    line(1, SIDEBAR_X, 718.2, 10.5, '9999999999 (Home)'),
    line(1, SIDEBAR_X, 705.6, 10.5, 'sample.person@example.com'),
    line(1, SIDEBAR_X, 605.4, 13, 'Top Skills'),
    line(1, SIDEBAR_X, 585.9, 10.5, 'Sample Skill One'),
    line(1, SIDEBAR_X, 568.3, 10.5, 'Sample Skill Two'),
    line(1, MAIN_X, 726.5, 26, 'Sample Person'),
    line(1, MAIN_X, 705.3, 12, 'Sample Engineer # Example Co & Co'),
    line(1, MAIN_X, 689.9, 12, 'Ho Chi Minh City, Vietnam'),
    line(1, MAIN_X, 652.3, 16, 'Summary'),
    line(1, MAIN_X, 626.8, 12, 'A summary line about sample work.'),
    line(1, MAIN_X, 608.8, 12, 'A second summary line.'),
    line(1, MAIN_X, 339.0, 16, 'Experience'),
    line(1, MAIN_X, 308.5, 12, 'Example Co.'),
    line(1, MAIN_X, 292.4, 11.5, 'Senior Sample Engineer'),
    line(1, MAIN_X, 277.9, 10.5, 'July 2020 - August 2022 (2 years 2 months)'),
    footer(1, 3),
  ];
  const page2: Line[] = [
    line(2, SIDEBAR_X, 700.0, 10.5, 'Sample Skill Three'),
    line(2, SIDEBAR_X, 682.5, 10.5, 'Sample Skill Four'),
    line(2, MAIN_X, 700.0, 10.5, 'Ho Chi Minh City, Vietnam'),
    line(2, MAIN_X, 682.0, 10.5, 'Continued description of the role.'),
    line(2, MAIN_X, 500.0, 16, 'Education'),
    line(2, MAIN_X, 470.0, 12, 'Example University'),
    footer(2, 3),
  ];
  const page3: Line[] = [
    line(
      3,
      MAIN_X,
      700.0,
      10.5,
      'Bachelor\'s, Computer Science · (2012 - 2016)',
    ),
    footer(3, 3),
  ];
  return [page1, page2, page3];
}

describe('analyzeLayout on a three-page profile', () => {
  it('finds the name and column boundary from the largest page-1 line', () => {
    const result = analyzeLayout(buildValidPages());
    expect(result.kind).toBe('ok');
    if (result.kind !== 'ok') return;
    expect(result.layout.name.text).toBe('Sample Person');
    expect(result.layout.name.size).toBe(26);
    expect(result.layout.name.gap).toBeNull();
  });

  it('puts the headline and location in the intro, before main heading', () => {
    const result = analyzeLayout(buildValidPages());
    if (result.kind !== 'ok') throw new Error('expected ok');
    expect(result.layout.intro.map((l) => l.text)).toEqual([
      'Sample Engineer # Example Co & Co',
      'Ho Chi Minh City, Vietnam',
    ]);
  });

  it('builds sidebar sections in order, known', () => {
    const result = analyzeLayout(buildValidPages());
    if (result.kind !== 'ok') throw new Error('expected ok');
    expect(result.layout.sidebar.map((s) => s.heading)).toEqual([
      'Contact',
      'Top Skills',
    ]);
    expect(result.layout.sidebar.every((s) => s.known)).toBe(true);
  });

  it('joins a sidebar section across pages, at x 21.6 on page 2', () => {
    const result = analyzeLayout(buildValidPages());
    if (result.kind !== 'ok') throw new Error('expected ok');
    const topSkills = result.layout.sidebar.find(
      (s) => s.heading === 'Top Skills',
    );
    expect(topSkills?.lines.map((l) => l.text)).toEqual([
      'Sample Skill One',
      'Sample Skill Two',
      'Sample Skill Three',
      'Sample Skill Four',
    ]);
  });

  it('gives the first line of a later page a null gap', () => {
    const result = analyzeLayout(buildValidPages());
    if (result.kind !== 'ok') throw new Error('expected ok');
    const topSkills = result.layout.sidebar.find(
      (s) => s.heading === 'Top Skills',
    );
    const firstOnPage2 = topSkills?.lines.find((l) => l.page === 2);
    expect(firstOnPage2?.text).toBe('Sample Skill Three');
    expect(firstOnPage2?.gap).toBeNull();
    // the second page-2 line does have a gap, to the previous page-2 line
    const secondOnPage2 = topSkills?.lines.find(
      (l) => l.text === 'Sample Skill Four',
    );
    expect(secondOnPage2?.gap).toBe(17.5);
  });

  it('builds main sections across pages, Experience ends at Education', () => {
    const result = analyzeLayout(buildValidPages());
    if (result.kind !== 'ok') throw new Error('expected ok');
    expect(result.layout.main.map((s) => s.heading)).toEqual([
      'Summary',
      'Experience',
      'Education',
    ]);
    const experience = result.layout.main.find(
      (s) => s.heading === 'Experience',
    );
    expect(experience?.lines.map((l) => l.text)).toEqual([
      'Example Co.',
      'Senior Sample Engineer',
      'July 2020 - August 2022 (2 years 2 months)',
      'Ho Chi Minh City, Vietnam',
      'Continued description of the role.',
    ]);
    const education = result.layout.main.find(
      (s) => s.heading === 'Education',
    );
    expect(education?.lines.map((l) => l.text)).toEqual([
      'Example University',
      'Bachelor\'s, Computer Science · (2012 - 2016)',
    ]);
  });
});

describe('analyzeLayout footer and page failures', () => {
  it('is notLinkedIn when a page is missing its footer', () => {
    const pages = buildValidPages();
    pages[1] = pages[1].filter((l) => !l.text.startsWith('Page '));
    expect(analyzeLayout(pages)).toEqual({ kind: 'notLinkedIn' });
  });

  it('is notLinkedIn when a footer names the wrong page count', () => {
    const pages = buildValidPages();
    pages[1] = pages[1].map((l) =>
      l.text === 'Page 2 of 3' ? { ...l, text: 'Page 2 of 5' } : l,
    );
    expect(analyzeLayout(pages)).toEqual({ kind: 'notLinkedIn' });
  });

  it('is notLinkedIn for an empty file', () => {
    expect(analyzeLayout([])).toEqual({ kind: 'notLinkedIn' });
  });

  it('is notLinkedIn when page 1 has no lines', () => {
    expect(analyzeLayout([[]])).toEqual({ kind: 'notLinkedIn' });
  });
});

describe('analyzeLayout English check', () => {
  it('is notEnglish when headings are Vietnamese', () => {
    const pages: Line[][] = [
      [
        line(1, SIDEBAR_X, 737.6, 13, 'Liên hệ'),
        line(1, SIDEBAR_X, 718.2, 10.5, '9999999999 (Nhà riêng)'),
        line(1, MAIN_X, 726.5, 26, 'Mẫu Người'),
        line(1, MAIN_X, 652.3, 16, 'Kinh nghiệm'),
        line(1, MAIN_X, 626.8, 12, 'Một dòng kinh nghiệm.'),
        footer(1, 1),
      ],
    ];
    expect(analyzeLayout(pages)).toEqual({ kind: 'notEnglish' });
  });

  it('is notEnglish with only one recognized English heading', () => {
    const pages: Line[][] = [
      [
        line(1, SIDEBAR_X, 737.6, 13, 'Contact'),
        line(1, SIDEBAR_X, 718.2, 10.5, '9999999999 (Home)'),
        line(1, SIDEBAR_X, 705.6, 10.5, 'sample.person@example.com'),
        line(1, MAIN_X, 726.5, 26, 'Sample Person'),
        line(1, MAIN_X, 652.3, 16, 'Tóm tắt'),
        line(1, MAIN_X, 626.8, 12, 'A summary line.'),
        line(1, MAIN_X, 608.8, 12, 'A second summary line.'),
        footer(1, 1),
      ],
    ];
    expect(analyzeLayout(pages)).toEqual({ kind: 'notEnglish' });
  });

  it('is ok with two recognized English headings', () => {
    const pages: Line[][] = [
      [
        line(1, SIDEBAR_X, 737.6, 13, 'Contact'),
        line(1, SIDEBAR_X, 718.2, 10.5, '9999999999 (Home)'),
        line(1, SIDEBAR_X, 705.6, 10.5, 'sample.person@example.com'),
        line(1, MAIN_X, 726.5, 26, 'Sample Person'),
        line(1, MAIN_X, 652.3, 16, 'Summary'),
        line(1, MAIN_X, 626.8, 12, 'A summary line.'),
        line(1, MAIN_X, 608.8, 12, 'A second summary line.'),
        footer(1, 1),
      ],
    ];
    expect(analyzeLayout(pages).kind).toBe('ok');
  });
});

describe('analyzeLayout heading edge cases', () => {
  it('keeps an employer named "Experience" as text, below heading size', () => {
    const pages: Line[][] = [
      [
        line(1, SIDEBAR_X, 737.6, 13, 'Contact'),
        line(1, SIDEBAR_X, 718.2, 10.5, '9999999999 (Home)'),
        line(1, MAIN_X, 726.5, 26, 'Sample Person'),
        line(1, MAIN_X, 652.3, 16, 'Summary'),
        line(1, MAIN_X, 626.8, 12, 'A summary line.'),
        line(1, MAIN_X, 500.0, 16, 'Experience'),
        // an employer named "Experience", at the employer size
        line(1, MAIN_X, 470.0, 12, 'Experience'),
        line(1, MAIN_X, 450.0, 11.5, 'Sample Title'),
        footer(1, 1),
      ],
    ];
    const result = analyzeLayout(pages);
    if (result.kind !== 'ok') throw new Error('expected ok');
    expect(result.layout.main.map((s) => s.heading)).toEqual([
      'Summary',
      'Experience',
    ]);
    const experience = result.layout.main.find(
      (s) => s.heading === 'Experience',
    );
    expect(experience?.lines.map((l) => l.text)).toEqual([
      'Experience',
      'Sample Title',
    ]);
  });

  it('ends the previous section at an unknown heading, same size', () => {
    const pages: Line[][] = [
      [
        line(1, SIDEBAR_X, 737.6, 13, 'Contact'),
        line(1, SIDEBAR_X, 718.2, 10.5, '9999999999 (Home)'),
        line(1, SIDEBAR_X, 705.6, 10.5, 'sample.person@example.com'),
        line(1, MAIN_X, 726.5, 26, 'Sample Person'),
        line(1, MAIN_X, 652.3, 16, 'Summary'),
        line(1, MAIN_X, 626.8, 12, 'A summary line.'),
        // unknown main heading, at the same size as the known headings
        line(1, MAIN_X, 500.0, 16, 'Recommendations'),
        line(1, MAIN_X, 470.0, 12, 'A recommendation line.'),
        footer(1, 1),
      ],
    ];
    const result = analyzeLayout(pages);
    if (result.kind !== 'ok') throw new Error('expected ok');
    expect(
      result.layout.main.map((s) => ({ heading: s.heading, known: s.known })),
    ).toEqual([
      { heading: 'Summary', known: true },
      { heading: 'Recommendations', known: false },
    ]);
    const summary = result.layout.main.find((s) => s.heading === 'Summary');
    expect(summary?.lines.map((l) => l.text)).toEqual(['A summary line.']);
    const recommendations = result.layout.main.find(
      (s) => s.heading === 'Recommendations',
    );
    expect(recommendations?.lines.map((l) => l.text)).toEqual([
      'A recommendation line.',
    ]);
  });

  it('keeps a sidebar heading in the main column as text, off size', () => {
    const pages: Line[][] = [
      [
        line(1, SIDEBAR_X, 737.6, 13, 'Contact'),
        line(1, SIDEBAR_X, 718.2, 10.5, '9999999999 (Home)'),
        line(1, SIDEBAR_X, 705.6, 10.5, 'sample.person@example.com'),
        line(1, MAIN_X, 726.5, 26, 'Sample Person'),
        line(1, MAIN_X, 652.3, 16, 'Summary'),
        // "Contact" is a sidebar heading, not a main one
        line(1, MAIN_X, 626.8, 12, 'Contact'),
        line(1, MAIN_X, 608.8, 16, 'Experience'),
        line(1, MAIN_X, 590.0, 12, 'Example Co.'),
        footer(1, 1),
      ],
    ];
    const result = analyzeLayout(pages);
    if (result.kind !== 'ok') throw new Error('expected ok');
    expect(result.layout.main.map((s) => s.heading)).toEqual([
      'Summary',
      'Experience',
    ]);
    const summary = result.layout.main.find((s) => s.heading === 'Summary');
    expect(summary?.lines.map((l) => l.text)).toEqual(['Contact']);
  });
});

describe('the known heading lists', () => {
  it('names the sidebar headings from the design doc', () => {
    expect(SIDEBAR_HEADINGS).toEqual([
      'Contact',
      'Top Skills',
      'Languages',
      'Certifications',
      'Honors-Awards',
      'Publications',
      'Patents',
    ]);
  });

  it('names the main headings from the design doc', () => {
    expect(MAIN_HEADINGS).toEqual([
      'Summary',
      'Experience',
      'Education',
      'Volunteer Experience',
      'Projects',
    ]);
  });
});
