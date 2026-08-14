import type { components } from '../../../app/api/generated/openapi';

import validatePublicResume from '#public-render-validator';

export const PUBLIC_RENDER_REQUEST_MAX_BYTES = 532_480;
export const PUBLIC_RENDER_HTML_MAX_BYTES = 2_097_152;
export const PUBLIC_RENDER_FAILURE = 'public render failed';

export type PublicResume = components['schemas']['PublicResume'];

export interface PublicRenderRequest {
  publicResume: PublicResume;
  mode: 'continuous';
  canonicalOrigin: string;
  discoveryEnabled: boolean;
}

const fail = (): never => {
  throw new Error(PUBLIC_RENDER_FAILURE);
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
    const pattern
      = /^(?:true|false|null|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?)/u;
    const value = pattern.exec(this.source.slice(this.position));
    if (value === null) {
      return fail();
    }
    this.position += value[0].length;
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
      if (character === undefined) {
        return fail();
      }
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

function normalizedOrigin(value: unknown): value is string {
  if (typeof value !== 'string' || Buffer.byteLength(value, 'utf8') > 512) {
    return false;
  }
  if ([...value].some((character) => character.codePointAt(0)! > 0x7f)) {
    return false;
  }
  try {
    const parsed = new URL(value);
    return (
      (parsed.protocol === 'http:' || parsed.protocol === 'https:')
      && parsed.username === ''
      && parsed.password === ''
      && parsed.pathname === '/'
      && parsed.search === ''
      && parsed.hash === ''
      && parsed.origin === value
    );
  } catch {
    return false;
  }
}

export function isPublicResume(value: unknown): value is PublicResume {
  return validatePublicResume(value) === true;
}

export function decodePublicRenderEnvelope(
  source: string,
): PublicRenderRequest {
  if (Buffer.byteLength(source, 'utf8') > PUBLIC_RENDER_REQUEST_MAX_BYTES) {
    return fail();
  }
  try {
    new DuplicateKeyScanner(source).scan();
    const value = JSON.parse(source) as unknown;
    if (value === null || typeof value !== 'object' || Array.isArray(value)) {
      fail();
    }
    const envelope = value as Record<string, unknown>;
    const keys = Object.keys(envelope).sort();
    const expectedKeys = 'canonicalOrigin,discoveryEnabled,mode,publicResume';
    if (keys.join(',') !== expectedKeys) {
      fail();
    }
    if (
      envelope.mode !== 'continuous'
      || !normalizedOrigin(envelope.canonicalOrigin)
      || typeof envelope.discoveryEnabled !== 'boolean'
      || !isPublicResume(envelope.publicResume)
    ) {
      fail();
    }
    return envelope as unknown as PublicRenderRequest;
  } catch {
    return fail();
  }
}

export function decodePublicRenderEnvelopeBytes(
  source: Uint8Array,
): PublicRenderRequest {
  try {
    return decodePublicRenderEnvelope(
      new TextDecoder('utf-8', { fatal: true }).decode(source),
    );
  } catch {
    return fail();
  }
}
