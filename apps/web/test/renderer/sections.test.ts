// @vitest-environment node

import type { Resume, Section } from '@aboutme/schema';
import { readFileSync } from 'node:fs';
import { JSDOM } from 'jsdom';
import { createSSRApp, h } from 'vue';
import { renderToString } from 'vue/server-renderer';
import { describe, expect, it } from 'vitest';

import ResumeDocument from '../../app/components/resume/ResumeDocument.vue';
import {
  renderPageRule,
  useResumeStyles,
} from '../../app/components/resume/useResumeStyles';

const minimal = JSON.parse(
  readFileSync('../../packages/schema/fixtures/minimal.json', 'utf8'),
) as Resume;

const fixture = (name: string): Resume =>
  JSON.parse(
    readFileSync(`../../packages/schema/fixtures/${name}.json`, 'utf8'),
  ) as Resume;

const renderDocument = (document: Resume, lng = 'en'): Promise<string> =>
  renderToString(
    createSSRApp({
      render: () =>
        h(ResumeDocument, {
          document,
          context: {
            lng,
            mode: 'continuous',
            ...(document.personalDetails.photo === undefined
              ? {}
              : { photoUrl: 'data:image/png;base64,AA==' }),
          },
        }),
    }),
  );

const customization: Resume['customization'] = {
  font: { family: 'inter', baseSizePx: 14 },
  colors: {
    primary: '#111111',
    text: '#222222',
    background: '#ffffff',
  },
  spacing: { sectionGap: 16, entryGap: 8, lineHeight: 1.4 },
  heading: { style: 'normal', showRule: false },
  layout: { columns: 1, sections: { main: [], sidebar: [] } },
  sectionDisplay: {
    skill: { style: 'text' },
    language: { style: 'text' },
  },
  pageFormat: 'a4',
  dateFormat: 'MM/YYYY',
};

describe('pure resume renderer', () => {
  it('renders through plain Vue SSR with no DOM', async () => {
    const html = await renderToString(
      createSSRApp({
        render: () =>
          h(ResumeDocument, {
            document: minimal,
            context: { lng: 'en', mode: 'continuous' },
          }),
      }),
    );
    expect(html).toContain('Ada Lovelace');
    expect(html).toContain('class="resume-document"');
    expect(html).not.toContain('@page');
  });

  it('renders the skill level widget inside the entry meta slot', async () => {
    const document: Resume = {
      schemaVersion: 2,
      personalDetails: { fullName: 'Ada Lovelace' },
      content: {
        skill: {
          sectionType: 'skill',
          displayName: 'Skills',
          entries: [
            {
              id: '00000000-0000-4000-8000-000000000001',
              name: 'Go',
              level: 5,
            },
          ],
        },
      },
      customization: {
        ...customization,
        layout: {
          ...customization.layout,
          sections: { main: ['skill'], sidebar: [] },
        },
        sectionDisplay: {
          ...customization.sectionDisplay,
          skill: { style: 'bar' },
        },
      },
    };
    const html = await renderDocument(document);
    const dom = new JSDOM(html);
    expect(
      dom.window.document.querySelector('.entry-meta .level-widget'),
    ).not.toBeNull();
    expect(
      dom.window.document.querySelector('.entry > .level-widget'),
    ).toBeNull();
  });

  it(
    'renders no entry meta for a text-style or absent skill level',
    async () => {
      const document: Resume = {
        schemaVersion: 2,
        personalDetails: { fullName: 'Ada Lovelace' },
        content: {
          skill: {
            sectionType: 'skill',
            displayName: 'Skills',
            entries: [
              {
                id: '00000000-0000-4000-8000-000000000001',
                name: 'Go',
                level: 5,
              },
              {
                id: '00000000-0000-4000-8000-000000000002',
                name: 'Drafting',
              },
            ],
          },
        },
        customization: {
          ...customization,
          layout: {
            ...customization.layout,
            sections: { main: ['skill'], sidebar: [] },
          },
          sectionDisplay: {
            ...customization.sectionDisplay,
            skill: { style: 'text' },
          },
        },
      };
      const html = await renderDocument(document);
      const dom = new JSDOM(html);
      expect(
        dom.window.document.querySelectorAll('.level-widget'),
      ).toHaveLength(0);
      expect(
        dom.window.document.querySelectorAll('.entry-meta'),
      ).toHaveLength(0);
    });

  it(
    'omits a cleared empty section entirely with no placeholder text',
    async () => {
      const html = await renderDocument(
        fixture('draft-cleared-name-empty-section'),
      );
      const dom = new JSDOM(html);
      expect(dom.window.document.querySelector('.resume-section')).toBeNull();
      expect(dom.window.document.querySelector('.section-heading')).toBeNull();
      expect(dom.window.document.querySelector('.resume-name')).toBeNull();
      expect(dom.window.document.body.textContent).not.toContain('Untitled');
      expect(
        dom.window.document.body.textContent,
      ).not.toContain('Company Name');
    });

  it(
    'renders a draft-partial entry without placeholder separators',
    async () => {
      const html = await renderDocument(fixture('draft-partial'));
      const dom = new JSDOM(html);
      const entryHeader = dom.window.document.querySelector('.entry-header');
      expect(entryHeader).not.toBeNull();
      expect(entryHeader?.textContent).toContain('Engineer');
      expect(entryHeader?.textContent).not.toContain(' · ');
      expect(entryHeader?.textContent).not.toContain(' – ');
      expect(
        dom.window.document.body.textContent,
      ).not.toContain('Company Name');
    });

  it.each([undefined, ''])(
    'renders no heading text or substitute when displayName is %s',
    async (displayName) => {
      const document: Resume = {
        schemaVersion: 2,
        personalDetails: { fullName: 'Ada Lovelace' },
        content: {
          work: {
            sectionType: 'work',
            ...(displayName === undefined ? {} : { displayName }),
            entries: [
              {
                id: '00000000-0000-4000-8000-000000000001',
                jobTitle: 'Engineer',
              },
            ],
          },
        },
        customization: {
          ...customization,
          layout: {
            ...customization.layout,
            sections: { main: ['work'], sidebar: [] },
          },
        },
      };
      const html = await renderDocument(document);
      const heading = new JSDOM(html).window.document.querySelector(
        '.section-heading',
      );
      expect(heading).not.toBeNull();
      expect(heading?.querySelector('h2')).toBeNull();
      expect(heading?.textContent).not.toContain('work');
    },
  );

  it('omits hidden entries and hidden details from the DOM', async () => {
    const document: Resume = {
      schemaVersion: 2,
      personalDetails: {
        fullName: 'Ada Lovelace',
        details: [
          {
            id: '00000000-0000-4000-8000-000000000010',
            type: 'custom',
            label: 'Secret',
            value: 'hidden detail',
            isHidden: true,
          },
        ],
      },
      content: {
        work: {
          sectionType: 'work',
          entries: [
            {
              id: '00000000-0000-4000-8000-000000000011',
              jobTitle: 'Visible Engineer',
            },
            {
              id: '00000000-0000-4000-8000-000000000012',
              jobTitle: 'Secret Job',
              isHidden: true,
            },
          ],
        },
      },
      customization: {
        ...customization,
        layout: {
          ...customization.layout,
          sections: { main: ['work'], sidebar: [] },
        },
      },
    };
    const html = await renderDocument(document);
    expect(html).toContain('Visible Engineer');
    expect(html).not.toContain('Secret Job');
    expect(html).not.toContain('hidden detail');
  });

  it('emits meta separators only between two present values', async () => {
    const document: Resume = {
      schemaVersion: 2,
      personalDetails: { fullName: 'Ada Lovelace' },
      content: {
        work: {
          sectionType: 'work',
          entries: [
            {
              id: '00000000-0000-4000-8000-000000000013',
              jobTitle: 'One Place',
              city: 'Hanoi',
            },
            {
              id: '00000000-0000-4000-8000-000000000014',
              jobTitle: 'Two Places',
              city: 'Hanoi',
              country: 'Vietnam',
            },
          ],
        },
      },
      customization: {
        ...customization,
        layout: {
          ...customization.layout,
          sections: { main: ['work'], sidebar: [] },
        },
      },
    };
    const renderedDocument = new JSDOM(
      await renderDocument(document),
    ).window.document;
    const headers = [...renderedDocument.querySelectorAll('.entry-header')];
    expect(headers[0]?.textContent).not.toContain(' · ');
    expect(headers[1]?.textContent).toContain('Hanoi · Vietnam');
  });

  it(
    'emits the six print class names, lang, and print-color-adjust on the root',
    async () => {
      const html = await renderDocument(fixture('full'), 'vi');
      const dom = new JSDOM(html);
      for (const className of [
        'resume-header',
        'resume-section',
        'section-heading',
        'entry',
        'entry-header',
        'entry-body',
      ]) {
        expect(
          dom.window.document.querySelector(`.${className}`),
        ).not.toBeNull();
      }
      const root = dom.window.document.querySelector('.resume-document');
      expect(root?.getAttribute('lang')).toBe('vi');
      expect(root?.getAttribute('style')).toMatch(
        /print-color-adjust:\s*exact/,
      );
      expect(html).not.toContain('@page');
    });

  it(
    'keeps section heading and first entry siblings with both avoid rules',
    async () => {
      const html = await renderDocument(fixture('full'), 'vi');
      const section = [
        ...new JSDOM(html).window.document.querySelectorAll('.resume-section'),
      ].find((candidate) => candidate.querySelector('.entry-header'));
      const heading = section?.querySelector(':scope > .section-heading');
      const firstEntry = section?.querySelector(':scope > .entry');
      expect(heading).not.toBeNull();
      expect(firstEntry).not.toBeNull();
      expect(firstEntry?.previousElementSibling).toBe(heading);
      expect(heading?.parentElement).toBe(section);
      expect(firstEntry?.parentElement).toBe(section);

      const source = readFileSync(
        'app/components/resume/ResumeDocument.vue',
        'utf8',
      );
      expect(source).toMatch(
        /\.section-heading[^{]*\{[^}]*break-after:\s*avoid/s,
      );
      expect(source).toMatch(/\.entry-header[^{]*\{[^}]*break-after:\s*avoid/s);
      expect(source.match(/break-after:\s*avoid/g)).toHaveLength(2);
    });

  it(
    'renders exact A4 and Letter page rules with default and explicit margins',
    () => {
      expect(
        renderPageRule(
          useResumeStyles(fixture('draft-partial').customization).page,
        ),
      ).toBe('@page {\n  size: 210mm 297mm;\n  margin: 15mm 15mm;\n}');
      expect(
        renderPageRule(useResumeStyles(fixture('full').customization).page),
      ).toBe('@page {\n  size: 8.5in 11in;\n  margin: 12mm 18mm;\n}');
    });

  it('removes screen padding only on an explicit print surface', () => {
    const source = readFileSync(
      'app/components/resume/ResumeDocument.vue',
      'utf8',
    );
    const printReset = new RegExp([
      '@media\\s+print\\s*\\{[\\s\\S]*body\\.resume-print\\s*\\{',
      '[^}]*margin:\\s*0;[^}]*padding:\\s*0;[^}]*\\}[\\s\\S]*',
      'body\\.resume-print\\s+\\.resume-document\\s*\\{',
      '[^}]*padding:\\s*0;[^}]*\\}',
    ].join(''));
    expect(source).toMatch(printReset);
    expect(source).toMatch(new RegExp([
      '\\.resume-document\\s*\\{[^}]*padding:\\s*',
      'var\\(--page-margin-y\\)\\s+var\\(--page-margin-x\\);',
    ].join(''), 's'));
  });

  it(
    'dispatches every supported section type and rejects an unknown one',
    async () => {
      const html = await renderDocument(fixture('full'), 'vi');
      expect(
        new JSDOM(html).window.document.querySelectorAll('.resume-section'),
      ).toHaveLength(8);

      const unknown = {
        sectionType: 'unknown',
        entries: [],
      } as unknown as Section;
      const document: Resume = {
        schemaVersion: 2,
        personalDetails: { fullName: 'Ada Lovelace' },
        content: { bad: unknown },
        customization: {
          ...customization,
          layout: {
            ...customization.layout,
            sections: { main: ['bad'], sidebar: [] },
          },
        },
      };
      await expect(renderDocument(document)).rejects.toThrow(
        'Unsupported section type',
      );
    });
});
