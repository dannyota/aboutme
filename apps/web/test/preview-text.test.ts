import type { PersonalDetail, Resume, Section } from '@aboutme/schema';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

import {
  cutPreviewDescription,
  normalizePreviewText,
  previewCardText,
  previewContactValues,
  previewDescription,
  previewImageText,
  previewLocale,
  previewTitle,
  scrubPreviewText,
} from '../app/utils/previewText';

// The corpus is shared with the server's internal/previewmeta tests, so the
// publish-panel preview matches the authoritative Go text rules
// (docs/design/link-previews.md, "Text rules").

interface NormalizeCase { name: string; input: string; want: string }
interface ScrubCase {
  name: string;
  input: string;
  contacts: string[];
  want: string;
}
interface CutCase { name: string; input: string; want: string }
interface LocaleCase { lng: string; want: string }

const corpus = JSON.parse(readFileSync(resolve(
  process.cwd(),
  '../server/internal/previewmeta/testdata/text-cases.json',
), 'utf8')) as {
  normalize: NormalizeCase[];
  scrub: ScrubCase[];
  cut: CutCase[];
  locale: LocaleCase[];
};

describe('normalizePreviewText matches the Go corpus', () => {
  it.each(corpus.normalize.map((test) => [test.name, test] as const))(
    '%s',
    (_name, test) => {
      expect(normalizePreviewText(test.input)).toBe(test.want);
    },
  );
});

describe('scrubPreviewText matches the Go corpus', () => {
  it.each(corpus.scrub.map((test) => [test.name, test] as const))(
    '%s',
    (_name, test) => {
      expect(scrubPreviewText(test.input, test.contacts)).toBe(test.want);
    },
  );
});

describe('cutPreviewDescription matches the Go corpus', () => {
  it.each(corpus.cut.map((test) => [test.name, test] as const))(
    '%s',
    (_name, test) => {
      expect(cutPreviewDescription(test.input)).toBe(test.want);
    },
  );
});

describe('previewLocale matches the Go corpus', () => {
  it.each(corpus.locale.map((test) => [test.lng, test] as const))(
    '%s',
    (_lng, test) => {
      expect(previewLocale(test.lng)).toBe(test.want);
    },
  );
});

// Fixture builders. Every field an owning export reads is set explicitly;
// every other Customization field takes a fixed, valid value so the document
// matches the Resume schema without affecting the text rules under test.

let nextId = 0;
const id = (): string => `id-${String(nextId++)}`;

function detail(
  type: PersonalDetail['type'],
  value: string,
  isHidden = false,
): PersonalDetail {
  return { id: id(), type, value, isHidden };
}

function profileSection(
  entries: { text?: string; isHidden?: boolean }[],
): Section {
  return {
    sectionType: 'profile',
    entries: entries.map((entry) => ({
      id: id(),
      isHidden: entry.isHidden,
      text: entry.text,
    })),
  };
}

function workSection(
  entries: { jobTitle?: string; employer?: string; isHidden?: boolean }[],
): Section {
  return {
    sectionType: 'work',
    entries: entries.map((entry) => ({
      id: id(),
      isHidden: entry.isHidden,
      jobTitle: entry.jobTitle,
      employer: entry.employer,
    })),
  };
}

function baseCustomization(
  main: string[],
  sidebar: string[],
): Resume['customization'] {
  return {
    font: { family: 'inter', baseSizePx: 12 },
    colors: { primary: '#000000', text: '#000000', background: '#ffffff' },
    spacing: { sectionGap: 12, entryGap: 8, lineHeight: 1.4 },
    heading: { style: 'normal', showRule: false },
    layout: { columns: 1, sections: { main, sidebar } },
    sectionDisplay: { skill: { style: 'text' }, language: { style: 'text' } },
    pageFormat: 'a4',
    dateFormat: 'MM/YYYY',
  };
}

function makeResume(params: {
  personalDetails?: Resume['personalDetails'];
  content?: Resume['content'];
  main?: string[];
  sidebar?: string[];
}): Resume {
  return {
    schemaVersion: 4,
    personalDetails: params.personalDetails ?? {},
    content: params.content ?? {},
    customization: baseCustomization(params.main ?? [], params.sidebar ?? []),
  };
}

describe('previewTitle', () => {
  it('uses the public title as written', () => {
    const document = makeResume({
      personalDetails: { fullName: 'Ada Lovelace' },
    });
    expect(previewTitle('  Ada, Engineer  ', document, 'ada'))
      .toBe('  Ada, Engineer  ');
  });

  it('cuts the full name to 70 clusters when there is no public title', () => {
    const document = makeResume({
      personalDetails: { fullName: 'A'.repeat(80) },
    });
    expect(previewTitle(null, document, 'ada')).toBe('A'.repeat(70));
  });

  it('falls back to the site and slug with no title or name', () => {
    const document = makeResume({});
    expect(previewTitle(null, document, 'ada')).toBe('aboutme.vn/ada');
    expect(previewTitle('', document, 'ada')).toBe('aboutme.vn/ada');
  });
});

describe('previewDescription', () => {
  it('uses the summary from the first profile section in layout order', () => {
    const document = makeResume({
      content: {
        sidebarProfile: profileSection([{ text: '<p>Sidebar summary</p>' }]),
        mainProfile: profileSection([{ text: '<p>Main summary</p>' }]),
      },
      main: ['mainProfile'],
      sidebar: ['sidebarProfile'],
    });
    expect(previewDescription(document, 'en')).toBe('Main summary');
  });

  it('skips a hidden profile entry', () => {
    const document = makeResume({
      content: {
        profile: profileSection([
          { text: '<p>Hidden</p>', isHidden: true },
          { text: '<p>Visible summary</p>' },
        ]),
      },
      main: ['profile'],
    });
    expect(previewDescription(document, 'en')).toBe('Visible summary');
  });

  it('ignores a profile section outside the layout', () => {
    const document = makeResume({
      content: {
        outside: profileSection([{ text: '<p>Outside summary</p>' }]),
        work: workSection([{ jobTitle: 'Engineer', employer: 'Acme' }]),
      },
      main: ['work'],
    });
    expect(previewDescription(document, 'en')).toBe('Engineer, Acme');
  });

  it('joins the headline and the latest role with a middle dot', () => {
    const document = makeResume({
      personalDetails: { headline: 'Backend engineer' },
      content: {
        work: workSection([{ jobTitle: 'Engineer', employer: 'Acme' }]),
      },
      main: ['work'],
    });
    expect(previewDescription(document, 'en'))
      .toBe('Backend engineer · Engineer, Acme');
  });

  it('falls back to English with no summary, headline, or role', () => {
    expect(previewDescription(makeResume({}), 'en'))
      .toBe('Resume on aboutme.vn');
  });

  it('falls back to Vietnamese for a Vietnamese resume', () => {
    expect(previewDescription(makeResume({}), 'vi')).toBe('CV trên aboutme.vn');
  });
});

describe('previewImageText', () => {
  it('joins the name and headline with a middle dot', () => {
    const document = makeResume({
      personalDetails: { fullName: 'Ada Lovelace', headline: 'Mathematician' },
    });
    expect(previewImageText(document, 'ada'))
      .toBe('Ada Lovelace · Mathematician');
  });

  it('falls back to the site and slug with no name', () => {
    expect(previewImageText(makeResume({}), 'ada')).toBe('aboutme.vn/ada');
  });

  it('drops a headline that loses a scrubbed token, keeping the name', () => {
    const document = makeResume({
      personalDetails: {
        fullName: 'Ada Lovelace',
        headline: 'Call me at 0912345678',
      },
    });
    expect(previewImageText(document, 'ada')).toBe('Ada Lovelace');
  });
});

describe('previewCardText', () => {
  it('returns the name and headline when neither is scrubbed', () => {
    const document = makeResume({
      personalDetails: { fullName: 'Ada Lovelace', headline: 'Mathematician' },
    });
    expect(previewCardText(document))
      .toEqual({ name: 'Ada Lovelace', headline: 'Mathematician' });
  });

  it('drops a headline that would be scrubbed, keeping the name', () => {
    const document = makeResume({
      personalDetails: {
        fullName: 'Ada Lovelace',
        headline: 'Reach me at 0912345678',
      },
    });
    expect(previewCardText(document))
      .toEqual({ name: 'Ada Lovelace', headline: null });
  });

  it('drops both when the name contains a visible contact value', () => {
    const document = makeResume({
      personalDetails: {
        fullName: 'Ada Lovelace, Hanoi',
        headline: 'Mathematician',
        details: [detail('location', 'Hanoi')],
      },
    });
    expect(previewCardText(document)).toEqual({ name: null, headline: null });
  });

  it('drops both when the name contains an email', () => {
    const document = makeResume({
      personalDetails: {
        fullName: 'Ada ada@example.com',
        headline: 'Mathematician',
      },
    });
    expect(previewCardText(document)).toEqual({ name: null, headline: null });
  });

  it('drops both when the name contains a nine-digit phone', () => {
    const document = makeResume({
      personalDetails: { fullName: 'Ada 123456789', headline: 'Mathematician' },
    });
    expect(previewCardText(document)).toEqual({ name: null, headline: null });
  });
});

describe('contact sentinels never leak into preview text', () => {
  const hiddenContactSentinel = 'ZZ-HIDDEN-CONTACT-9f2b';
  const visibleContactSentinel = 'ZZ-VISIBLE-CONTACT-7c31';
  const hiddenProfileSentinel = 'ZZ-HIDDEN-PROFILE-4a10';
  const outsideSectionSentinel = 'ZZ-OUTSIDE-SECTION-88de';

  const document = makeResume({
    personalDetails: {
      fullName: `Ada ${visibleContactSentinel} Lovelace`,
      headline: `Works with ${visibleContactSentinel}`,
      details: [
        detail('location', visibleContactSentinel),
        detail('phone', hiddenContactSentinel, true),
      ],
    },
    content: {
      profile: profileSection([
        { text: `<p>${hiddenProfileSentinel}</p>`, isHidden: true },
        { text: '<p>Visible summary</p>' },
      ]),
      outside: profileSection([{ text: `<p>${outsideSectionSentinel}</p>` }]),
    },
    main: ['profile'],
  });

  const sentinels = [
    hiddenContactSentinel,
    visibleContactSentinel,
    hiddenProfileSentinel,
    outsideSectionSentinel,
  ];

  it('never leaks into the title, description, image, or card text', () => {
    const outputs = [
      previewTitle(null, document, 'ada'),
      previewDescription(document, 'en'),
      previewImageText(document, 'ada'),
      JSON.stringify(previewCardText(document)),
    ];
    for (const output of outputs) {
      for (const sentinel of sentinels) {
        expect(output).not.toContain(sentinel);
      }
    }
  });

  it('excludes the hidden contact from previewContactValues', () => {
    expect(previewContactValues(document)).not.toContain(hiddenContactSentinel);
  });
});
