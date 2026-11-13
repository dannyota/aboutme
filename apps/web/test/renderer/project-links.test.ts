// @vitest-environment node

import type { Resume } from '@aboutme/schema';
import { readFileSync } from 'node:fs';
import { JSDOM } from 'jsdom';
import { createSSRApp, h } from 'vue';
import { renderToString } from 'vue/server-renderer';
import { describe, expect, it } from 'vitest';

import PrintResumeApp from '../../app/components/print/PrintResumeApp.vue';
import ResumeDocument from '../../app/components/resume/ResumeDocument.vue';

const LINKED = '00000000-0000-4000-8000-0000000000a1';
const UNLINKED = '00000000-0000-4000-8000-0000000000a2';
const OWN_LINK = '00000000-0000-4000-8000-0000000000a3';
const HIDDEN = '00000000-0000-4000-8000-0000000000a4';
const WORK = '00000000-0000-4000-8000-0000000000b1';

const page = (slug: string) =>
  `https://aboutme.example/ada-lovelace/projects/${slug}`;

const LINKS: ReadonlyMap<string, string> = new Map([
  [LINKED, page('atlas')],
  [OWN_LINK, page('beacon')],
  [HIDDEN, page('hidden')],
  [WORK, page('work')],
]);

const document = (): Resume => {
  const source = JSON.parse(
    readFileSync('../../packages/schema/fixtures/minimal.json', 'utf8'),
  ) as Resume;
  source.content = {
    projects: {
      sectionType: 'project',
      entries: [
        { id: LINKED, title: 'Atlas' },
        { id: UNLINKED, title: 'Atlas' },
        { id: OWN_LINK, title: 'Beacon', link: 'https://beacon.example/' },
        { id: HIDDEN, title: 'Hidden', isHidden: true },
      ],
    },
    work: {
      sectionType: 'work',
      entries: [{ id: WORK, jobTitle: 'Engineer', employer: 'Acme' }],
    },
  } as unknown as Resume['content'];
  source.customization.layout.sections.main = ['projects', 'work'];
  return source;
};

const parse = (html: string) => new JSDOM(html).window.document;

const renderDocument = async (projectLinks?: ReadonlyMap<string, string>) =>
  parse(await renderToString(createSSRApp({
    render: () => h(ResumeDocument, {
      document: document(),
      context: {
        lng: 'en',
        mode: 'continuous',
        ...(projectLinks === undefined ? {} : { projectLinks }),
      },
    }),
  })));

const titles = (root: Document) =>
  [...root.querySelectorAll('.entry-title')].map((title) => title.innerHTML);

const ANCHOR_STYLE = 'color:var(--color-link);text-decoration:underline;';

describe('project title links', () => {
  it('links a mapped entry title to its project page inside strong',
    async () => {
      const root = await renderDocument(LINKS);
      const anchor = root.querySelector(`a[data-project-entry="${LINKED}"]`);
      expect(anchor?.parentElement?.tagName).toBe('STRONG');
      expect(anchor?.parentElement?.parentElement?.className)
        .toBe('entry-title');
      expect(Object.fromEntries([...anchor!.attributes]
        .map(({ name, value }) => [name, value]))).toEqual({
        'data-project-entry': LINKED,
        'href': page('atlas'),
        'style': ANCHOR_STYLE,
      });
      expect(anchor?.textContent).toBe('Atlas');
    });

  it('keeps an unmapped entry title as plain strong text', async () => {
    const root = await renderDocument(LINKS);
    expect(titles(root)[1]).toBe('<strong>Atlas</strong>');
  });

  it('replaces the entry\'s own link with its project page', async () => {
    const root = await renderDocument(LINKS);
    expect(titles(root)[2]).toBe(
      `<strong><a data-project-entry="${OWN_LINK}" href="${page('beacon')}" `
      + `style="${ANCHOR_STYLE}">Beacon</a></strong>`,
    );
    expect(root.querySelector('a[href="https://beacon.example/"]')).toBeNull();
  });

  it('renders nothing for a hidden entry the map names', async () => {
    const root = await renderDocument(LINKS);
    expect(root.querySelector(`[data-project-entry="${HIDDEN}"]`)).toBeNull();
    expect(root.body.textContent).not.toContain('Hidden');
  });

  it('links only project entries', async () => {
    const root = await renderDocument(LINKS);
    expect(root.querySelectorAll('[data-project-entry]')).toHaveLength(2);
    expect(root.querySelector(`[data-project-entry="${WORK}"]`)).toBeNull();
  });

  it('changes nothing without a map', async () => {
    const root = await renderDocument();
    expect(root.querySelector('[data-project-entry]')).toBeNull();
    expect(titles(root).slice(0, 3)).toEqual([
      '<strong>Atlas</strong>',
      '<strong>Atlas</strong>',
      '<a href="https://beacon.example/" rel="noopener noreferrer" '
      + `style="${ANCHOR_STYLE}">Beacon</a>`,
    ]);
  });

  it('gives the print document the same title links', async () => {
    const source = document();
    const printed = (projectLinks?: ReadonlyMap<string, string>) =>
      renderToString(createSSRApp({
        render: () => h(PrintResumeApp, {
          document: source,
          lng: 'en',
          ...(projectLinks === undefined ? {} : { projectLinks }),
        }),
      }));
    const linked = parse(await printed(LINKS));
    const plain = parse(await printed());
    expect(titles(linked).slice(0, 3)).toEqual(
      titles(await renderDocument(LINKS)).slice(0, 3),
    );
    expect(plain.querySelector('[data-project-entry]')).toBeNull();
    expect(titles(plain).slice(0, 3)).toEqual(
      titles(await renderDocument()).slice(0, 3),
    );
  });
});
