import type { Resume, Section } from '@aboutme/schema';

export const SITE_NAME = 'aboutme.vn';

const MAX_TITLE_GRAPHEMES = 70;
const MAX_DESCRIPTION_GRAPHEMES = 160;
const MAX_IMAGE_TEXT_GRAPHEMES = 200;
const MAX_DESCRIPTION_BYTES = 1024;
const MAX_IMAGE_TEXT_BYTES = 1024;
const MIN_SENTENCE_GRAPHEMES = 60;
const WORD_CUT_WINDOW = 40;
const MIN_PHONE_DIGITS = 9;
const ELLIPSIS = '…';

const encoder = new TextEncoder();
const byteLength = (text: string): number => encoder.encode(text).length;

// JS \s matches U+FEFF; Go's unicode.IsSpace does not (ECMA-262, Go unicode).
const WHITE_SPACE_CODE_POINTS = new Set([
  0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x20, 0x85, 0xa0, 0x1680, 0x2028, 0x2029,
  0x202f, 0x205f, 0x3000,
]);
const isSpaceRune = (character: string): boolean => {
  const codePoint = character.codePointAt(0)!;
  if (codePoint >= 0x2000 && codePoint <= 0x200a) return true;
  return WHITE_SPACE_CODE_POINTS.has(codePoint);
};
const isControlRune = (character: string): boolean => /\p{Cc}/u.test(character);
const isBidiFormatting = (character: string): boolean => {
  const codePoint = character.codePointAt(0)!;
  return codePoint === 0x061c || codePoint === 0x200e || codePoint === 0x200f
    || (codePoint >= 0x202a && codePoint <= 0x202e)
    || (codePoint >= 0x2066 && codePoint <= 0x2069);
};

export function normalizePreviewText(text: string): string {
  let out = '';
  let pendingSpace = false;
  for (const character of text) {
    if (
      isSpaceRune(character) || isControlRune(character)
      || isBidiFormatting(character)
    ) {
      pendingSpace = true;
      continue;
    }
    if (pendingSpace && out.length !== 0) out += ' ';
    pendingSpace = false;
    out += character;
  }
  return out;
}

// Go's RE2 \s is only [\t\n\f\r ], unlike JavaScript's \s.
const EMAIL_PATTERN = /[^\t\n\f\r @]+@[^\t\n\f\r @]+\.[^\t\n\f\r @]+/g;
const PHONE_PATTERN = /[+(]{0,2}[0-9]+\)?(?:[ .-]\(?[0-9]+\)?)*/g;

const charBefore = (text: string, index: number): string => {
  if (index <= 0) return '';
  const low = text.charCodeAt(index - 1);
  if (low >= 0xdc00 && low <= 0xdfff && index >= 2) {
    const high = text.charCodeAt(index - 2);
    if (high >= 0xd800 && high <= 0xdbff) return text.slice(index - 2, index);
  }
  return text.slice(index - 1, index);
};
const charAfter = (text: string, index: number): string => {
  if (index >= text.length) return '';
  const high = text.charCodeAt(index);
  if (high >= 0xd800 && high <= 0xdbff && index + 1 < text.length) {
    const low = text.charCodeAt(index + 1);
    if (low >= 0xdc00 && low <= 0xdfff) return text.slice(index, index + 2);
  }
  return text.slice(index, index + 1);
};
const isLetter = (character: string): boolean => /\p{L}/u.test(character);
const isDigit = (character: string): boolean => /\p{Nd}/u.test(character);

const isPhoneToken = (text: string, start: number, end: number): boolean => {
  const before = charBefore(text, start);
  const after = charAfter(text, end);
  if (before !== '' && (isLetter(before) || isDigit(before))) return false;
  if (after !== '' && (isLetter(after) || isDigit(after))) return false;
  const groups = text.slice(start, end).match(/[0-9]+/g) ?? [];
  let digits = 0;
  let years = 0;
  for (const group of groups) {
    digits += group.length;
    const isYear = group.startsWith('19') || group.startsWith('20');
    if (group.length === 4 && isYear) years++;
  }
  return digits >= MIN_PHONE_DIGITS && years !== groups.length;
};

const scrubPhones = (text: string): string => {
  let out = '';
  let last = 0;
  for (const match of text.matchAll(PHONE_PATTERN)) {
    const start = match.index!;
    const end = start + match[0].length;
    if (!isPhoneToken(text, start, end)) continue;
    out += text.slice(last, start) + ' ';
    last = end;
  }
  out += text.slice(last);
  return out;
};

export function scrubPreviewText(
  text: string,
  contacts: readonly string[],
): string {
  let out = normalizePreviewText(text);
  for (const contact of contacts) out = out.replaceAll(contact, ' ');
  out = out.replace(EMAIL_PATTERN, ' ');
  out = scrubPhones(out);
  return normalizePreviewText(out);
}

const graphemeClusters = (text: string): string[] => [
  ...new Intl.Segmenter('und', { granularity: 'grapheme' }).segment(text),
].map(({ segment }) => segment);
const graphemeCount = (text: string): number => graphemeClusters(text).length;
const segmentSentences = (text: string): string[] => [
  ...new Intl.Segmenter('und', { granularity: 'sentence' }).segment(text),
].map(({ segment }) => segment);

const trimRightSpace = (text: string): string => text.replace(/ +$/, '');
const trimRightCutset = (text: string, cutset: string): string => {
  let end = text.length;
  while (end > 0 && cutset.includes(text[end - 1]!)) end--;
  return text.slice(0, end);
};

const fitsDescription = (text: string): boolean =>
  byteLength(text) <= MAX_DESCRIPTION_BYTES
  && graphemeCount(text) <= MAX_DESCRIPTION_GRAPHEMES;

export function cutPreviewDescription(text: string): string {
  if (fitsDescription(text)) return text;
  let run = '';
  for (const sentence of segmentSentences(text)) {
    const candidate = trimRightSpace(run + sentence);
    if (!fitsDescription(candidate)) break;
    run += sentence;
  }
  run = trimRightSpace(run);
  if (graphemeCount(run) >= MIN_SENTENCE_GRAPHEMES) return run;

  const clusters = graphemeClusters(text);
  const ellipsisBytes = byteLength(ELLIPSIS);
  let keep = 0;
  let size = 0;
  while (
    keep < clusters.length
    && keep < MAX_DESCRIPTION_GRAPHEMES - 1
    && size + byteLength(clusters[keep]!)
    <= MAX_DESCRIPTION_BYTES - ellipsisBytes
  ) {
    size += byteLength(clusters[keep]!);
    keep++;
  }
  let end = keep;
  for (
    let index = keep - 1;
    index >= 0 && index >= keep - WORD_CUT_WINDOW;
    index--
  ) {
    if (clusters[index] === ' ') {
      end = index;
      break;
    }
  }
  return trimRightCutset(clusters.slice(0, end).join(''), ',;:–- ') + ELLIPSIS;
}

function firstGraphemes(text: string, limit: number, maxBytes: number): string {
  const clusters = graphemeClusters(text);
  let out = '';
  let size = 0;
  for (let count = 0; count < clusters.length && count < limit; count++) {
    const cluster = clusters[count]!;
    if (maxBytes > 0 && size + byteLength(cluster) > maxBytes) break;
    out += cluster;
    size += byteLength(cluster);
  }
  return trimRightSpace(out);
}

export function previewLocale(lng: string): string {
  if (lng === 'vi') return 'vi_VN';
  if (lng === 'en') return 'en_US';
  const dashIndex = lng.indexOf('-');
  if (dashIndex === -1) return '';
  const language = lng.slice(0, dashIndex);
  const region = lng.slice(dashIndex + 1);
  if (!isLowerLetters(language, 2, 3) || !isUpperLetters(region, 2)) return '';
  return `${language}_${region}`;
}
const isLowerLetters = (
  value: string,
  minimum: number,
  maximum: number,
): boolean =>
  value.length >= minimum && value.length <= maximum
  && /^[a-z]+$/.test(value);
const isUpperLetters = (value: string, length: number): boolean =>
  value.length === length && /^[A-Z]+$/.test(value);

const joinPresent = (separator: string, ...values: string[]): string =>
  values.filter((value) => value !== '').join(separator);
const siteSlug = (slug: string): string => `${SITE_NAME}/${slug}`;
const isVietnamese = (lng: string): boolean =>
  (lng.split('-', 1)[0] ?? '').toLowerCase() === 'vi';

function cardField(value: string, contacts: readonly string[]): string {
  const normalized = normalizePreviewText(value);
  const scrubbed = scrubPreviewText(normalized, contacts);
  return scrubbed === normalized ? normalized : '';
}

const BLOCK_TAGS = new Set(['P', 'UL', 'OL', 'LI', 'BR']);

function richTextPlain(source: string): string {
  if (typeof DOMParser === 'undefined') return '';
  const parsed = new DOMParser().parseFromString(source, 'text/html');
  let out = '';
  const walk = (node: ChildNode): void => {
    if (node.nodeType === 3) {
      out += node.textContent ?? '';
      return;
    }
    const block = node.nodeType === 1
      && BLOCK_TAGS.has((node as Element).tagName);
    if (block) out += ' ';
    node.childNodes.forEach(walk);
    if (block) out += ' ';
  };
  parsed.body.childNodes.forEach(walk);
  return normalizePreviewText(out);
}

type EntryLike = { isHidden?: boolean };
const isVisible = (entry: EntryLike): boolean => entry.isHidden !== true;
const hasVisibleEntry = (section: Section): boolean =>
  section.entries.some(isVisible);

function firstVisibleSection<T extends Section['sectionType']>(
  document: Resume,
  sectionType: T,
): Extract<Section, { sectionType: T }> | undefined {
  const { main, sidebar } = document.customization.layout.sections;
  for (const key of [...main, ...sidebar]) {
    const section = document.content[key];
    if (section === undefined || !hasVisibleEntry(section)) continue;
    if (section.sectionType === sectionType) {
      return section as Extract<Section, { sectionType: T }>;
    }
  }
  return undefined;
}

function summary(document: Resume): string {
  const section = firstVisibleSection(document, 'profile');
  if (section === undefined) return '';
  const parts: string[] = [];
  for (const entry of section.entries) {
    if (!isVisible(entry) || entry.text === undefined) continue;
    parts.push(richTextPlain(entry.text));
  }
  return normalizePreviewText(parts.join(' '));
}

function latestRole(document: Resume): string {
  const section = firstVisibleSection(document, 'work');
  if (section === undefined) return '';
  const entry = section.entries.find(isVisible);
  if (entry === undefined) return '';
  const title = entry.jobTitle !== undefined
    ? normalizePreviewText(entry.jobTitle)
    : '';
  const employer = entry.employer !== undefined
    ? normalizePreviewText(entry.employer)
    : '';
  return joinPresent(', ', title, employer);
}

export function previewContactValues(document: Resume): string[] {
  const values: string[] = [];
  for (const detail of document.personalDetails.details ?? []) {
    if (detail.isHidden || detail.value === '') continue;
    const value = normalizePreviewText(detail.value);
    if (value !== '') values.push(value);
  }
  return [...values].sort((a, b) => byteLength(b) - byteLength(a));
}

export function previewTitle(
  publicTitle: string | null,
  document: Resume,
  slug: string,
): string {
  if (publicTitle !== null && publicTitle !== '') return publicTitle;
  const contacts = previewContactValues(document);
  const name = firstGraphemes(
    scrubPreviewText(document.personalDetails.fullName ?? '', contacts),
    MAX_TITLE_GRAPHEMES,
    0,
  );
  return name !== '' ? name : siteSlug(slug);
}

export function previewDescription(document: Resume, lng: string): string {
  const contacts = previewContactValues(document);
  const summaryText = scrubPreviewText(summary(document), contacts);
  if (summaryText !== '') return cutPreviewDescription(summaryText);

  const headline = document.personalDetails.headline !== undefined
    ? normalizePreviewText(document.personalDetails.headline)
    : '';
  const combined = scrubPreviewText(
    joinPresent(' · ', headline, latestRole(document)),
    contacts,
  );
  if (combined !== '') return cutPreviewDescription(combined);

  return isVietnamese(lng) ? `CV trên ${SITE_NAME}` : `Resume on ${SITE_NAME}`;
}

export function previewImageText(document: Resume, slug: string): string {
  const contacts = previewContactValues(document);
  const name = cardField(document.personalDetails.fullName ?? '', contacts);
  if (name === '') return siteSlug(slug);
  const headline = document.personalDetails.headline !== undefined
    ? cardField(document.personalDetails.headline, contacts)
    : '';
  return firstGraphemes(
    joinPresent(' · ', name, headline),
    MAX_IMAGE_TEXT_GRAPHEMES,
    MAX_IMAGE_TEXT_BYTES,
  );
}

export function previewCardText(
  document: Resume,
): { name: string | null; headline: string | null } {
  const contacts = previewContactValues(document);
  const name = cardField(document.personalDetails.fullName ?? '', contacts);
  if (name === '') return { name: null, headline: null };
  const rawHeadline = document.personalDetails.headline;
  const headline = rawHeadline === undefined
    ? ''
    : cardField(rawHeadline, contacts);
  return { name, headline: headline === '' ? null : headline };
}
