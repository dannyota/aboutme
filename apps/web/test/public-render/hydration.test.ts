// @vitest-environment happy-dom

import { describe, expect, it, vi } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { createSSRApp, h, nextTick } from 'vue';
import { renderToString } from 'vue/server-renderer';

import { hydratePublicResume } from '../../app/public/public-resume.client';
import {
  bindPublicRealtimeLifetime,
  createPublicResumeRealtime,
  readPublicResume,
  type PublicResumeReadResult,
} from '../../app/public/public-resume.client';
import type { RealtimeEventSource } from '../../app/realtime/controller';
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

class FakeEventSource implements RealtimeEventSource {
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;
  private readonly listeners = new Map<
    string,
    Set<(event: MessageEvent<string>) => void>
  >();

  addEventListener(
    type: string,
    listener: (event: MessageEvent<string>) => void,
  ): void {
    const listeners = this.listeners.get(type) ?? new Set();
    listeners.add(listener);
    this.listeners.set(type, listeners);
  }

  removeEventListener(
    type: string,
    listener: (event: MessageEvent<string>) => void,
  ): void {
    this.listeners.get(type)?.delete(listener);
  }

  close(): void {
    this.closed = true;
  }

  emitOpen(): void {
    this.onopen?.();
  }

  emitRevision(revision: string): void {
    const event = new MessageEvent('revision', {
      data: JSON.stringify({ version: 1, revision }),
    });
    this.listeners.get('revision')?.forEach((listener) => listener(event));
  }
}

describe('public resume hydration', () => {
  it('restarts realtime after a persisted page is restored', () => {
    const target = new EventTarget();
    const realtime = { start: vi.fn(), stop: vi.fn() };
    bindPublicRealtimeLifetime(realtime, target);
    const pagehide = new Event('pagehide');
    const pageshow = new Event('pageshow');
    Object.defineProperty(pageshow, 'persisted', { value: true });

    target.dispatchEvent(pagehide);
    target.dispatchEvent(pageshow);

    expect(realtime.stop).toHaveBeenCalledOnce();
    expect(realtime.start).toHaveBeenCalledOnce();
  });

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
    await hydratePublicResume(root, 'ada1', '1', async () => resume('1'), {
      hydrate: () => {
        throw new Error('mount');
      },
      replace: () => undefined,
    });
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

  it('updates the hydrated Vue app in place on a live revision', async () => {
    document.body.innerHTML = [
      '<main id="public-resume" data-revision="1">',
      await ssrRoot('1'),
      '</main>',
    ].join('');
    const root = document.querySelector<HTMLElement>('#public-resume')!;
    await hydratePublicResume(root, 'ada1', '1', async () => resume('1'));
    const renderedRoot = root.firstElementChild;
    const source = new FakeEventSource();
    const realtime = createPublicResumeRealtime({
      root,
      slug: 'ada1',
      revision: '1',
      read: async () => ({
        kind: 'complete',
        resume: resume('2'),
        etag: '"r2"',
      }),
      eventSourceFactory: () => source,
      reload: vi.fn(),
    });

    realtime.start();
    source.emitRevision('2');
    await Promise.resolve();
    await nextTick();

    expect(root.firstElementChild).toBe(renderedRoot);
    expect(root.textContent).toContain('Grace Hopper');
  });

  // eslint-disable-next-line max-len -- exact regression name.
  it('does not let a stopped generation patch a restarted session', async () => {
    document.body.innerHTML = [
      '<main id="public-resume" data-revision="1">',
      await ssrRoot('1'),
      '</main>',
    ].join('');
    const root = document.querySelector<HTMLElement>('#public-resume')!;
    await hydratePublicResume(root, 'ada1', '1', async () => resume('1'));
    const oldSource = new FakeEventSource();
    const newSource = new FakeEventSource();
    const sources = [oldSource, newSource];
    let resolveOld!: (result: PublicResumeReadResult) => void;
    let resolveNew!: (result: PublicResumeReadResult) => void;
    const oldRead = new Promise<PublicResumeReadResult>((resolve) => {
      resolveOld = resolve;
    });
    const newRead = new Promise<PublicResumeReadResult>((resolve) => {
      resolveNew = resolve;
    });
    const read = vi
      .fn()
      .mockReturnValueOnce(oldRead)
      .mockReturnValueOnce(newRead);
    const realtime = createPublicResumeRealtime({
      root,
      slug: 'ada1',
      revision: '1',
      read,
      eventSourceFactory: () => sources.shift()!,
      reload: vi.fn(),
    });

    realtime.start();
    oldSource.emitOpen();
    realtime.stop();
    realtime.start();
    newSource.emitOpen();
    resolveOld({ kind: 'complete', resume: resume('2'), etag: '"r2"' });
    await Promise.resolve();
    await nextTick();
    expect(root.textContent).toContain('Ada Lovelace');

    resolveNew({ kind: 'complete', resume: resume('3'), etag: '"r3"' });
    await Promise.resolve();
    await nextTick();
    expect(root.textContent).toContain('Grace Hopper');
  });

  it('rejects unsolicited and mismatched public 304 validators', async () => {
    for (const [sent, received] of [
      [undefined, '"r1"'],
      ['"r1"', '"r2"'],
    ] as const) {
      vi.stubGlobal(
        'fetch',
        vi.fn().mockResolvedValue(
          new Response(null, {
            status: 304,
            headers: { ETag: received },
          }),
        ),
      );
      await expect(readPublicResume('ada1', sent)).resolves.toEqual({
        kind: 'failed',
      });
    }
    vi.unstubAllGlobals();
  });

  it('reloads only for an unsupported public document version', async () => {
    const malformed = { ...resume('2'), slug: 1 };
    const unsupported = {
      ...resume('2'),
      document: { ...resume('2').document, schemaVersion: 999 },
    };
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ data: malformed })))
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ data: unsupported })),
      );
    vi.stubGlobal('fetch', fetcher);

    await expect(readPublicResume('ada1')).resolves.toEqual({
      kind: 'failed',
    });
    await expect(readPublicResume('ada1')).resolves.toEqual({
      kind: 'unknown-version',
    });
    vi.unstubAllGlobals();
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

  it('reloads when live refetch becomes authoritative public 404', async () => {
    document.body.innerHTML = [
      '<main id="public-resume" data-revision="1"><h1>',
      'Ada Lovelace</h1></main>',
    ].join('');
    const root = document.querySelector<HTMLElement>('#public-resume')!;
    const source = new FakeEventSource();
    const reload = vi.fn();
    const read = vi.fn(async (): Promise<PublicResumeReadResult> => ({
      kind: 'not-found',
    }));
    const realtime = createPublicResumeRealtime({
      root,
      slug: 'ada1',
      revision: '1',
      read,
      reload,
      eventSourceFactory: () => source,
    });
    realtime.start();
    source.emitOpen();
    await Promise.resolve();
    await Promise.resolve();

    expect(read).toHaveBeenCalledWith(undefined);
    expect(reload).toHaveBeenCalledTimes(1);
    expect(root.textContent).toContain('Ada Lovelace');
  });

  // eslint-disable-next-line max-len -- exact regression name.
  it('preserves root and region scroll positions across a live replacement', async () => {
    document.body.innerHTML = [
      '<main id="public-resume" data-revision="1">',
      '<div data-region="preview">Ada Lovelace</div></main>',
    ].join('');
    const root = document.querySelector<HTMLElement>('#public-resume')!;
    const region = root.querySelector<HTMLElement>('[data-region="preview"]')!;
    root.scrollTop = 19;
    region.scrollTop = 47;
    region.scrollLeft = 5;
    const source = new FakeEventSource();
    const replace = vi.fn((_value: unknown, target: HTMLElement) => {
      target.innerHTML = '<div data-region="preview">Grace Hopper</div>';
    });
    const realtime = createPublicResumeRealtime({
      root,
      slug: 'ada1',
      revision: '1',
      read: async () => ({
        kind: 'complete',
        resume: resume('2'),
        etag: '"r2"',
      }),
      mounter: { hydrate: () => undefined, replace },
      eventSourceFactory: () => source,
      reload: vi.fn(),
    });
    realtime.start();
    source.emitRevision('2');
    await Promise.resolve();
    await Promise.resolve();

    const replacement = root.querySelector<HTMLElement>(
      '[data-region="preview"]',
    );
    expect(replace).toHaveBeenCalledTimes(1);
    expect(root.textContent).toContain('Grace Hopper');
    expect(root.scrollTop).toBe(19);
    expect(replacement?.scrollTop).toBe(47);
    expect(replacement?.scrollLeft).toBe(5);
  });
});
