import { Worker } from 'node:worker_threads';

import {
  PRINT_FAILURE,
  PRINT_HTML_MAX_BYTES,
  type PrintEnvelope,
} from './envelope';

export const PRINT_WORKER_DEADLINE_MS = 5_000;

export interface PrintWorker {
  terminate: () => Promise<number>;
  on: Worker['on'];
  once: Worker['once'];
}

export interface PrintWorkerOptions {
  signal: AbortSignal;
  deadlineMs: 5_000;
  workerUrl: URL | string;
  /** Test seam. Production always creates a Node worker. */
  workerFactory?: (
    url: URL | string,
    workerData: PrintEnvelope,
  ) => PrintWorker;
}

const failure = (): Error => new Error(PRINT_FAILURE);

const freeze = <T>(value: T): T => {
  if (value !== null && typeof value === 'object') {
    Object.freeze(value);
    Object.values(value).forEach(freeze);
  }
  return value;
};

export function runPrintWorker(
  envelope: PrintEnvelope,
  options: PrintWorkerOptions,
): Promise<string> {
  if (
    options.deadlineMs !== PRINT_WORKER_DEADLINE_MS
    || options.signal.aborted
  ) return Promise.reject(failure());

  const rendered = new Promise<string>((resolve, reject) => {
    let result: string | undefined;
    let failureReason: Error | undefined;
    let terminated = false;
    let termination: Promise<number> | undefined;
    let worker: PrintWorker;
    const workerData = freeze(structuredClone(envelope));
    try {
      worker = options.workerFactory?.(options.workerUrl, workerData)
        ?? new Worker(
          typeof options.workerUrl === 'string'
            ? new URL(options.workerUrl)
            : options.workerUrl,
          { workerData },
        );
    } catch {
      reject(failure());
      return;
    }

    const finish = (): void => {
      clearTimeout(timer);
      options.signal.removeEventListener('abort', abort);
      if (failureReason !== undefined || result === undefined) {
        reject(failureReason ?? failure());
      } else {
        resolve(result);
      }
    };
    const terminate = (): void => {
      failureReason ??= failure();
      if (terminated) return;
      terminated = true;
      termination = worker.terminate();
    };
    const abort = (): void => terminate();
    worker.on('message', (message: unknown) => {
      if (
        failureReason !== undefined
        || result !== undefined
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
        || Buffer.byteLength(candidate.html, 'utf8') > PRINT_HTML_MAX_BYTES
        || /<script\b/iu.test(candidate.html)
        || Object.keys(candidate).sort().join(',') !== 'html,type'
      ) {
        terminate();
        return;
      }
      result = candidate.html;
    });
    worker.once('error', terminate);
    worker.once('messageerror', terminate);
    worker.once('exit', async (code) => {
      if (code !== 0) failureReason ??= failure();
      try {
        await termination;
      } catch {
        failureReason ??= failure();
      }
      finish();
    });
    options.signal.addEventListener('abort', abort, { once: true });
    const timer = setTimeout(terminate, options.deadlineMs);
    if (options.signal.aborted) terminate();
  });
  void rendered.catch(() => undefined);
  return rendered;
}
