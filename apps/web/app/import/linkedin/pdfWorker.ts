import type * as Pdfjs from 'pdfjs-dist/legacy/build/pdf.mjs';

export type PdfjsModule = typeof Pdfjs;

export interface PdfWorkerHandle {
  readonly port: Worker;
  terminate(): void;
}

export interface PdfReader {
  readonly pdfjs: PdfjsModule;
  startWorker(): PdfWorkerHandle;
}

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

export async function loadPdfReader(): Promise<PdfReader> {
  // Nuxt's server build sets import.meta.client false and drops this block.
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
