import type { components } from '../../../app/api/generated/openapi';

import validatePublicResume from '#public-render-validator';
import {
  normalizedOrigin,
  validCardImageURL,
  validFaviconHref,
  validPageTitle,
  validPreview,
  validProjectSlug,
  validSlug,
} from './envelope-fields';
import {
  decodeProjectEnvelope,
  decodeProjectListEnvelope,
} from './project-decode';
import type {
  PublicRenderProjectListRequest,
  PublicRenderProjectRequest,
} from './project-envelope';

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
  /**
   * The absolute URL of the current stored preview card, sent only while
   * stored cards are on; without it the page names the og.png alias.
   */
  imageUrl?: string;
}

/** The route the sign-in-to-view join invite links to (AC-VIEW-008). */
type PublicRenderJoinInvite = '/register' | '/login';

/**
 * The project page one visible resume entry's title links to. It never
 * carries the project id or slug.
 */
export interface PublicRenderEntryProjectLink {
  /** The id of the linking project entry. */
  entryId: string;
  /** The project page's absolute https address at the canonical origin. */
  url: string;
}

export interface PublicRenderResumeRequest {
  publicResume: PublicResume;
  mode: 'continuous';
  canonicalOrigin: string;
  discoveryEnabled: boolean;
  /** The exact <title> text, computed by the server (ADR 0014). */
  pageTitle: string;
  /** The exact favicon data: URL, or '' when the owner set no icon. */
  faviconHref: string;
  /** Absent from a server that predates link previews. */
  preview?: PublicRenderPreview;
  /**
   * Present only for a sign_in resume; the marker the join invite reads
   * (docs/design/viewer-analytics/sign-in-to-view.md#join-invite).
   */
  joinInvite?: PublicRenderJoinInvite;
  /**
   * The same entryId and url pairs as the public JSON's projectLinks at this
   * revision, in layout and entry order. Present only together with
   * projectsUrl, and only when at least one project is available
   * (docs/design/projects-contract.md "Entry points on the resume page").
   */
  projectLinks?: PublicRenderEntryProjectLink[];
  /**
   * The page bar's link to the project list: canonicalOrigin + '/' + slug +
   * '/projects'. Present only together with projectLinks.
   */
  projectsUrl?: string;
}

/** A provider offered on the sign-in gate, in envelope order. */
export type PublicGateProvider = 'google' | 'linkedin';

/** The closed message code the gate shows after a failed or cancelled try. */
export type PublicGateMessage = 'none' | 'cancelled' | 'failed';

export interface PublicRenderGateRequest {
  mode: 'gate';
  canonicalOrigin: string;
  slug: string;
  lng: 'vi' | 'en';
  pageTitle: string;
  faviconHref: string;
  preview: PublicRenderPreview;
  providers: readonly PublicGateProvider[];
  message: PublicGateMessage;
  /**
   * 'projects' or 'projects/{project-slug}' when the gate stands in for a
   * project page; the provider links carry it. Absent for the resume page
   * (docs/design/projects-contract.md "Render envelope").
   */
  page?: string;
}

export type PublicRenderRequest
  = | PublicRenderResumeRequest
    | PublicRenderGateRequest
    | PublicRenderProjectListRequest
    | PublicRenderProjectRequest;

const validLng = (value: unknown): value is 'vi' | 'en' =>
  value === 'vi' || value === 'en';

const GATE_PROVIDER_ORDER: readonly PublicGateProvider[] = [
  'google',
  'linkedin',
];

const validProviders = (
  value: unknown,
): value is readonly PublicGateProvider[] => {
  if (!Array.isArray(value)) return false;
  if (new Set(value).size !== value.length) return false;
  if (value.some((entry) => !GATE_PROVIDER_ORDER.includes(entry))) {
    return false;
  }
  const positions = value.map((entry) => GATE_PROVIDER_ORDER.indexOf(entry));
  return positions.every(
    (position, index) => index === 0 || position > positions[index - 1]!,
  );
};

const validMessage = (value: unknown): value is PublicGateMessage =>
  value === 'none' || value === 'cancelled' || value === 'failed';

const validJoinInvite = (value: unknown): value is PublicRenderJoinInvite =>
  value === '/register' || value === '/login';

// The project list or one project page. The provider links append it to
// their query unencoded, so it holds nothing but a fixed word, a slash and a
// project slug.
const validGatePage = (value: unknown): value is string =>
  value === 'projects'
  || (typeof value === 'string' && value.startsWith('projects/')
    && validProjectSlug(value.slice('projects/'.length)));

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

function isPublicResume(value: unknown): value is PublicResume {
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
    if (envelope.mode === 'gate') {
      return decodeGateEnvelope(envelope, keys);
    }
    if (envelope.mode === 'project-list') {
      return decodeProjectListEnvelope(envelope);
    }
    if (envelope.mode === 'project') {
      return decodeProjectEnvelope(envelope);
    }
    return decodeResumeEnvelope(envelope, keys);
  } catch {
    return fail();
  }
}

function decodeResumeEnvelope(
  envelope: Record<string, unknown>,
  keys: readonly string[],
): PublicRenderResumeRequest {
  const baseKeys = [
    'canonicalOrigin',
    'discoveryEnabled',
    'faviconHref',
    'mode',
    'pageTitle',
    'publicResume',
  ];
  // A server that predates link previews sends no preview object, and one
  // that predates sign in to view sends no joinInvite marker.
  const hasPreview = keys.includes('preview');
  const hasJoinInvite = keys.includes('joinInvite');
  const expectedKeys = [
    ...baseKeys,
    ...(hasPreview ? ['preview'] : []),
    ...(hasJoinInvite ? ['joinInvite'] : []),
  ].sort().join(',');
  if (keys.join(',') !== expectedKeys) {
    fail();
  }
  if (hasPreview && !validPreview(envelope.preview)) {
    fail();
  }
  if (hasJoinInvite && !validJoinInvite(envelope.joinInvite)) {
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
  const request = envelope as unknown as PublicRenderResumeRequest;
  const imageUrl = request.preview?.imageUrl;
  if (
    imageUrl !== undefined
    && !validCardImageURL(
      imageUrl,
      request.canonicalOrigin,
      request.publicResume.slug,
    )
  ) {
    fail();
  }
  return request;
}

function decodeGateEnvelope(
  envelope: Record<string, unknown>,
  keys: readonly string[],
): PublicRenderGateRequest {
  const hasPage = keys.includes('page');
  const expectedKeys = [
    'canonicalOrigin',
    'faviconHref',
    'lng',
    'message',
    'mode',
    'pageTitle',
    'preview',
    'providers',
    'slug',
    ...(hasPage ? ['page'] : []),
  ].sort().join(',');
  if (keys.join(',') !== expectedKeys) {
    fail();
  }
  if (hasPage && !validGatePage(envelope.page)) {
    fail();
  }
  if (
    !normalizedOrigin(envelope.canonicalOrigin)
    || !validSlug(envelope.slug)
    || !validLng(envelope.lng)
    || !validPageTitle(envelope.pageTitle)
    || !validFaviconHref(envelope.faviconHref)
    || !validPreview(envelope.preview)
    || !validProviders(envelope.providers)
    || !validMessage(envelope.message)
  ) {
    fail();
  }
  const request = envelope as unknown as PublicRenderGateRequest;
  const imageUrl = request.preview.imageUrl;
  if (
    imageUrl !== undefined
    && !validCardImageURL(imageUrl, request.canonicalOrigin, request.slug)
  ) {
    fail();
  }
  return request;
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
