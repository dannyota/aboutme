import type { Customization, Resume } from '@aboutme/schema';
import { CURRENT_VERSION } from '@aboutme/schema/released';
import { loadSample, type SampleLanguage } from '@aboutme/schema/samples';
import type { TemplatePreset } from '@aboutme/schema/templates';

import { applyTemplate } from '../components/resume/applyTemplate';
import {
  galleryTemplate,
  type GalleryTemplate,
  sampleRole,
} from './catalog';

export type NewResumeRequest
  = | {
    readonly kind: 'sample';
    readonly template: GalleryTemplate;
    readonly lng: SampleLanguage;
  }
  | {
    readonly kind: 'template';
    readonly template: GalleryTemplate;
    readonly lng: string;
  }
  | { readonly kind: 'invalid' };

type Query = Readonly<Record<string, unknown>>;

function single(query: Query, key: string): string | undefined {
  const value = query[key];
  return typeof value === 'string' ? value : undefined;
}

export function parseNewResumeQuery(
  query: Query,
  siteLanguage: SampleLanguage,
): NewResumeRequest {
  const sampleId = single(query, 'sample');
  if (sampleId !== undefined) {
    const template = galleryTemplate(sampleId);
    const requested = single(query, 'lng');
    const lng = requested === 'vi' || requested === 'en'
      ? requested
      : siteLanguage;
    return template !== undefined && template.sampleLanguages.includes(lng)
      ? { kind: 'sample', template, lng }
      : { kind: 'invalid' };
  }
  const template = galleryTemplate(single(query, 'template') ?? '');
  return template === undefined
    ? { kind: 'invalid' }
    : { kind: 'template', template, lng: siteLanguage };
}

export function blankTemplateDocument(
  preset: TemplatePreset,
  lng: string,
): Resume {
  const { placement: _placement, sidebarSectionTypes: _types, ...layout }
    = preset.customization.layout;
  const base = {
    ...structuredClone(preset.customization),
    layout: { ...layout, sections: { main: [], sidebar: [] } },
  } as Customization;
  return {
    schemaVersion: CURRENT_VERSION,
    personalDetails: { details: [] },
    content: {},
    customization: applyTemplate(base, preset, {}, lng),
  } as Resume;
}

export async function startDocument(
  request: Exclude<NewResumeRequest, { kind: 'invalid' }>,
): Promise<Resume> {
  if (request.kind === 'template') {
    return blankTemplateDocument(request.template.preset, request.lng);
  }
  const sample = await loadSample(request.template.id, request.lng);
  if (sample === undefined) {
    throw new Error(`no ${request.lng} sample for ${request.template.id}`);
  }
  return sample;
}

export function suggestedTitle(
  request: Exclude<NewResumeRequest, { kind: 'invalid' }>,
): string {
  if (request.kind === 'template') return `${request.template.name} resume`;
  const role = sampleRole(request.template, request.lng, request.lng);
  if (role === undefined) return `${request.template.name} resume`;
  if (request.lng !== 'vi') return `${role} resume`;
  return /\p{Lu}/u.test(role.slice(1))
    ? `CV ${role}`
    : `CV ${role.charAt(0).toLocaleLowerCase('vi')}${role.slice(1)}`;
}
