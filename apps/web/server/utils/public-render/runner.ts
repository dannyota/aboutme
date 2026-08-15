import { Worker } from 'node:worker_threads';

import {
  PUBLIC_RENDER_FAILURE,
  PUBLIC_RENDER_HTML_MAX_BYTES,
  type PublicRenderRequest,
} from './envelope';

export interface PublicRenderWorkerOptions {
  signal: AbortSignal;
  deadlineMs: 5_000;
  /** Internal seam used by lifecycle tests and the direct route. */
  workerUrl?: URL | string;
  /** Test-only lifecycle seam; production always creates a Node Worker. */
  workerFactory?: (
    url: URL | string,
    workerData: PublicRenderRequest,
  ) => PublicRenderWorker;
}

export interface PublicRenderWorker {
  terminate: () => Promise<number>;
  on: Worker['on'];
  once: Worker['once'];
}

export const PUBLIC_RENDER_DEADLINE_MS = 5_000;

const failure = (): Error => new Error(PUBLIC_RENDER_FAILURE);

export class PublicRenderDeadlineError extends Error {
  constructor() {
    super(PUBLIC_RENDER_FAILURE);
  }
}

const freeze = <T>(value: T): T => {
  if (value !== null && typeof value === 'object') {
    Object.freeze(value);
    for (const item of Object.values(value)) freeze(item);
  }
  return value;
};

function runtimeWorkerUrl(): URL {
  const output = process.env.NUXT_PUBLIC_RENDER_WORKER_URL;
  if (output === undefined) throw failure();
  return new URL(output);
}

export function runPublicRenderWorker(
  request: PublicRenderRequest,
  options: PublicRenderWorkerOptions,
): Promise<string> {
  if (options.deadlineMs !== PUBLIC_RENDER_DEADLINE_MS) {
    return Promise.reject(failure());
  }
  if (options.signal.aborted) return Promise.reject(failure());
  const rendered = new Promise<string>((resolve, reject) => {
    let result: string | undefined;
    let failureReason: Error | undefined;
    let terminated = false;
    let termination: Promise<number> | undefined;
    const workerData = freeze(structuredClone(request));
    const worker
      = options.workerFactory?.(
        options.workerUrl ?? runtimeWorkerUrl(),
        workerData,
      ) ?? new Worker(options.workerUrl ?? runtimeWorkerUrl(), { workerData });
    const finish = (): void => {
      clearTimeout(timer);
      options.signal.removeEventListener('abort', abort);
      if (failureReason !== undefined || result === undefined) {
        reject(failureReason ?? failure());
      } else resolve(result);
    };
    const terminate = (reason: Error = failure()): void => {
      failureReason ??= reason;
      if (terminated) return;
      terminated = true;
      termination = worker.terminate();
    };
    const abort = (): void => terminate();
    const timer = setTimeout(
      () => terminate(new PublicRenderDeadlineError()),
      options.deadlineMs,
    );
    options.signal.addEventListener('abort', abort, { once: true });
    worker.on('message', (message: unknown) => {
      if (
        result !== undefined
        || message === null
        || typeof message !== 'object'
      ) {
        terminate();
        return;
      }
      const candidate = message as { type?: unknown; html?: unknown };
      if (
        candidate.type !== 'result'
        || typeof candidate.html !== 'string'
        || Buffer.byteLength(candidate.html, 'utf8')
        > PUBLIC_RENDER_HTML_MAX_BYTES
      ) {
        terminate();
        return;
      }
      result = candidate.html;
    });
    worker.once('error', () => terminate());
    worker.once('messageerror', () => terminate());
    worker.once('exit', async (code) => {
      if (code !== 0) failureReason ??= failure();
      try {
        await termination;
      } catch {
        failureReason ??= failure();
      }
      finish();
    });
  });
  void rendered.catch(() => undefined);
  return rendered;
}
