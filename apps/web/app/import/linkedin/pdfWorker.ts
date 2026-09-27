/**
 * Loads pdf.js 6.3.289 (legacy build) for the LinkedIn import page and starts
 * its dedicated module worker (docs/design/linkedin-import.md, "Reading the
 * file"; ADR 0023 decision 3). Only the import page calls this, in the
 * browser; the `import.meta.client` guard keeps pdf.js out of the server
 * bundle.
 */
import type * as Pdfjs from 'pdfjs-dist/legacy/build/pdf.mjs';

export type PdfjsModule = typeof Pdfjs;

/** One worker for one pick; `terminate` ends it for good. */
export interface PdfWorkerHandle {
  readonly port: Worker;
  terminate(): void;
}

export interface PdfReader {
  readonly pdfjs: PdfjsModule;
  /** A fresh worker: every pick gets its own, never reused. */
  startWorker(): PdfWorkerHandle;
}

/**
 * Whether this browser can run the import: a module worker and what pdf.js's
 * legacy build needs (Chrome 125+, Firefox ESR, Safari 18+). `URL.parse`
 * ships in Chrome 126, Firefox 126, and Safari 18, so it stands in for that
 * floor without starting a worker.
 */
export function supportsImport(scope: typeof globalThis = globalThis): boolean {
  return typeof scope.Worker === 'function'
    && typeof scope.Blob === 'function'
    && typeof scope.Blob.prototype.arrayBuffer === 'function'
    && typeof scope.ReadableStream === 'function'
    && typeof (scope.URL as { parse?: unknown } | undefined)?.parse
    === 'function';
}

function startWorker(url: string): PdfWorkerHandle {
  const port = new Worker(url, { type: 'module' });
  let ended = false;
  return {
    port,
    terminate() {
      if (ended) return;
      ended = true;
      port.terminate();
    },
  };
}

/**
 * Loads the pdf.js API and the URL of its worker script, a same-origin
 * `/_nuxt/` file that `worker-src 'self'` allows. It passes no URL to pdf.js
 * itself, so pdf.js has nothing to fetch.
 */
export async function loadPdfReader(): Promise<PdfReader> {
  // The server build replaces import.meta.client with false and drops this
  // block, dynamic imports included.
  if (import.meta.client) {
    const [pdfjs, worker] = await Promise.all([
      import('pdfjs-dist/legacy/build/pdf.mjs'),
      import('pdfjs-dist/legacy/build/pdf.worker.min.mjs?url'),
    ]);
    const workerUrl = worker.default;
    return { pdfjs, startWorker: () => startWorker(workerUrl) };
  }
  throw new Error('pdf.js loads only in the browser');
}
