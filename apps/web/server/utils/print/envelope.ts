import type { components } from '../../../app/api/generated/openapi';

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

const validPhoto = (document: PublicResumeDocument): boolean => {
  const photo = document.personalDetails.photo;
  if (photo === undefined) return true;
  const matched
    = /^data:image\/(?:jpeg|png);base64,([A-Za-z0-9+/]*(?:={1,2})?)$/u
      .exec(photo.url);
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

export function decodePrintEnvelope(source: string): PrintEnvelope {
  if (Buffer.byteLength(source, 'utf8') > PRINT_ENVELOPE_MAX_BYTES) fail();
  try {
    new DuplicateKeyScanner(source).scan();
    const value = JSON.parse(source) as unknown;
    if (value === null || typeof value !== 'object' || Array.isArray(value)) {
      fail();
    }
    const envelope = value as Record<string, unknown>;
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
    if (document.schemaVersion !== 2 || !validPhoto(document)) fail();
    return envelope as unknown as PrintEnvelope;
  } catch {
    return fail();
  }
}

export function decodePrintEnvelopeBytes(source: Uint8Array): PrintEnvelope {
  try {
    return decodePrintEnvelope(
      new TextDecoder('utf-8', { fatal: true }).decode(source),
    );
  } catch {
    return fail();
  }
}
