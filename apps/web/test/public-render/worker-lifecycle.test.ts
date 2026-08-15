// @vitest-environment node

import { readFileSync } from 'node:fs';
import { EventEmitter } from 'node:events';
import { describe, expect, it, vi } from 'vitest';

import {
  PUBLIC_RENDER_HTML_MAX_BYTES,
} from '../../server/utils/public-render/envelope';
import { runPublicRenderWorker } from '../../server/utils/public-render/runner';

class ObservedWorker extends EventEmitter {
  terminations = 0;

  async terminate(): Promise<number> {
    this.terminations += 1;
    return 1;
  }
}

const fixture = JSON.parse(
  readFileSync(
    new URL(
      '../../../../packages/schema/fixtures/minimal.json',
      import.meta.url,
    ),
    'utf8',
  ),
);
fixture.content = {
  profile: {
    sectionType: 'profile',
    entries: [{ id: '00000000-0000-4000-8000-000000000001' }],
  },
};
fixture.customization.layout.sections.main = ['profile'];

const request = {
  publicResume: {
    slug: 'ada1',
    revision: '1',
    lng: 'en',
    downloadEnabled: false,
    document: fixture,
  },
  mode: 'continuous' as const,
  canonicalOrigin: 'https://resume.example',
  discoveryEnabled: false,
};

describe('public render worker lifecycle', () => {
  it('waits for clean worker exit before resolving rendered HTML', async () => {
    await expect(
      runPublicRenderWorker(request, {
        signal: new AbortController().signal,
        deadlineMs: 5_000,
        workerUrl: new URL('./result-worker.mjs', import.meta.url),
      }),
    ).resolves.toContain('Ada Lovelace');
  });

  it('loads a worker from a file:// URL string', async () => {
    await expect(
      runPublicRenderWorker(request, {
        signal: new AbortController().signal,
        deadlineMs: 5_000,
        workerUrl: new URL('./result-worker.mjs', import.meta.url).href,
      }),
    ).resolves.toContain('Ada Lovelace');
  });

  it('rejects a non-cooperative worker generically', async () => {
    const controller = new AbortController();
    const spinningWorkerUrl = new URL('./spin-worker.mjs', import.meta.url);
    const rendered = runPublicRenderWorker(request, {
      signal: controller.signal,
      deadlineMs: 5_000,
      workerUrl: spinningWorkerUrl,
    });
    controller.abort();
    await expect(rendered).rejects.toThrow('public render failed');
  });

  it('does not spawn after a pre-aborted request', async () => {
    const controller = new AbortController();
    controller.abort();
    await expect(
      runPublicRenderWorker(request, {
        signal: controller.signal,
        deadlineMs: 5_000,
        workerUrl: new URL('./does-not-exist.mjs', import.meta.url),
      }),
    ).rejects.toThrow('public render failed');
  });

  it('rejects every closed worker protocol failure after exit', async () => {
    const faults = [
      'malformed',
      'multiple',
      'oversize',
      'error',
      'nonzero',
      'none',
    ];
    for (const fault of faults) {
      await expect(
        runPublicRenderWorker(request, {
          signal: new AbortController().signal,
          deadlineMs: 5_000,
          workerUrl: new URL(
            `./fault-worker.mjs?fault=${fault}`,
            import.meta.url,
          ),
        }),
      ).rejects.toThrow('public render failed');
    }
  });

  it('uses the exact deadline for a non-cooperative worker', async () => {
    vi.useFakeTimers();
    try {
      const rendered = runPublicRenderWorker(request, {
        signal: new AbortController().signal,
        deadlineMs: 5_000,
        workerUrl: new URL('./spin-worker.mjs', import.meta.url),
      });
      await vi.advanceTimersByTimeAsync(5_000);
      await expect(rendered).rejects.toThrow('public render failed');
    } finally {
      vi.useRealTimers();
    }
  });

  it('does not leave the deadline rejection unhandled while joining exit',
    async () => {
      vi.useFakeTimers();
      const worker = new ObservedWorker();
      let unhandled: unknown;
      const observeUnhandled = (reason: unknown): void => {
        unhandled = reason;
      };
      process.on('unhandledRejection', observeUnhandled);
      try {
        const rendered = runPublicRenderWorker(request, {
          signal: new AbortController().signal,
          deadlineMs: 5_000,
          workerUrl: new URL('./spin-worker.mjs', import.meta.url),
          workerFactory: () => worker,
        });
        await vi.advanceTimersByTimeAsync(5_000);
        worker.emit('exit', 1);
        await vi.runAllTimersAsync();
        expect(unhandled).toBeUndefined();
        await expect(rendered).rejects.toThrow('public render failed');
      } finally {
        process.off('unhandledRejection', observeUnhandled);
        vi.useRealTimers();
      }
    });

  it('terminates once and waits for the observed abort exit', async () => {
    const worker = new ObservedWorker();
    const controller = new AbortController();
    let settled = false;
    const rendered = runPublicRenderWorker(request, {
      signal: controller.signal,
      deadlineMs: 5_000,
      workerUrl: new URL('./spin-worker.mjs', import.meta.url),
      workerFactory: () => worker,
    }).finally(() => {
      settled = true;
    });
    controller.abort();
    await Promise.resolve();
    expect(worker.terminations).toBe(1);
    expect(settled).toBe(false);
    worker.emit('exit', 1);
    await expect(rendered).rejects.toThrow('public render failed');
    expect(worker.terminations).toBe(1);
  });

  it('stays pending through 4999ms and joins at the deadline', async () => {
    vi.useFakeTimers();
    try {
      const worker = new ObservedWorker();
      let settled = false;
      const rendered = runPublicRenderWorker(request, {
        signal: new AbortController().signal,
        deadlineMs: 5_000,
        workerUrl: new URL('./spin-worker.mjs', import.meta.url),
        workerFactory: () => worker,
      }).finally(() => {
        settled = true;
      });
      await vi.advanceTimersByTimeAsync(4_999);
      expect(worker.terminations).toBe(0);
      expect(settled).toBe(false);
      await vi.advanceTimersByTimeAsync(1);
      expect(worker.terminations).toBe(1);
      expect(settled).toBe(false);
      worker.emit('exit', 1);
      await expect(rendered).rejects.toThrow('public render failed');
    } finally {
      vi.useRealTimers();
    }
  });

  it('terminates once for a late message after an accepted result',
    async () => {
      const worker = new ObservedWorker();
      const rendered = runPublicRenderWorker(request, {
        signal: new AbortController().signal,
        deadlineMs: 5_000,
        workerUrl: new URL('./result-worker.mjs', import.meta.url),
        workerFactory: () => worker,
      });
      worker.emit('message', { type: 'result', html: '<p>first</p>' });
      worker.emit('message', { type: 'result', html: '<p>late</p>' });
      expect(worker.terminations).toBe(1);
      worker.emit('exit', 1);
      await expect(rendered).rejects.toThrow('public render failed');
    });

  it('accepts HTML at the exact cap after a clean exit', async () => {
    const worker = new ObservedWorker();
    const rendered = runPublicRenderWorker(request, {
      signal: new AbortController().signal,
      deadlineMs: 5_000,
      workerUrl: new URL('./result-worker.mjs', import.meta.url),
      workerFactory: () => worker,
    });
    const html = 'x'.repeat(PUBLIC_RENDER_HTML_MAX_BYTES);
    worker.emit('message', { type: 'result', html });
    worker.emit('exit', 0);
    await expect(rendered).resolves.toBe(html);
    expect(worker.terminations).toBe(0);
  });

  it('terminates once on messageerror and waits for its exit', async () => {
    const worker = new ObservedWorker();
    let settled = false;
    const rendered = runPublicRenderWorker(request, {
      signal: new AbortController().signal,
      deadlineMs: 5_000,
      workerUrl: new URL('./result-worker.mjs', import.meta.url),
      workerFactory: () => worker,
    }).finally(() => {
      settled = true;
    });
    worker.emit('messageerror', new Error('decode'));
    expect(worker.terminations).toBe(1);
    expect(settled).toBe(false);
    worker.emit('exit', 1);
    await expect(rendered).rejects.toThrow('public render failed');
  });
});
