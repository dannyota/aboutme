import { CURRENT_VERSION } from '@aboutme/schema/released';
import { createApp, createSSRApp, h, nextTick, shallowRef } from 'vue';
import type { ShallowRef } from 'vue';

import type { components } from '../api/generated/openapi';
import PublicResumeApp from '../components/public/PublicResumeApp.vue';
import { compareRevision, parseRevision } from '../editor/revision';
import {
  createRealtimeController,
  type RealtimeController,
  type RealtimeEventSource,
  type RealtimeReadResult,
  type RevisionDecision,
} from '../realtime/controller';
import validatePublicResume from '#public-render-validator';

type PublicResume = components['schemas']['PublicResume'];

export interface PublicResumeMounter {
  hydrate: (resume: PublicResume, root: HTMLElement) => void | Promise<void>;
  replace: (resume: PublicResume, root: HTMLElement) => void | Promise<void>;
}

export type PublicResumeReadResult
  = | {
    readonly kind: 'complete';
    readonly resume: PublicResume;
    readonly etag: string | undefined;
  }
  | { readonly kind: 'not-modified'; readonly etag: string }
  | { readonly kind: 'not-found' }
  | { readonly kind: 'failed' }
  | { readonly kind: 'unknown-version' };

export interface PublicResumeRealtime {
  start(): void;
  stop(): void;
}

export function bindPublicRealtimeLifetime(
  realtime: PublicResumeRealtime,
  target: EventTarget = window,
): void {
  target.addEventListener('pagehide', () => realtime.stop());
  target.addEventListener('pageshow', (event) => {
    if ((event as PageTransitionEvent).persisted) realtime.start();
  });
}

const mountedRoots = new WeakMap<HTMLElement, ShallowRef<PublicResume>>();

function publicResumeRoot(publicResume: ShallowRef<PublicResume>) {
  return {
    render: () => h(PublicResumeApp, { publicResume: publicResume.value }),
  };
}

const vueMounter: PublicResumeMounter = {
  hydrate: async (value, root) => {
    const publicResume = shallowRef(value);
    createSSRApp(publicResumeRoot(publicResume)).mount(root);
    mountedRoots.set(root, publicResume);
    await nextTick();
  },
  replace: async (value, root) => {
    const mounted = mountedRoots.get(root);
    if (mounted !== undefined) {
      mounted.value = value;
      await nextTick();
      return;
    }
    const publicResume = shallowRef(value);
    createApp(publicResumeRoot(publicResume)).mount(root);
    mountedRoots.set(root, publicResume);
    await nextTick();
  },
};

const slugPattern = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/u;

function validPublicResume(
  value: unknown,
  slug: string,
): value is PublicResume {
  return (
    validatePublicResume(value) === true
    && (value as PublicResume).slug === slug
  );
}

function unsupportedDocumentVersion(value: unknown): boolean {
  if (typeof value !== 'object' || value === null || !('document' in value)) {
    return false;
  }
  const document = value.document;
  if (
    typeof document !== 'object'
    || document === null
    || !('schemaVersion' in document)
  ) {
    return false;
  }
  return (
    typeof document.schemaVersion === 'number'
    && Number.isInteger(document.schemaVersion)
    && document.schemaVersion !== CURRENT_VERSION
  );
}

const diagnostic = (reason: string): void => {
  // Public diagnostics exclude URLs, response bodies, and resume data.
  console.debug(`public resume hydration skipped: ${reason}`);
};

export async function hydratePublicResume(
  root: HTMLElement,
  slug: string,
  serverRevision: string,
  request: (slug: string) => Promise<unknown> = async (value) => {
    const response = await fetch(
      `/api/v1/public/resumes/${encodeURIComponent(value)}`,
      {
        cache: 'no-store',
        credentials: 'omit',
      },
    );
    if (!response.ok) throw new Error('public response failed');
    const envelope = (await response.json()) as { data?: unknown };
    return envelope.data;
  },
  mounter: PublicResumeMounter = vueMounter,
): Promise<void> {
  try {
    const publicResume = await request(slug);
    if (!validPublicResume(publicResume, slug)) {
      throw new Error('invalid public response');
    }
    const original = root.innerHTML;
    if (publicResume.revision === serverRevision) {
      try {
        await mounter.hydrate(publicResume, root);
      } catch {
        root.innerHTML = original;
        throw new Error('public hydration failed');
      }
      root.dataset.revision = publicResume.revision;
      return;
    }
    try {
      root.replaceChildren();
      await mounter.replace(publicResume, root);
    } catch {
      root.innerHTML = original;
      throw new Error('public mount failed');
    }
    root.dataset.revision = publicResume.revision;
  } catch {
    diagnostic('failure');
  }
}

function scrollState(root: HTMLElement): {
  readonly rootTop: number;
  readonly windowX: number;
  readonly windowY: number;
  readonly regions: readonly [string, number, number][];
} {
  return {
    rootTop: root.scrollTop,
    windowX: window.scrollX,
    windowY: window.scrollY,
    regions: [...root.querySelectorAll<HTMLElement>('[data-region]')].map(
      (element) => [
        element.dataset.region ?? '',
        element.scrollTop,
        element.scrollLeft,
      ],
    ),
  };
}

function restoreScrollState(
  root: HTMLElement,
  state: ReturnType<typeof scrollState>,
): void {
  root.scrollTop = state.rootTop;
  for (const [region, top, left] of state.regions) {
    if (region === '') continue;
    const element = root.querySelector<HTMLElement>(
      `[data-region="${CSS.escape(region)}"]`,
    );
    if (element === null) continue;
    element.scrollTop = top;
    element.scrollLeft = left;
  }
  try {
    window.scrollTo(state.windowX, state.windowY);
  } catch {
    // jsdom and happy-dom may leave scrolling unimplemented.
  }
}

export async function readPublicResume(
  slug: string,
  etag?: string,
): Promise<PublicResumeReadResult> {
  const headers = new Headers();
  if (etag !== undefined) headers.set('If-None-Match', etag);
  let response: Response;
  try {
    response = await fetch(
      `/api/v1/public/resumes/${encodeURIComponent(slug)}`,
      { cache: 'no-store', credentials: 'omit', headers },
    );
  } catch {
    return { kind: 'failed' };
  }
  if (response.status === 404) return { kind: 'not-found' };
  if (response.status === 304) {
    try {
      if (
        response.headers.get('Content-Type') !== null
        || (await response.arrayBuffer()).byteLength !== 0
      ) {
        throw new Error();
      }
      const responseETag = response.headers.get('ETag');
      if (
        etag === undefined
        || responseETag === null
        || responseETag !== etag
      ) {
        throw new Error();
      }
      return { kind: 'not-modified', etag: responseETag };
    } catch {
      return { kind: 'failed' };
    }
  }
  if (!response.ok) return { kind: 'failed' };
  try {
    const envelope = (await response.json()) as { data?: unknown };
    if (unsupportedDocumentVersion(envelope.data)) {
      return { kind: 'unknown-version' };
    }
    if (!validPublicResume(envelope.data, slug)) {
      return { kind: 'failed' };
    }
    return {
      kind: 'complete',
      resume: envelope.data,
      etag: response.headers.get('ETag') ?? undefined,
    };
  } catch {
    return { kind: 'failed' };
  }
}

export function createPublicResumeRealtime(options: {
  root: HTMLElement;
  slug: string;
  revision: string;
  etag?: string;
  read?: (etag?: string) => Promise<PublicResumeReadResult>;
  mounter?: PublicResumeMounter;
  eventSourceFactory?: (
    url: string,
    withCredentials: boolean,
  ) => RealtimeEventSource;
  reload?: () => void;
}): PublicResumeRealtime {
  let currentRevision = parseRevision(options.revision);
  let currentETag = options.etag;
  let running = false;
  let generation = 0;
  const mounter = options.mounter ?? vueMounter;
  const reload = options.reload ?? (() => window.location.reload());
  const read = options.read ?? ((etag) => readPublicResume(options.slug, etag));
  let controller: RealtimeController | null = null;

  const revisionDecision = (value: unknown): RevisionDecision => {
    if (typeof value !== 'object' || value === null) return 'unknown';
    const event = value as Record<string, unknown>;
    if (typeof event.revision !== 'string') return 'unknown';
    let revision;
    try {
      revision = parseRevision(event.revision);
    } catch {
      return 'unknown';
    }
    return compareRevision(revision, currentRevision) <= 0
      ? 'ignore'
      : 'accept';
  };

  const refresh = async (
    mode: 'unconditional' | 'conditional',
  ): Promise<RealtimeReadResult> => {
    const refreshGeneration = generation;
    const result = await read(mode === 'conditional' ? currentETag : undefined);
    if (!running || refreshGeneration !== generation) return { kind: 'failed' };
    if (result.kind === 'not-found') return { kind: 'not-found' };
    if (result.kind === 'unknown-version') return result;
    if (result.kind === 'failed') return result;
    if (result.kind === 'not-modified') {
      currentETag = result.etag;
      return { kind: 'unchanged' };
    }
    const state = scrollState(options.root);
    const nextRevision = parseRevision(result.resume.revision);
    if (compareRevision(nextRevision, currentRevision) === -1) {
      return { kind: 'failed' };
    }
    if (compareRevision(nextRevision, currentRevision) === 0) {
      currentETag = result.etag;
      return { kind: 'unchanged' };
    }
    const original = options.root.innerHTML;
    try {
      await mounter.replace(result.resume, options.root);
      if (!running || refreshGeneration !== generation) {
        return { kind: 'failed' };
      }
      restoreScrollState(options.root, state);
    } catch {
      options.root.innerHTML = original;
      return { kind: 'failed' };
    }
    currentRevision = nextRevision;
    currentETag = result.etag;
    options.root.dataset.revision = result.resume.revision;
    return { kind: 'updated' };
  };

  return {
    start: () => {
      if (running) return;
      running = true;
      generation += 1;
      controller = createRealtimeController({
        url: `/api/v1/live/${encodeURIComponent(options.slug)}`,
        eventSourceFactory: options.eventSourceFactory,
        withCredentials: false,
        refetch: refresh,
        onRevision: revisionDecision,
        onUnknownVersion: reload,
        onNotFound: reload,
      });
      controller.start();
    },
    stop: () => {
      if (!running) return;
      running = false;
      generation += 1;
      controller?.stop();
      controller = null;
    },
  };
}

function boot(): void {
  const root = document.querySelector<HTMLElement>('#public-resume');
  const rawRevision = root?.dataset.revision;
  const slug = location.pathname.split('/').filter(Boolean);
  if (
    root === null
    || rawRevision === undefined
    || slug.length !== 1
    || !slugPattern.test(slug[0]!)
    || !/^[1-9][0-9]*$/u.test(rawRevision)
  ) {
    return;
  }
  void hydratePublicResume(root, slug[0]!, rawRevision).then(() => {
    const realtime = createPublicResumeRealtime({
      root,
      slug: slug[0]!,
      revision: root.dataset.revision ?? rawRevision,
    });
    realtime.start();
    bindPublicRealtimeLifetime(realtime);
  });
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', boot, { once: true });
} else {
  boot();
}
