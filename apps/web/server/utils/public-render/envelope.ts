import type { components } from '../../../app/api/generated/openapi';

import validatePublicResume from '#public-render-validator';

export const PUBLIC_RENDER_REQUEST_MAX_BYTES = 532_480;
export const PUBLIC_RENDER_HTML_MAX_BYTES = 2_097_152;
export const PUBLIC_RENDER_FAILURE = 'public render failed';

export type PublicResume = components['schemas']['PublicResume'];

/**
 * The link-preview text for the page head, computed by the server
 * (docs/design/link-previews.md, "Page head").
 */
export interface PublicRenderPreview {
  title: string;
  description: string;
  /** og:locale, or '' when the resume language maps to none. */
  locale: string;
  imageAlt: string;
}

export interface PublicRenderRequest {
  publicResume: PublicResume;
  mode: 'continuous';
  canonicalOrigin: string;
  discoveryEnabled: boolean;
  /** The exact <title> text, computed by the server (ADR 0042). */
  pageTitle: string;
  /** The exact favicon data: URL, or '' when the owner set no icon. */
  faviconHref: string;
  /** Absent from a server that predates link previews. */
  preview?: PublicRenderPreview;
}

// The server percent-encodes every byte outside the URL-unreserved set, so a
// favicon href holds only these characters and cannot leave its attribute.
const FAVICON_HREF = /^data:image\/svg\+xml,[A-Za-z0-9._~%-]{1,2048}$/u;

const validPageTitle = (value: unknown): value is string =>
  typeof value === 'string' && value.length > 0 && value.length <= 4096;

const validFaviconHref = (value: unknown): value is string =>
  value === '' || (typeof value === 'string' && FAVICON_HREF.test(value));

const PREVIEW_LOCALE = /^[a-z]{2,3}_[A-Z]{2}$/u;
const PREVIEW_KEYS = ['description', 'imageAlt', 'locale', 'title'].join(',');

const boundedText = (value: unknown, maxBytes: number): value is string =>
  typeof value === 'string'
  && value.length > 0
  && Buffer.byteLength(value, 'utf8') <= maxBytes;

const validPreview = (value: unknown): value is PublicRenderPreview => {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    return false;
  }
  const preview = value as Record<string, unknown>;
  return Object.keys(preview).sort().join(',') === PREVIEW_KEYS
    && boundedText(preview.title, 2240)
    && boundedText(preview.description, 1024)
    && boundedText(preview.imageAlt, 1024)
    && typeof preview.locale === 'string'
    && (preview.locale === '' || PREVIEW_LOCALE.test(preview.locale));
};

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
    const baseKeys = [
      'canonicalOrigin',
      'discoveryEnabled',
      'faviconHref',
      'mode',
      'pageTitle',
      'publicResume',
    ];
    // A server that predates link previews sends no preview object.
    const hasPreview = keys.includes('preview');
    const expectedKeys = [...baseKeys, ...(hasPreview ? ['preview'] : [])]
      .sort()
      .join(',');
    if (keys.join(',') !== expectedKeys) {
      fail();
    }
    if (hasPreview && !validPreview(envelope.preview)) {
      fail();
    }
    if (
      envelope.mode !== 'continuous'
      || !normalizedOrigin(envelope.canonicalOrigin)
      || typeof envelope.discoveryEnabled !== 'boolean'
      || !validPageTitle(envelope.pageTitle)
      || !validFaviconHref(envelope.faviconHref)
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
