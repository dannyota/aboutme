/**
 * LinkedIn "Save to PDF" rich text mapping, per
 * docs/design/linkedin-import.md "Rich text" and "Limits".
 */

/**
 * A line of extracted text. `gap` is the baseline distance to the previous
 * line in points; it is null for the first line and the first line of a
 * later page.
 */
export interface TextLine {
  text: string;
  gap: number | null;
}

/** Output UTF-8 byte length stays at or under this many bytes. */
export const RICH_TEXT_MAX_BYTES = 16384;

/** The resume schema's richText field also limits code points to this many. */
const RICH_TEXT_MAX_CODE_POINTS = 16384;

const NEW_PARAGRAPH_GAP_FACTOR = 1.3;

type Block
  = | { type: 'p'; fragments: string[] }
    | { type: 'ul'; items: string[][] };

const BULLET_DASH_RE = /^[-*–]\s+(.*)$/s;

/** Strips a leading list marker (`•`, `-`, `*`, `–`) and its space. */
function stripBulletMarker(text: string): string | null {
  if (text.startsWith('•')) {
    return text.slice(1).replace(/^\s+/, '');
  }
  const match = BULLET_DASH_RE.exec(text);
  // The single group in BULLET_DASH_RE is mandatory, so it exists on a match.
  return match === null ? null : match[1]!;
}

/** The most common value in a non-empty list of numbers. */
function mode(values: readonly number[]): number {
  const counts = new Map<number, number>();
  // Callers only call mode() with a non-empty list.
  let best = values[0]!;
  let bestCount = 0;
  for (const value of values) {
    const count = (counts.get(value) ?? 0) + 1;
    counts.set(value, count);
    if (count > bestCount) {
      best = value;
      bestCount = count;
    }
  }
  return best;
}

function buildBlocks(lines: readonly TextLine[]): Block[] {
  const nonNullGaps = lines
    .map((line) => line.gap)
    .filter((gap): gap is number => gap !== null);
  const hasEnoughGapData = nonNullGaps.length >= 2;
  const threshold = hasEnoughGapData
    ? mode(nonNullGaps) * NEW_PARAGRAPH_GAP_FACTOR
    : Infinity;

  const blocks: Block[] = [];

  for (const line of lines) {
    const trimmed = line.text.trim();
    if (trimmed === '') continue;

    const markerContent = stripBulletMarker(trimmed);
    const isBullet = markerContent !== null;
    const content = isBullet ? markerContent : trimmed;

    const isNewParagraphGap
      = line.gap !== null && hasEnoughGapData && line.gap > threshold;

    const last = blocks[blocks.length - 1] as Block | undefined;

    if (isNewParagraphGap || last === undefined) {
      blocks.push(
        isBullet
          ? { type: 'ul', items: [[content]] }
          : { type: 'p', fragments: [content] },
      );
      continue;
    }

    if (isBullet) {
      if (last.type === 'ul') {
        last.items.push([content]);
      } else {
        blocks.push({ type: 'ul', items: [[content]] });
      }
      continue;
    }

    if (last.type === 'ul') {
      last.items[last.items.length - 1]!.push(content);
    } else {
      last.fragments.push(content);
    }
  }

  return blocks;
}

const ESCAPE_RE = /[&<>"']/g;
const ESCAPES: Readonly<Record<string, string>> = Object.freeze({
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  '\'': '&#39;',
});

/** Escapes `&`, `<`, `>`, `"`, `'` so text is safe as an HTML text node. */
function escapeHtml(text: string): string {
  return text.replace(ESCAPE_RE, (character) => ESCAPES[character]!);
}

const ENTITY_TAIL_RE = /^[a-zA-Z0-9#]*$/;

/** Cut to at most `max` code points at a code point boundary. */
export function clipText(
  text: string,
  max: number,
): { text: string; cut: boolean } {
  const codePoints = [...text];
  if (codePoints.length <= max) return { text, cut: false };
  return { text: codePoints.slice(0, max).join(''), cut: true };
}

/**
 * Like clipText, for escaped text: it never ends inside an escape such as
 * `&amp;`, backing off to before its `&`.
 */
function clipEscaped(
  text: string,
  max: number,
): { text: string; cut: boolean } {
  const clipped = clipText(text, max);
  if (!clipped.cut) return clipped;
  const ampersandIndex = clipped.text.lastIndexOf('&');
  if (ampersandIndex !== -1) {
    const afterAmpersand = clipped.text.slice(ampersandIndex + 1);
    if (!afterAmpersand.includes(';') && ENTITY_TAIL_RE.test(afterAmpersand)) {
      return { text: clipped.text.slice(0, ampersandIndex), cut: true };
    }
  }
  return clipped;
}

function utf8ByteLength(text: string): number {
  return new TextEncoder().encode(text).length;
}

function codePointLength(text: string): number {
  return [...text].length;
}

function fitsBudget(html: string): boolean {
  return (
    codePointLength(html) <= RICH_TEXT_MAX_CODE_POINTS
    && utf8ByteLength(html) <= RICH_TEXT_MAX_BYTES
  );
}

/** Clips already-escaped `text` to fit alongside the given `overhead`. */
function clipToFit(
  text: string,
  overhead: string,
): { text: string; cut: boolean } {
  const remainingCodePoints
    = RICH_TEXT_MAX_CODE_POINTS - codePointLength(overhead);
  const remainingBytes = RICH_TEXT_MAX_BYTES - utf8ByteLength(overhead);
  if (remainingCodePoints <= 0 || remainingBytes <= 0) {
    return { text: '', cut: true };
  }

  let candidate = clipEscaped(text, remainingCodePoints);
  if (utf8ByteLength(candidate.text) <= remainingBytes) return candidate;

  let lo = 0;
  let hi = remainingCodePoints;
  while (lo < hi) {
    const mid = Math.ceil((lo + hi) / 2);
    const attempt = clipEscaped(text, mid);
    if (utf8ByteLength(attempt.text) <= remainingBytes) {
      lo = mid;
    } else {
      hi = mid - 1;
    }
  }
  candidate = clipEscaped(text, lo);
  return { text: candidate.text, cut: true };
}

/** Lines to sanitized-allowlist HTML using only p, ul, li. */
export function linesToRichText(
  lines: readonly TextLine[],
): { html: string; cut: boolean } {
  const blocks = buildBlocks(lines);
  if (blocks.length === 0) return { html: '', cut: false };

  let html = '';
  let cut = false;

  for (const block of blocks) {
    if (block.type === 'p') {
      const text = escapeHtml(block.fragments.join(' '));
      const candidate = `${html}<p>${text}</p>`;
      if (fitsBudget(candidate)) {
        html = candidate;
        continue;
      }

      const overhead = `${html}<p></p>`;
      const clipped = clipToFit(text, overhead);
      html += `<p>${clipped.text}</p>`;
      cut = true;
      break;
    }

    let liHtml = '';
    let blockCut = false;
    for (const item of block.items) {
      const text = escapeHtml(item.join(' '));
      const candidate = `${html}<ul>${liHtml}<li>${text}</li></ul>`;
      if (fitsBudget(candidate)) {
        liHtml += `<li>${text}</li>`;
        continue;
      }

      const overhead = `${html}<ul>${liHtml}<li></li></ul>`;
      const clipped = clipToFit(text, overhead);
      liHtml += `<li>${clipped.text}</li>`;
      blockCut = true;
      break;
    }

    html += `<ul>${liHtml}</ul>`;
    if (blockCut) {
      cut = true;
      break;
    }
  }

  return { html, cut };
}
