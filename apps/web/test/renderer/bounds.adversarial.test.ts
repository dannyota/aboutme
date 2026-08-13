// @vitest-environment node

import type { Resume } from '@aboutme/schema';
import { createSSRApp, h } from 'vue';
import { renderToString } from 'vue/server-renderer';
import { describe, expect, it } from 'vitest';

import ResumeDocument from '../../app/components/resume/ResumeDocument.vue';

const customization: Resume['customization'] = {
  font: { family: 'inter', baseSizePx: 10 },
  colors: {
    primary: '#111111',
    text: '#222222',
    background: '#ffffff',
  },
  spacing: { sectionGap: 0, entryGap: 0, lineHeight: 1 },
  heading: { style: 'normal', showRule: false },
  layout: {
    columns: 1,
    sections: {
      main: Array.from({ length: 24 }, (_, index) => `s${index}`),
      sidebar: [],
    },
  },
  sectionDisplay: {
    skill: { style: 'text' },
    language: { style: 'text' },
  },
  pageFormat: 'letter',
  dateFormat: 'YYYY',
};

describe('renderer bounds', () => {
  it('renders maximum section and entry counts in order', async () => {
    const document: Resume = {
      schemaVersion: 2,
      personalDetails: {},
      content: Object.fromEntries(
        Array.from({ length: 24 }, (_, sectionIndex) => [
          `s${sectionIndex}`,
          {
            sectionType: 'language',
            displayName: `Section ${sectionIndex}`,
            entries: Array.from({ length: 64 }, (_, entryIndex) => ({
              id: `${sectionIndex}-${entryIndex}`,
              name: `S${sectionIndex}E${entryIndex}`,
            })),
          },
        ]),
      ),
      customization,
    };
    const html = await renderToString(
      createSSRApp({
        render: () =>
          h(ResumeDocument, {
            document,
            context: { lng: 'und', mode: 'continuous' },
          }),
      }),
    );
    expect(html.indexOf('S0E0')).toBeLessThan(html.indexOf('S23E63'));
    expect((html.match(/class="entry"/g) ?? []).length).toBe(24 * 64);
  });

  it('renders a 16 KiB already-sanitized rich-text field', async () => {
    const text = 'x'.repeat(16 * 1024);
    const document: Resume = {
      schemaVersion: 2,
      personalDetails: {},
      content: {
        profile: {
          sectionType: 'profile',
          entries: [{ id: 'entry', text: `<p>${text}</p>` }],
        },
      },
      customization: {
        ...customization,
        layout: {
          ...customization.layout,
          sections: { main: ['profile'], sidebar: [] },
        },
      },
    };
    const html = await renderToString(
      createSSRApp({
        render: () =>
          h(ResumeDocument, {
            document,
            context: { lng: 'en', mode: 'continuous' },
          }),
      }),
    );
    expect(html).toContain(text);
  });

  it(
    'renders a near-512 KiB valid aggregate without losing content',
    async () => {
      const text = 'x'.repeat(16 * 1024);
      const filler = 'y'.repeat(4_050);
      const document: Resume = {
        schemaVersion: 2,
        personalDetails: { fullName: 'Ada Lovelace' },
        content: {
          profile: {
            sectionType: 'profile',
            entries: Array.from({ length: 32 }, (_, index) => ({
              id: `00000000-0000-4000-8000-${String(index).padStart(12, '0')}`,
              text: index === 31 ? filler : text,
            })),
          },
        },
        customization: {
          ...customization,
          layout: {
            ...customization.layout,
            sections: { main: ['profile'], sidebar: [] },
          },
        },
      };
      const inputBytes = Buffer.byteLength(JSON.stringify(document));
      expect(inputBytes).toBeGreaterThan(500 * 1024);
      expect(inputBytes).toBeLessThanOrEqual(512 * 1024);

      const startedAt = performance.now();
      const html = await renderToString(
        createSSRApp({
          render: () =>
            h(ResumeDocument, {
              document,
              context: { lng: 'und', mode: 'continuous' },
            }),
        }),
      );
      const wallMs = performance.now() - startedAt;
      const outputBytes = Buffer.byteLength(html);
      console.log(
        'near-512 KiB aggregate: '
        + `input=${inputBytes}B output=${outputBytes}B `
        + `wall=${wallMs.toFixed(1)}ms`,
      );

      expect(html.split(text).length - 1).toBe(31);
      expect(html).toContain(filler);
      expect(outputBytes).toBeGreaterThan(500 * 1024);
      expect(html).not.toContain('<img');
      expect(html).not.toContain('<script');
      expect(html).not.toContain('onerror');
    });
});
