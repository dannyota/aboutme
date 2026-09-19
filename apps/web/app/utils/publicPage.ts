// The owner's public page title and emoji favicon. These mirror the server's
// internal/publicpage checks so the Publish dialog can explain an issue before
// saving; the server stays authoritative
// (docs/adr/0042-public-page-title-and-favicon.md).

export const MAX_TITLE_GRAPHEMES = 70;
const MAX_TITLE_CODE_POINTS = 560;
const MAX_FAVICON_CODE_POINTS = 16;

export type PublicPageIssue
  = 'too_long' | 'invalid_characters' | 'invalid_emoji';

export interface NormalizedPublicPageField {
  /** The value the server stores, or null when the field clears. */
  readonly value: string | null;
  /** Every issue, sorted; empty when the value is valid. */
  readonly codes: PublicPageIssue[];
}

const EDGE_WHITE_SPACE = /^\p{White_Space}+|\p{White_Space}+$/gu;
// Control and format characters, except U+200D ZERO WIDTH JOINER.
const INVISIBLE = /\p{Cc}|(?!\u200D)\p{Cf}/u;
const PICTOGRAPHIC = /^\p{Extended_Pictographic}$/u;
const REGIONAL_INDICATOR = /^\p{Regional_Indicator}$/u;
const UNRESERVED = /[A-Za-z0-9\-._~]/u;

const graphemes = (text: string): string[] => [
  ...new Intl.Segmenter('und', { granularity: 'grapheme' }).segment(text),
].map(({ segment }) => segment);

// What may follow an emoji base inside one sequence: ZWJ, variation
// selectors, skin-tone modifiers, and tag characters.
const emojiComponent = (codePoint: number): boolean =>
  codePoint === 0x200d || codePoint === 0xfe0e || codePoint === 0xfe0f
  || (codePoint >= 0x1f3fb && codePoint <= 0x1f3ff)
  || (codePoint >= 0xe0020 && codePoint <= 0xe007f);

const emojiCluster = (cluster: string): boolean => {
  const characters = [...cluster];
  if (
    characters.length === 2
    && characters.every((character) => REGIONAL_INDICATOR.test(character))
  ) {
    return true;
  }
  const [base, ...rest] = characters;
  if (base === undefined || !PICTOGRAPHIC.test(base)) return false;
  return rest.every((character) =>
    PICTOGRAPHIC.test(character) || emojiComponent(character.codePointAt(0)!));
};

export function normalizePublicTitle(raw: string): NormalizedPublicPageField {
  const title = raw.replace(EDGE_WHITE_SPACE, '');
  if (title === '') return { value: null, codes: [] };
  const codes: PublicPageIssue[] = [];
  if (INVISIBLE.test(title)) codes.push('invalid_characters');
  if (
    [...title].length > MAX_TITLE_CODE_POINTS
    || graphemes(title).length > MAX_TITLE_GRAPHEMES
  ) {
    codes.push('too_long');
  }
  return codes.length === 0
    ? { value: title, codes }
    : { value: null, codes: codes.sort() };
}

export function normalizeFaviconEmoji(raw: string): NormalizedPublicPageField {
  const emoji = raw.replace(EDGE_WHITE_SPACE, '');
  if (emoji === '') return { value: null, codes: [] };
  const clusters = graphemes(emoji);
  if (
    [...emoji].length > MAX_FAVICON_CODE_POINTS
    || clusters.length !== 1
    || !emojiCluster(emoji)
  ) {
    return { value: null, codes: ['invalid_emoji'] };
  }
  return { value: emoji, codes: [] };
}

/** The favicon data: URL the public page uses, byte for byte the server's. */
export function faviconHref(emoji: string): string {
  const svg = '<svg xmlns=\'http://www.w3.org/2000/svg\' viewBox=\'0 0 100 100\'>'
    + `<text y='.9em' font-size='90'>${emoji}</text></svg>`;
  let encoded = '';
  for (const byte of new TextEncoder().encode(svg)) {
    const character = String.fromCharCode(byte);
    encoded += byte < 0x80 && UNRESERVED.test(character)
      ? character
      : `%${byte.toString(16).toUpperCase().padStart(2, '0')}`;
  }
  return `data:image/svg+xml,${encoded}`;
}
