// @vitest-environment happy-dom

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { createSSRApp, defineComponent, h, nextTick } from 'vue';
import { renderToString } from 'vue/server-renderer';
import { afterEach, describe, expect, it, vi } from 'vitest';

import PublicResumeApp from '../../app/components/public/PublicResumeApp.vue';
import { hydratePublicResume } from '../../app/public/public-resume.client';

const resume = (revision: string, pageFormat: 'a4' | 'letter' = 'a4') => {
  const document = JSON.parse(readFileSync(
    resolve(process.cwd(), '../../packages/schema/fixtures/minimal.json'),
    'utf8',
  ));
  document.personalDetails.fullName = `Ada ${revision}`;
  document.content = {
    profile: {
      sectionType: 'profile',
      entries: [{ id: '00000000-0000-4000-8000-000000000001' }],
    },
  };
  document.customization.layout.sections.main = ['profile'];
  document.customization.pageFormat = pageFormat;
  return { slug: 'ada1', revision, lng: 'en', downloadEnabled: false,
    document };
};

const ssr = (revision: string, pages?: unknown) => renderToString(
  createSSRApp({
    render: () => h(PublicResumeApp, {
      publicResume: resume(revision),
      homeHref: `${window.location.origin}/`,
      ...(pages === undefined ? {} : { pages }),
    }),
  }),
);

const originalFonts = Object.getOwnPropertyDescriptor(document, 'fonts');

const holdFonts = (): void => {
  const fontEvents = new EventTarget();
  Object.assign(fontEvents, {
    load: vi.fn(() => new Promise(() => undefined)),
    ready: new Promise(() => undefined),
  });
  Object.defineProperty(document, 'fonts', {
    configurable: true,
    value: fontEvents,
  });
};

afterEach(() => {
  vi.restoreAllMocks();
  document.body.replaceChildren();
  document.head.replaceChildren();
  if (originalFonts === undefined) {
    Reflect.deleteProperty(document, 'fonts');
  } else {
    Object.defineProperty(document, 'fonts', originalFonts);
  }
});

describe('public pages after hydration', () => {
  it('adds nothing to the server HTML when given a pages component',
    async () => {
      const Probe = defineComponent({ render: () => h('div', 'probe') });
      expect(await ssr('1', Probe)).toBe(await ssr('1'));
      expect(await ssr('1', Probe)).not.toContain('probe');
    });

  it('mounts the pages after hydration, after the continuous article',
    async () => {
      holdFonts();
      const warn = vi.spyOn(console, 'warn');
      document.body.innerHTML = [
        '<main id="public-resume" data-revision="1">',
        await ssr('1'),
        '</main>',
      ].join('');
      const root = document.querySelector<HTMLElement>('#public-resume')!;
      const article = root.querySelector('.public-resume-page > article');
      await hydratePublicResume(root, 'ada1', '1', async () => resume('1'));
      await nextTick();
      const page = root.querySelector<HTMLElement>('.public-resume-page')!;
      expect(page.querySelector(':scope > article')).toBe(article);
      expect([...page.children].map((child) => child.className)).toEqual([
        'public-toolbar', 'resume-document', 'public-pages',
      ]);
      expect(page.getAttribute('data-pages')).toBe('loading');
      expect(page.style.getPropertyValue('--public-page-width'))
        .toBe('794px');
      expect(warn.mock.calls.flat().join('\n')).not.toContain('Hydration');
    });

  it('mounts the pages on the replace path with the page width',
    async () => {
      holdFonts();
      document.body.innerHTML = [
        '<main id="public-resume" data-revision="1">',
        '<h1>Ada</h1></main>',
      ].join('');
      const root = document.querySelector<HTMLElement>('#public-resume')!;
      await hydratePublicResume(
        root, 'ada1', '1', async () => resume('2', 'letter'),
      );
      await nextTick();
      const page = root.querySelector<HTMLElement>('.public-resume-page')!;
      expect(page.querySelector(':scope > .public-pages')).not.toBeNull();
      expect(page.style.getPropertyValue('--public-page-width'))
        .toBe('816px');
      expect(page.querySelector('.resume-name')?.textContent).toBe('Ada 2');
    });
});
