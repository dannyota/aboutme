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

export const PUBLIC_RENDER_REQUEST_MAX_BYTES = 557_056;
export const PUBLIC_RENDER_HTML_MAX_BYTES = 2_097_152;
export const PUBLIC_RENDER_FAILURE = 'public render failed';

export type PublicResume = components['schemas']['PublicResume'];

export interface PublicRenderPreview {
  title: string;
  description: string;
  locale: string;
  imageAlt: string;
  imageUrl?: string;
}

type PublicRenderJoinInvite = '/register' | '/login';

export interface PublicRenderEntryProjectLink {
  entryId: string;
  url: string;
}

export interface PublicRenderResumeRequest {
  publicResume: PublicResume;
  mode: 'continuous';
  canonicalOrigin: string;
  discoveryEnabled: boolean;
  pageTitle: string;
  faviconHref: string;
  preview?: PublicRenderPreview;
  joinInvite?: PublicRenderJoinInvite;
  projectLinks?: PublicRenderEntryProjectLink[];
  projectsUrl?: string;
}

export type PublicGateProvider = 'google' | 'linkedin';

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
  const hasPreview = keys.includes('preview');
  const hasJoinInvite = keys.includes('joinInvite');
  const hasProjectsUrl = keys.includes('projectsUrl');
  const hasProjectLinks = keys.includes('projectLinks');
  const expectedKeys = [
    ...baseKeys,
    ...(hasPreview ? ['preview'] : []),
    ...(hasJoinInvite ? ['joinInvite'] : []),
    ...(hasProjectsUrl ? ['projectsUrl'] : []),
    ...(hasProjectLinks ? ['projectLinks'] : []),
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
  if (hasProjectLinks && !hasProjectsUrl) {
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
  if (
    Object.hasOwn(request.publicResume, 'projectLinks')
    || (hasProjectsUrl && !validEntryPoints(request))
  ) {
    fail();
  }
  return request;
}

const visibleProjectEntries = (
  document: PublicResume['document'],
): ReadonlySet<string> => {
  const ids = new Set<string>();
  const { main, sidebar } = document.customization.layout.sections;
  for (const key of [...main, ...sidebar]) {
    if (!Object.hasOwn(document.content, key)) continue;
    const section = document.content[key]!;
    if (section.sectionType !== 'project') continue;
    for (const entry of section.entries) ids.add(entry.id);
  }
  return ids;
};

function validEntryPoints(request: PublicRenderResumeRequest): boolean {
  const projectsUrl = [
    request.canonicalOrigin, request.publicResume.slug, 'projects',
  ].join('/');
  if (request.projectsUrl !== projectsUrl) return false;
  const links: unknown = request.projectLinks;
  if (links === undefined) return true;
  if (!Array.isArray(links) || links.length === 0) return false;
  const visible = visibleProjectEntries(request.publicResume.document);
  const seen = new Set<string>();
  return links.every((link: unknown) => {
    if (
      link === null
      || typeof link !== 'object'
      || Object.keys(link).sort().join(',') !== 'entryId,url'
    ) {
      return false;
    }
    const { entryId, url } = link as Record<string, unknown>;
    if (
      typeof entryId !== 'string'
      || !visible.has(entryId)
      || seen.has(entryId)
      || typeof url !== 'string'
      || !url.startsWith(`${projectsUrl}/`)
    ) {
      return false;
    }
    seen.add(entryId);
    return validProjectSlug(url.slice(projectsUrl.length + 1));
  });
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
