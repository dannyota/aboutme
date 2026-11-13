// @vitest-environment happy-dom

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createSSRApp, h, nextTick } from 'vue';
import { renderToString } from 'vue/server-renderer';

import PublicResumeApp from '../../app/components/public/PublicResumeApp.vue';
import {
  createPublicResumeRealtime,
  hydratePublicResume,
  type PublicResumeReadResult,
} from '../../app/public/public-resume.client';
import type { RealtimeEventSource } from '../../app/realtime/controller';

const ORIGIN = 'https://aboutme.example';
const PROJECTS_URL = `${ORIGIN}/ada1/projects`;
const ATLAS = '00000000-0000-4000-8000-0000000000a1';
const BEACON = '00000000-0000-4000-8000-0000000000a2';

interface Link {
  entryId: string;
  projectSlug: string;
  url: string;
}

const link = (entryId: string, projectSlug: string): Link => ({
  entryId, projectSlug, url: `${PROJECTS_URL}/${projectSlug}`,
});

const resume = (
  revision: string,
  projectLinks?: Link[],
  untitled = false,
) => {
  const document = JSON.parse(readFileSync(
    resolve(process.cwd(), '../../packages/schema/fixtures/minimal.json'),
    'utf8',
  ));
  document.personalDetails.fullName = 'Ada Lovelace';
  document.content = {
    projects: {
      sectionType: 'project',
      entries: [
        { id: ATLAS, title: 'Atlas', link: 'https://atlas.example/' },
        untitled ? { id: BEACON } : { id: BEACON, title: 'Beacon' },
      ],
    },
  };
  document.customization.layout.sections.main = ['projects'];
  return {
    slug: 'ada1',
    revision,
    lng: 'en',
    downloadEnabled: true,
    document,
    ...(projectLinks === undefined ? {} : { projectLinks }),
  };
};

const mountServerPage = async (
  marker: boolean,
  links: ReadonlyMap<string, string> = new Map(),
) => {
  const html = await renderToString(createSSRApp({
    render: () => h(PublicResumeApp, {
      publicResume: resume('1'),
      homeHref: `${ORIGIN}/`,
      ...(marker ? { projectsUrl: PROJECTS_URL, projectLinks: links } : {}),
    }),
  }));
  document.body.innerHTML = [
    '<main id="public-resume" ',
    marker ? 'data-project-links="" ' : '',
    `data-revision="1">${html}</main>`,
  ].join('');
  return document.querySelector<HTMLElement>('#public-resume')!;
};

class FakeEventSource implements RealtimeEventSource {
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  private readonly listeners = new Set<
    (event: MessageEvent<string>) => void
  >();

  addEventListener(
    _type: string,
    listener: (event: MessageEvent<string>) => void,
  ): void {
    this.listeners.add(listener);
  }

  removeEventListener(
    _type: string,
    listener: (event: MessageEvent<string>) => void,
  ): void {
    this.listeners.delete(listener);
  }

  close(): void {}

  emitRevision(revision: string): void {
    const event = new MessageEvent('revision', {
      data: JSON.stringify({ version: 1, revision }),
    });
    this.listeners.forEach((listener) => listener(event));
  }
}

const live = (root: HTMLElement, revisions: ReturnType<typeof resume>[]) => {
  const source = new FakeEventSource();
  const queue = [...revisions];
  const realtime = createPublicResumeRealtime({
    root,
    slug: 'ada1',
    revision: root.dataset.revision!,
    read: async (): Promise<PublicResumeReadResult> => {
      const next = queue.shift()!;
      return { kind: 'complete', resume: next, etag: `"r${next.revision}"` };
    },
    eventSourceFactory: () => source,
    reload: vi.fn(),
  });
  realtime.start();
  return {
    next: async () => {
      const { revision } = revisions[revisions.length - queue.length]!;
      source.emitRevision(revision);
      await vi.waitFor(() => {
        expect(root.dataset.revision).toBe(revision);
      });
      await nextTick();
    },
    stop: () => realtime.stop(),
  };
};

const titleLinks = (root: HTMLElement) =>
  [...root.querySelectorAll('.entry-title a')].map((anchor) => [
    anchor.getAttribute('data-project-entry'),
    anchor.getAttribute('href'),
  ]);

const barLink = (root: HTMLElement) =>
  root.querySelector('a.public-projects')?.getAttribute('href') ?? null;

describe('public page entry point hydration', () => {
  let canonical: HTMLLinkElement;

  beforeEach(() => {
    canonical = document.createElement('link');
    canonical.rel = 'canonical';
    canonical.href = `${ORIGIN}/ada1`;
    document.head.append(canonical);
  });

  afterEach(() => {
    canonical.remove();
    document.body.innerHTML = '';
  });

  it('keeps server links on hydration at the same revision', async () => {
    const root = await mountServerPage(true, new Map([
      [ATLAS, `${PROJECTS_URL}/atlas`],
    ]));
    const rendered = root.firstElementChild;
    await hydratePublicResume(root, 'ada1', '1',
      async () => resume('1', [link(ATLAS, 'atlas')]));
    expect(root.firstElementChild).toBe(rendered);
    expect(titleLinks(root)).toEqual([[ATLAS, `${PROJECTS_URL}/atlas`]]);
    expect(barLink(root)).toBe(PROJECTS_URL);
  });

  it('shows a first link, renames it and removes it with each revision',
    async () => {
      const root = await mountServerPage(true);
      await hydratePublicResume(root, 'ada1', '1', async () => resume('1', []));
      expect(titleLinks(root)).toEqual([[null, 'https://atlas.example/']]);
      expect(barLink(root)).toBeNull();

      const updates = live(root, [
        resume('2', [link(BEACON, 'beacon')]),
        resume('3', [link(BEACON, 'beacon-two')]),
        resume('4', []),
      ]);
      await updates.next();
      expect(titleLinks(root)).toEqual([
        [null, 'https://atlas.example/'],
        [BEACON, `${PROJECTS_URL}/beacon`],
      ]);
      expect(barLink(root)).toBe(PROJECTS_URL);

      await updates.next();
      expect(titleLinks(root)).toEqual([
        [null, 'https://atlas.example/'],
        [BEACON, `${PROJECTS_URL}/beacon-two`],
      ]);
      expect(root.innerHTML).not.toContain(`${PROJECTS_URL}/beacon"`);

      await updates.next();
      expect(titleLinks(root)).toEqual([[null, 'https://atlas.example/']]);
      expect(barLink(root)).toBeNull();
      updates.stop();
    });

  it('replaces the map whole when a revision drops one of two links',
    async () => {
      const root = await mountServerPage(true, new Map([
        [ATLAS, `${PROJECTS_URL}/atlas`],
        [BEACON, `${PROJECTS_URL}/beacon`],
      ]));
      await hydratePublicResume(root, 'ada1', '1', async () =>
        resume('1', [link(ATLAS, 'atlas'), link(BEACON, 'beacon')]));
      const updates = live(root, [resume('2', [link(BEACON, 'beacon')])]);
      await updates.next();
      expect(titleLinks(root)).toEqual([
        [null, 'https://atlas.example/'],
        [BEACON, `${PROJECTS_URL}/beacon`],
      ]);
      updates.stop();
    });

  it('ignores a link outside this resume\'s projects address', async () => {
    const root = await mountServerPage(true);
    await hydratePublicResume(root, 'ada1', '1', async () => resume('2', [
      { entryId: ATLAS, projectSlug: 'atlas', url: `${ORIGIN}/other/atlas` },
      link(BEACON, 'beacon'),
    ]));
    expect(titleLinks(root)).toEqual([
      [null, 'https://atlas.example/'],
      [BEACON, `${PROJECTS_URL}/beacon`],
    ]);
  });

  it('gives an untitled linked entry no anchor and keeps the bar link',
    async () => {
      const root = await mountServerPage(true);
      await hydratePublicResume(root, 'ada1', '1', async () => resume('1', []));
      const updates = live(root, [
        resume('2', [link(BEACON, 'beacon')], true),
      ]);
      await updates.next();
      expect(root.querySelector('[data-project-entry]')).toBeNull();
      expect(titleLinks(root)).toEqual([[null, 'https://atlas.example/']]);
      expect(barLink(root)).toBe(PROJECTS_URL);
      updates.stop();
    });

  it('builds no map without the root marker', async () => {
    const root = await mountServerPage(false);
    await hydratePublicResume(root, 'ada1', '1',
      async () => resume('1', [link(ATLAS, 'atlas')]));
    expect(titleLinks(root)).toEqual([[null, 'https://atlas.example/']]);
    expect(barLink(root)).toBeNull();

    const updates = live(root, [resume('2', [link(BEACON, 'beacon')])]);
    await updates.next();
    expect(root.textContent).toContain('Beacon');
    expect(root.querySelector('[data-project-entry]')).toBeNull();
    expect(barLink(root)).toBeNull();
    updates.stop();
  });
});
