// @vitest-environment node

import { EventEmitter } from 'node:events';

import { describe, expect, it, vi } from 'vitest';

import { PRINT_HTML_MAX_BYTES } from '../../server/utils/print/envelope';
import {
  PRINT_WORKER_DEADLINE_MS,
  runPrintWorker,
} from '../../server/utils/print/runner';
import { printEnvelope } from './fixture';

class ObservedWorker extends EventEmitter {
  terminations = 0;

  async terminate(): Promise<number> {
    this.terminations += 1;
    return 1;
  }
}

const options = (
  workerUrl: URL | string,
  signal = new AbortController().signal,
) => ({ deadlineMs: PRINT_WORKER_DEADLINE_MS, signal, workerUrl });

describe('print worker lifecycle', () => {
  it('waits for a clean worker exit before returning one result', async () => {
    await expect(runPrintWorker(
      printEnvelope(),
      options(new URL('./result-worker.mjs', import.meta.url)),
    )).resolves.toBe('<main>Resume</main>');
  });

  it(
    'passes only a detached deeply frozen envelope to the worker',
    async () => {
      const worker = new ObservedWorker();
      const envelope = printEnvelope();
      const result = runPrintWorker(envelope, {
        ...options(new URL('./result-worker.mjs', import.meta.url)),
        workerFactory: (_url, workerData) => {
          expect(workerData).not.toBe(envelope);
          expect(Object.isFrozen(workerData)).toBe(true);
          expect(Object.isFrozen(workerData.document.personalDetails))
            .toBe(true);
          return worker;
        },
      });
      worker.emit('message', { type: 'result', html: '<p>ok</p>' });
      worker.emit('exit', 0);
      await expect(result).resolves.toBe('<p>ok</p>');
    },
  );

  it('cancels once and joins the worker before rejecting', async () => {
    const worker = new ObservedWorker();
    const controller = new AbortController();
    let settled = false;
    const result = runPrintWorker(printEnvelope(), {
      ...options(
        new URL('./spin-worker.mjs', import.meta.url),
        controller.signal,
      ),
      workerFactory: () => worker,
    }).finally(() => {
      settled = true;
    });
    controller.abort();
    await Promise.resolve();
    expect(worker.terminations).toBe(1);
    expect(settled).toBe(false);
    worker.emit('message', { type: 'result', html: '<p>late</p>' });
    worker.emit('exit', 1);
    await expect(result).rejects.toThrow('print failed');
    expect(worker.terminations).toBe(1);
  });

  it('joins cancellation that occurs during worker construction', async () => {
    const worker = new ObservedWorker();
    const controller = new AbortController();
    const result = runPrintWorker(printEnvelope(), {
      ...options(
        new URL('./spin-worker.mjs', import.meta.url),
        controller.signal,
      ),
      workerFactory: () => {
        controller.abort();
        return worker;
      },
    });
    try {
      expect(worker.terminations).toBe(1);
    } finally {
      worker.emit('exit', 1);
    }
    await expect(result).rejects.toThrow('print failed');
  });

  it('uses the exact deadline and joins after terminating', async () => {
    vi.useFakeTimers();
    try {
      const worker = new ObservedWorker();
      let settled = false;
      const result = runPrintWorker(printEnvelope(), {
        ...options(new URL('./spin-worker.mjs', import.meta.url)),
        workerFactory: () => worker,
      }).finally(() => {
        settled = true;
      });
      await vi.advanceTimersByTimeAsync(PRINT_WORKER_DEADLINE_MS - 1);
      expect(worker.terminations).toBe(0);
      await vi.advanceTimersByTimeAsync(1);
      expect(worker.terminations).toBe(1);
      expect(settled).toBe(false);
      worker.emit('exit', 1);
      await expect(result).rejects.toThrow('print failed');
    } finally {
      vi.useRealTimers();
    }
  });

  it(
    'rejects malformed, duplicate, oversized, and failed workers',
    async () => {
      for (const fault of [
        'malformed', 'duplicate', 'oversize', 'error', 'nonzero', 'none',
      ]) {
        await expect(runPrintWorker(
          printEnvelope(),
          options(new URL(
            `./fault-worker.mjs?fault=${fault}`,
            import.meta.url,
          )),
        )).rejects.toThrow('print failed');
      }
    },
  );

  it('accepts the HTML cap and rejects one UTF-8 byte more', async () => {
    for (const [bytes, accepted] of [
      [PRINT_HTML_MAX_BYTES, true],
      [PRINT_HTML_MAX_BYTES + 1, false],
    ] as const) {
      const worker = new ObservedWorker();
      const result = runPrintWorker(printEnvelope(), {
        ...options(new URL('./result-worker.mjs', import.meta.url)),
        workerFactory: () => worker,
      });
      const html = 'x'.repeat(bytes);
      worker.emit('message', { type: 'result', html });
      worker.emit('exit', accepted ? 0 : 1);
      if (accepted) await expect(result).resolves.toBe(html);
      else await expect(result).rejects.toThrow('print failed');
    }
  });

  it('rejects active HTML even when the worker exits cleanly', async () => {
    const worker = new ObservedWorker();
    const result = runPrintWorker(printEnvelope(), {
      ...options(new URL('./result-worker.mjs', import.meta.url)),
      workerFactory: () => worker,
    });
    worker.emit('message', {
      type: 'result',
      html: '<script>alert(1)</script>',
    });
    worker.emit('exit', 1);
    await expect(result).rejects.toThrow('print failed');
  });

  it('does not spawn for a pre-canceled job', async () => {
    const controller = new AbortController();
    controller.abort();
    const factory = vi.fn();
    await expect(runPrintWorker(printEnvelope(), {
      ...options(new URL('./missing.mjs', import.meta.url), controller.signal),
      workerFactory: factory,
    })).rejects.toThrow('print failed');
    expect(factory).not.toHaveBeenCalled();
  });
});
