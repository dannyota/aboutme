// @vitest-environment node

// A long entry breaks between its body blocks, as the PDF does
// (docs/design/templates/print.md §3), instead of moving whole to the next
// page and leaving the previous one half empty.

import type { Section } from '@aboutme/schema';
import { createSSRApp, h } from 'vue';
import { renderToString } from 'vue/server-renderer';
import { describe, expect, it } from 'vitest';

import SectionRenderer from '../../app/components/resume/SectionRenderer.vue';
import {
  entryBlocks,
  entryPartSection,
  splitRichTextBlocks,
} from '../../app/components/resume/entryParts';
import {
  type MeasuredBlock,
  paginate,
} from '../../app/components/resume/paginate';

const split = splitRichTextBlocks;

describe('splitRichTextBlocks', () => {
  it('splits top-level paragraphs and unordered list items', () => {
    expect(split(
      '<p>One <strong>b</strong></p><p>Two</p>'
      + '<ul><li>A</li><li>B <em>b</em></li></ul>',
    )).toEqual([
      '<p>One <strong>b</strong></p>',
      '<p>Two</p>',
      '<ul><li>A</li></ul>',
      '<ul><li>B <em>b</em></li></ul>',
    ]);
  });

  it('keeps an ordered list whole so its numbering stays intact', () => {
    expect(split('<p>Intro</p><ol><li>1</li><li>2</li></ol>'))
      .toEqual(['<p>Intro</p>', '<ol><li>1</li><li>2</li></ol>']);
  });

  it('keeps a nested list inside its parent item', () => {
    expect(split('<ul><li>A<ul><li>a1</li></ul></li><li>B</li></ul>'))
      .toEqual([
        '<ul><li>A<ul><li>a1</li></ul></li></ul>',
        '<ul><li>B</li></ul>',
      ]);
  });

  it('leaves a single block, bare text, or unbalanced markup whole', () => {
    for (const html of ['<p>Only</p>', 'Bare text', '<p>Open<p>Two', '']) {
      expect(split(html)).toEqual([html]);
    }
  });

  it('never drops text that sits between blocks', () => {
    for (const html of [
      '<p>A</p>between<p>B</p>',
      '<p>A</p><br><p>B</p>',
      '<p>A</p><p>B</p>tail',
      '<ul><li>A</li>loose<li>B</li></ul>',
    ]) {
      expect(split(html)).toEqual([html]);
    }
  });
});

const id = (last: number): string =>
  `00000000-0000-4000-8000-00000000000${last}`;

const work: Extract<Section, { sectionType: 'work' }> = {
  sectionType: 'work',
  displayName: 'Work',
  entries: [
    { id: id(1), jobTitle: 'Short', description: '<p>One line.</p>' },
    {
      id: id(2),
      jobTitle: 'Long',
      description: '<p>P1</p><p>P2</p><ul><li>B1</li><li>B2</li></ul>',
    },
  ],
};

describe('entryBlocks and entryPartSection', () => {
  it('gives a multi-block entry one part per body block', () => {
    expect(entryBlocks('work', work, 0, 'main')).toEqual([
      { sectionKey: 'work', kind: 'entry', entryIndex: 0, column: 'main' },
    ]);
    const parts = entryBlocks('work', work, 1, 'main');
    expect(parts.map((block) => block.part)).toEqual([0, 1, 2, 3]);
  });

  it('slices each part to its own body block', () => {
    const first = entryPartSection(work, 1, 0);
    expect(first.entries).toHaveLength(1);
    expect(first.entries[0])
      .toMatchObject({ jobTitle: 'Long', description: '<p>P1</p>' });
    expect(entryPartSection(work, 1, 3).entries[0])
      .toMatchObject({ description: '<ul><li>B2</li></ul>' });
  });

  it('never splits skills or languages', () => {
    const skill: Section = {
      sectionType: 'skill',
      displayName: 'Skills',
      entries: [{ id: id(3), name: 'Go', infoHtml: '<p>a</p><p>b</p>' }],
    };
    expect(entryBlocks('skill', skill, 0, 'main')).toHaveLength(1);
  });
});

describe('paginating a long entry', () => {
  const block = (
    kind: 'heading' | 'entry',
    heightPx: number,
    gapBeforePx: number,
    entryIndex?: number,
    part?: number,
  ): MeasuredBlock => ({
    sectionKey: 'work',
    kind,
    column: 'main',
    heightPx,
    gapBeforePx,
    ...(entryIndex === undefined ? {} : { entryIndex }),
    ...(part === undefined ? {} : { part }),
  });

  it('fills page one before breaking between body blocks', () => {
    // Page content 1000px; the 100px header and 20px gap leave 880px.
    const blocks = [
      block('heading', 30, 20),
      block('entry', 100, 8, 0),
      // The long entry: its header with the first block, then 8 blocks.
      block('entry', 150, 8, 1, 0),
      ...Array.from({ length: 8 }, (_, index) =>
        block('entry', 100, 0, 1, index + 1)),
    ];
    const pages = paginate(blocks, 1, 1000, 100, 20);
    expect(pages).toHaveLength(2);
    const first = pages[0]!.main.filter((placed) => placed.entryIndex === 1);
    // 30 + 8 + 100 + 8 + 150 = 296px used, so five 100px blocks still fit.
    expect(first.map((placed) => placed.part)).toEqual([0, 1, 2, 3, 4, 5]);
    expect(pages[1]!.main.map((placed) => placed.part)).toEqual([6, 7, 8]);
    expect(pages.every((page) => !page.overflow)).toBe(true);
  });

  it('keeps a section heading with the entry header and first block', () => {
    const blocks = [
      block('entry', 850, 0, 0),
      block('heading', 30, 20),
      block('entry', 150, 8, 1, 0),
      block('entry', 100, 0, 1, 1),
    ];
    const pages = paginate(blocks, 1, 1000, 100, 20);
    expect(pages[0]!.main.map((placed) => placed.kind)).toEqual(['entry']);
    expect(pages[1]!.main.map((placed) => placed.kind))
      .toEqual(['heading', 'entry', 'entry']);
  });
});

describe('rendering entry parts', () => {
  const render = (section: Section, renderPart: 'entry' | 'continuation') =>
    renderToString(createSSRApp({
      render: () => h(SectionRenderer, {
        section,
        dateFormat: 'MM/YYYY',
        sectionDisplay: {
          skill: { style: 'text' },
          language: { style: 'text' },
        },
        renderPart,
      }),
    }));

  it('renders the header only with the first part', async () => {
    const first = await render(entryPartSection(work, 1, 0), 'entry');
    expect(first).toContain('entry-header');
    expect(first).toContain('P1');
    expect(first).not.toContain('P2');

    const later = await render(entryPartSection(work, 1, 2), 'continuation');
    expect(later).not.toContain('entry-header');
    expect(later).not.toContain('section-heading');
    expect(later).toContain('B1');
    expect(later).not.toContain('Long');
  });
});
