// @vitest-environment node
// Tests for turning a parsed LinkedIn profile into the review and the resume
// document. See docs/design/linkedin-import.md, "Mapping" (including
// "Limits") and "What is dropped", and docs/design/linkedin-import-ui.md,
// "Review state"; ADR 0064.
import * as pdfjs from 'pdfjs-dist/legacy/build/pdf.mjs';
import { describe, expect, it } from 'vitest';

import {
  buildDocument,
  buildReview,
  checkDocument,
  IMPORT_SECTION_ORDER,
  importLinkedInPdf,
  REQUEST_MAX_BYTES,
  requestBytes,
  type ImportReview,
  type ReviewChoices,
} from '../../../app/import/linkedin/build';
import type {
  ParsedProfile,
  WorkDraft,
} from '../../../app/import/linkedin/sections';
import { GALLERY } from '../../../app/templates/catalog';
import { pdfBlob, textPdf } from './helpers/pdfBuilder';
import type { Resume } from '@aboutme/schema';

/** A deterministic, schema-valid (format: uuid) id generator for tests. */
function makeUuid(): () => string {
  let n = 0;
  return () => {
    n += 1;
    return `00000000-0000-4000-8000-${n.toString(16).padStart(12, '0')}`;
  };
}

function workDraft(overrides: Partial<WorkDraft> = {}): WorkDraft {
  return {
    employer: 'Example Co.',
    jobTitle: 'Senior Sample Engineer',
    dates: { start: { y: 2020, m: 1 }, end: null, present: true },
    city: 'Ho Chi Minh City',
    country: 'Vietnam',
    description: '<p>Built things.</p>',
    descriptionCut: false,
    ...overrides,
  };
}

/** One ParsedProfile covering every mapped section. */
function fullParsedProfile(): ParsedProfile {
  return {
    profile: {
      fullName: 'Sample Person',
      headline: 'Senior Sample Engineer',
      location: 'Ho Chi Minh City, Vietnam',
    },
    contacts: [
      { type: 'email', value: 'sample.person@example.com' },
      { type: 'phone', value: '0900000000' },
      { type: 'linkedin', value: 'https://linkedin.com/in/sampleperson' },
      { type: 'website', value: 'https://example.com', label: 'Portfolio' },
    ],
    summary: { html: '<p>Sample summary.</p>', cut: false },
    work: [workDraft()],
    education: [
      {
        school: 'Sample University',
        degree: 'B.Eng, Computer Science',
        dates: {
          start: { y: 2014, m: 9 },
          end: { y: 2018, m: 6 },
          present: false,
        },
      },
    ],
    skills: ['Sample Skill'],
    languages: [{ name: 'English', level: 4 }],
    certificates: ['Sample Certificate'],
    dropped: [{ heading: 'Volunteer Experience', count: 2 }],
  };
}

function defaultChoices(
  review: ImportReview,
  title = 'Test title',
): ReviewChoices {
  const detailIds = new Set(
    review.details
      .filter((detail) => detail.selectedByDefault)
      .map((detail) => detail.id),
  );
  const entryIds = new Set(
    review.sections.flatMap((section) =>
      section.entries
        .filter((entry) => entry.selectedByDefault)
        .map((entry) => entry.id)),
  );
  return {
    title,
    fullName: review.fullName,
    headline: review.headline,
    detailIds,
    entryIds,
  };
}

describe('buildReview', () => {
  it('builds the review of a full profile', () => {
    const review = buildReview(fullParsedProfile(), makeUuid());

    expect(review.fullName).toBe('Sample Person');
    expect(review.headline).toBe('Senior Sample Engineer');
    expect(review.nameMarks).toEqual([]);
    expect(review.dropped).toEqual([
      { heading: 'Volunteer Experience', count: 2 },
    ]);
    expect(review.template).toEqual({
      id: GALLERY[0].id,
      name: GALLERY[0].name,
    });
    expect(review.sections.map((s) => s.key)).toEqual(IMPORT_SECTION_ORDER);
  });

  it('starts email and phone unselected, everything else selected', () => {
    const review = buildReview(fullParsedProfile(), makeUuid());
    const byType = Object.fromEntries(
      review.details.map((detail) => [detail.type, detail.selectedByDefault]),
    );
    expect(byType).toEqual({
      email: false,
      phone: false,
      location: true,
      linkedin: true,
      website: true,
    });
    for (const section of review.sections) {
      for (const entry of section.entries) {
        expect(entry.selectedByDefault).toBe(true);
      }
    }
  });

  it('orders details email, phone, location, linkedin, then websites', () => {
    const review = buildReview(fullParsedProfile(), makeUuid());
    expect(review.details.map((d) => d.type)).toEqual([
      'email', 'phone', 'location', 'linkedin', 'website',
    ]);
    const website = review.details.find((d) => d.type === 'website')!;
    expect(website.label).toBe('Portfolio');
  });

  it('caps a section at 64 selected by default and marks it over limit', () => {
    const parsed = fullParsedProfile();
    parsed.work = Array.from({ length: 70 }, (_, i) =>
      workDraft({ employer: `Employer ${i}`, jobTitle: `Title ${i}` }));
    const review = buildReview(parsed, makeUuid());
    const work = review.sections.find((s) => s.key === 'work')!;

    expect(work.entries).toHaveLength(70);
    expect(work.overLimit).toBe(true);
    expect(
      work.entries.slice(0, 64).every((e) => e.selectedByDefault),
    ).toBe(true);
    expect(
      work.entries.slice(64).every((e) => !e.selectedByDefault),
    ).toBe(true);
  });

  it('cuts every limited field and marks it', () => {
    const long160 = 'A'.repeat(200);
    const long120 = 'B'.repeat(150);
    const parsed: ParsedProfile = {
      ...fullParsedProfile(),
      profile: { fullName: long160, headline: long160 },
      summary: { html: '<p>Sample summary.</p>', cut: true },
      work: [workDraft({
        employer: long160,
        jobTitle: long160,
        city: long120,
        country: long120,
        descriptionCut: true,
      })],
      education: [{ school: long160, degree: long160 }],
      skills: [long120],
      languages: [{ name: long120, level: 4 }],
      certificates: [long160],
    };
    const review = buildReview(parsed, makeUuid());

    expect(review.fullName).toHaveLength(160);
    expect(review.headline).toHaveLength(160);
    expect(review.nameMarks).toEqual([
      { kind: 'cut', field: 'fullName', max: 160 },
      { kind: 'cut', field: 'headline', max: 160 },
    ]);

    const byKey = Object.fromEntries(review.sections.map((s) => [s.key, s]));
    expect(byKey.profile.entries[0].marks).toEqual([
      { kind: 'cut', field: 'summary', max: 16384 },
    ]);
    expect(byKey.work.entries[0].marks).toEqual(expect.arrayContaining([
      { kind: 'cut', field: 'employer', max: 160 },
      { kind: 'cut', field: 'jobTitle', max: 160 },
      { kind: 'cut', field: 'city', max: 120 },
      { kind: 'cut', field: 'country', max: 120 },
      { kind: 'cut', field: 'description', max: 16384 },
    ]));
    expect(byKey.work.entries[0].entry.employer).toHaveLength(160);
    expect(byKey.work.entries[0].entry.city).toHaveLength(120);
    expect(byKey.education.entries[0].marks).toEqual(expect.arrayContaining([
      { kind: 'cut', field: 'school', max: 160 },
      { kind: 'cut', field: 'degree', max: 160 },
    ]));
    expect(byKey.skill.entries[0].marks).toEqual([
      { kind: 'cut', field: 'skill', max: 120 },
    ]);
    expect(byKey.language.entries[0].marks).toEqual([
      { kind: 'cut', field: 'language', max: 120 },
    ]);
    expect(byKey.certificate.entries[0].marks).toEqual([
      { kind: 'cut', field: 'certificate', max: 160 },
    ]);
  });

  it('marks a work entry with a start-only date and an unreadable date', () => {
    const parsed = fullParsedProfile();
    parsed.work = [
      workDraft({ dates: undefined, dateNotice: 'startOnly' }),
      workDraft({ dates: undefined, dateNotice: 'unreadable' }),
    ];
    const review = buildReview(parsed, makeUuid());
    const work = review.sections.find((s) => s.key === 'work')!;

    expect(work.entries[0].marks).toEqual([{ kind: 'startOnly' }]);
    expect(work.entries[0].entry.dates).toBeUndefined();
    expect(work.entries[1].marks).toEqual([{ kind: 'noDates' }]);
  });

  it('marks an education entry with an unreadable date', () => {
    const parsed = fullParsedProfile();
    parsed.education = [
      { school: 'Sample University', dateNotice: 'unreadable' },
    ];
    const review = buildReview(parsed, makeUuid());
    const education = review.sections.find((s) => s.key === 'education')!;

    expect(education.entries[0].marks).toEqual([{ kind: 'noDates' }]);
  });

  it('keeps a <script> string plain in a field, escaped in rich text', () => {
    const parsed = fullParsedProfile();
    parsed.skills = ['<script>alert(1)</script>'];
    parsed.summary = {
      html: '<p>&lt;script&gt;alert(1)&lt;/script&gt;</p>',
      cut: false,
    };
    const review = buildReview(parsed, makeUuid());

    const skill = review.sections.find((s) => s.key === 'skill')!.entries[0];
    expect(skill.entry.name).toBe('<script>alert(1)</script>');

    const profile = review.sections.find((s) => s.key === 'profile')!
      .entries[0];
    expect(profile.entry.text).not.toContain('<script>');
    expect(profile.entry.text).toContain('&lt;script&gt;');
  });
});

describe('buildDocument and checkDocument', () => {
  it('builds a document from the full profile passing checkDocument', () => {
    const review = buildReview(fullParsedProfile(), makeUuid());
    const choices = defaultChoices(review);
    const document = buildDocument(review, choices);

    expect(document.customization.layout.sections).toBeDefined();
    expect(Object.keys(document.content)).toEqual(IMPORT_SECTION_ORDER);
    expect(checkDocument(document, review, choices)).toEqual({ ok: true });
  });

  it('leaves a deselected section out of the document', () => {
    const review = buildReview(fullParsedProfile(), makeUuid());
    const choices = defaultChoices(review);
    const skillIds = review.sections.find((s) => s.key === 'skill')!
      .entries.map((e) => e.id);
    const withoutSkill: ReviewChoices = {
      ...choices,
      entryIds: new Set(
        [...choices.entryIds].filter((id) => !skillIds.includes(id)),
      ),
    };

    const document = buildDocument(review, withoutSkill);
    expect(document.content.skill).toBeUndefined();
    expect(Object.keys(document.content)).not.toContain('skill');
  });

  it('never puts more than 64 entries in a section, even if chosen', () => {
    const parsed = fullParsedProfile();
    parsed.work = Array.from({ length: 70 }, (_, i) =>
      workDraft({ employer: `Employer ${i}`, jobTitle: `Title ${i}` }));
    const review = buildReview(parsed, makeUuid());
    const work = review.sections.find((s) => s.key === 'work')!;
    const choices: ReviewChoices = {
      title: 'Test title',
      fullName: review.fullName,
      headline: review.headline,
      detailIds: new Set(),
      entryIds: new Set(work.entries.map((e) => e.id)),
    };

    const document = buildDocument(review, choices);
    const entries = document.content.work!.entries;
    expect(entries).toHaveLength(64);
    expect(entries.map((e) => (e as { id: string }).id))
      .toEqual(work.entries.slice(0, 64).map((e) => e.id));
  });

  it('maps an invalid entry (end before start) to its entry id', () => {
    const review = buildReview(fullParsedProfile(), makeUuid());
    const choices = defaultChoices(review);
    const work = review.sections.find((s) => s.key === 'work')!.entries[0];
    work.entry.dates = {
      start: { y: 2022, m: 1 },
      end: { y: 2020, m: 1 },
      present: false,
    };

    const document = buildDocument(review, choices);
    const check = checkDocument(document, review, choices);

    expect(check).toEqual({ ok: false, entryIds: [work.id], general: false });
  });
});

describe('requestBytes', () => {
  it('counts UTF-8 bytes of the exact create request body', () => {
    const document = {
      schemaVersion: 4,
      personalDetails: {
        fullName: 'Nguyễn Văn Mẫu',
        headline: '',
        details: [],
      },
      content: {},
      customization: {},
    } as unknown as Resume;

    expect(requestBytes('LinkedIn resume', document)).toBe(181);
  });

  it('goes over the limit for a large set of descriptions', () => {
    const parsed = fullParsedProfile();
    const bigDescription = `<p>${'x'.repeat(100_000)}</p>`;
    parsed.work = [
      workDraft({ description: bigDescription }),
      workDraft({ description: bigDescription }),
      workDraft({ description: bigDescription }),
    ];
    const review = buildReview(parsed, makeUuid());
    const choices = defaultChoices(review);
    const document = buildDocument(review, choices);

    expect(requestBytes(choices.title, document))
      .toBeGreaterThan(REQUEST_MAX_BYTES);
  });
});

describe('importLinkedInPdf', () => {
  it('reads a minimal English LinkedIn PDF into a review', async () => {
    const pdf = textPdf([[
      { text: 'Sample Person', x: 223.6, y: 726.5, size: 26 },
      { text: 'Summary', x: 223.6, y: 690, size: 16 },
      { text: 'Sample summary text.', x: 223.6, y: 670, size: 12 },
      { text: 'Experience', x: 223.6, y: 650, size: 16 },
      { text: 'Senior Sample Engineer', x: 223.6, y: 630, size: 11.5 },
      { text: 'Jan 2020 - Present', x: 223.6, y: 614, size: 10.5 },
      { text: 'Page 1 of 1', x: 264, y: 40, size: 9 },
    ]]);

    const result = await importLinkedInPdf(
      pdfBlob(pdf), { pdfjs }, makeUuid(),
    );

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.review.fullName).toBe('Sample Person');
    expect(result.review.sections.map((s) => s.key))
      .toEqual(['profile', 'work']);
  });

  it('reports notLinkedIn when no page has the footer', async () => {
    const pdf = textPdf([[{ text: 'Hello', x: 100, y: 700, size: 12 }]]);
    const result = await importLinkedInPdf(
      pdfBlob(pdf), { pdfjs }, makeUuid(),
    );
    expect(result).toEqual({ ok: false, reason: 'notLinkedIn' });
  });

  it('forwards a read failure such as notPdf', async () => {
    const bytes = new TextEncoder().encode('not a pdf');
    const result = await importLinkedInPdf(
      pdfBlob(bytes), { pdfjs }, makeUuid(),
    );
    expect(result).toEqual({ ok: false, reason: 'notPdf' });
  });
});
