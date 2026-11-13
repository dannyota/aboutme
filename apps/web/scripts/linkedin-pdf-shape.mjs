#!/usr/bin/env node
// Usage: node apps/web/scripts/linkedin-pdf-shape.mjs <file.pdf>

import { readFileSync } from 'node:fs';

const ALLOWLIST_PHRASES = [
  'Contact',
  'Top Skills',
  'Languages',
  'Certifications',
  'Honors-Awards',
  'Publications',
  'Patents',
  'Summary',
  'Experience',
  'Education',
  'Volunteer Experience',
  'Projects',
  'Present',
  'Page',
  'of',
  'year',
  'years',
  'month',
  'months',
  'less than a year',
  'LinkedIn',
  'Mobile',
  'Home',
  'Work',
  'January',
  'February',
  'March',
  'April',
  'May',
  'June',
  'July',
  'August',
  'September',
  'October',
  'November',
  'December',
  'Jan',
  'Feb',
  'Mar',
  'Apr',
  'Jun',
  'Jul',
  'Aug',
  'Sep',
  'Sept',
  'Oct',
  'Nov',
  'Dec',
];

const ALLOWLIST_WORDS = new Set();
for (const phrase of ALLOWLIST_PHRASES) {
  for (const word of phrase.match(/\p{L}+/gu) ?? []) {
    ALLOWLIST_WORDS.add(word.toLowerCase());
  }
}

function escapeRegExp(text) {
  return text.replace(/[.*+?^${}()|[\]\\]/gu, '\\$&');
}

const ALLOWLIST_RE = new RegExp(
  `\\b(?:${[...ALLOWLIST_PHRASES]
    .sort((a, b) => b.length - a.length)
    .map(escapeRegExp)
    .join('|')})\\b`,
  'giu',
);

const HEADING_SET = new Set([
  'Contact',
  'Top Skills',
  'Languages',
  'Certifications',
  'Honors-Awards',
  'Publications',
  'Patents',
  'Summary',
  'Experience',
  'Education',
  'Volunteer Experience',
  'Projects',
]);

const FOOTER_RE = /^Page \d+ of \d+$/u;

const MONTH_PATTERN
  = '(?:Jan(?:uary)?|Feb(?:ruary)?|Mar(?:ch)?|Apr(?:il)?|May|Jun(?:e)?|'
    + 'Jul(?:y)?|Aug(?:ust)?|Sep(?:t(?:ember)?)?|Oct(?:ober)?|Nov(?:ember)?|'
    + 'Dec(?:ember)?)';
const DATE_TOKEN_PATTERN = `(?:${MONTH_PATTERN}\\s+)?\\d{4}`;
const DATE_RANGE_RE = new RegExp(
  `^${DATE_TOKEN_PATTERN}\\s*[-–]\\s*`
  + `(?:Present|${DATE_TOKEN_PATTERN})(?:\\s*\\([^)]*\\))?$`,
  'iu',
);
const DURATION_RE = new RegExp(
  '^\\(?(?:less than a year|\\d+\\s+years?(?:\\s+\\d+\\s+months?)?|'
  + '\\d+\\s+months?)\\)?$',
  'iu',
);

const TOOL_ALLOWLIST_RE = [/^Apache\s+FOP\b/iu, /^FOP\b/iu, /^LinkedIn\b/iu];

function maskChar(ch) {
  if (/\p{Lu}/u.test(ch)) return 'A';
  if (/\p{L}/u.test(ch)) return 'a';
  if (/\p{Nd}/u.test(ch)) return '9';
  if (/\p{P}/u.test(ch)) return ch;
  if (/\s/u.test(ch)) return ch;
  return '#';
}

function maskSegment(segment) {
  let out = '';
  for (const ch of segment) out += maskChar(ch);
  return out;
}

function maskLine(text) {
  let result = '';
  let lastIndex = 0;
  const re = new RegExp(ALLOWLIST_RE.source, ALLOWLIST_RE.flags);
  let match = re.exec(text);
  while (match !== null) {
    result += maskSegment(text.slice(lastIndex, match.index));
    result += match[0];
    lastIndex = match.index + match[0].length;
    match = re.exec(text);
  }
  result += maskSegment(text.slice(lastIndex));
  return result;
}

function median(numbers) {
  const sorted = [...numbers].sort((a, b) => a - b);
  const mid = Math.floor(sorted.length / 2);
  if (sorted.length === 0) return 0;
  return sorted.length % 2 === 0
    ? (sorted[mid - 1] + sorted[mid]) / 2
    : sorted[mid];
}

function roundToHalf(value) {
  return Math.round(value * 2) / 2;
}

function columnFor(x, boundary) {
  if (boundary === null) return 'n/a';
  return x < boundary ? 'left' : 'right';
}

function classifyType(line, medianSize) {
  if (line.isFooter) return 'FOOTER';
  if (HEADING_SET.has(line.text) && line.fontSize > medianSize) {
    return 'HEADING';
  }
  if (DATE_RANGE_RE.test(line.text)) return 'DATE';
  if (DURATION_RE.test(line.text)) return 'DURATION';
  return 'TEXT';
}

function extractItems(rawItems, pageNumber) {
  const items = [];
  for (const raw of rawItems) {
    if (typeof raw.str !== 'string' || raw.str === '') continue;
    const transform = raw.transform;
    items.push({
      page: pageNumber,
      str: raw.str,
      x: transform[4],
      y: transform[5],
      width: raw.width,
      fontSize: Math.abs(transform[3]),
      fontId: raw.fontName,
    });
  }
  return items;
}

function buildLines(items) {
  const clusters = [];
  let current = null;
  for (const item of items) {
    if (
      current !== null
      && Math.abs(item.y - current.y) <= 0.4 * current.fontSize
    ) {
      current.items.push(item);
    } else {
      current = { page: item.page, y: item.y, fontSize: item.fontSize,
        items: [item] };
      clusters.push(current);
    }
  }
  return clusters.map(finalizeLine);
}

function finalizeLine(cluster) {
  const items = [...cluster.items].sort((a, b) => a.x - b.x);
  let text = '';
  let prevEnd = null;
  for (const item of items) {
    if (prevEnd !== null) {
      const gap = item.x - prevEnd;
      const atBoundary = /\s$/u.test(text) || /^\s/u.test(item.str);
      if (gap > 0.25 * cluster.fontSize && !atBoundary) text += ' ';
    }
    text += item.str;
    prevEnd = item.x + item.width;
  }
  return {
    page: cluster.page,
    x: Math.min(...items.map((item) => item.x)),
    y: cluster.y,
    fontSize: roundToHalf(cluster.fontSize),
    fontId: items[0].fontId,
    text: text.replace(/\s+/gu, ' ').trim().normalize('NFC'),
  };
}

function countNonAsciiLetters(text) {
  let count = 0;
  for (const ch of text) {
    if (ch.codePointAt(0) > 127 && /\p{L}/u.test(ch)) count += 1;
  }
  return count;
}

function mentionsTool(value) {
  return typeof value === 'string' && /linkedin|fop/iu.test(value);
}

function toolDisplay(value) {
  if (typeof value !== 'string' || value.length === 0) return '(absent)';
  return TOOL_ALLOWLIST_RE.some((re) => re.test(value)) ? value : 'other';
}

async function readShape(filePath) {
  const bytes = readFileSync(filePath);
  const header = bytes.subarray(0, 1024).toString('latin1');
  if (!header.includes('%PDF-')) {
    throw new Error(
      'not a PDF file: no %PDF- header in the first 1024 bytes',
    );
  }

  const pdfjsLib = await import('pdfjs-dist/legacy/build/pdf.mjs');
  const loadingTask = pdfjsLib.getDocument({
    data: new Uint8Array(bytes),
    useWasm: false,
    enableXfa: false,
    isOffscreenCanvasSupported: false,
    isImageDecoderSupported: false,
    useSystemFonts: false,
    disableFontFace: true,
    stopAtErrors: true,
  });
  loadingTask.onPassword = (updatePassword) => {
    updatePassword(new Error('this PDF requires a password'));
  };
  const pdfDocument = await loadingTask.promise;

  try {
    const pageSizes = [];
    const rawLines = [];
    for (let pageNumber = 1; pageNumber <= pdfDocument.numPages;
      pageNumber += 1) {
      const page = await pdfDocument.getPage(pageNumber);
      const view = page.view;
      pageSizes.push({
        pageNumber,
        width: view[2] - view[0],
        height: view[3] - view[1],
        bottom: view[1],
      });
      const content = await page.getTextContent();
      const items = extractItems(content.items, pageNumber);
      rawLines.push(...buildLines(items));
    }

    for (const line of rawLines) {
      const pageInfo = pageSizes.find((p) => p.pageNumber === line.page);
      const nearBottom = line.y - pageInfo.bottom < 72;
      line.isFooter = nearBottom && FOOTER_RE.test(line.text);
    }

    const medianSize = median(
      rawLines.filter((line) => !line.isFooter).map((line) => line.fontSize),
    );
    for (const line of rawLines) {
      line.type = classifyType(line, medianSize);
    }

    const page1Lines = rawLines.filter(
      (line) => line.page === 1 && !line.isFooter,
    );
    const nameLine = page1Lines.reduce(
      (best, line) =>
        best === null || line.fontSize > best.fontSize ? line : best,
      null,
    );
    const boundary = nameLine === null ? null : nameLine.x - 12;
    for (const line of rawLines) {
      line.column = columnFor(line.x, boundary);
    }

    const fontFrequency = new Map();
    for (const line of rawLines) {
      const nextCount = (fontFrequency.get(line.fontId) ?? 0) + 1;
      fontFrequency.set(line.fontId, nextCount);
    }
    let normalFontId = null;
    let bestCount = -1;
    for (const [fontId, count] of fontFrequency) {
      if (count > bestCount) {
        bestCount = count;
        normalFontId = fontId;
      }
    }
    for (const line of rawLines) {
      line.weight = line.fontId === normalFontId ? 'normal' : 'bold';
    }

    orderAndGap(rawLines);

    const metadataResult = await pdfDocument.getMetadata();
    return { pageSizes, boundary, lines: rawLines, metadataResult,
      numPages: pdfDocument.numPages };
  } finally {
    await loadingTask.destroy();
  }
}

function matchesColumn(line, column) {
  return column === 'left' ? line.column === 'left' : line.column !== 'left';
}

function orderAndGap(lines) {
  const pages = [...new Set(lines.map((line) => line.page))].sort(
    (a, b) => a - b,
  );
  for (const page of pages) {
    for (const column of ['left', 'right']) {
      const inColumn = lines
        .filter((line) => line.page === page)
        .filter((line) => matchesColumn(line, column))
        .sort((a, b) => b.y - a.y);
      let prevY = null;
      for (const line of inColumn) {
        line.gap = prevY === null ? null : roundToHalf(prevY - line.y);
        prevY = line.y;
      }
    }
  }
}

function formatNumber(value) {
  return Number.isInteger(value) ? String(value) : value.toFixed(1);
}

function printShape(shape) {
  const info = shape.metadataResult.info ?? {};
  const encrypted = (info.EncryptFilterName ?? null) !== null;
  console.log(`pdf version: ${info.PDFFormatVersion ?? 'unknown'}`);
  console.log(`pages: ${shape.numPages}`);
  console.log(`encrypted: ${encrypted ? 'yes' : 'no'}`);

  const infoDictFields = ['Title', 'Author', 'Subject', 'Keywords',
    'Creator', 'Producer', 'CreationDate', 'ModDate', 'Trapped'];
  const metadataKeys = infoDictFields.filter((key) => key in info);
  if (info.Custom instanceof Map) {
    for (const key of info.Custom.keys()) metadataKeys.push(`Custom.${key}`);
  }
  if (shape.metadataResult.metadata) {
    for (const [key] of shape.metadataResult.metadata) {
      metadataKeys.push(`xmp:${key}`);
    }
  }
  console.log(`metadata keys: ${metadataKeys.join(', ') || '(none)'}`);

  const producerMentions = mentionsTool(info.Producer);
  const creatorMentions = mentionsTool(info.Creator);
  const authorMentions = mentionsTool(info.Author);
  console.log(`producer mentions LinkedIn/FOP: ${producerMentions}`);
  console.log(`producer: ${toolDisplay(info.Producer)}`);
  console.log(`creator mentions LinkedIn/FOP: ${creatorMentions}`);
  console.log(`creator: ${toolDisplay(info.Creator)}`);
  console.log(`author mentions LinkedIn/FOP: ${authorMentions}`);

  for (const page of shape.pageSizes) {
    const size = `${formatNumber(page.width)}x${formatNumber(page.height)}`;
    console.log(`page ${page.pageNumber} size: ${size} pt`);
  }
  const boundaryText = shape.boundary === null
    ? 'n/a'
    : formatNumber(shape.boundary);
  for (const page of shape.pageSizes) {
    console.log(`page ${page.pageNumber} column split x: ${boundaryText}`);
  }

  const fontTally = new Map();
  for (const line of shape.lines) {
    const key = `${line.type}|${line.fontId}|${line.fontSize}`;
    fontTally.set(key, (fontTally.get(key) ?? 0) + 1);
  }
  for (const [key, count] of [...fontTally].sort()) {
    const [type, fontId, size] = key.split('|');
    const label = `type=${type} font=${fontId} size=${size}`;
    console.log(`font ${label} count=${count}`);
  }

  let nonAsciiLines = 0;
  let nonAsciiChars = 0;
  for (const line of shape.lines) {
    const count = countNonAsciiLetters(line.text);
    if (count > 0) {
      nonAsciiLines += 1;
      nonAsciiChars += count;
    }
  }
  const nonAsciiSummary = `${nonAsciiLines} line(s), ${nonAsciiChars} `
    + 'letter(s) total';
  console.log(`non-ascii letters: ${nonAsciiSummary}`);

  for (const line of shape.lines) {
    const gapText = line.gap === null ? '-' : formatNumber(line.gap);
    const position = `x=${formatNumber(line.x)} y=${formatNumber(line.y)}`;
    const style = `size=${formatNumber(line.fontSize)} weight=${
      line.weight}`;
    console.log(
      `line page=${line.page} column=${line.column} ${position} `
      + `gap=${gapText} ${style} type=${line.type} `
      + `text=${maskLine(line.text)}`,
    );
  }
}

const SELF_CHECK_SAMPLES = [
  'Nguyen Van Mau',
  'Nguyễn Văn Mẫu',
  'Example Software Engineer',
  'January 2020 - Present (5 years 3 months)',
  'Jun 2018 - Aug 2020 (2 years 2 months)',
  'Page 3 of 7',
  'Top Skills',
  'Honors-Awards',
  'less than a year',
  '(LinkedIn)',
  '(Mobile)',
  'jane.doe@example.com',
  '+1 (555) 123-4567',
  'https://www.linkedin.com/in/example-person',
  'Software Engineer at Example Co.',
];

function runSelfCheck() {
  let failures = 0;
  for (const sample of SELF_CHECK_SAMPLES) {
    const masked = maskLine(sample);
    console.log(`${sample}\n  -> ${masked}`);
    for (const word of sample.match(/\p{L}+/gu) ?? []) {
      if (word.length < 2) continue;
      if (ALLOWLIST_WORDS.has(word.toLowerCase())) continue;
      const leaked = new RegExp(`\\b${escapeRegExp(word)}\\b`, 'u').test(
        masked,
      );
      if (leaked) {
        failures += 1;
        console.error(`self-check: "${word}" leaked in "${masked}"`);
      }
    }
  }
  if (failures > 0) {
    console.error(`self-check failed: ${failures} leak(s) found`);
    return false;
  }
  console.log('self-check passed: no non-allowlisted word survived masking');
  return true;
}

async function main() {
  const args = process.argv.slice(2);
  if (args.includes('--self-check')) {
    if (!runSelfCheck()) process.exit(1);
    return;
  }

  const filePath = args[0];
  if (!filePath) {
    console.error(
      'usage: node apps/web/scripts/linkedin-pdf-shape.mjs <file.pdf>',
    );
    process.exit(1);
    return;
  }

  try {
    const shape = await readShape(filePath);
    printShape(shape);
  } catch (err) {
    console.error(`cannot read this file: ${err.message}`);
    process.exit(1);
  }
}

await main();
