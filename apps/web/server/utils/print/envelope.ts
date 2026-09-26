import { CURRENT_VERSION } from '@aboutme/schema/released';

import type { components } from '../../../app/api/generated/openapi';
import {
  CARD_LAYOUT_VERSION,
  type PreviewCardContent,
} from '../../../app/components/preview/cardLayout';

import validatePrintDocument from '#print-document-validator';

export const PRINT_ENVELOPE_MAX_BYTES = 3_407_872;
export const PRINT_HTML_MAX_BYTES = 6_291_456;
export const PRINT_PHOTO_MAX_BYTES = 2_097_152;
export const PRINT_FAILURE = 'print failed';

export type PublicResumeDocument
  = components['schemas']['PublicResumeDocument'];

export interface PrintEnvelope {
  version: 1;
  resumeId: string;
  revision: string;
  publicGeneration: string | null;
  lng: string;
  document: PublicResumeDocument;
}

/**
 * A link-preview card job: the closed envelope of
 * docs/design/link-previews.md, "Preview card". It has no document, so no
 * contact detail can reach the card.
 */
export interface PrintCardEnvelope {
  version: 1;
  kind: 'card';
  resumeId: string;
  card: PreviewCardContent;
}

/** What the print route renders: a resume document or a preview card. */
export type PrintJobEnvelope = PrintEnvelope | PrintCardEnvelope;

export const isCardEnvelope = (
  envelope: PrintJobEnvelope,
): envelope is PrintCardEnvelope => 'kind' in envelope;

const PRINT_CARD_TEXT_MAX_CHARACTERS = 160;

const fail = (): never => {
  throw new Error(PRINT_FAILURE);
};

class DuplicateKeyScanner {
  private position = 0;

  constructor(private readonly source: string) {}

  scan(): void {
    this.value();
    this.space();
    if (this.position !== this.source.length) fail();
  }

  private space(): void {
    while (/\s/u.test(this.source[this.position] ?? '')) this.position += 1;
  }

  private value(): void {
    this.space();
    const character = this.source[this.position];
    if (character === '{') return this.object();
    if (character === '[') return this.array();
    if (character === '"') return void this.string();
    const scalar
      = /^(?:true|false|null|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?)/u
        .exec(this.source.slice(this.position));
    if (scalar === null) return fail();
    this.position += scalar[0].length;
  }

  private object(): void {
    this.position += 1;
    this.space();
    const keys = new Set<string>();
    if (this.source[this.position] === '}') {
      this.position += 1;
      return;
    }
    for (;;) {
      this.space();
      if (this.source[this.position] !== '"') fail();
      const key = this.string();
      if (keys.has(key)) fail();
      keys.add(key);
      this.space();
      if (this.source[this.position++] !== ':') fail();
      this.value();
      this.space();
      const separator = this.source[this.position++];
      if (separator === '}') return;
      if (separator !== ',') fail();
    }
  }

  private array(): void {
    this.position += 1;
    this.space();
    if (this.source[this.position] === ']') {
      this.position += 1;
      return;
    }
    for (;;) {
      this.value();
      this.space();
      const separator = this.source[this.position++];
      if (separator === ']') return;
      if (separator !== ',') fail();
    }
  }

  private string(): string {
    const start = this.position++;
    let escaped = false;
    while (this.position < this.source.length) {
      const character = this.source[this.position++];
      if (character === undefined) return fail();
      if (escaped) {
        escaped = false;
        continue;
      }
      if (character === '\\') {
        escaped = true;
        continue;
      }
      if (character === '"') {
        try {
          return JSON.parse(this.source.slice(start, this.position)) as string;
        } catch {
          return fail();
        }
      }
      if (character < ' ') fail();
    }
    return fail();
  }
}

const canonicalUUID = (value: unknown): value is string =>
  typeof value === 'string'
  && value !== '00000000-0000-0000-0000-000000000000'
  && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/u
    .test(value);

const positiveInt64 = (value: unknown): value is string => {
  if (typeof value !== 'string' || !/^[1-9]\d*$/u.test(value)) return false;
  try {
    return BigInt(value) <= 9_223_372_036_854_775_807n;
  } catch {
    return false;
  }
};

const canonicalLanguage = (value: unknown): value is string => {
  if (typeof value !== 'string' || value.length > 35) return false;
  if (/^x-(?:[a-z0-9]{1,8})(?:-[a-z0-9]{1,8})*$/u.test(value)) {
    return true;
  }
  try {
    const canonical = Intl.getCanonicalLocales(value);
    return canonical.length === 1 && canonical[0] === value;
  } catch {
    return false;
  }
};

const validDataPhoto = (url: string): boolean => {
  const matched
    = /^data:image\/(?:jpeg|png);base64,([A-Za-z0-9+/]*(?:={1,2})?)$/u
      .exec(url);
  if (matched === null || matched[1] === undefined || matched[1] === '') {
    return false;
  }
  const encoded = matched[1];
  if (encoded.length % 4 !== 0) return false;
  try {
    const decoded = Buffer.from(encoded, 'base64');
    return decoded.length <= PRINT_PHOTO_MAX_BYTES
      && decoded.toString('base64') === encoded;
  } catch {
    return false;
  }
};

const validPhoto = (document: PublicResumeDocument): boolean => {
  const photo = document.personalDetails.photo;
  return photo === undefined || validDataPhoto(photo.url);
};

const plainObject = (value: unknown): value is Record<string, unknown> =>
  value !== null && typeof value === 'object' && !Array.isArray(value);

const exactKeys = (value: Record<string, unknown>, keys: string): boolean =>
  Object.keys(value).sort().join(',') === keys;

// Go's unicode.IsSpace: the Unicode White_Space characters.
const WHITE_SPACE = '[\\t\\n\\v\\f\\r \\u0085\\u00a0\\u1680\\u2000-\\u200a'
  + '\\u2028\\u2029\\u202f\\u205f\\u3000]';
const EDGE_SPACE = new RegExp(`^${WHITE_SPACE}|${WHITE_SPACE}$`, 'u');

// Card text is normalized by the server: 1 to 160 code points, well formed,
// no control character, and no space at either end.
const validCardText = (value: unknown): value is string | null =>
  value === null
  || (
    typeof value === 'string'
    && value !== ''
    && value.isWellFormed()
    && [...value].length <= PRINT_CARD_TEXT_MAX_CHARACTERS
    && !/\p{Cc}/u.test(value)
    && !EDGE_SPACE.test(value)
  );

// Public slugs: 4 to 30 of a-z, 0-9, and single inner hyphens.
const validSlug = (value: unknown): value is string =>
  typeof value === 'string'
  && value.length >= 4
  && value.length <= 30
  && /^[a-z0-9]+(?:-[a-z0-9]+)*$/u.test(value);

const unitFraction = (value: unknown, allowZero: boolean): boolean =>
  typeof value === 'number'
  && Number.isFinite(value)
  && value <= 1
  && (allowZero ? value >= 0 : value > 0);

const validCardPhoto = (value: unknown): boolean => {
  if (value === null) return true;
  if (!plainObject(value) || !exactKeys(value, 'crop,url')) return false;
  if (typeof value.url !== 'string' || !validDataPhoto(value.url)) {
    return false;
  }
  const crop = value.crop;
  if (crop === null) return true;
  return plainObject(crop)
    && exactKeys(crop, 'height,width,x,y')
    && unitFraction(crop.x, true)
    && unitFraction(crop.y, true)
    && unitFraction(crop.width, false)
    && unitFraction(crop.height, false);
};

// The card envelope is closed at both levels: an unknown key anywhere, such
// as a contact or document field, rejects it.
const decodeCardEnvelope = (
  envelope: Record<string, unknown>,
): PrintCardEnvelope => {
  const card = envelope.card;
  if (
    !exactKeys(envelope, 'card,kind,resumeId,version')
    || envelope.version !== 1
    || envelope.kind !== 'card'
    || !canonicalUUID(envelope.resumeId)
    || !plainObject(card)
    || !exactKeys(
      card,
      'accent,headline,layoutVersion,lng,name,photo,slug',
    )
    || card.layoutVersion !== CARD_LAYOUT_VERSION
    || !canonicalLanguage(card.lng)
    || !validSlug(card.slug)
    || !validCardText(card.name)
    || !validCardText(card.headline)
    || (card.name === null && card.headline !== null)
    || !validCardPhoto(card.photo)
    || typeof card.accent !== 'string'
    || !/^#[0-9a-f]{6}$/u.test(card.accent)
  ) fail();
  return envelope as unknown as PrintCardEnvelope;
};

/**
 * Decodes a redeemed print envelope: a resume print envelope, which has no
 * kind, or a card envelope, whose kind is "card".
 */
export function decodePrintEnvelope(source: string): PrintJobEnvelope {
  if (Buffer.byteLength(source, 'utf8') > PRINT_ENVELOPE_MAX_BYTES) fail();
  try {
    new DuplicateKeyScanner(source).scan();
    const value = JSON.parse(source) as unknown;
    if (value === null || typeof value !== 'object' || Array.isArray(value)) {
      fail();
    }
    const envelope = value as Record<string, unknown>;
    if (Object.hasOwn(envelope, 'kind')) return decodeCardEnvelope(envelope);
    if (
      Object.keys(envelope).sort().join(',')
      !== 'document,lng,publicGeneration,resumeId,revision,version'
    ) fail();
    if (
      envelope.version !== 1
      || !canonicalUUID(envelope.resumeId)
      || !positiveInt64(envelope.revision)
      || (
        envelope.publicGeneration !== null
        && (
          !positiveInt64(envelope.publicGeneration)
          || envelope.publicGeneration !== envelope.revision
        )
      )
      || !canonicalLanguage(envelope.lng)
      || !validatePrintDocument(envelope.document)
    ) fail();
    const document = envelope.document as PublicResumeDocument;
    if (
      document.schemaVersion !== CURRENT_VERSION || !validPhoto(document)
    ) fail();
    return envelope as unknown as PrintEnvelope;
  } catch {
    return fail();
  }
}

export function decodePrintEnvelopeBytes(
  source: Uint8Array,
): PrintJobEnvelope {
  try {
    return decodePrintEnvelope(
      new TextDecoder('utf-8', { fatal: true }).decode(source),
    );
  } catch {
    return fail();
  }
}
