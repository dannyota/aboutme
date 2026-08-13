// @vitest-environment node

import type { Resume } from '@aboutme/schema';
import { readFileSync } from 'node:fs';
import { createSSRApp, h } from 'vue';
import { renderToString } from 'vue/server-renderer';
import { describe, expect, it, vi } from 'vitest';

import { PaginationMeasureKey } from '../../app/components/resume/measure';
import { PaginationError } from '../../app/components/resume/paginate';
import ResumeDocument from '../../app/components/resume/ResumeDocument.vue';
import { syntheticMeasure } from './synthetic-measure';

const fixture = (name: string): Resume => JSON.parse(
  readFileSync(`../../packages/schema/fixtures/${name}.json`, 'utf8'),
) as Resume;

const renderPaged = (
  document: Resume,
  measure = syntheticMeasure,
): Promise<string> => {
  const context = {
    lng: 'en',
    mode: 'paged',
    ...(document.personalDetails.photo === undefined
      ? {}
      : { photoUrl: 'data:image/png;base64,AA==' }),
  } as const;
  const app = createSSRApp({
    render: () => h(ResumeDocument, { document, context }),
  });
  app.provide(PaginationMeasureKey, measure);
  return renderToString(app);
};

describe('paged resume SSR', () => {
  it('awaits the provider and preserves the original context', async () => {
    const document = fixture('full');
    let release!: () => void;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    const measure = vi.fn(async (request) => {
      await gate;
      return syntheticMeasure(request);
    });
    let finished = false;
    const pending = renderPaged(document, measure).then((html) => {
      finished = true;
      return html;
    });
    await Promise.resolve();
    expect(measure).toHaveBeenCalledOnce();
    expect(finished).toBe(false);
    expect(globalThis).not.toHaveProperty('document');

    release();
    const html = await pending;
    expect(measure.mock.calls[0]?.[0].context).toEqual({
      lng: 'en',
      mode: 'paged',
      photoUrl: 'data:image/png;base64,AA==',
    });
    expect(html).toContain('data-pagination-settled="true"');
    expect(html).toContain('data-render-mode="paged"');
    expect(html).toContain('Ada Lovelace');
    expect(html).toContain('Analytical Engine');
    expect(html).not.toContain('Junior Engineer');
    expect(html.match(/data-page-index=/g)).toHaveLength(1);
    const visibleEntries = Object.values(document.content).reduce(
      (total, section) =>
        total + section.entries.filter((entry) => !entry.isHidden).length,
      0,
    );
    expect(html.match(/data-block-kind="entry"/g)).toHaveLength(visibleEntries);
    expect(html).not.toContain('@page');
  });

  it('fails with the typed code when SSR has no provider', async () => {
    const document = fixture('minimal');
    const app = createSSRApp({
      render: () => h(ResumeDocument, {
        document,
        context: { lng: 'en', mode: 'paged' },
      }),
    });

    await expect(renderToString(app)).rejects.toMatchObject({
      name: 'PaginationError',
      code: 'pagination_measurement_required',
    });
  });

  it('uses the selected A4 and Letter paper geometry', async () => {
    const a4 = fixture('full');
    a4.customization.pageFormat = 'a4';
    const letter = structuredClone(a4);
    letter.customization.pageFormat = 'letter';

    expect(await renderPaged(a4)).toContain('width:794px;min-height:1123px');
    expect(await renderPaged(letter)).toContain(
      'width:816px;min-height:1056px',
    );
  });

  it('normalizes one column as main then sidebar', async () => {
    const document = fixture('full');
    document.customization.layout.columns = 1;
    const html = await renderPaged(document);

    expect(html).toContain('Skills');
    expect(html).toContain('Languages');
    expect(html).not.toContain('resume-sidebar');
    expect(html.indexOf('Experience')).toBeLessThan(html.indexOf('Skills'));
  });

  it('keeps a titleless heading structural without adding text', async () => {
    const document = fixture('draft-partial');
    delete document.content.work!.displayName;
    delete document.content.work!.iconKey;
    document.customization.heading.showRule = false;
    const html = await renderPaged(document);

    expect(html).toContain('class="section-heading"');
    expect(html).not.toContain('<h2');
    expect(html).not.toContain('briefcase');
    expect(html).toContain('Engineer');
  });

  it('expands and visibly marks an oversized entry', async () => {
    const document = fixture('full');
    const html = await renderPaged(document, (request) => {
      const layout = syntheticMeasure(request);
      const entry = layout.blocks.find((block) => block.kind === 'entry');
      expect(entry).toBeDefined();
      entry!.heightPx = 2_000;
      return layout;
    });

    expect(html).toContain('data-page-overflow="true"');
    expect(html).toContain('data-block-overflow="true"');
    expect(html).toMatch(/min-height:1056px;height:2\d{3}(?:\.\d+)?px/);
  });

  it('uses the typed pagination error class', () => {
    expect(new PaginationError('invalid_measurement', 'bad')).toBeInstanceOf(
      PaginationError,
    );
  });
});
