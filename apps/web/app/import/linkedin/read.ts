/**
 * Reads a picked PDF with pdf.js and returns each page's text items, or the
 * check it failed (docs/design/linkedin-import.md, "Reading the file"; ADR
 * 0064 decisions 3 and 5). Every limit counts bytes read and items received,
 * never `File.size` or a value the file declares. The worker is terminated
 * when the read ends, however it ends, so each pick needs a fresh one.
 */
import type { TextItemLike } from './lines';
import type { PdfjsModule, PdfWorkerHandle } from './pdfWorker';

export const MAX_FILE_BYTES = 4 * 1024 * 1024;
export const HEADER_WINDOW_BYTES = 1024;
export const MAX_PAGES = 20;
export const MAX_TEXT_CHARS = 100_000;
export const MAX_TEXT_ITEMS = 20_000;
export const TIME_LIMIT_MS = 15_000;
/** Share of unreadable characters above which the text is unreadable. */
export const MAX_UNREADABLE_SHARE = 0.01;

export type ReadFailure
  = | 'notPdf'
    | 'unreadable'
    | 'encrypted'
    | 'tooLarge'
    | 'tooManyPages'
    | 'timedOut'
    | 'stopped';

export type ReadResult
  = | { ok: true; pages: TextItemLike[][] }
    | { ok: false; reason: ReadFailure };

export interface ReadOptions {
  readonly pdfjs: PdfjsModule;
  /**
   * The worker for this pick. The browser always passes one; Node tests omit
   * it and pdf.js runs in-process.
   */
  readonly worker?: PdfWorkerHandle;
  /** Aborting it stops the read (the page's Stop button). */
  readonly signal?: AbortSignal;
  readonly timeLimitMs?: number;
  /** Called after each page with the pages read and the page count. */
  readonly onProgress?: (pagesRead: number, pageCount: number) => void;
}

/** The `getDocument` options the design fixes; `data` is added per read. */
export const DOCUMENT_OPTIONS = Object.freeze({
  useWasm: false,
  enableXfa: false,
  isOffscreenCanvasSupported: false,
  isImageDecoderSupported: false,
  useSystemFonts: false,
  disableFontFace: true,
  stopAtErrors: true,
});

class ReadStop extends Error {
  constructor(readonly reason: ReadFailure) {
    super(reason);
  }
}

const PDF_HEADER = [0x25, 0x50, 0x44, 0x46, 0x2d]; // %PDF-

function hasPdfHeader(bytes: Uint8Array): boolean {
  const end = Math.min(bytes.length, HEADER_WINDOW_BYTES) - PDF_HEADER.length;
  for (let start = 0; start <= end; start += 1) {
    if (PDF_HEADER.every((byte, i) => bytes[start + i] === byte)) return true;
  }
  return false;
}

/**
 * U+FFFD, private use (U+E000 to U+F8FF), and controls other than line
 * breaks mark text that has no usable Unicode map.
 */
// eslint-disable-next-line no-control-regex
const UNREADABLE = /[\uFFFD\uE000-\uF8FF\0-\t\v\f\x0E-\x1F\x7F-\x9F]/gu;

export function unreadableShare(texts: readonly string[]): number {
  let total = 0;
  let bad = 0;
  for (const text of texts) {
    total += [...text].length;
    bad += text.match(UNREADABLE)?.length ?? 0;
  }
  return total === 0 ? 0 : bad / total;
}

interface PdfTextItem {
  str: string;
  transform: number[];
  width: number;
  height: number;
}

function isTextItem(item: unknown): item is PdfTextItem {
  return typeof item === 'object' && item !== null
    && typeof (item as { str?: unknown }).str === 'string'
    && Array.isArray((item as { transform?: unknown }).transform);
}

/** Reads one file; resolves with the text items or the failed check. */
export async function readLinkedInPdf(
  file: Blob,
  options: ReadOptions,
): Promise<ReadResult> {
  const { pdfjs, worker, signal, onProgress } = options;
  const timeLimitMs = options.timeLimitMs ?? TIME_LIMIT_MS;
  let loadingTask: ReturnType<PdfjsModule['getDocument']> | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let onAbort: (() => void) | undefined;
  let passwordAsked = false;

  // Rejects the moment the time limit passes or Stop is pressed; every step
  // races it, so a stuck parse cannot hold the page.
  const halt = new Promise<never>((_, reject) => {
    timer = setTimeout(() => reject(new ReadStop('timedOut')), timeLimitMs);
    onAbort = () => reject(new ReadStop('stopped'));
    if (signal?.aborted === true) onAbort();
    signal?.addEventListener('abort', onAbort, { once: true });
  });
  halt.catch(() => {});
  const guard = <T>(step: Promise<T>): Promise<T> =>
    Promise.race([step, halt]);

  try {
    const bytes = new Uint8Array(
      await guard(file.slice(0, MAX_FILE_BYTES + 1).arrayBuffer()),
    );
    if (bytes.length > MAX_FILE_BYTES) return fail('tooLarge');
    if (!hasPdfHeader(bytes)) return fail('notPdf');

    if (worker !== undefined) {
      pdfjs.GlobalWorkerOptions.workerPort = worker.port;
    }
    loadingTask = pdfjs.getDocument({ ...DOCUMENT_OPTIONS, data: bytes });
    loadingTask.onPassword = (
      update: (password: string | Error) => void,
    ) => {
      passwordAsked = true;
      update(new Error('password required'));
    };
    const doc = await guard(loadingTask.promise);

    const pageCount = doc.numPages;
    if (!Number.isInteger(pageCount) || pageCount < 1) {
      return fail('unreadable');
    }
    if (pageCount > MAX_PAGES) return fail('tooManyPages');

    const pages: TextItemLike[][] = [];
    const texts: string[] = [];
    let chars = 0;
    let items = 0;
    for (let number = 1; number <= pageCount; number += 1) {
      const page = await guard(doc.getPage(number));
      const [left, bottom] = page.view;
      const reader = page.streamTextContent().getReader();
      const pageItems: TextItemLike[] = [];
      try {
        for (;;) {
          const chunk = await guard(reader.read());
          if (chunk.done) break;
          for (const item of chunk.value.items as unknown[]) {
            items += 1;
            if (items > MAX_TEXT_ITEMS) throw new ReadStop('tooLarge');
            if (!isTextItem(item)) continue;
            chars += [...item.str].length;
            if (chars > MAX_TEXT_CHARS) throw new ReadStop('tooLarge');
            const [a, b, c, d, x, y] = item.transform;
            pageItems.push({
              str: item.str,
              transform: [a!, b!, c!, d!, x! - left!, y! - bottom!],
              width: item.width,
              height: item.height,
            });
            texts.push(item.str);
          }
        }
      } finally {
        reader.cancel().catch(() => {});
      }
      pages.push(pageItems);
      onProgress?.(number, pageCount);
    }

    if (unreadableShare(texts) > MAX_UNREADABLE_SHARE) {
      return fail('unreadable');
    }
    return { ok: true, pages };
  } catch (error) {
    if (error instanceof ReadStop) return fail(error.reason);
    if (
      passwordAsked
      || (error as { name?: unknown } | null)?.name === 'PasswordException'
    ) {
      return fail('encrypted');
    }
    return fail('unreadable');
  } finally {
    clearTimeout(timer);
    if (onAbort !== undefined) signal?.removeEventListener('abort', onAbort);
    // Terminate first: a hostile file can keep the worker too busy to answer
    // the destroy message.
    worker?.terminate();
    loadingTask?.destroy().catch(() => {});
  }
}

function fail(reason: ReadFailure): ReadResult {
  return { ok: false, reason };
}
