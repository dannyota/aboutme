// @vitest-environment node

import type { Section } from '@aboutme/schema';
import { compileStyle, parse } from '@vue/compiler-sfc';
import { readFileSync } from 'node:fs';
import { createSSRApp, h } from 'vue';
import { renderToString } from 'vue/server-renderer';
import { describe, expect, it } from 'vitest';

import SectionRenderer from '../../app/components/resume/SectionRenderer.vue';

const section: Extract<Section, { sectionType: 'work' }> = {
  sectionType: 'work',
  displayName: 'Work',
  entries: [
    {
      id: '00000000-0000-4000-8000-000000000001',
      jobTitle: 'First role',
      employer: 'First employer',
    },
    {
      id: '00000000-0000-4000-8000-000000000002',
      jobTitle: 'Second role',
      employer: 'Second employer',
    },
  ],
};

const renderPart = (part: 'all' | 'heading' | 'entry', value = section):
Promise<string> => renderToString(createSSRApp({
  render: () => h(SectionRenderer, {
    section: value,
    dateFormat: 'MM/YYYY',
    sectionDisplay: {
      skill: { style: 'text' },
      language: { style: 'text' },
    },
    renderPart: part,
  }),
}));

describe('section pagination parts', () => {
  it('compiles shared renderer styles as matching global descendants', () => {
    const filename = 'app/components/resume/ResumeDocument.vue';
    const source = readFileSync(filename, 'utf8');
    const { descriptor } = parse(source, { filename });
    const style = descriptor.styles[0];
    expect(style?.scoped).not.toBe(true);

    const compiled = compileStyle({
      source: style?.content ?? '',
      filename,
      id: 'resume-document',
      scoped: false,
    });
    expect(compiled.errors).toEqual([]);
    expect(compiled.code).toContain('.resume-document .resume-header');
    expect(compiled.code).not.toContain(':deep(');
  });

  it('gives pagination gap resets higher specificity than base margins', () => {
    const source = readFileSync(
      'app/components/resume/PagedResume.vue',
      'utf8',
    );
    const { descriptor } = parse(source, { filename: 'PagedResume.vue' });
    const compiled = compileStyle({
      source: descriptor.styles[0]?.content ?? '',
      filename: 'PagedResume.vue',
      id: 'paged-resume',
      scoped: false,
    });

    expect(compiled.errors).toEqual([]);
    expect(compiled.code).toContain(
      '.resume-document .pagination-atomic .resume-section',
    );
    expect(compiled.code).toContain(
      '.resume-document.resume-page .resume-header',
    );
    expect(compiled.code).toContain(
      '.resume-document .pagination-atomic .section-heading:empty',
    );
    expect(compiled.code).toContain(
      '.resume-page[data-page-overflow=\'true\']',
    );
    expect(compiled.code).toContain(
      '.pagination-atomic[data-block-overflow=\'true\']',
    );
  });

  it('renders a heading without retaining entry content', async () => {
    const html = await renderPart('heading');
    expect(html).toContain('Work');
    expect(html).not.toContain('First role');
    expect(html).not.toContain('Second role');
  });

  it('renders one entry without retaining heading or siblings', async () => {
    const oneEntry = structuredClone(section);
    oneEntry.entries.splice(1);
    const html = await renderPart('entry', oneEntry);
    expect(html).not.toContain('Work');
    expect(html).toContain('First role');
    expect(html).not.toContain('Second role');
  });

  it('keeps the continuous all-part behavior as the default', async () => {
    const html = await renderPart('all');
    expect(html).toContain('Work');
    expect(html).toContain('First role');
    expect(html).toContain('Second role');
  });
});
