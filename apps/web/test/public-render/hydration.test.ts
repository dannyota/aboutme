// @vitest-environment happy-dom

import { describe, expect, it, vi } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { createSSRApp, h } from 'vue';
import { renderToString } from 'vue/server-renderer';

import { hydratePublicResume } from '../../app/public/public-resume.client';
import PublicResumeApp from '../../app/components/public/PublicResumeApp.vue';

const resume = (revision: string) => ({
  slug: 'ada1',
  revision,
  lng: 'en',
  downloadEnabled: false,
  document: (() => {
    const document = JSON.parse(
      readFileSync(
        resolve(process.cwd(), '../../packages/schema/fixtures/minimal.json'),
        'utf8',
      ),
    );
    document.personalDetails.fullName
      = revision === '1' ? 'Ada Lovelace' : 'Grace Hopper';
    document.content = {
      profile: {
        sectionType: 'profile',
        entries: [{ id: '00000000-0000-4000-8000-000000000001' }],
      },
    };
    document.customization.layout.sections.main = ['profile'];
    return document;
  })(),
});

const ssrRoot = async (revision: string): Promise<string> =>
  renderToString(
    createSSRApp({
      render: () => h(PublicResumeApp, { publicResume: resume(revision) }),
    }),
  );

describe('public resume hydration', () => {
  it('hydrates matching SSR revision without replacing the root', async () => {
    document.body.innerHTML = [
      '<main id="public-resume" data-revision="1">',
      await ssrRoot('1'),
      '</main>',
    ].join('');
    const root = document.querySelector<HTMLElement>('#public-resume')!;
    const original = root.firstElementChild;
    await hydratePublicResume(root, 'ada1', '1', async () => resume('1'));
    expect(root.firstElementChild).toBe(original);
  });

  it('restores matching SSR when hydration mounting throws', async () => {
    document.body.innerHTML = [
      '<main id="public-resume" data-revision="1"><h1>Ada Lovelace</h1>',
      '<a href="/x">Link</a></main>',
    ].join('');
    const root = document.querySelector<HTMLElement>('#public-resume')!;
    const original = root.innerHTML;
    await hydratePublicResume(
      root,
      'ada1',
      '1',
      async () => resume('1'),
      {
        hydrate: () => { throw new Error('mount'); },
        replace: () => undefined,
      },
    );
    expect(root.innerHTML).toBe(original);
    expect(root.querySelector('a')?.textContent).toBe('Link');
  });

  it('replaces stale SSR only after a valid public snapshot', async () => {
    document.body.innerHTML = [
      '<main id="public-resume" data-revision="1">',
      '<h1>Ada Lovelace</h1></main>',
    ].join('');
    const root = document.querySelector<HTMLElement>('#public-resume')!;
    await hydratePublicResume(root, 'ada1', '1', async () => resume('2'));
    expect(root.textContent).toContain('Grace Hopper');
    expect(root.textContent).not.toContain('Ada Lovelace');
  });

  it('leaves accessible SSR content intact after fetch failure', async () => {
    document.body.innerHTML = [
      '<main id="public-resume" data-revision="1"><h1>Ada Lovelace</h1>',
      '<a href="/x">Link</a></main>',
    ].join('');
    const root = document.querySelector<HTMLElement>('#public-resume')!;
    const original = root.innerHTML;
    await hydratePublicResume(root, 'ada1', '1', async () => {
      throw new Error('network');
    });
    expect(root.innerHTML).toBe(original);
    expect(vi.isMockFunction(console.debug)).toBe(false);
  });

  it('keeps accessible SSR content after invalid public JSON', async () => {
    document.body.innerHTML = [
      '<main id="public-resume" data-revision="1"><h1>Ada Lovelace</h1>',
      '<a href="/x">Link</a></main>',
    ].join('');
    const root = document.querySelector<HTMLElement>('#public-resume')!;
    const original = root.innerHTML;
    await hydratePublicResume(root, 'ada1', '1', async () => ({
      slug: 'ada1',
    }));
    expect(root.innerHTML).toBe(original);
  });

  it('restores exact SSR after stale-root mounting throws', async () => {
    document.body.innerHTML = [
      '<main id="public-resume" data-revision="1"><h1>Ada Lovelace</h1>',
      '<a href="/x">Link</a></main>',
    ].join('');
    const root = document.querySelector<HTMLElement>('#public-resume')!;
    const original = root.innerHTML;
    await hydratePublicResume(root, 'ada1', '1', async () => resume('2'), {
      hydrate: () => undefined,
      replace: () => {
        throw new Error('mount');
      },
    });
    expect(root.innerHTML).toBe(original);
    expect(root.querySelector('a')?.textContent).toBe('Link');
  });
});
