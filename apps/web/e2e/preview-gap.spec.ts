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
import { mkdir, writeFile } from 'node:fs/promises';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { denyExternalRequests, waitForImages } from './support';

// Measures how far the editor's paged preview drifts from the PDF for every
// gallery sample, in both languages. The preview is approximate and the PDF
// is authoritative (docs/design/templates/print.md §1); this check reports
// the size of that gap rather than enforcing pixel or text equality. It
// fails only when a case's drift exceeds preview-gap-expected.json, so a
// later renderer fix can tighten the file without this spec changing.
//
// Comparison unit: words, extracted from the DOM (Range.getBoundingClientRect
// per word, clustered into visual lines) for the preview, and from pdfjs text
// content for the PDF. A word-level LCS alignment tells which words the two
// sides share; the boundaries between aligned words classify each
// disagreement as a line-break, a page-break, or a missing/extra run.

const TYPICAL_ZOOM = 0.84;
const FULL_ZOOM = 1;
const ZOOM_VARIANTS = [
  ['typical', TYPICAL_ZOOM],
  ['full', FULL_ZOOM],
] as const;
type ZoomLabel = (typeof ZOOM_VARIANTS)[number][0];
type Engine = 'chromium-normal' | 'webkit';

interface EntryStart {
  readonly sectionKey: string;
  readonly headerText: string;
}

interface FlatWord {
  readonly text: string;
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
}

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

function caseKey(
  sample: string,
  lng: string,
  engine: string,
  zoomLabel: string,
): string {
  return `${sample}|${lng}|${engine}|${zoomLabel}`;
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
  const key = caseKey(
    result.sample,
    result.lng,
    result.engine,
    result.zoomLabel,
  );
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

function causesFor(engine: Engine, zoomLabel: ZoomLabel): string[] {
  const causes = [
    'continuous-vs-paged-pagination-model',
    'screen-vs-print-css',
  ];
  if (zoomLabel === 'typical') causes.push('css-zoom-scaling');
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

function clusterRawLines(raw: readonly RawWord[]): RawWord[][] {
  const sorted = [...raw].sort((a, b) => a.top - b.top || a.left - b.left);
  const lines: RawWord[][] = [];
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

async function extractPdf(pdfBytes: Buffer): Promise<Extraction> {
  const loadingTask = getDocument({
    data: new Uint8Array(pdfBytes),
    isImageDecoderSupported: false,
    isOffscreenCanvasSupported: false,
    useSystemFonts: false,
  });
  try {
    const document = await loadingTask.promise;
    const pages: PageLines[] = [];
    const words: FlatWord[] = [];
    let lineOrdinal = 0;
    for (let n = 1; n <= document.numPages; n += 1) {
      const pdfPage = await document.getPage(n);
      const content = await pdfPage.getTextContent();
      const raw: RawWord[] = [];
      for (const item of content.items) {
        if (!('str' in item) || item.str.trim() === '') continue;
        const transform = item.transform;
        const top = -transform[5]!;
        const height = item.height > 0
          ? item.height
          : Math.abs(transform[3]!) || 10;
        for (const match of item.str.matchAll(/\S+/gu)) {
          if (match.index === undefined) continue;
          const fraction = match.index / Math.max(item.str.length, 1);
          raw.push({
            text: match[0],
            top,
            left: transform[4]! + fraction * item.width,
            bottom: top + height,
          });
        }
      }
      const pageIndex = n - 1;
      const lineSummaries: LineSummary[] = [];
      for (const line of clusterRawLines(raw)) {
        lineSummaries.push({
          first: line[0]!.text,
          last: line.at(-1)!.text,
          count: line.length,
        });
        for (const word of line) {
          words.push({
            text: word.text,
            page: pageIndex,
            line: lineOrdinal,
            entryStart: null,
          });
        }
        lineOrdinal += 1;
      }
      pages.push({ page: pageIndex, lines: lineSummaries });
    }
    return { pages, words };
  } finally {
    await loadingTask.destroy();
  }
}

// --- preview extraction (DOM) --------------------------------------------

interface BrowserExtraction {
  pages: { page: number; lines: LineSummary[] }[];
  words: {
    text: string;
    page: number;
    line: number;
    entryStart: EntryStart | null;
  }[];
}

// Runs inside the page via page.evaluate: no reference to anything outside
// this function body survives serialization, so every helper it needs is
// declared inline.
function browserExtract(): BrowserExtraction {
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
      for (const match of text.matchAll(/\S+/gu)) {
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
    sectionKey: string | null;
    isEntry: boolean;
  }

  function blocksOf(pageEl: Element): Block[] {
    const blocks: Block[] = [];
    const header = pageEl.querySelector(':scope > div > .pagination-header');
    if (header !== null) {
      blocks.push({ el: header, sectionKey: null, isEntry: false });
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
      for (const atomic of atomics) {
        blocks.push({
          el: atomic,
          sectionKey: atomic.getAttribute('data-section-key'),
          isEntry: atomic.getAttribute('data-block-kind') === 'entry',
        });
      }
    }
    return blocks;
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

  return { pages, words };
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

async function producePreview(
  browser: Browser,
  baseURL: string,
  templateId: string,
  lng: SampleLanguage,
  zoomValue: number,
): Promise<Extraction> {
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
    // The zoom is requested in the URL, not applied after the fact: the
    // harness bakes it into the paper's style before Vue ever mounts, the
    // same way EditorPreview.vue's zoom is set once and not toggled for a
    // stable viewport class. Mutating the style after the first settle
    // leaves PagedResume's pagination watcher unable to settle again.
    const response = await page.goto(
      `${baseURL}${sampleUrl('paged', templateId, lng, { zoom: zoomValue })}`,
    );
    expect(response?.ok()).toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    await expect(page.locator('[data-pagination-settled="true"]'))
      .toHaveCount(1);
    const extraction = await page.evaluate(browserExtract);
    expect(external).toEqual([]);
    return extraction;
  } finally {
    await context.close();
  }
}

// --- report ---------------------------------------------------------------

async function writeReport(
  cases: readonly CaseResult[],
  webkitCovered: boolean,
  webkitSkipReason: string,
): Promise<void> {
  const resultsRoot = process.env.PLAYWRIGHT_RESULTS_DIR;
  if (resultsRoot === undefined) {
    throw new Error('PLAYWRIGHT_RESULTS_DIR is required.');
  }
  const surface = process.env.PLAYWRIGHT_SURFACE ?? 'harness';
  const target = resolve(resultsRoot, surface, 'preview-gap-report.json');
  const casesWithDrift = cases.filter((c) =>
    c.counts.lineBreak > 0
    || c.counts.pageBreak > 0
    || c.counts.missing > 0
    || c.counts.extra > 0
    || c.counts.entryPageMismatches > 0).length;
  const summary = {
    totalCases: cases.length,
    casesWithDrift,
    webkitCovered,
    webkitSkipReason: webkitCovered ? null : webkitSkipReason,
  };
  await mkdir(resolve(target, '..'), { recursive: true });
  await writeFile(target, JSON.stringify({ summary, cases }, null, 2));
  // eslint-disable-next-line no-console
  console.log(
    `preview-gap: ${summary.totalCases} case(s), `
    + `${summary.casesWithDrift} with drift; webkit `
    + `${webkitCovered ? 'covered' : `skipped (${webkitSkipReason})`}.`,
  );
}

// --- test wiring -----------------------------------------------------------

const results: CaseResult[] = [];
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
  await writeReport(results, webkitBrowser !== undefined, webkitSkipReason);
});

for (const { templateId, lng } of SAMPLES) {
  test(`preview gap: ${templateId} (${lng})`, async ({ page, baseURL }) => {
    test.setTimeout(60_000);
    if (baseURL === undefined) throw new Error('baseURL is required.');
    const pdfBytes = await producePdf(page, baseURL, templateId, lng);
    const pdfExtraction = await extractPdf(pdfBytes);

    const variants: Array<{ engine: Engine; browser: Browser }> = [
      { engine: 'chromium-normal', browser: normalChromium! },
    ];
    if (webkitBrowser !== undefined) {
      variants.push({ engine: 'webkit', browser: webkitBrowser });
    }

    for (const { engine, browser } of variants) {
      for (const [zoomLabel, zoomValue] of ZOOM_VARIANTS) {
        const previewExtraction = await producePreview(
          browser,
          baseURL,
          templateId,
          lng,
          zoomValue,
        );
        const pairs = alignIndices(
          previewExtraction.words.map((w) => w.text),
          pdfExtraction.words.map((w) => w.text),
        );
        const { diffs, counts } = classify(
          previewExtraction,
          pdfExtraction,
          pairs,
        );
        const starts = entryPageStarts(previewExtraction, pdfExtraction, pairs);
        const entryPageMismatches = starts.filter((s) => !s.matches).length;
        const result: CaseResult = {
          sample: templateId,
          lng,
          engine,
          zoomLabel,
          zoomValue,
          counts: { ...counts, entryPageMismatches },
          firstDiffs: diffs.slice(0, 10),
          entryPageStarts: starts,
          knownCauses: causesFor(engine, zoomLabel),
        };
        results.push(result);
        assertWithinExpectation(result);
      }
    }
  });
}
