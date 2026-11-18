// @vitest-environment node

import type { Resume } from '@aboutme/schema';
import { JSDOM } from 'jsdom';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { createSSRApp, h } from 'vue';
import { renderToString } from 'vue/server-renderer';
import { describe, expect, it } from 'vitest';

import { PaginationMeasureKey } from '../../app/components/resume/measure';
import ResumeDocument from '../../app/components/resume/ResumeDocument.vue';
import type {
  RenderContext,
} from '../../app/components/resume/resolveRenderModel';
import { syntheticMeasure } from './synthetic-measure';

const PHOTO = 'data:image/png;base64,AA==';

const full = (): Resume =>
  JSON.parse(
    readFileSync(
      resolve(
        import.meta.dirname,
        '../../../../packages/schema/fixtures/full.json',
      ),
      'utf8',
    ),
  ) as Resume;

const render = async (
  document: Resume,
  context: Partial<RenderContext> = {},
): Promise<Document> => {
  const app = createSSRApp({
    render: () => h(ResumeDocument, {
      document,
      context: {
        lng: 'en',
        mode: 'sheets',
        ...(document.personalDetails.photo === undefined
          ? {}
          : { photoUrl: PHOTO }),
        ...context,
      },
    }),
  });
  app.provide(PaginationMeasureKey, syntheticMeasure);
  return new JSDOM(await renderToString(app)).window.document;
};

const named = (fullName: string | undefined): Resume => {
  const document = full();
  if (fullName === undefined) delete document.personalDetails.fullName;
  else document.personalDetails.fullName = fullName;
  return document;
};

const folios = (document: Document): Element[] =>
  [...document.querySelectorAll('.resume-folio')];

describe('sheets markup', () => {
  it('renders one article whose children are plain sheet divs', async () => {
    const document = await render(full());
    const articles = document.querySelectorAll('article.resume-document');
    expect(articles).toHaveLength(1);
    const article = articles[0]!;
    expect(article.classList.contains('resume-sheets')).toBe(true);
    expect(article.getAttribute('lang')).toBe('en');
    const sheets = [...article.children];
    expect(sheets.length).toBe(6);
    for (const sheet of sheets) {
      expect(sheet.tagName).toBe('DIV');
      expect(sheet.className).toBe('resume-sheet');
      expect(sheet.hasAttribute('role')).toBe(false);
      expect(sheet.hasAttribute('aria-label')).toBe(false);
      expect(sheet.hasAttribute('aria-labelledby')).toBe(false);
      expect(sheet.hasAttribute('style')).toBe(false);
      expect(sheet.lastElementChild?.classList.contains('resume-folio'))
        .toBe(true);
    }
  });

  it('holds the header and the summary on sheet 1', async () => {
    const document = await render(full());
    const [first, second] = document.querySelectorAll('.resume-sheet');
    const header = first!.querySelector('header.resume-header');
    expect(header).not.toBeNull();
    expect(header!.querySelector('h1.resume-name')?.textContent?.trim())
      .toBe('Ada Lovelace');
    expect(first!.querySelectorAll('section.resume-section')).toHaveLength(1);
    expect(second!.querySelector('header.resume-header')).toBeNull();
    expect(document.querySelectorAll('header.resume-header')).toHaveLength(1);
  });

  it('renders side sections as ordinary sections with no aside', async () => {
    const document = await render(full());
    expect(document.querySelector('aside')).toBeNull();
    expect(document.querySelector('.layout-two-columns')).toBeNull();
    expect(document.querySelector('.layout-one-column')).toBeNull();
    expect(document.querySelector('.resume-sidebar')).toBeNull();
    const continuous = await render(full(), { mode: 'continuous' });
    const levels = (root: Document) =>
      [...root.querySelectorAll('h1, h2, h3, h4, h5, h6')]
        .map((heading) => `${heading.tagName}:${heading.textContent?.trim()}`);
    expect(levels(document)).toEqual(levels(continuous));
  });

  it('keeps every visible section in one-column reading order', async () => {
    const document = await render(full());
    const continuous = await render(full(), { mode: 'continuous' });
    const titles = (root: Document) =>
      [...root.querySelectorAll('section.resume-section h2')]
        .map((heading) => heading.textContent?.trim());
    const mainFirst = full();
    mainFirst.customization.layout.columns = 1;
    const oneColumn = await render(mainFirst, { mode: 'continuous' });
    expect(titles(document)).toEqual(titles(oneColumn));
    expect(titles(document).sort()).toEqual(titles(continuous).sort());
  });

  it('marks the article with the owner color scheme', async () => {
    const light = await render(full());
    expect(light.querySelector('article')!.hasAttribute('data-color-scheme'))
      .toBe(false);
    for (const colorScheme of ['dark', 'system'] as const) {
      const document = await render(full(), { colorScheme });
      expect(
        document.querySelector('article')!.getAttribute('data-color-scheme'),
      ).toBe(colorScheme);
    }
  });

  it('leaves continuous and paged output free of sheets', async () => {
    for (const mode of ['continuous', 'paged'] as const) {
      const document = await render(full(), { mode });
      expect(document.querySelector('.resume-sheet')).toBeNull();
      expect(document.querySelector('.resume-folio')).toBeNull();
      expect(document.querySelector('.resume-sheets')).toBeNull();
      expect(document.querySelector('.contact-chip--sheets')).toBeNull();
    }
  });
});

describe('sheet folio', () => {
  it('numbers every sheet n / N and carries the name', async () => {
    const document = await render(full());
    const all = folios(document);
    expect(all.map((folio) => folio.getAttribute('data-folio-page'))).toEqual(
      ['1 / 6', '2 / 6', '3 / 6', '4 / 6', '5 / 6', '6 / 6'],
    );
    for (const folio of all) {
      expect(folio.getAttribute('data-folio-name')).toBe('Ada Lovelace');
      expect(folio.getAttribute('aria-hidden')).toBe('true');
      expect(folio.tagName).toBe('DIV');
      expect(folio.textContent).toBe('');
      expect(folio.children).toHaveLength(0);
    }
  });

  it.each([
    ['an empty name', ''],
    ['no name', undefined],
  ])('omits the name attribute for %s', async (_label, fullName) => {
    const all = folios(await render(named(fullName)));
    expect(all.length).toBeGreaterThan(0);
    for (const folio of all) {
      expect(folio.hasAttribute('data-folio-name')).toBe(false);
      expect(folio.getAttribute('data-folio-page')).toMatch(/^\d+ \/ \d+$/u);
    }
  });

  it.each([
    ['a whitespace-only name', '   '],
    ['separators in the name', 'Ada / Lovelace · 1 / 2 | "x" <b>&amp;'],
    ['a Vietnamese name', 'Nguyễn Thị Ánh'],
  ])('keeps %s exactly', async (_label, fullName) => {
    for (const folio of folios(await render(named(fullName)))) {
      expect(folio.getAttribute('data-folio-name')).toBe(fullName);
    }
  });

  it('uses single spaces around the slash and no other text', async () => {
    const document = await render(full());
    for (const folio of folios(document)) {
      expect(folio.getAttribute('data-folio-page')).toMatch(/^\d \/ 6$/u);
    }
  });
});

describe('sheet 1 with an empty header', () => {
  it('still renders the header sheet and its folio', async () => {
    const document = full();
    delete document.personalDetails.fullName;
    delete document.personalDetails.headline;
    delete document.personalDetails.photo;
    document.personalDetails.details = [];
    document.customization.layout.sections = { main: ['work'], sidebar: [] };
    const parsed = await render(document);
    const sheets = parsed.querySelectorAll('.resume-sheet');
    expect(sheets).toHaveLength(2);
    const header = sheets[0]!.querySelector('header.resume-header');
    expect(header).not.toBeNull();
    expect(header!.textContent?.trim()).toBe('');
    expect(sheets[0]!.querySelector('section')).toBeNull();
    expect(folios(parsed).map((folio) =>
      folio.getAttribute('data-folio-page'))).toEqual(['1 / 2', '2 / 2']);
  });

  it('renders one sheet when the document has no visible section', async () => {
    const document = full();
    document.customization.layout.sections = { main: [], sidebar: [] };
    const parsed = await render(document);
    expect(parsed.querySelectorAll('.resume-sheet')).toHaveLength(1);
    expect(folios(parsed)[0]!.getAttribute('data-folio-page')).toBe('1 / 1');
  });
});

describe('sheets header photo', () => {
  it.each(['left', 'right'] as const)(
    'keeps a %s photo beside the text in the markup',
    async (photoPosition) => {
      const document = full();
      document.customization.header = {
        ...document.customization.header!,
        photoPosition,
      };
      const header = (await render(document))
        .querySelector('header.resume-header')!;
      expect(header.getAttribute('data-photo-position')).toBe(photoPosition);
      expect([...header.children].map((child) => child.className)).toEqual([
        'resume-photo',
        'resume-header-text',
      ]);
    },
  );

  it('keeps a top photo on top', async () => {
    const header = (await render(full()))
      .querySelector('header.resume-header')!;
    expect(header.hasAttribute('data-photo-position')).toBe(false);
    expect(header.firstElementChild?.className).toBe('resume-photo');
  });
});
