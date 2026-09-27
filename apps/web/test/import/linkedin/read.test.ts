// @vitest-environment node
// The read checks and hostile files of docs/design/linkedin-import.md
// ("Reading the file", "Tests") and ADR 0064 decisions 3 and 5.
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, join } from 'node:path';

import * as pdfjs from 'pdfjs-dist/legacy/build/pdf.mjs';
import { describe, expect, it, vi } from 'vitest';

import {
  DOCUMENT_OPTIONS,
  MAX_FILE_BYTES,
  readLinkedInPdf,
  unreadableShare,
} from '../../../app/import/linkedin/read';
import type {
  PdfjsModule,
  PdfWorkerHandle,
} from '../../../app/import/linkedin/pdfWorker';
import {
  buildPdf,
  pdfBlob,
  stream,
  textPdf,
} from './helpers/pdfBuilder';

const encoder = new TextEncoder();

function line(text: string, y = 700, size = 12) {
  return { text, x: 72, y, size };
}

function fakeWorker(): PdfWorkerHandle & { terminated: number } {
  const handle = {
    port: {} as Worker,
    terminated: 0,
    terminate() {
      handle.terminated += 1;
    },
  };
  return handle;
}

/**
 * A pdf.js stand-in that records `getDocument` and whose loading task never
 * settles, for the time limit and Stop.
 */
function stalledPdfjs() {
  const calls: Record<string, unknown>[] = [];
  const destroy = vi.fn(async () => {});
  const fake = {
    GlobalWorkerOptions: { workerPort: null as unknown },
    getDocument(params: Record<string, unknown>) {
      calls.push(params);
      return {
        promise: new Promise(() => {}),
        destroy,
        onPassword: null,
      };
    },
  };
  return { fake: fake as unknown as PdfjsModule, calls, destroy };
}

type Options = Parameters<typeof readLinkedInPdf>[1];
const read = (bytes: Uint8Array, extra: Partial<Options> = {}) =>
  readLinkedInPdf(pdfBlob(bytes), { pdfjs, ...extra });

describe('readLinkedInPdf on well-formed files', () => {
  it('returns each page\'s text items relative to the page', async () => {
    const bytes = textPdf(
      [[line('Sample Person', 726.5, 26)], [line('Page two', 700)]],
      { mediaBox: '[10 20 622 812]' },
    );
    const progress: [number, number][] = [];
    const result = await read(bytes, {
      onProgress: (n, m) => progress.push([n, m]),
    });
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.pages).toHaveLength(2);
    const first = result.pages[0]!.find((item) => item.str !== '')!;
    expect(first.str).toBe('Sample Person');
    expect(first.transform[3]).toBeCloseTo(26);
    // The media box starts at (10, 20), so positions shift by it.
    expect(first.transform[4]).toBeCloseTo(62);
    expect(first.transform[5]).toBeCloseTo(706.5);
    expect(progress).toEqual([[1, 2], [2, 2]]);
  });

  it('terminates the worker when the read succeeds', async () => {
    // The real pdf.js, with a stand-in GlobalWorkerOptions so the fake port
    // never reaches it; pdf.js then parses in this process.
    const wrapped = {
      GlobalWorkerOptions: { workerPort: null },
      getDocument: pdfjs.getDocument,
    } as unknown as PdfjsModule;
    const worker = fakeWorker();
    const result = await readLinkedInPdf(
      pdfBlob(textPdf([[line('Hello')]])),
      { pdfjs: wrapped, worker },
    );
    expect(result.ok).toBe(true);
    expect(worker.terminated).toBe(1);
  });
});

describe('getDocument options', () => {
  it('passes the data, the fixed options, and the worker port', async () => {
    const { fake, calls } = stalledPdfjs();
    const worker = fakeWorker();
    const controller = new AbortController();
    const pending = readLinkedInPdf(pdfBlob(textPdf([[line('x')]])), {
      pdfjs: fake,
      worker,
      signal: controller.signal,
    });
    await vi.waitFor(() => expect(calls).toHaveLength(1));
    controller.abort();
    expect(await pending).toEqual({ ok: false, reason: 'stopped' });
    const params = calls[0]!;
    expect(Object.keys(params).sort()).toEqual(
      [...Object.keys(DOCUMENT_OPTIONS), 'data'].sort(),
    );
    expect(params).toMatchObject({
      useWasm: false,
      enableXfa: false,
      isOffscreenCanvasSupported: false,
      isImageDecoderSupported: false,
      useSystemFonts: false,
      disableFontFace: true,
      stopAtErrors: true,
    });
    for (const key of ['url', 'cMapUrl', 'standardFontDataUrl', 'wasmUrl']) {
      expect(params).not.toHaveProperty(key);
    }
    expect(params.data).toBeInstanceOf(Uint8Array);
    expect(
      (fake as unknown as { GlobalWorkerOptions: { workerPort: unknown } })
        .GlobalWorkerOptions.workerPort,
    ).toBe(worker.port);
    expect(worker.terminated).toBe(1);
  });
});

describe('size and header checks on bytes read', () => {
  it('rejects more than 4 MiB of bytes read', async () => {
    const { fake, calls } = stalledPdfjs();
    const bytes = new Uint8Array(MAX_FILE_BYTES + 1);
    bytes.set(encoder.encode('%PDF-1.4\n'));
    const worker = fakeWorker();
    const result = await readLinkedInPdf(pdfBlob(bytes), {
      pdfjs: fake,
      worker,
    });
    expect(result).toEqual({ ok: false, reason: 'tooLarge' });
    expect(calls).toHaveLength(0);
    expect(worker.terminated).toBe(1);
  });

  it('ignores a File.size that understates the bytes', async () => {
    const { fake } = stalledPdfjs();
    const bytes = new Uint8Array(MAX_FILE_BYTES + 10);
    bytes.set(encoder.encode('%PDF-1.4\n'));
    const real = pdfBlob(bytes);
    const liar = {
      size: 100,
      slice: (start: number, end: number) => real.slice(start, end),
    } as unknown as Blob;
    const result = await readLinkedInPdf(liar, { pdfjs: fake });
    expect(result).toEqual({ ok: false, reason: 'tooLarge' });
  });

  it('reads at most 4 MiB + 1 bytes', async () => {
    const { fake } = stalledPdfjs();
    const real = pdfBlob(new Uint8Array(MAX_FILE_BYTES * 3));
    const slice = vi.fn((start: number, end: number) => real.slice(start, end));
    await readLinkedInPdf({ size: 0, slice } as unknown as Blob, {
      pdfjs: fake,
    });
    expect(slice).toHaveBeenCalledWith(0, MAX_FILE_BYTES + 1);
  });

  it('accepts exactly 4 MiB', async () => {
    const { fake, calls } = stalledPdfjs();
    const bytes = new Uint8Array(MAX_FILE_BYTES);
    bytes.set(encoder.encode('%PDF-1.4\n'));
    const controller = new AbortController();
    const pending = readLinkedInPdf(pdfBlob(bytes), {
      pdfjs: fake,
      signal: controller.signal,
    });
    await vi.waitFor(() => expect(calls).toHaveLength(1));
    controller.abort();
    expect(await pending).toEqual({ ok: false, reason: 'stopped' });
  });

  it('needs %PDF- within the first 1,024 bytes', async () => {
    const { fake } = stalledPdfjs();
    const late = new Uint8Array(2048);
    late.set(encoder.encode('%PDF-1.4'), 1020);
    expect(await readLinkedInPdf(pdfBlob(late), { pdfjs: fake }))
      .toEqual({ ok: false, reason: 'notPdf' });
    const early = new Uint8Array(2048);
    early.set(encoder.encode('%PDF-1.4'), 1019);
    const controller = new AbortController();
    controller.abort();
    expect(await readLinkedInPdf(pdfBlob(early), {
      pdfjs: fake,
      signal: controller.signal,
    })).toEqual({ ok: false, reason: 'stopped' });
    expect(await read(encoder.encode('hello, not a pdf')))
      .toEqual({ ok: false, reason: 'notPdf' });
  });
});

describe('time limit and Stop', () => {
  it('stops at the time limit and ends the worker and task', async () => {
    const { fake, destroy } = stalledPdfjs();
    const worker = fakeWorker();
    const result = await readLinkedInPdf(pdfBlob(textPdf([[line('x')]])), {
      pdfjs: fake,
      worker,
      timeLimitMs: 50,
    });
    expect(result).toEqual({ ok: false, reason: 'timedOut' });
    expect(worker.terminated).toBe(1);
    expect(destroy).toHaveBeenCalledTimes(1);
  });

  it('stops when the signal aborts', async () => {
    const { fake, calls } = stalledPdfjs();
    const worker = fakeWorker();
    const controller = new AbortController();
    const pending = readLinkedInPdf(pdfBlob(textPdf([[line('x')]])), {
      pdfjs: fake,
      worker,
      signal: controller.signal,
    });
    await vi.waitFor(() => expect(calls).toHaveLength(1));
    controller.abort();
    expect(await pending).toEqual({ ok: false, reason: 'stopped' });
    expect(worker.terminated).toBe(1);
  });
});

describe('hostile files built in the test', () => {
  it('a truncated file is unreadable', async () => {
    const bytes = textPdf([[line('Sample Person')]]);
    expect(await read(bytes.slice(0, Math.floor(bytes.length / 3))))
      .toEqual({ ok: false, reason: 'unreadable' });
  });

  it('an encrypted file is rejected', async () => {
    const hex32 = 'A1'.repeat(32);
    const id = `<${'0F'.repeat(16)}>`;
    const bytes = buildPdf([
      '<< /Type /Catalog /Pages 2 0 R >>',
      '<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
      '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>',
      `<< /Filter /Standard /V 1 /R 2 /O <${hex32}> /U <${hex32}> /P -4 >>`,
    ], `/Root 1 0 R /Encrypt 4 0 R /ID [${id} ${id}]`);
    expect(await read(bytes)).toEqual({ ok: false, reason: 'encrypted' });
  });

  it('21 pages are too many, checked before any page is read', async () => {
    const pages = Array.from({ length: 21 }, (_, i) => [line(`Page ${i + 1}`)]);
    const progress = vi.fn();
    expect(await read(textPdf(pages), { onProgress: progress }))
      .toEqual({ ok: false, reason: 'tooManyPages' });
    expect(progress).not.toHaveBeenCalled();
  });

  it('20 pages are fine', async () => {
    const pages = Array.from({ length: 20 }, (_, i) => [line(`Page ${i + 1}`)]);
    const result = await read(textPdf(pages));
    expect(result.ok && result.pages.length).toBe(20);
  });

  it('text past 100,000 characters is too large', async () => {
    // pdf.js drops glyphs outside the page, so each line stays on it.
    const lines = Array.from({ length: 201 }, (_, i) =>
      line('A'.repeat(500), 785 - i * 3.8, 1));
    expect(await read(textPdf([lines])))
      .toEqual({ ok: false, reason: 'tooLarge' });
  });

  it('more than 20,000 text items is too large', async () => {
    // Alternating fonts keep pdf.js from merging neighbors into one item.
    const ops = Array.from({ length: 20_001 }, (_, i) =>
      `BT /F${(i % 2) + 1} 1 Tf 1 0 0 1 ${(i % 500) + 1} `
      + `${(Math.floor(i / 500) * 15) + 5} Tm (A) Tj ET`)
      .join('\n');
    const bytes = buildPdf([
      '<< /Type /Catalog /Pages 2 0 R >>',
      '<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
      '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] '
      + '/Resources << /Font << /F1 5 0 R /F2 6 0 R >> >> /Contents 4 0 R >>',
      stream(ops, true),
      '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',
      '<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>',
    ]);
    expect(await read(bytes)).toEqual({ ok: false, reason: 'tooLarge' });
  });

  it('a font without a Unicode map gives unreadable text', async () => {
    const hex = Array.from({ length: 40 }, (_, i) =>
      (0xE001 + i).toString(16).toUpperCase().padStart(4, '0')).join('');
    const bytes = buildPdf([
      '<< /Type /Catalog /Pages 2 0 R >>',
      '<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
      '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] '
      + '/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>',
      stream(`BT /F1 12 Tf 72 700 Td <${hex}> Tj ET`),
      '<< /Type /Font /Subtype /Type0 /BaseFont /NoMap '
      + '/Encoding /Identity-H /DescendantFonts [6 0 R] >>',
      '<< /Type /Font /Subtype /CIDFontType2 /BaseFont /NoMap '
      + '/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) '
      + '/Supplement 0 >> /FontDescriptor 7 0 R /CIDToGIDMap /Identity >>',
      '<< /Type /FontDescriptor /FontName /NoMap /Flags 4 '
      + '/FontBBox [0 0 1000 1000] /ItalicAngle 0 /Ascent 800 '
      + '/Descent -200 /CapHeight 700 /StemV 80 >>',
    ]);
    expect(await read(bytes)).toEqual({ ok: false, reason: 'unreadable' });
  });

  it('an OpenAction JavaScript action is never run', async () => {
    const marker = '__linkedinImportOpenAction';
    const bytes = textPdf([[line('Sample Person')]], {
      extraCatalog: '/OpenAction 6 0 R',
      extraObjects: [
        `<< /Type /Action /S /JavaScript /JS (globalThis.${marker} = 1;) >>`,
      ],
    });
    const result = await read(bytes);
    expect(result.ok).toBe(true);
    expect((globalThis as Record<string, unknown>)[marker]).toBeUndefined();
  });

  it('an XFA form is not rendered as XFA', async () => {
    const bytes = textPdf([[line('Sample Person')]], {
      extraCatalog: '/AcroForm << /Fields [] /XFA 6 0 R >>',
      extraObjects: [stream('<xdp:xdp xmlns:xdp="http://ns.adobe.com/xdp/"><template><subform><draw><value><text>xfa text</text></value></draw></subform></template></xdp:xdp>')],
    });
    const result = await read(bytes);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    const texts = result.pages.flat().map((item) => item.str).join(' ');
    expect(texts).toContain('Sample Person');
    expect(texts).not.toContain('xfa text');
  });

  it('a Type 4 PostScript function does not stop the text', async () => {
    const bytes = buildPdf([
      '<< /Type /Catalog /Pages 2 0 R >>',
      '<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
      '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] '
      + '/Resources << /Font << /F1 5 0 R >> /Shading << /Sh1 6 0 R >> >> '
      + '/Contents 4 0 R >>',
      stream('/Sh1 sh BT /F1 12 Tf 72 700 Td (Sample Person) Tj ET'),
      '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',
      '<< /ShadingType 1 /ColorSpace /DeviceRGB /Function 7 0 R >>',
      {
        dict: '/FunctionType 4 /Domain [0 1 0 1] /Range [0 1 0 1 0 1]',
        stream: encoder.encode('{ pop pop 0 0 0 }'),
      },
    ]);
    const result = await read(bytes);
    expect(result.ok).toBe(true);
  });

  it('a stream inflating far past its size settles the read', async () => {
    // 64 MiB of spaces deflate to about 64 KiB. In this in-process read the
    // parse shares the test's thread, so only settling is asserted; the time
    // limit and terminate paths are proven with the stalled stand-in above,
    // and the browser proof covers a real worker on a stream past 1 GiB.
    const spaces = ' '.repeat(64 * 1024 * 1024);
    const bytes = buildPdf([
      '<< /Type /Catalog /Pages 2 0 R >>',
      '<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
      '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] '
      + '/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>',
      stream(`${spaces}BT /F1 12 Tf 72 700 Td (late) Tj ET`, true),
      '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',
    ]);
    expect(bytes.length).toBeLessThan(256 * 1024);
    const result = await read(bytes, { timeLimitMs: 100 });
    expect(result.ok || result.reason === 'timedOut').toBe(true);
  }, 30_000);

  it('deeply nested objects do not crash the read', async () => {
    const depth = 100_000;
    const nested = `${'['.repeat(depth)}${']'.repeat(depth)}`;
    const bytes = textPdf([[line('Sample Person')]], {
      extraCatalog: '/Nested 6 0 R',
      extraObjects: [nested],
    });
    const result = await read(bytes);
    expect(result.ok || result.reason === 'unreadable').toBe(true);
  });
});

describe('unreadableShare', () => {
  it('counts U+FFFD, private use, and controls other than line breaks', () => {
    expect(unreadableShare(['abc\n\r'])).toBe(0);
    expect(unreadableShare(['a\uFFFD'])).toBe(0.5);
    expect(unreadableShare(['a\uE000'])).toBe(0.5);
    expect(unreadableShare(['a\u0007'])).toBe(0.5);
    expect(unreadableShare(['a\t'])).toBe(0.5);
    expect(unreadableShare([])).toBe(0);
  });

  it('keeps Vietnamese letters readable', () => {
    expect(unreadableShare(['Nguyễn Văn Mẫu'])).toBe(0);
  });
});

describe('the pinned pdf.js files', () => {
  const require = createRequire(import.meta.url);
  const root = dirname(require.resolve('pdfjs-dist/package.json'));

  it('is exactly 6.3.289', () => {
    const pkg = JSON.parse(readFileSync(join(root, 'package.json'), 'utf8'));
    expect(pkg.version).toBe('6.3.289');
  });

  it.each([
    'legacy/build/pdf.mjs',
    'legacy/build/pdf.worker.min.mjs',
  ])('%s calls no eval and only core-js\'s global-object Function', (file) => {
    const source = readFileSync(join(root, file), 'utf8');
    expect(source.match(/(?<![\w$.])eval\s*\(/g)).toBeNull();
    const call = /(?<![\w$.])(?:new\s+)?Function\s*\([^)]{0,40}\)/g;
    const calls = source.match(call) ?? [];
    expect(calls.map((call) => call.replace(/"/g, '\''))).toEqual([
      'Function(\'return this\')',
    ]);
  });
});
