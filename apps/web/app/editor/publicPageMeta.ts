export const PUBLIC_TITLE_MAX = 70;

export type PublicTitleIssue = 'too_long' | 'invalid_characters';
export type FaviconEmojiIssue = 'invalid_emoji';

const graphemes = new Intl.Segmenter(undefined, { granularity: 'grapheme' });

const HIDDEN_CHARACTERS
  = /[\p{Cc}\u00AD\u200B\u200C\u200E\u200F\u202A-\u202E\u2066-\u2069\uFEFF]/u;

export function graphemeCount(value: string): number {
  let count = 0;
  for (const _segment of graphemes.segment(value)) count += 1;
  return count;
}

export function publicTitleIssue(value: string): PublicTitleIssue | null {
  const trimmed = value.trim();
  if (HIDDEN_CHARACTERS.test(trimmed)) return 'invalid_characters';
  return graphemeCount(trimmed) > PUBLIC_TITLE_MAX ? 'too_long' : null;
}

export function isSingleEmoji(value: string): boolean {
  const segments = [...graphemes.segment(value.trim())];
  if (segments.length !== 1) return false;
  const grapheme = segments[0]!.segment;
  return /^\p{Extended_Pictographic}/u.test(grapheme)
    || /^\p{Regional_Indicator}{2}$/u.test(grapheme);
}

export function faviconEmojiIssue(value: string): FaviconEmojiIssue | null {
  return value.trim() === '' || isSingleEmoji(value) ? null : 'invalid_emoji';
}

export function defaultPublicTitle(fullName: string | undefined): string {
  const name = fullName?.trim() ?? '';
  return name === '' ? 'Resume' : `${name} — Resume`;
}

export interface PublicPageFields {
  readonly publicTitle: string;
  readonly faviconEmoji: string;
}

export function changedPublicPageFields(
  stored: {
    readonly publicTitle: string | null;
    readonly faviconEmoji: string | null;
  },
  draft: PublicPageFields,
): Partial<PublicPageFields> {
  const title = draft.publicTitle.trim();
  const emoji = draft.faviconEmoji.trim();
  return {
    ...(title === (stored.publicTitle ?? '') ? {} : { publicTitle: title }),
    ...(emoji === (stored.faviconEmoji ?? '') ? {} : { faviconEmoji: emoji }),
  };
}

export function publicPageIssueField(
  path: string,
): keyof PublicPageFields | null {
  const last = path.split(/[./]/u).filter(Boolean).at(-1);
  return last === 'publicTitle' || last === 'faviconEmoji' ? last : null;
}
