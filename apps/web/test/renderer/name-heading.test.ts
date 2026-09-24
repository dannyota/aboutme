// @vitest-environment node

import type { Resume } from '@aboutme/schema';
import { readFileSync } from 'node:fs';
import { JSDOM } from 'jsdom';
import { createSSRApp, h } from 'vue';
import { renderToString } from 'vue/server-renderer';
import { describe, expect, it } from 'vitest';

import ResumeDocument from '../../app/components/resume/ResumeDocument.vue';
import { resolveRenderModel } from
  '../../app/components/resume/resolveRenderModel';

// The renderer always emits exactly one heading for the resume name: an h1
// by default (the sole h1 on a public resume page or the print path) or a
// p when a caller embeds the resume inside a page that owns its own h1
// (the homepage sample and template thumbnails).

const minimal = JSON.parse(
  readFileSync('../../packages/schema/fixtures/minimal.json', 'utf8'),
) as Resume;

const renderDocument = (nameHeading?: 'h1' | 'p'): Promise<string> =>
  renderToString(
    createSSRApp({
      render: () =>
        h(ResumeDocument, {
          document: minimal,
          context: {
            lng: 'en',
            mode: 'continuous',
            ...(nameHeading === undefined ? {} : { nameHeading }),
          },
        }),
    }),
  );

describe('resume name heading', () => {
  it('resolves an absent context field to h1', () => {
    expect(
      resolveRenderModel(minimal, { lng: 'en', mode: 'continuous' })
        .nameHeading,
    ).toBe('h1');
  });

  it('renders an h1 by default, with no p.resume-name', async () => {
    const html = await renderDocument();
    const dom = new JSDOM(html);
    const heading = dom.window.document.querySelector('.resume-name');
    expect(heading?.tagName).toBe('H1');
    expect(dom.window.document.querySelectorAll('h1')).toHaveLength(1);
    expect(dom.window.document.querySelector('p.resume-name')).toBeNull();
  });

  it('renders a p and no h1 when the context asks for one', async () => {
    const html = await renderDocument('p');
    const dom = new JSDOM(html);
    const heading = dom.window.document.querySelector('.resume-name');
    expect(heading?.tagName).toBe('P');
    expect(dom.window.document.querySelectorAll('h1')).toHaveLength(0);
  });

  it('keeps every other attribute the same across the two headings',
    async () => {
      const [defaultHtml, pHtml] = await Promise.all([
        renderDocument(),
        renderDocument('p'),
      ]);
      const defaultTag = new JSDOM(defaultHtml).window.document
        .querySelector('.resume-name')!;
      const pTag = new JSDOM(pHtml).window.document
        .querySelector('.resume-name')!;
      expect(defaultTag.className).toBe(pTag.className);
      expect(defaultTag.textContent).toBe(pTag.textContent);
    });
});
