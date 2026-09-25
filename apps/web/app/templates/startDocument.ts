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

/**
 * What /app/new?sample=<id>&lng=<vi|en> or /app/new?template=<id> asks for.
 * Anyone can type that URL, so every value is checked against the catalog.
 */
export type NewResumeRequest
  = | {
    readonly kind: 'sample';
    readonly template: GalleryTemplate;
    readonly lng: SampleLanguage;
  }
  | { readonly kind: 'template'; readonly template: GalleryTemplate }
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
    // Any language but vi or en falls back to the site language.
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
    : { kind: 'template', template };
}

/** A blank resume that already wears a template. */
export function blankTemplateDocument(preset: TemplatePreset): Resume {
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
    customization: applyTemplate(base, preset, {}),
  } as Resume;
}

/** The document a new resume starts from. */
export async function startDocument(
  request: Exclude<NewResumeRequest, { kind: 'invalid' }>,
): Promise<Resume> {
  if (request.kind === 'template') {
    return blankTemplateDocument(request.template.preset);
  }
  const sample = await loadSample(request.template.id, request.lng);
  if (sample === undefined) {
    throw new Error(`no ${request.lng} sample for ${request.template.id}`);
  }
  return sample;
}

/** A title to prefill, in the sample's language: "Backend engineer resume". */
export function suggestedTitle(
  request: Exclude<NewResumeRequest, { kind: 'invalid' }>,
): string {
  if (request.kind === 'template') return `${request.template.name} resume`;
  const role = sampleRole(request.template, request.lng, request.lng);
  if (role === undefined) return `${request.template.name} resume`;
  // A Vietnamese role reads in lower case after "CV" ("CV trưởng nhóm
  // kho"). A role holding another capital is an English job title or an
  // acronym ("Senior Frontend Engineer", "BrSE", "QA Lead") and keeps its
  // case (docs/design/vietnam-tech-resumes.md).
  if (request.lng !== 'vi') return `${role} resume`;
  return /\p{Lu}/u.test(role.slice(1))
    ? `CV ${role}`
    : `CV ${role.charAt(0).toLocaleLowerCase('vi')}${role.slice(1)}`;
}
