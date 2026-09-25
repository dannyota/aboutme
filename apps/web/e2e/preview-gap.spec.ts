import {
  type Browser,
  chromium,
  expect,
  type Page,
  test,
  webkit,
} from '@playwright/test';
import { SAMPLES, type SampleLanguage } from '@aboutme/schema/samples';
import { getDocument } from 'pdfjs-dist/legacy/build/pdf.mjs';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { denyExternalRequests, waitForImages } from './support';

// Measures how far the editor's paged preview drifts from the PDF for every
// gallery sample, in both languages. The preview is approximate and the PDF
// is authoritative (docs/design/templates/print.md §1); this check reports
// the size of that gap rather than enforcing pixel or text equality. It
// fails when a case's drift exceeds preview-gap-expected.json, when the
// preview's page count differs from the PDF's, or when a display scale
// changes the layout the preview measures at full scale.
//
// Comparison unit: words, extracted from the DOM (Range.getBoundingClientRect
// per word, clustered into visual lines) for the preview, and from pdfjs text
// content for the PDF. A word-level LCS alignment tells which words the two
// sides share; the boundaries between aligned words classify each
// disagreement as a line-break, a page-break, or a missing/extra run.
//
// Both sides are read the same way so that only layout differences count:
// - Words are compared case-folded, because the PDF text layer carries a
//   heading's CSS text-transform while DOM text does not.
// - A word splits after each hyphen, because the PDF text layer splits a
//   word that wraps at its hyphen while a DOM text node does not.
// - Words are ordered by flow (header, main column, sidebar), then page,
//   then line. The PDF has no flow structure, so its words take the flow
//   whose region they sit in, using the column split and header bottom the
//   preview's first page reports. Without this, a PDF line that crosses both
//   columns interleaves main and sidebar words and misaligns them.

const TYPICAL_ZOOM = 0.84;
const FULL_ZOOM = 1;
// The editor's display scale on a 390 px phone, as EditorPreview.vue's
// sheetZoom computes it. At this scale, text laid out at the scaled size
// would fall below WebKit's minimum font size and grow; the preview must
// still lay out at full size and match the PDF.
const PHONE_ZOOM = (390 - 32) / (210 / 25.4 * 96);
// Full zoom runs first so a settle failure at the typical (non-1) zoom, and
// not at full zoom, points at display scale rather than the fixture itself.
const ZOOM_VARIANTS = [
  ['full', FULL_ZOOM],
  ['typical', TYPICAL_ZOOM],
  ['phone', PHONE_ZOOM],
] as const;
type ZoomLabel = (typeof ZOOM_VARIANTS)[number][0];
type Engine = 'chromium-normal' | 'webkit';
type Flow = 'header' | 'main' | 'sidebar';

const FLOW_RANK: Readonly<Record<Flow, number>> = {
  header: 0,
  main: 1,
  sidebar: 2,
};

// A word, or the part of a word up to and including a run of hyphens.
const WORD_PATTERN = '[^\\s-]*-+|[^\\s-]+';

// Where the flows sit on a page, as fractions of the page width so that
// preview zoom and PDF points compare directly. bodyTop is measured on the
// first page only; later pages carry no header.
interface FlowGeometry {
  readonly bodyTop: number;
  readonly splitX: number | null;
}

interface EntryStart {
  readonly sectionKey: string;
  readonly headerText: string;
}

interface FlatWord {
  readonly text: string;
  readonly flow: Flow;
  readonly page: number;
  readonly line: number;
  readonly entryStart: EntryStart | null;
}

interface LineSummary {
  readonly first: string;
  readonly last: string;
  readonly count: number;
}

interface PageLines {
  readonly page: number;
  readonly lines: readonly LineSummary[];
}

interface Extraction {
  readonly pages: readonly PageLines[];
  readonly words: readonly FlatWord[];
}

interface WordDiff {
  readonly kind: 'line-break' | 'page-break' | 'missing' | 'extra';
  readonly detail: string;
}

interface EntryPageStart {
  readonly sectionKey: string;
  readonly ordinal: number;
  readonly headerText: string;
  readonly previewPage: number;
  readonly pdfPage: number | null;
  readonly matches: boolean;
}

interface CaseCounts {
  readonly lineBreak: number;
  readonly pageBreak: number;
  readonly missing: number;
  readonly extra: number;
  readonly entryPageMismatches: number;
}

interface CaseResult {
  readonly sample: string;
  readonly lng: SampleLanguage;
  readonly engine: Engine;
  readonly zoomLabel: ZoomLabel;
  readonly zoomValue: number;
  readonly counts: CaseCounts;
  readonly firstDiffs: readonly WordDiff[];
  readonly entryPageStarts: readonly EntryPageStart[];
  readonly knownCauses: readonly string[];
  readonly previewPages: number;
  readonly pdfPages: number;
}

// A case that never rendered, never settled, or drifted past the committed
// expectation. Recorded instead of thrown, so one broken case never hides
// the other 39; the test still fails at the end if this list is non-empty.
interface DiagnosticSnapshot {
  readonly harnessDataset: Readonly<Record<string, string>> | null;
  readonly pagedResumeDataset: Readonly<Record<string, string>> | null;
  readonly resumePageCount: number;
  readonly visiblePageCount: number;
  readonly bodySnippet: string;
}

interface FailedCase {
  readonly sample: string;
  readonly lng: SampleLanguage;
  readonly engine: Engine;
  readonly zoomLabel: ZoomLabel;
  readonly zoomValue: number;
  readonly url: string;
  readonly error: string;
  readonly diagnostics: DiagnosticSnapshot | null;
}

type PreviewResult
  = | {
    readonly ok: true;
    readonly url: string;
    readonly extraction: Extraction;
    readonly geometry: FlowGeometry;
  }
  | {
    readonly ok: false;
    readonly url: string;
    readonly error: string;
    readonly diagnostics: DiagnosticSnapshot | null;
  };

// --- expectation file -------------------------------------------------

interface ExpectationMetrics {
  readonly maxLineBreak: number | null;
  readonly maxPageBreak: number | null;
  readonly maxMissing: number | null;
  readonly maxExtra: number | null;
  readonly maxEntryPageMismatches: number | null;
}

interface ExpectationFile {
  readonly version: number;
  readonly cases: Readonly<Record<string, ExpectationMetrics>>;
}

const expectationsPath = resolve(
  import.meta.dirname,
  'preview-gap-expected.json',
);
const expectations = JSON.parse(
  readFileSync(expectationsPath, 'utf8'),
) as ExpectationFile;

// Keyed without the display scale: every scale must lay out alike.
function caseKey(sample: string, lng: string, engine: string): string {
  return `${sample}|${lng}|${engine}`;
}

const METRIC_CHECKS: ReadonlyArray<
  readonly [keyof CaseCounts, keyof ExpectationMetrics]
> = [
  ['lineBreak', 'maxLineBreak'],
  ['pageBreak', 'maxPageBreak'],
  ['missing', 'maxMissing'],
  ['extra', 'maxExtra'],
  ['entryPageMismatches', 'maxEntryPageMismatches'],
];

function assertWithinExpectation(result: CaseResult): void {
  const key = caseKey(result.sample, result.lng, result.engine);
  const metrics = expectations.cases[key];
  for (const [countKey, limitKey] of METRIC_CHECKS) {
    const limit = metrics?.[limitKey] ?? null;
    if (limit === null) continue;
    expect(
      result.counts[countKey],
      `${key} ${countKey} exceeds the committed expectation`,
    ).toBeLessThanOrEqual(limit);
  }
}

// --- word alignment (LCS) ----------------------------------------------

function compareKey(text: string): string {
  return text.normalize('NFC').toUpperCase();
}

function orderByFlow<T extends { readonly flow: Flow }>(
  words: readonly T[],
): T[] {
  // Array.prototype.sort is stable, so page and line order hold per flow.
  return [...words].sort((a, b) => FLOW_RANK[a.flow] - FLOW_RANK[b.flow]);
}

function firstOfEachKind(
  diffs: readonly WordDiff[],
  limit: number,
): WordDiff[] {
  const seen = new Map<WordDiff['kind'], number>();
  return diffs.filter((diff) => {
    const count = seen.get(diff.kind) ?? 0;
    seen.set(diff.kind, count + 1);
    return count < limit;
  });
}

function alignIndices(
  a: readonly string[],
  b: readonly string[],
): ReadonlyArray<readonly [number, number]> {
  const n = a.length;
  const m = b.length;
  const dp: number[][] = Array.from(
    { length: n + 1 },
    () => new Array<number>(m + 1).fill(0),
  );
  for (let i = n - 1; i >= 0; i -= 1) {
    for (let j = m - 1; j >= 0; j -= 1) {
      dp[i]![j] = a[i] === b[j]
        ? dp[i + 1]![j + 1]! + 1
        : Math.max(dp[i + 1]![j]!, dp[i]![j + 1]!);
    }
  }
  const pairs: Array<readonly [number, number]> = [];
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      pairs.push([i, j]);
      i += 1;
      j += 1;
    } else if (dp[i + 1]![j]! >= dp[i]![j + 1]!) {
      i += 1;
    } else {
      j += 1;
    }
  }
  return pairs;
}

// --- classification ------------------------------------------------------

function collectRuns(
  words: readonly FlatWord[],
  matched: ReadonlySet<number>,
  kind: 'missing' | 'extra',
  diffs: WordDiff[],
): number {
  let count = 0;
  let start = -1;
  for (let i = 0; i <= words.length; i += 1) {
    const isGap = i < words.length && !matched.has(i);
    if (isGap && start === -1) start = i;
    if (!isGap && start !== -1) {
      const run = words.slice(start, i);
      const first = run[0]!;
      const last = run.at(-1)!;
      const side = kind === 'missing' ? 'pdf-only' : 'preview-only';
      diffs.push({
        kind,
        detail: `page ${first.page} (${side}): "${first.text}" .. `
          + `"${last.text}" (${run.length} word`
          + `${run.length === 1 ? '' : 's'})`,
      });
      count += 1;
      start = -1;
    }
  }
  return count;
}

function classify(
  preview: Extraction,
  pdf: Extraction,
  pairs: ReadonlyArray<readonly [number, number]>,
): { diffs: WordDiff[]; counts: Omit<CaseCounts, 'entryPageMismatches'> } {
  const matchedPreview = new Set(pairs.map(([previewIndex]) => previewIndex));
  const matchedPdf = new Set(pairs.map(([, pdfIndex]) => pdfIndex));
  const diffs: WordDiff[] = [];
  const extra = collectRuns(preview.words, matchedPreview, 'extra', diffs);
  const missing = collectRuns(pdf.words, matchedPdf, 'missing', diffs);

  let lineBreak = 0;
  let pageBreak = 0;
  for (let k = 1; k < pairs.length; k += 1) {
    const [pi0, pj0] = pairs[k - 1]!;
    const [pi1, pj1] = pairs[k]!;
    const previewWord0 = preview.words[pi0]!;
    const previewWord1 = preview.words[pi1]!;
    const pdfWord0 = pdf.words[pj0]!;
    const pdfWord1 = pdf.words[pj1]!;
    const previewPageChanged = previewWord0.page !== previewWord1.page;
    const pdfPageChanged = pdfWord0.page !== pdfWord1.page;
    if (previewPageChanged !== pdfPageChanged) {
      pageBreak += 1;
      diffs.push({
        kind: 'page-break',
        detail: `"${previewWord0.text}" -> "${previewWord1.text}": `
          + `preview page ${previewWord0.page}->${previewWord1.page}, `
          + `pdf page ${pdfWord0.page}->${pdfWord1.page}`,
      });
      continue;
    }
    const previewLineChanged = previewWord0.line !== previewWord1.line;
    const pdfLineChanged = pdfWord0.line !== pdfWord1.line;
    if (previewLineChanged !== pdfLineChanged) {
      lineBreak += 1;
      diffs.push({
        kind: 'line-break',
        detail: `"${previewWord0.text}" -> "${previewWord1.text}" on `
          + `page ${previewWord0.page}`,
      });
    }
  }

  return { diffs, counts: { lineBreak, pageBreak, missing, extra } };
}

function entryPageStarts(
  preview: Extraction,
  pdf: Extraction,
  pairs: ReadonlyArray<readonly [number, number]>,
): EntryPageStart[] {
  const previewToPdf = new Map<number, number>(pairs);
  const ordinals = new Map<string, number>();
  const starts: EntryPageStart[] = [];
  preview.words.forEach((word, index) => {
    if (word.entryStart === null) return;
    const key = word.entryStart.sectionKey;
    const ordinal = ordinals.get(key) ?? 0;
    ordinals.set(key, ordinal + 1);
    const pdfIndex = previewToPdf.get(index);
    const pdfPage = pdfIndex === undefined ? null : pdf.words[pdfIndex]!.page;
    starts.push({
      sectionKey: key,
      ordinal,
      headerText: word.entryStart.headerText,
      previewPage: word.page,
      pdfPage,
      matches: pdfPage === word.page,
    });
  });
  return starts;
}

function causesFor(engine: Engine): string[] {
  const causes = [
    'continuous-vs-paged-pagination-model',
    'screen-vs-print-css',
  ];
  if (engine === 'webkit') causes.push('webkit-vs-chromium-font-rendering');
  return causes;
}

// --- PDF text extraction (pdfjs) -----------------------------------------

interface RawWord {
  readonly text: string;
  readonly top: number;
  readonly left: number;
  readonly bottom: number;
}

// A PDF word with its position as a fraction of the page width, used to
// assign it a flow once the preview reports the flow geometry.
interface PdfWord extends RawWord {
  readonly x: number;
  readonly centerY: number;
}

interface PdfPage {
  readonly words: readonly PdfWord[];
}

function clusterRawLines<T extends RawWord>(raw: readonly T[]): T[][] {
  const sorted = [...raw].sort((a, b) => a.top - b.top || a.left - b.left);
  const lines: T[][] = [];
  let anchor = Number.NaN;
  for (const word of sorted) {
    const tolerance = Math.max(3, (word.bottom - word.top) * 0.6);
    const current = lines.at(-1);
    if (current !== undefined && Math.abs(anchor - word.top) <= tolerance) {
      current.push(word);
    } else {
      lines.push([word]);
      anchor = word.top;
    }
  }
  for (const line of lines) line.sort((a, b) => a.left - b.left);
  return lines;
}

async function readPdfPages(pdfBytes: Buffer): Promise<PdfPage[]> {
  const loadingTask = getDocument({
    data: new Uint8Array(pdfBytes),
    isImageDecoderSupported: false,
    isOffscreenCanvasSupported: false,
    useSystemFonts: false,
  });
  const pattern = new RegExp(WORD_PATTERN, 'gu');
  try {
    const document = await loadingTask.promise;
    const pages: PdfPage[] = [];
    for (let n = 1; n <= document.numPages; n += 1) {
      const pdfPage = await document.getPage(n);
      const [viewLeft, , viewRight, viewTop] = pdfPage.view;
      const pageWidth = viewRight! - viewLeft!;
      const content = await pdfPage.getTextContent();
      const words: PdfWord[] = [];
      for (const item of content.items) {
        if (!('str' in item) || item.str.trim() === '') continue;
        const transform = item.transform;
        const top = -transform[5]!;
        const height = item.height > 0
          ? item.height
          : Math.abs(transform[3]!) || 10;
        for (const match of item.str.matchAll(pattern)) {
          if (match.index === undefined) continue;
          const fraction = match.index / Math.max(item.str.length, 1);
          const left = transform[4]! + fraction * item.width;
          words.push({
            text: match[0],
            top,
            left,
            bottom: top + height,
            x: (left - viewLeft!) / pageWidth,
            centerY: (viewTop! - transform[5]! - (height / 2)) / pageWidth,
          });
        }
      }
      pages.push({ words });
    }
    return pages;
  } finally {
    await loadingTask.destroy();
  }
}

function pdfFlow(word: PdfWord, page: number, geometry: FlowGeometry): Flow {
  if (page === 0 && word.centerY < geometry.bodyTop) return 'header';
  if (geometry.splitX !== null && word.x >= geometry.splitX) return 'sidebar';
  return 'main';
}

function buildPdfExtraction(
  pdfPages: readonly PdfPage[],
  geometry: FlowGeometry,
): Extraction {
  const pages: PageLines[] = [];
  const words: FlatWord[] = [];
  let lineOrdinal = 0;
  pdfPages.forEach((pdfPage, pageIndex) => {
    const lineSummaries: LineSummary[] = [];
    for (const flow of ['header', 'main', 'sidebar'] as const) {
      const flowWords = pdfPage.words.filter(
        (word) => pdfFlow(word, pageIndex, geometry) === flow,
      );
      for (const line of clusterRawLines(flowWords)) {
        lineSummaries.push({
          first: line[0]!.text,
          last: line.at(-1)!.text,
          count: line.length,
        });
        for (const word of line) {
          words.push({
            text: word.text,
            flow,
            page: pageIndex,
            line: lineOrdinal,
            entryStart: null,
          });
        }
        lineOrdinal += 1;
      }
    }
    pages.push({ page: pageIndex, lines: lineSummaries });
  });
  return { pages, words: orderByFlow(words) };
}

// --- preview extraction (DOM) --------------------------------------------

interface BrowserExtraction {
  pages: { page: number; lines: LineSummary[] }[];
  words: {
    text: string;
    flow: Flow;
    page: number;
    line: number;
    entryStart: EntryStart | null;
  }[];
  geometry: FlowGeometry;
}

// Runs inside the page via page.evaluate: no reference to anything outside
// this function body survives serialization, so every helper it needs is
// declared inline, and the word pattern arrives as an argument.
function browserExtract(wordPattern: string): BrowserExtraction {
  interface Word {
    text: string;
    top: number;
    left: number;
    bottom: number;
  }

  function collectWords(container: Element): Word[] {
    const out: Word[] = [];
    const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT);
    let node = walker.nextNode();
    while (node !== null) {
      const text = node.textContent ?? '';
      for (const match of text.matchAll(new RegExp(wordPattern, 'gu'))) {
        if (match.index === undefined) continue;
        const range = document.createRange();
        range.setStart(node, match.index);
        range.setEnd(node, match.index + match[0].length);
        const rect = range.getBoundingClientRect();
        if (rect.width === 0 && rect.height === 0) continue;
        out.push({
          text: match[0],
          top: rect.top + (rect.height / 2),
          left: rect.left,
          bottom: rect.bottom,
        });
      }
      node = walker.nextNode();
    }
    return out;
  }

  function clusterLines(raw: Word[]): Word[][] {
    const sorted = [...raw].sort((a, b) => a.top - b.top || a.left - b.left);
    const lines: Word[][] = [];
    let anchor = Number.NaN;
    for (const word of sorted) {
      const tolerance = Math.max(3, (word.bottom - word.top) * 0.6);
      const current = lines.at(-1);
      if (current !== undefined && Math.abs(anchor - word.top) <= tolerance) {
        current.push(word);
      } else {
        lines.push([word]);
        anchor = word.top;
      }
    }
    for (const line of lines) line.sort((a, b) => a.left - b.left);
    return lines;
  }

  interface Block {
    el: Element;
    flow: Flow;
    sectionKey: string | null;
    isEntry: boolean;
  }

  function blocksOf(pageEl: Element): Block[] {
    const blocks: Block[] = [];
    const header = pageEl.querySelector(':scope > div > .pagination-header');
    if (header !== null) {
      blocks.push({
        el: header,
        flow: 'header',
        sectionKey: null,
        isEntry: false,
      });
    }
    const columns = pageEl.querySelector(
      ':scope > .layout-one-column, :scope > .layout-two-columns',
    );
    if (columns === null) return blocks;
    const flows = columns.classList.contains('layout-one-column')
      ? [columns]
      : Array.from(columns.querySelectorAll(
          ':scope > .resume-main, :scope > .resume-sidebar',
        ));
    for (const flow of flows) {
      const atomics = flow.querySelectorAll(':scope > .pagination-atomic');
      const flowName = flow.classList.contains('resume-sidebar')
        ? 'sidebar'
        : 'main';
      for (const atomic of atomics) {
        blocks.push({
          el: atomic,
          flow: flowName,
          sectionKey: atomic.getAttribute('data-section-key'),
          isEntry: atomic.getAttribute('data-block-kind') === 'entry',
        });
      }
    }
    return blocks;
  }

  function geometryOf(pageEl: Element | undefined): FlowGeometry {
    if (pageEl === undefined) return { bodyTop: 0, splitX: null };
    const page = pageEl.getBoundingClientRect();
    const header = pageEl.querySelector(':scope > div > .pagination-header');
    const columns = pageEl.querySelector(
      ':scope > .layout-one-column, :scope > .layout-two-columns',
    );
    const columnsTop = columns?.getBoundingClientRect().top ?? page.top;
    const headerBottom = header?.getBoundingClientRect().bottom ?? columnsTop;
    const main = columns?.querySelector(':scope > .resume-main') ?? null;
    const sidebar = columns?.querySelector(':scope > .resume-sidebar') ?? null;
    const splitX = main === null || sidebar === null
      ? null
      : (((main.getBoundingClientRect().right
        + sidebar.getBoundingClientRect().left) / 2) - page.left)
      / page.width;
    return {
      bodyTop: (((headerBottom + columnsTop) / 2) - page.top) / page.width,
      splitX,
    };
  }

  const pageEls = Array.from(
    document.querySelectorAll('.resume-page'),
  ).filter((el) => !el.classList.contains('pagination-measurement'));

  const pages: BrowserExtraction['pages'] = [];
  const words: BrowserExtraction['words'] = [];
  let lineOrdinal = 0;

  for (const pageEl of pageEls) {
    const pageIndex = Number(pageEl.getAttribute('data-page-index'));
    const lineSummaries: LineSummary[] = [];
    for (const block of blocksOf(pageEl)) {
      const raw = collectWords(block.el);
      if (raw.length === 0) continue;
      const header = block.isEntry
        ? block.el.querySelector('.entry-header')
        : null;
      let tagged = false;
      for (const line of clusterLines(raw)) {
        lineSummaries.push({
          first: line[0]!.text,
          last: line.at(-1)!.text,
          count: line.length,
        });
        for (const word of line) {
          const entryStart = !tagged && header !== null
            ? {
                sectionKey: block.sectionKey ?? '',
                headerText: (header.textContent ?? '')
                  .trim().replace(/\s+/gu, ' ').slice(0, 80),
              }
            : null;
          if (entryStart !== null) tagged = true;
          words.push({
            text: word.text,
            flow: block.flow,
            page: pageIndex,
            line: lineOrdinal,
            entryStart,
          });
        }
        lineOrdinal += 1;
      }
    }
    pages.push({ page: pageIndex, lines: lineSummaries });
  }

  return { pages, words, geometry: geometryOf(pageEls[0]) };
}

// --- fixture URLs and rendering ------------------------------------------

function sampleUrl(
  mode: 'continuous' | 'paged',
  templateId: string,
  lng: SampleLanguage,
  options: { readonly print?: boolean; readonly zoom?: number } = {},
): string {
  const printParam = options.print === true ? '&print=1' : '';
  const zoomParam = options.zoom === undefined ? '' : `&zoom=${options.zoom}`;
  return `/_harness/render?fixture=sample-${templateId}-${lng}`
    + `&template=${templateId}&mode=${mode}${printParam}${zoomParam}`;
}

async function producePdf(
  page: Page,
  baseURL: string,
  templateId: string,
  lng: SampleLanguage,
): Promise<Buffer> {
  const external = await denyExternalRequests(page);
  const response = await page.goto(
    `${baseURL}${sampleUrl('continuous', templateId, lng, { print: true })}`,
  );
  expect(response?.ok()).toBe(true);
  await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
  await waitForImages(page);
  await page.emulateMedia({ media: 'print' });
  const pdf = await page.pdf({
    displayHeaderFooter: false,
    margin: { bottom: 0, left: 0, right: 0, top: 0 },
    preferCSSPageSize: true,
    printBackground: true,
    scale: 1,
  });
  expect(external).toEqual([]);
  return pdf;
}

async function captureDiagnostics(page: Page): Promise<DiagnosticSnapshot> {
  return page.evaluate(() => {
    const harnessRoot = document.querySelector('.harness-render');
    const pagedResume = document.querySelector('.paged-resume');
    const resumePages = document.querySelectorAll('.resume-page');
    const visiblePages = Array.from(resumePages).filter(
      (el) => !el.classList.contains('pagination-measurement'),
    );
    const datasetOf = (el: Element | null) => el === null
      ? null
      : { ...(el as HTMLElement).dataset };
    return {
      harnessDataset: datasetOf(harnessRoot),
      pagedResumeDataset: datasetOf(pagedResume),
      resumePageCount: resumePages.length,
      visiblePageCount: visiblePages.length,
      bodySnippet: (document.body.textContent ?? '').slice(0, 500),
    };
  });
}

async function safeCaptureDiagnostics(
  page: Page,
): Promise<DiagnosticSnapshot | null> {
  try {
    return await captureDiagnostics(page);
  } catch {
    return null;
  }
}

// The zoom is requested in the URL and resolved once from it, never
// toggled after the fact: the harness scales the paper with the same
// ScaledSheet transform component EditorPreview.vue uses for a stable
// viewport class. A failure to settle is recorded with a DOM snapshot
// rather than thrown, so one broken case does not hide the rest.
async function producePreview(
  browser: Browser,
  baseURL: string,
  templateId: string,
  lng: SampleLanguage,
  zoomValue: number,
): Promise<PreviewResult> {
  const context = await browser.newContext({
    colorScheme: 'light',
    locale: 'en-US',
    reducedMotion: 'reduce',
    timezoneId: 'UTC',
    viewport: { height: 1123, width: 794 },
  });
  try {
    const page = await context.newPage();
    const external = await denyExternalRequests(page);
    const url = sampleUrl('paged', templateId, lng, { zoom: zoomValue });
    try {
      const response = await page.goto(`${baseURL}${url}`);
      expect(response?.ok()).toBe(true);
      await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
      await expect(page.locator('[data-pagination-settled="true"]'))
        .toHaveCount(1);
    } catch (error) {
      return {
        ok: false,
        url,
        error: error instanceof Error ? error.message : String(error),
        diagnostics: await safeCaptureDiagnostics(page),
      };
    }
    const raw = await page.evaluate(browserExtract, WORD_PATTERN);
    expect(external).toEqual([]);
    return {
      ok: true,
      url,
      extraction: { pages: raw.pages, words: orderByFlow(raw.words) },
      geometry: raw.geometry,
    };
  } finally {
    await context.close();
  }
}

// --- report ---------------------------------------------------------------

interface ReportSummary {
  readonly totalCases: number;
  readonly casesWithDrift: number;
  readonly failedCases: number;
  readonly webkitCovered: boolean;
  readonly webkitSkipReason: string | null;
}

interface ReportFile {
  readonly summary: ReportSummary;
  readonly cases: CaseResult[];
  readonly failures: FailedCase[];
}

function reportPath(): string {
  const resultsRoot = process.env.PLAYWRIGHT_RESULTS_DIR;
  if (resultsRoot === undefined) {
    throw new Error('PLAYWRIGHT_RESULTS_DIR is required.');
  }
  const surface = process.env.PLAYWRIGHT_SURFACE ?? 'harness';
  return resolve(resultsRoot, surface, 'preview-gap-report.json');
}

async function readExistingReport(
  target: string,
): Promise<{ cases: CaseResult[]; failures: FailedCase[] }> {
  try {
    const raw = JSON.parse(await readFile(target, 'utf8')) as ReportFile;
    return { cases: [...raw.cases], failures: [...raw.failures] };
  } catch {
    return { cases: [], failures: [] };
  }
}

// Each test appends its own cases and failures to the shared report file,
// merged with what earlier tests already wrote. Playwright restarts the
// worker (re-importing this module) after any test failure, so a
// module-level accumulator would lose every case written before that.
async function appendReport(
  newCases: readonly CaseResult[],
  newFailures: readonly FailedCase[],
  webkitCovered: boolean,
  webkitSkipReason: string,
): Promise<void> {
  const target = reportPath();
  await mkdir(resolve(target, '..'), { recursive: true });
  const existing = await readExistingReport(target);
  const cases = [...existing.cases, ...newCases];
  const failures = [...existing.failures, ...newFailures];
  const casesWithDrift = cases.filter((c) =>
    c.counts.lineBreak > 0
    || c.counts.pageBreak > 0
    || c.counts.missing > 0
    || c.counts.extra > 0
    || c.counts.entryPageMismatches > 0).length;
  const summary: ReportSummary = {
    totalCases: cases.length,
    casesWithDrift,
    failedCases: failures.length,
    webkitCovered,
    webkitSkipReason: webkitCovered ? null : webkitSkipReason,
  };
  const report: ReportFile = { summary, cases, failures };
  await writeFile(target, JSON.stringify(report, null, 2));

  console.log(
    `preview-gap: ${summary.totalCases} case(s), `
    + `${summary.casesWithDrift} with drift, `
    + `${summary.failedCases} failed so far; webkit `
    + `${webkitCovered ? 'covered' : `skipped (${webkitSkipReason})`}.`,
  );
}

// --- test wiring -----------------------------------------------------------

let normalChromium: Browser | undefined;
let webkitBrowser: Browser | undefined;
let webkitSkipReason = '';

test.beforeAll(async () => {
  // @playwright/test's chromium/webkit exports default to the active
  // project's launchOptions (the harness surface's print flags), so a
  // "normal browser" launch must override args explicitly to stay empty;
  // WebKit's binary rejects Chromium-only flags outright if they leak in.
  normalChromium = await chromium.launch({ args: [] });
  try {
    webkitBrowser = await webkit.launch({ args: [] });
  } catch (error) {
    webkitSkipReason = error instanceof Error ? error.message : String(error);
  }
});

test.afterAll(async () => {
  await normalChromium?.close();
  await webkitBrowser?.close();
});

for (const { templateId, lng } of SAMPLES) {
  test(`preview gap: ${templateId} (${lng})`, async ({ page, baseURL }) => {
    test.setTimeout(60_000);
    if (baseURL === undefined) throw new Error('baseURL is required.');
    const pdfBytes = await producePdf(page, baseURL, templateId, lng);
    const pdfPages = await readPdfPages(pdfBytes);

    const variants: Array<{ engine: Engine; browser: Browser }> = [
      { engine: 'chromium-normal', browser: normalChromium! },
    ];
    if (webkitBrowser !== undefined) {
      variants.push({ engine: 'webkit', browser: webkitBrowser });
    }

    const caseResults: CaseResult[] = [];
    const caseFailures: FailedCase[] = [];

    for (const { engine, browser } of variants) {
      let fullScale: CaseResult | undefined;
      for (const [zoomLabel, zoomValue] of ZOOM_VARIANTS) {
        const previewResult = await producePreview(
          browser,
          baseURL,
          templateId,
          lng,
          zoomValue,
        );
        if (!previewResult.ok) {
          caseFailures.push({
            sample: templateId,
            lng,
            engine,
            zoomLabel,
            zoomValue,
            url: previewResult.url,
            error: previewResult.error,
            diagnostics: previewResult.diagnostics,
          });
          continue;
        }
        const previewExtraction = previewResult.extraction;
        const pdfExtraction = buildPdfExtraction(
          pdfPages,
          previewResult.geometry,
        );
        const pairs = alignIndices(
          previewExtraction.words.map((w) => compareKey(w.text)),
          pdfExtraction.words.map((w) => compareKey(w.text)),
        );
        const { diffs, counts } = classify(
          previewExtraction,
          pdfExtraction,
          pairs,
        );
        const starts = entryPageStarts(
          previewExtraction,
          pdfExtraction,
          pairs,
        );
        const entryPageMismatches = starts.filter((s) => !s.matches).length;
        const previewPageCount = previewExtraction.pages.length;
        const pdfPageCount = pdfExtraction.pages.length;
        const result: CaseResult = {
          sample: templateId,
          lng,
          engine,
          zoomLabel,
          zoomValue,
          counts: { ...counts, entryPageMismatches },
          firstDiffs: firstOfEachKind(diffs, 10),
          entryPageStarts: starts,
          knownCauses: causesFor(engine),
          previewPages: previewPageCount,
          pdfPages: pdfPageCount,
        };
        caseResults.push(result);
        try {
          assertWithinExpectation(result);
          // A one-page PDF that spills into two preview pages (or the
          // reverse) is the capacity gap this spec exists to catch, not a
          // word-level drift the ceiling file can express.
          if (previewPageCount !== pdfPageCount) {
            throw new Error(
              `preview has ${String(previewPageCount)} page(s), `
              + `PDF has ${String(pdfPageCount)}`,
            );
          }
          // The sheet is scaled for display with a transform, so the layout
          // is the same at every scale (docs/design/templates/print.md §1).
          if (zoomLabel === 'full') {
            fullScale = result;
          } else if (
            fullScale !== undefined
            && JSON.stringify(result.counts)
              !== JSON.stringify(fullScale.counts)
          ) {
            throw new Error(
              `${zoomLabel} scale drifts ${JSON.stringify(result.counts)}, `
              + `full scale ${JSON.stringify(fullScale.counts)}`,
            );
          }
        } catch (error) {
          caseFailures.push({
            sample: templateId,
            lng,
            engine,
            zoomLabel,
            zoomValue,
            url: previewResult.url,
            error: error instanceof Error ? error.message : String(error),
            diagnostics: null,
          });
        }
      }
    }

    await appendReport(
      caseResults,
      caseFailures,
      webkitBrowser !== undefined,
      webkitSkipReason,
    );
    expect(
      caseFailures,
      'preview-gap cases failed; see preview-gap-report.json',
    ).toEqual([]);
  });
}
